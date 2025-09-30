package config

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pelletier/go-toml/v2"
	log "github.com/sirupsen/logrus"
)

var (
	allRooms         []string
	allOverviewRooms []string
	allTargetRooms   []string
	roomAliases      map[string]string   // alias -> room
	roomAliasesInv   map[string]string   // room -> alias
	roomsOverviewInv map[string][]string // target -> overview rooms
)

type LLMConfig struct {
	ApiUrl string `toml:"python_api"`
}

type MailSourceConfig struct {
	Hostname  string   `toml:"hostname"`
	Port      int      `toml:"port"`
	Mailboxes []string `toml:"mailboxes"`
	Username  string
	Password  string
}

type MailConfig struct {
	MaxAge        int                          `toml:"max_age"`
	Sources       map[string]*MailSourceConfig `toml:"sources"`
	ListMailboxes bool
}

type MatrixConfig struct {
	Aliases       map[string]string   `toml:"aliases"`
	DefaultRoom   string              `toml:"default_room"`
	RoomsAddrFrom map[string]string   `toml:"rooms_addr_from"`
	RoomsAddrTo   map[string]string   `toml:"rooms_addr_to"`
	RoomsMailbox  map[string]string   `toml:"rooms_mailbox"`
	RoomsOverview map[string][]string `toml:"overview"` // overview room -> targets
	HeadBlacklist []string            `toml:"head_blacklist"`

	RoomsAddrFromRegex map[*regexp.Regexp]string
	RoomsAddrToRegex   map[*regexp.Regexp]string
	RoomsMailboxRegex  map[*regexp.Regexp]string
	HeadBlacklistRegex []*regexp.Regexp

	HomeServer    string
	Username      string
	Password      string
	VerifySession bool
}

type Config struct {
	Mail   *MailConfig   `toml:"mail"`
	Matrix *MatrixConfig `toml:"matrix"`
	LLM    *LLMConfig    `toml:"llm"`

	DatabaseUrl string
}

func (c *Config) getenv(name string) string {
	return os.Getenv(name)
}

// Convert a roomId (or an alias) to an alias
func (c *MatrixConfig) AliasOfRoom(room string) string {
	if alias, ok := roomAliasesInv[room]; ok {
		return alias
	}
	return room // fallback
}

// Get all rooms that are mentioned in the config
func (c *MatrixConfig) AllRooms() []string {
	return allRooms
}

// Get all rooms that are configured to have message threads created
func (c *MatrixConfig) AllTargetRooms() (rooms []string) {
	return allTargetRooms
}

// Get all rooms that are configured to provide overviews of others
func (c *MatrixConfig) AllOverviewRooms() (rooms []string) {
	return allOverviewRooms
}

// Get the rooms that the `overviewRoom` provides an overview of
func (c *MatrixConfig) GetOverviewRoomTargets(overviewRoom string) []string {
	if rooms, ok := c.RoomsOverview[overviewRoom]; ok {
		if len(rooms) == 0 {
			return c.AllTargetRooms()
		}
		return rooms
	}
	return []string{}
}

// Get the rooms that provide an overview of `target`
func (c *MatrixConfig) GetOverviewRooms(target string) []string {
	if rooms, ok := roomsOverviewInv[target]; ok {
		return rooms
	}
	return []string{}
}

func resolveRoomValue(room string) (res string) {
	if roomId, ok := roomAliases[room]; ok {
		res = roomId
		roomAliasesInv[res] = room
	} else {
		res = room
	}
	allRooms = append(allRooms, res)
	return
}

func filterRooms(rooms []string) []string {
	res := make([]string, 0, len(rooms))
	for _, r := range rooms {
		if r != "" {
			res = append(res, r)
		}
	}
	slices.Sort(res)
	return slices.Compact(res)
}

