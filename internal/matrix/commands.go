package matrix

import (
	"regexp"
	"slices"
	"strings"
	"sync"

	log "github.com/sirupsen/logrus"
	"maunium.net/go/mautrix/event"

	config "github.com/arne314/inbox-collab/internal/config"
)

type Actions interface {
	OpenThread(roomId string, threadId string) bool
	CloseThread(roomId string, threadId string) bool
	ForceCloseThread(roomId string, threadId string) bool
	ResendThreadOverview(roomId string) bool
	ResendThreadOverviewAll() bool
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
		"resendoverview",
		"resendoverviewall",
	}
	commandRegex          *regexp.Regexp = regexp.MustCompile(`^!\s*([a-zA-Z]+)`)
	CommandStateReactions []string       = []string{"👀", "⏳", "✅", "❌"}
	roomMutexes           map[string]*sync.Mutex
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
	case "resendoverview":
		ok = c.actions.ResendThreadOverview(c.roomId)
	case "resendoverviewall":
		ok = c.actions.ResendThreadOverviewAll()
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

func NewCommandHandler(
	cfg *config.MatrixConfig, actions Actions, client *MatrixClient,
) *CommandHandler {
	allRooms := cfg.AllRooms()
	roomMutexes = make(map[string]*sync.Mutex, len(allRooms))
	for _, r := range allRooms {
		roomMutexes[r] = new(sync.Mutex)
	}
	return &CommandHandler{actions: actions, client: client}
}

func (ch *CommandHandler) ProcessMessage(evt *event.Event) {
	message := evt.Content.AsMessage().Body
	parsed := commandRegex.FindStringSubmatch(message)
	if parsed == nil {
		return
	}
	cmd := strings.ToLower(parsed[1])
	if slices.Contains(commands, cmd) {
		c := NewCommand(cmd, evt, ch.client, ch.actions)
		go c.Run()
	}
}
