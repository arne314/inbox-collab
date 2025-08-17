package config

import (
	"flag"
	"fmt"
	"iter"
	"maps"
	"os"
	"regexp"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pelletier/go-toml/v2"
	log "github.com/sirupsen/logrus"
)

var (
	roomAliases    map[string]string // alias -> room
	roomAliasesInv map[string]string // room -> alias
)

type Room string // either a room id or an alias

func (r Room) String() string { // returns a room id
	s := string(r)
	if res, ok := roomAliases[s]; ok {
		roomAliasesInv[res] = s
		return res
	}
	return s
}

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
	Aliases       map[string]string `toml:"aliases"`
	DefaultRoom   Room              `toml:"default_room"`
	RoomsAddrFrom map[string]Room   `toml:"rooms_addr_from"`
	RoomsAddrTo   map[string]Room   `toml:"rooms_addr_to"`
	RoomsMailbox  map[string]Room   `toml:"rooms_mailbox"`
	RoomsOverview map[Room][]Room   `toml:"overview"`
	HeadBlacklist []string          `toml:"head_blacklist"`

	roomsOverviewInv   map[string][]string // target -> overview rooms
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

func (c *MatrixConfig) AliasOfRoom(roomId string) string {
	if alias, ok := roomAliasesInv[roomId]; ok {
		return alias
	}
	return roomId // fallback
}

func (c *MatrixConfig) allRoomsMergeDefault(rooms iter.Seq[string]) []string {
	res := []string{}
	addDefault := true
	defaultRoom := c.DefaultRoom.String()
	for room := range rooms {
		res = append(res, room)
		if room == defaultRoom {
			addDefault = false
		}
	}
	if addDefault {
		res = append(res, defaultRoom)
	}
	return res
}

func (c *MatrixConfig) AllRooms() (rooms []string) {
	return c.allRoomsMergeDefault(maps.Keys(roomAliasesInv))
}

func (c *MatrixConfig) AllTargetRooms() (rooms []string) {
	return c.allRoomsMergeDefault(maps.Keys(c.roomsOverviewInv))
}

func (c *MatrixConfig) GetOverviewRoomTargets(overviewRoom string) (targets []string) {
	targetsRaw := c.RoomsOverview[Room(overviewRoom)]
	if len(targetsRaw) == 0 {
		return c.AllRooms()
	}
	targets = make([]string, len(targetsRaw))
	for i, t := range targetsRaw {
		targets[i] = t.String()
	}
	return
}

func (c *MatrixConfig) GetOverviewRooms(target string) []string {
	if rooms, ok := c.roomsOverviewInv[target]; ok {
		return rooms
	}
	return c.roomsOverviewInv[""]
}

func (c *MatrixConfig) AllOverviewRooms() (rooms []string) {
	rooms = make([]string, len(c.RoomsOverview))
	idx := 0
	for r := range c.RoomsOverview {
		rooms[idx] = r.String()
		idx++
	}
	return
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
	log.Infof("Loaded config: %+v", c)

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
	validateRoomsRegex := func(configs map[string]Room, regexps map[*regexp.Regexp]string) {
		for addr, room := range configs {
			regex, err := regexp.CompilePOSIX(addr)
			if err != nil {
				log.Fatalf("Matrix room config \"%s\" invalid: %v", addr, err)
			} else {
				regexps[regex] = room.String()
			}
		}
	}
	c.Matrix.RoomsAddrFromRegex = make(map[*regexp.Regexp]string)
	c.Matrix.RoomsAddrToRegex = make(map[*regexp.Regexp]string)
	c.Matrix.RoomsMailboxRegex = make(map[*regexp.Regexp]string)
	validateRoomsRegex(c.Matrix.RoomsAddrFrom, c.Matrix.RoomsAddrFromRegex)
	validateRoomsRegex(c.Matrix.RoomsAddrTo, c.Matrix.RoomsAddrToRegex)
	validateRoomsRegex(c.Matrix.RoomsMailbox, c.Matrix.RoomsMailboxRegex)

	// handle aliases in overview map
	overview := make(map[Room][]Room)
	overviewInv := make(map[string][]string)
	overviewInv[""] = make([]string, 0)
	for r, targets := range c.Matrix.RoomsOverview {
		room := r.String()
		targetRooms := make([]Room, len(targets))
		for i, t := range targets {
			target := t.String()
			targetRooms[i] = Room(target)
			overviewInv[target] = append(overviewInv[target], room)
		}
		if len(targets) == 0 {
			overviewInv[""] = append(overviewInv[""], room)
		}
		overview[Room(room)] = targetRooms
	}
	c.Matrix.RoomsOverview = overview
	c.Matrix.roomsOverviewInv = overviewInv
}
