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

type Config struct {
	Mail             map[string]*MailConfig `toml:"mail"`
	MatrixHomeServer string
	MatrixUsername   string
	MatrixPassword   string
	DatabaseUrl      string
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
	c.MatrixHomeServer = os.Getenv("MATRIX_HOMESERVER")
	c.MatrixUsername = os.Getenv("MATRIX_USERNAME")
	c.MatrixPassword = os.Getenv("MATRIX_PASSWORD")
	c.DatabaseUrl = os.Getenv("DATABASE_URL")

	for name, mailConfig := range c.Mail {
		mailConfig.Username = os.Getenv(fmt.Sprintf("MAIL_%s_USERNAME", strings.ToUpper(name)))
		mailConfig.Password = os.Getenv(fmt.Sprintf("MAIL_%s_PASSWORD", strings.ToUpper(name)))
		if mailConfig.Username == "" || mailConfig.Password == "" {
			log.Fatalf("Incomplete mail credentials provided for %v", name)
		}
	}
}
