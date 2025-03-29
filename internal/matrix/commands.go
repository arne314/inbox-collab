package matrix

import (
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"
	"maunium.net/go/mautrix/event"
)

type Actions interface {
	OpenThread(roomId string, threadId string) bool
	CloseThread(roomId string, threadId string) bool
	ForceCloseThread(roomId string, threadId string) bool
}

type CommandState int

const (
	Default CommandState = iota
	Pending
	Done
	Error
)

var (
	commands              []string       = []string{"open", "close", "forceclose"}
	commandRegex          *regexp.Regexp = regexp.MustCompile(`^!\s?([a-zA-Z]+)`)
	CommandStateReactions []string       = []string{"👀", "⏳", "✅", "❌"}
)

type Command struct {
	Name      string
	state     CommandState
	event     *event.Event
	roomId    string
	messageId string
	content   *event.MessageEventContent

	client  *MatrixClient
	actions Actions
}

func (c *Command) reportState(state CommandState) {
	c.state = state
	c.client.ReactToMessage(c.roomId, c.messageId, CommandStateReactions[c.state])
	log.Infof("Command state of %v changed to %v", c.Name, CommandStateReactions[c.state])
}

func (c *Command) Run() {
	var ok bool
	var threadId string
	log.Infof("Handling command %v...", c.Name)
	if rel := c.content.RelatesTo; rel != nil && rel.Type == event.RelThread {
		threadId = rel.EventID.String()
		log.Infof("threadId: %v", threadId)
	}
	switch c.Name {
	case "open":
		if threadId == "" {
			ok = false
		} else {
			ok = c.actions.OpenThread(c.roomId, threadId)
		}
	case "close":
		if threadId == "" {
			ok = false
		} else {
			ok = c.actions.CloseThread(c.roomId, threadId)
		}
	case "forceclose":
		if threadId == "" {
			ok = false
		} else {
			ok = c.actions.ForceCloseThread(c.roomId, threadId)
		}
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

func NewCommand(name string, evt *event.Event, client *MatrixClient, actions Actions) *Command {
	roomId := evt.RoomID.String()
	messageId := evt.ID.String()
	content := evt.Content.AsMessage()
	return &Command{
		Name:      name,
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
	actions Actions
	client  *MatrixClient
}

func (ch *CommandHandler) ProcessMessage(evt *event.Event) {
	message := evt.Content.AsMessage().Body
	parsed := commandRegex.FindStringSubmatch(message)
	if parsed == nil {
		return
	}
	cmd := strings.ToLower(parsed[1])
	for _, valid := range commands {
		if valid == cmd {
			c := NewCommand(cmd, evt, ch.client, ch.actions)
			go c.Run()
			break
		}
	}
}
