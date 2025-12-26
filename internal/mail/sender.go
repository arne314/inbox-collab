package mail

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/arne314/inbox-collab/internal/config"
	log "github.com/sirupsen/logrus"
	simplemail "github.com/xhit/go-simple-mail/v2"
)

type MailSender struct {
	name         string
	config       *config.MailSenderConfig
	mailConfig   *config.MailConfig
	authorAddr   string
	authorName   string
	authorDomain string

	sendMutex sync.Mutex
	server    *simplemail.SMTPServer
	client    *simplemail.SMTPClient
}

func NewMailSender(name string, cfg *config.MailSenderConfig, mailConfig *config.MailConfig) *MailSender {
	server := simplemail.NewSMTPClient()
	server.Host = cfg.Hostname
	server.Port = cfg.Port
	server.Username = cfg.Username
	server.Password = cfg.Password
	server.KeepAlive = true
	server.Encryption = simplemail.EncryptionSSLTLS
	return &MailSender{
		name: name, config: cfg, mailConfig: mailConfig, server: server,
		authorAddr:   parseAddresses(cfg.AddrFrom, false)[0],
		authorName:   parseNameFrom(cfg.AddrFrom),
		authorDomain: parseDomain(cfg.AddrFrom),
	}
}

func (ms *MailSender) TestConnection() bool {
	res := ms.login() && ms.logout()
	log.Infof("MailSender %s working: %v", ms.name, res)
	return res
}

func (ms *MailSender) generateMessageId(content string) string {
	input := fmt.Sprintf("%d: %s", time.Now().Unix(), content)
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("inbox-collab-%x@%s", hash[:16], ms.authorDomain)
}

func normalizeMessage(msg string) string {
	msg = strings.ReplaceAll(msg, "\r\n", "\n")
	msg = strings.ReplaceAll(msg, "\r", "\n")
	msg = strings.TrimSpace(msg)
	return msg
}

func wrapHeaderAngles(headers ...string) []string {
	wrapped := make([]string, len(headers))
	for i, h := range headers {
		wrapped[i] = fmt.Sprintf("<%s>", h)
	}
	return wrapped
}

func filterAddrCc(to string, cc []string) []string {
	return slices.DeleteFunc(cc, func(c string) bool {
		return parseAddresses(c, false)[0] == to
	})
}

func (ms *MailSender) createSimplemailEmail(mail *Mail) *simplemail.Email {
	return simplemail.NewMSG().
		SetFrom(ms.config.AddrFrom).
		AddCc(filterAddrCc(mail.AddrTo[0], ms.config.AddrCC)...).
		AddBcc(filterAddrCc(mail.AddrTo[0], ms.config.AddrBCC)...).
		AddHeader("Message-ID", wrapHeaderAngles(mail.MessageId)...).
		AddHeader("References", wrapHeaderAngles(mail.References...)...).
		AddHeader("In-Reply-To", wrapHeaderAngles(mail.InReplyTo)...).
		SetSubject(mail.Subject).
		SetBody(simplemail.TextPlain, mail.Text)
}

// login and send mail via smtp
func (ms *MailSender) SendReplyMail(reply string, cite string, originalSubject string,
	originalTimestamp time.Time, originalId string, originalReferences []string, nameTo string, addrTo string,
) (error, *Mail, string) {
	// authentication
	ms.sendMutex.Lock()
	defer ms.sendMutex.Unlock()
	if !ms.login() {
		return fmt.Errorf("Failed to login to server"), nil, ""
	}
	defer ms.logout()

	// format text
	var addressee, citeAuthor, content, subject string
	if nameTo != "" {
		addressee = fmt.Sprintf("%s <%s>", nameTo, addrTo)
		citeAuthor = nameTo
	} else {
		addressee = addrTo
		citeAuthor = addrTo
	}
	subject = fmt.Sprintf("Re: %s", originalSubject)
	reply = normalizeMessage(reply)
	if strings.TrimSpace(cite) != "" {
		cite = strings.ReplaceAll("\n"+normalizeMessage(cite), "\n", "\n> ")
		zone, _ := time.LoadLocation(ms.mailConfig.Timezone) // timezone has already been validated
		formatTime := originalTimestamp.In(zone).Format("2 Jan 2006 15:04")
		content = fmt.Sprintf("%s\n\n%s, %s:%s", reply, citeAuthor, formatTime, cite)
	} else {
		content = reply
	}

	// send smail
	mail := &Mail{
		MessageId:   ms.generateMessageId(content),
		InReplyTo:   originalId,
		References:  append(originalReferences, originalId),
		NameFrom:    ms.authorName,
		AddrFrom:    ms.authorAddr,
		AddrTo:      []string{addrTo},
		Subject:     subject,
		Date:        time.Now().UTC(),
		Text:        content,
		Attachments: []string{},
	}
	simplemailEmail := ms.createSimplemailEmail(mail).AddTo(addressee)

	if simplemailEmail.Error != nil {
		log.Errorf("MailSender %s failed to create a reply email: %v", ms.name, simplemailEmail.Error)
		return fmt.Errorf("Failed to create mail"), nil, ""
	}
	if err := simplemailEmail.Send(ms.client); err != nil {
		log.Errorf("MailSender %s failed to reply to mail %s: %v", ms.name, originalId, err)
		return fmt.Errorf("Failed to send mail"), nil, ""
	}
	log.Infof("MailSender %s successfully replied to mail %s", ms.name, originalId)
	return nil, mail, reply
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