func (c *Config) Load() {
	// load config.toml
	file, err := os.ReadFile("config/config.toml")
	if err != nil {
		log.Fatalf("Error reading config.toml: %v", err)
		return
	}
	if err := toml.Unmarshal(file, c); err != nil {
		log.Fatalf("Error decoding TOML: %s", err)
		return
	}

	// parse command line flags
	flagVerifyMatrix := flag.Bool(
		"verify-matrix", false,
		"Accept session verification requests and automatically confirm matching SAS",
	)
	flagListMailboxes := flag.Bool(
		"list-mailboxes", false,
		"List all mailboxes on the mail server that the authenticated user has access to",
	)
	flag.Parse()
	c.Matrix.VerifySession = *flagVerifyMatrix
	c.Mail.ListMailboxes = *flagListMailboxes
	roomAliases = c.Matrix.Aliases
	roomAliasesInv = make(map[string]string)

	// load .env
	err = godotenv.Load()
	if err != nil {
		log.Warnf("[Expected in docker] Error loading .env file: %v", err)
	}
	c.Matrix.HomeServer = c.getenv("MATRIX_HOMESERVER")
	c.Matrix.Username = c.getenv("MATRIX_USERNAME")
	c.Matrix.Password = c.getenv("MATRIX_PASSWORD")
	c.DatabaseUrl = c.getenv("DATABASE_URL")

	for name, source := range c.Mail.Sources {
		source.Username = c.getenv(fmt.Sprintf("MAIL_%s_USERNAME", strings.ToUpper(name)))
		source.Password = c.getenv(fmt.Sprintf("MAIL_%s_PASSWORD", strings.ToUpper(name)))
		if source.Username == "" || source.Password == "" {
			log.Fatalf("Incomplete mail credentials provided for %v", name)
		}
		if len(source.Mailboxes) == 0 {
			source.Mailboxes = []string{"INBOX"}
		}
	}

	// regex validation
	c.Matrix.HeadBlacklistRegex = make([]*regexp.Regexp, len(c.Matrix.HeadBlacklist))
	for i, addr := range c.Matrix.HeadBlacklist {
		regex, err := regexp.CompilePOSIX(addr)
		if err != nil {
			log.Fatalf("Head blacklist config \"%s\" invalid: %v", addr, err)
		} else {
			c.Matrix.HeadBlacklistRegex[i] = regex
		}
	}
	validateRoomsRegex := func(configs map[string]string, regexps map[*regexp.Regexp]string) {
		for addr, room := range configs {
			regex, err := regexp.CompilePOSIX(addr)
			if err != nil {
				log.Fatalf("Matrix room config \"%s\" for %v invalid: %v", addr, room, err)
			} else {
				room = resolveRoomValue(room)
				regexps[regex] = room
				allTargetRooms = append(allTargetRooms, room)
			}
		}
	}
	c.Matrix.DefaultRoom = resolveRoomValue(c.Matrix.DefaultRoom)
	c.Matrix.RoomsAddrFromRegex = make(map[*regexp.Regexp]string)
	c.Matrix.RoomsAddrToRegex = make(map[*regexp.Regexp]string)
	c.Matrix.RoomsMailboxRegex = make(map[*regexp.Regexp]string)
	validateRoomsRegex(c.Matrix.RoomsAddrFrom, c.Matrix.RoomsAddrFromRegex)
	validateRoomsRegex(c.Matrix.RoomsAddrTo, c.Matrix.RoomsAddrToRegex)
	validateRoomsRegex(c.Matrix.RoomsMailbox, c.Matrix.RoomsMailboxRegex)

	// load overview config
	roomsOverview := make(map[string][]string)
	roomsOverviewInv = make(map[string][]string)
	c.Matrix.RoomsOverview[""] = []string{c.Matrix.DefaultRoom} // also a target

	// handle aliases and populate lists
	roomsWithFullOverview := []string{} // rooms that provide an overview over all rooms
	for overview, ts := range c.Matrix.RoomsOverview {
		overview = resolveRoomValue(overview)
		allOverviewRooms = append(allOverviewRooms, overview)
		targets := make([]string, len(ts))
		for i, t := range ts {
			target := resolveRoomValue(t)
			targets[i] = target
			allTargetRooms = append(allTargetRooms, target)
		}
		if len(ts) == 0 {
			roomsWithFullOverview = append(roomsWithFullOverview, overview)
		}
		roomsOverview[overview] = filterRooms(targets)
	}
	roomsWithFullOverview = filterRooms(roomsWithFullOverview)

	// fill inverse map
	for overview, targets := range roomsOverview {
		for _, target := range targets {
			roomsOverviewInv[target] = append(roomsOverviewInv[target], overview)
		}
	}
	for target := range roomsOverviewInv {
		roomsOverviewInv[target] = filterRooms(append(roomsOverviewInv[target], roomsWithFullOverview...))
	}

	delete(roomsOverview, "")
	c.Matrix.RoomsOverview = roomsOverview
	allRooms = filterRooms(allRooms)
	allOverviewRooms = filterRooms(allOverviewRooms)
	allTargetRooms = filterRooms(allTargetRooms)
}
