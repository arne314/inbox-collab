package matrix

import (
	"context"
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
	ResendThreadOverview(ctx context.Context, roomId string) bool
	ResendThreadOverviewAll(ctx context.Context) bool
}

type CommandState int

const (
	Default CommandState = iota
	Pending
	Done
	Error
)

var (
	commands []string = []string{
		"open",
		"close",
		"forceclose",
		"move",
		"resendoverview",
		"resendoverviewall",
	}
	commandRegex          *regexp.Regexp = regexp.MustCompile(`^\s*!\s*([a-zA-Z]+)\s*(.*)\s*$`)
	argsRegex             *regexp.Regexp = regexp.MustCompile(`\S+`)
	CommandStateReactions []string       = []string{"👀", "⏳", "✅", "❌"}
	roomMutexes           map[string]*sync.Mutex
)

type Command struct {
	Name           string
	Arg            string
	Args           []string
	state          CommandState
	event          *event.Event
	roomId         string
	messageId      string
	lastReactionId string
	content        *event.MessageEventContent

	client  *MatrixClient
	actions Actions
}

func (c *Command) reportState(state CommandState) {
	c.state = state
	if c.lastReactionId != "" {
		c.client.RedactMessage(c.roomId, c.lastReactionId)
	}
	c.lastReactionId = c.client.ReactToMessage(c.roomId, c.messageId, CommandStateReactions[c.state])
	log.Infof("Command state of %v changed to %v", c.Name, CommandStateReactions[c.state])
}

func (c *Command) Run(ctx context.Context) {
	var ok bool
	var threadId string
	if lock, ok := roomMutexes[c.roomId]; ok {
		lock.Lock()
		defer lock.Unlock()
	} else {
		log.Warnf("Ignoring command in invalid room: %v", c.roomId)
		return
	}
	log.Infof("Handling command %v...", c.Name)
	if rel := c.content.RelatesTo; rel != nil && rel.Type == event.RelThread {
		threadId = rel.EventID.String()
	}

	switch c.Name {
	case "open":
		if threadId == "" {
			ok = false
		} else {
			ok = c.actions.OpenThread(ctx, c.roomId, threadId)
		}
	case "close":
		if threadId == "" {
			ok = false
		} else {
			ok = c.actions.CloseThread(ctx, c.roomId, threadId)
		}
	case "forceclose":
		if threadId == "" {
			ok = false
		} else {
			ok = c.actions.ForceCloseThread(ctx, c.roomId, threadId)
		}
	case "move":
		if threadId == "" {
			ok = false
		} else {
			c.reportState(Pending)
			ok = c.actions.MoveThread(ctx, c.roomId, threadId, c.Arg)
		}
	case "resendoverview":
		c.reportState(Pending)
		ok = c.actions.ResendThreadOverview(ctx, c.roomId)
	case "resendoverviewall":
		c.reportState(Pending)
		ok = c.actions.ResendThreadOverviewAll(ctx)
	default:
		ok = false
	}

	if ok {
		c.reportState(Done)
	} else {
		c.reportState(Error)
	}
	log.Infof("Done handling command %v", c.Name)
}

func NewCommand(name string, arg string, args []string, evt *event.Event, client *MatrixClient, actions Actions) *Command {
	roomId := evt.RoomID.String()
	messageId := evt.ID.String()
	content := evt.Content.AsMessage()
	return &Command{
		Name:      name,
		Arg:       arg,
		Args:      args,
		state:     Default,
		event:     evt,
		roomId:    roomId,
		messageId: messageId,
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
	message := evt.Content.AsMessage().Body
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
	if slices.Contains(commands, cmd) {
		c := NewCommand(cmd, arg, args, evt, ch.client, ch.Actions)
		go c.Run(ctx)
	}
}
