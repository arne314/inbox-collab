package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"github.com/pelletier/go-toml"
	log "github.com/sirupsen/logrus"
)

type MailConfig struct {
	Hostname string `toml:"hostname"`
	Port     int    `toml:"port"`
	Username string
	Password string
}

type LLMConfig struct {
	ApiUrl string `toml:"python_api"`
}

type Config struct {
	Mail             map[string]*MailConfig `toml:"mail"`
	MatrixHomeServer string
	MatrixUsername   string
	MatrixPassword   string
	DatabaseUrl      string
	LLM              *LLMConfig `toml:"llm"`
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
	log.Infof("Loaded toml config %+v", c)

	// load .env
	err = godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}
	c.MatrixHomeServer = c.getenv("MATRIX_HOMESERVER")
	c.MatrixUsername = c.getenv("MATRIX_USERNAME")
	c.MatrixPassword = c.getenv("MATRIX_PASSWORD")
	c.DatabaseUrl = c.getenv("DATABASE_URL")

	for name, mailConfig := range c.Mail {
		mailConfig.Username = c.getenv(fmt.Sprintf("MAIL_%s_USERNAME", strings.ToUpper(name)))
		mailConfig.Password = c.getenv(fmt.Sprintf("MAIL_%s_PASSWORD", strings.ToUpper(name)))
		if mailConfig.Username == "" || mailConfig.Password == "" {
			log.Fatalf("Incomplete mail credentials provided for %v", name)
		}
	}
}
