package matrix

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"maunium.net/go/mautrix/event"

	config "github.com/arne314/inbox-collab/internal/config"
)

type Actions interface {
	OpenThread(ctx context.Context, roomId string, threadId string) bool
	CloseThread(ctx context.Context, roomId string, threadId string) bool
	ForceCloseThread(ctx context.Context, roomId string, threadId string) bool
	MoveThread(ctx context.Context, roomId string, threadId string, query string) bool
	ReplyToMailInThread(ctx context.Context, roomId string, originalId string, replyToId string, text string, cite bool) error
	ResendThreadOverview(ctx context.Context, roomId string) bool
	ResendThreadOverviewAll(ctx context.Context) bool
}

type CommandState int

type CommandConfig struct {
	name          string
	aliases       []string
	description   string
	triggerOnEdit bool
	thread        bool
	admin         bool
}

const (
	Default CommandState = iota
	Pending
	Done
	Error
)

var (
	commands []CommandConfig = []CommandConfig{
		{
			name: "help", aliases: []string{"h"},
			description: "Get an overview of all available commands.",
		},
		{
			name: "close", aliases: []string{"c"}, thread: true,
			description: "Close a thread until it receives a new mail.",
		},
		{
			name: "forceclose", aliases: []string{"fc"}, thread: true,
			description: "Close a thread forever unless manually reopend.",
		},
		{
			name: "open", aliases: []string{"o"}, thread: true,
			description: "Manually reopen a closed thread.",
		},
		{
			name: "move", thread: true,
			description: "Move a thread into another room. Usage: `!move <room name substring>`",
		},
		{
			name: "reply", triggerOnEdit: true, thread: true,
			description: "Reply to an email by replying to it on Matrix. " +
				"Usage: Reply to a message with `!reply <response text>`. " +
				"Editing and adding the `!reply` prefix afterwards is allowed.",
		},
		{
			name: "send", triggerOnEdit: true, thread: true,
			description: "Same as `!reply` but won't cite the original message.",
		},
		{
			name: "resendoverview", admin: true,
			description: "Recreate overview message in this room.",
		},
		{
			name: "resendoverviewall", admin: true,
			description: "Recreate all overview messages.",
		},
	}
	// correctly handles cited commands
	commandRegex          *regexp.Regexp = regexp.MustCompile(`(?s)(?:^|\n)\s*!\s*([a-zA-Z]+)\s*(.*)\s*$`)
	argsRegex             *regexp.Regexp = regexp.MustCompile(`\S+`)
	CommandStateReactions []string       = []string{"👀", "⏳", "✅", "❌"}
	roomMutexes           map[string]*sync.Mutex
)

type Command struct {
	Name           string
	Config         *CommandConfig
	Arg            string
	Args           []string
	state          CommandState
	event          *event.Event
	roomId         string
	messageId      string
	originalId     string // in case of message edit; needed for reactions to work
	threadId       string
	replyToId      string
	lastReactionId string
	prevState      CommandState
	edited         bool
	content        *event.MessageEventContent

	client  *MatrixClient
	actions Actions
}

func (c *Command) cleanupState() {
	c.originalId, c.threadId, c.replyToId = c.client.GetMessageThreadAndReply(c.roomId, c.messageId, c.event)
	for id, reaction := range c.client.GetOwnReactions(c.roomId, c.originalId) {
		if reaction != CommandStateReactions[Done] {
			c.client.RedactMessage(c.roomId, id)
			if c.prevState == Default {
				for i, r := range CommandStateReactions {
					if r == reaction {
						c.prevState = CommandState(i)
					}
				}
			}
		} else {
			c.prevState = Done
		}
	}
}

func (c *Command) reportState(state CommandState) {
	c.state = state
	if c.prevState != Done { // don't update when already succeeded
		if c.lastReactionId != "" {
			c.client.RedactMessage(c.roomId, c.lastReactionId)
		}
		c.lastReactionId = c.client.ReactToMessage(c.roomId, c.originalId, CommandStateReactions[c.state])
	}
	log.Infof("Command state of %v changed to %v", c.Name, CommandStateReactions[c.state])
}

func (c *Command) reportStateMessage(message string) {
	c.reportStateMessageFormatted(message, message)
}

func (c *Command) reportStateMessageFormatted(message string, messageHtml string) {
	if c.threadId == "" {
		c.client.SendRoomMessage(c.roomId, message, messageHtml)
	} else {
		c.client.SendThreadMessage(c.roomId, c.threadId, message, messageHtml, true)
	}
}

