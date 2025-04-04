package config

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pelletier/go-toml/v2"
	log "github.com/sirupsen/logrus"
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
	RoomsOverview map[string][]string `toml:"overview"`
	HeadBlacklist []string            `toml:"head_blacklist"`

	AllRooms           []string
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

func (c *Config) getRoomByAlias(alias string) string {
	var result string
	if room, ok := c.Matrix.Aliases[alias]; ok {
		result = room
	} else {
		result = alias
	}
	c.Matrix.AllRooms = append(c.Matrix.AllRooms, result)
	return result
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

	// validation
	c.Matrix.AllRooms = []string{}
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
				log.Fatalf("Matrix room config \"%s\" invalid: %v", addr, err)
			} else {
				regexps[regex] = c.getRoomByAlias(room)
			}
		}
	}
	c.Matrix.RoomsAddrFromRegex = make(map[*regexp.Regexp]string)
	c.Matrix.RoomsAddrToRegex = make(map[*regexp.Regexp]string)
	c.Matrix.RoomsMailboxRegex = make(map[*regexp.Regexp]string)
	validateRoomsRegex(c.Matrix.RoomsAddrFrom, c.Matrix.RoomsAddrFromRegex)
	validateRoomsRegex(c.Matrix.RoomsAddrTo, c.Matrix.RoomsAddrToRegex)
	validateRoomsRegex(c.Matrix.RoomsMailbox, c.Matrix.RoomsMailboxRegex)

	c.Matrix.DefaultRoom = c.getRoomByAlias(c.Matrix.DefaultRoom)
	overview := make(map[string][]string)
	for room, targets := range c.Matrix.RoomsOverview {
		delete(c.Matrix.RoomsOverview, room)
		targetRooms := make([]string, len(targets))
		for i, t := range targets {
			targetRooms[i] = c.getRoomByAlias(t)
		}
		overview[c.getRoomByAlias(room)] = targetRooms
	}
	c.Matrix.RoomsOverview = overview
}
