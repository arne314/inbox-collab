package config

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pelletier/go-toml"
	log "github.com/sirupsen/logrus"
)

type LLMConfig struct {
	ApiUrl string `toml:"python_api"`
}

type MailConfig struct {
	Hostname  string   `toml:"hostname"`
	Port      int      `toml:"port"`
	Mailboxes []string `toml:"mailboxes"`
	Username  string
	Password  string
}

type MatrixConfig struct {
	Room string `toml:"room"`

	HomeServer    string
	Username      string
	Password      string
	VerifySession bool
}

type Config struct {
	Mail   map[string]*MailConfig `toml:"mail"`
	LLM    *LLMConfig             `toml:"llm"`
	Matrix *MatrixConfig          `toml:"matrix"`

	DatabaseUrl         string
	ListMailboxes       bool
}

func (c *Config) getenv(name string) string {
	return os.Getenv(name)
}

func (c *Config) Load() {
	// load config.toml
	file, err := os.ReadFile("config.toml")
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
	c.ListMailboxes = *flagListMailboxes
	log.Infof("Loaded config: %+v", c)

	// load .env
	err = godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}
	c.Matrix.HomeServer = c.getenv("MATRIX_HOMESERVER")
	c.Matrix.Username = c.getenv("MATRIX_USERNAME")
	c.Matrix.Password = c.getenv("MATRIX_PASSWORD")
	c.DatabaseUrl = c.getenv("DATABASE_URL")

	for name, mailConfig := range c.Mail {
		mailConfig.Username = c.getenv(fmt.Sprintf("MAIL_%s_USERNAME", strings.ToUpper(name)))
		mailConfig.Password = c.getenv(fmt.Sprintf("MAIL_%s_PASSWORD", strings.ToUpper(name)))
		if mailConfig.Username == "" || mailConfig.Password == "" {
			log.Fatalf("Incomplete mail credentials provided for %v", name)
		}
		if len(mailConfig.Mailboxes) == 0 {
			mailConfig.Mailboxes = []string{"INBOX"}
		}
	}
}