func (c *Command) helpCommand() {
	builder := NewTextHtmlBuilder()
	builder.WriteLine(formatBold("Command Overview"))
	for _, cmd := range commands {
		for i, handle := range append([]string{cmd.name}, cmd.aliases...) {
			handle = fmt.Sprintf("!%s", handle)
			builder.Write(formatCode(handle))
			if i != len(cmd.aliases) {
				builder.Write(", ", ", ")
			}
		}
		builder.WriteLine(convertMdCode(fmt.Sprintf(": %s", cmd.description)))
	}
	c.reportStateMessageFormatted(builder.String())
}

func (c *Command) Run(ctx context.Context) {
	if lock, ok := roomMutexes[c.roomId]; ok {
		lock.Lock()
		defer lock.Unlock()
	} else {
		log.Warnf("Ignoring command in invalid room: %v", c.roomId)
		return
	}
	ok := true
	log.Infof("Handling command %v...", c.Name)
	c.cleanupState()

	if c.Config.thread && c.threadId == "" {
		ok = false
		c.reportStateMessageFormatted(convertMdCode(
			fmt.Sprintf("Error: The command `!%s` is expected to be used in a thread.", c.Name)))
	}

	if ok {
		switch c.Name {
		case "help":
			c.helpCommand()
		case "open":
			ok = c.actions.OpenThread(ctx, c.roomId, c.threadId)
		case "close":
			ok = c.actions.CloseThread(ctx, c.roomId, c.threadId)
		case "forceclose":
			ok = c.actions.ForceCloseThread(ctx, c.roomId, c.threadId)
		case "move":
			c.reportState(Pending)
			ok = c.actions.MoveThread(ctx, c.roomId, c.threadId, c.Arg)
		case "resendoverview":
			c.reportState(Pending)
			ok = c.actions.ResendThreadOverview(ctx, c.roomId)
		case "resendoverviewall":
			c.reportState(Pending)
			ok = c.actions.ResendThreadOverviewAll(ctx)
		case "reply", "send":
			c.reportState(Pending)
			cite := c.Name == "reply"
			err := c.actions.ReplyToMailInThread(ctx, c.roomId, c.originalId, c.replyToId, c.Arg, cite)
			ok = err == nil
			if !ok {
				log.Errorf("Error handling command %s: %v", c.Name, err)
				c.reportStateMessage(fmt.Sprintf("Error: %v", err))
			}
		default:
			ok = false
		}
	}

	if ok {
		c.reportState(Done)
	} else {
		c.reportState(Error)
	}
	log.Infof("Done handling command %v", c.Name)
}

func NewCommand(config *CommandConfig, arg string, args []string, edited bool,
	evt *event.Event, client *MatrixClient, actions Actions,
) *Command {
	roomId := evt.RoomID.String()
	messageId := evt.ID.String()
	content := evt.Content.AsMessage()
	return &Command{
		Name:      config.name,
		Config:    config,
		Arg:       arg,
		Args:      args,
		state:     Default,
		event:     evt,
		roomId:    roomId,
		messageId: messageId,
		edited:    edited,
		content:   content,
		client:    client,
		actions:   actions,
	}
}

type CommandHandler struct {
	Actions Actions
	client  *MatrixClient
}

func NewCommandHandler(
	cfg *config.MatrixConfig, actions Actions, client *MatrixClient,
) *CommandHandler {
	allRooms := cfg.AllRooms()
	roomMutexes = make(map[string]*sync.Mutex, len(allRooms))
	for _, r := range allRooms {
		roomMutexes[r] = new(sync.Mutex)
	}
	return &CommandHandler{Actions: actions, client: client}
}

func (ch *CommandHandler) ProcessMessage(ctx context.Context, evt *event.Event) {
	// parse command
	var message string
	edited := evt.Content.AsMessage().NewContent != nil
	if edited {
		message = evt.Content.AsMessage().NewContent.Body
	} else {
		message = evt.Content.AsMessage().Body
	}
	parsed := commandRegex.FindStringSubmatch(message)
	if parsed == nil {
		return
	}
	cmd := strings.ToLower(parsed[1])
	arg := ""
	args := []string{}
	if len(parsed) > 2 {
		arg = strings.TrimSpace(parsed[2])
		args = argsRegex.FindAllString(parsed[2], -1)
	}

	// run command if available
	for _, cfg := range commands {
		if cfg.name == cmd || slices.Contains(cfg.aliases, cmd) {
			if !edited || cfg.triggerOnEdit {
				go NewCommand(&cfg, arg, args, edited, evt, ch.client, ch.Actions).Run(ctx)
			}
			break
		}
	}
}
