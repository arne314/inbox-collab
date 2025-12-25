package mail

import (
	"fmt"
	"strings"
	"sync"
	"unicode"

	"github.com/arne314/inbox-collab/internal/config"
	log "github.com/sirupsen/logrus"
	simplemail "github.com/xhit/go-simple-mail/v2"
)

type MailSender struct {
	name      string
	config    *config.MailSenderConfig
	sendMutex sync.Mutex
	server    *simplemail.SMTPServer
	client    *simplemail.SMTPClient
}

func NewMailSender(name string, cfg *config.MailSenderConfig) *MailSender {
	server := simplemail.NewSMTPClient()
	server.Host = cfg.Hostname
	server.Port = cfg.Port
	server.Username = cfg.Username
	server.Password = cfg.Password
	server.KeepAlive = true
	server.Encryption = simplemail.EncryptionSSLTLS
	return &MailSender{name: name, config: cfg, server: server}
}

func (ms *MailSender) TestConnection() bool {
	res := ms.login() && ms.logout()
	log.Infof("MailSender %s working: %v", ms.name, res)
	return res
}

func normalizeMessage(msg string) string {
	msg = strings.ReplaceAll(msg, "\r\n", "\n")
	msg = strings.ReplaceAll(msg, "\r", "\n")
	msg = strings.TrimLeftFunc(msg, unicode.IsSpace)
	msg = strings.TrimRightFunc(msg, unicode.IsSpace)
	return msg
}

// login and send mail via smtp
func (ms *MailSender) SendReplyMail(reply string, cite string, originalSubject string, inReplyToId string, address string) error {
	// authentication
	ms.sendMutex.Lock()
	defer ms.sendMutex.Unlock()
	if !ms.login() {
		return fmt.Errorf("Failed to login to server")
	}
	defer ms.logout()

	// format text
	var content string
	reply = normalizeMessage(reply)
	if strings.TrimSpace(cite) != "" {
		cite = strings.ReplaceAll("\n"+normalizeMessage(cite), "\n", "\n> ")
		content = fmt.Sprintf("%s\n%s", reply, cite)
	} else {
		content = reply
	}

	// send mail
	mail := simplemail.NewMSG().
		SetFrom(ms.config.AddrFrom).
		AddTo(address).
		AddCc(ms.config.AddrCC).
		AddBcc(ms.config.AddrBCC).
		AddHeader("In-Reply-To", inReplyToId).
		SetSubject(fmt.Sprintf("Re: %s", originalSubject)).
		SetBody(simplemail.TextPlain, content)

	if mail.Error != nil {
		log.Errorf("MailSender %s failed to create a reply email: %v", ms.name, mail.Error)
		return fmt.Errorf("Failed to create mail")
	}
	if err := mail.Send(ms.client); err != nil {
		log.Errorf("MailSender %s failed to reply to mail %s: %v", ms.name, inReplyToId, err)
		return fmt.Errorf("Failed to send mail")
	}
	log.Infof("MailSender %s successfully replied to mail %s", ms.name, inReplyToId)
	return nil
}

func (ms *MailSender) login() bool {
	client, err := ms.server.Connect()
	if err != nil {
		log.Errorf("Error logging into MailSender %s: %v", ms.name, err)
		return false
	}
	ms.client = client
	return true
}

func (ms *MailSender) logout() bool {
	if ms.client != nil {
		if err := ms.client.Close(); err != nil {
			log.Warnf("Error closing smtp connection of MailSender %s: %v", ms.name, err)
			return false
		}
		ms.client = nil
	}
	return true
}
