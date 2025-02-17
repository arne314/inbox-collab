package mail

import (
	"fmt"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/jhillyerd/enmime"
	log "github.com/sirupsen/logrus"

	"github.com/arne314/inbox-collab/internal/config"
)

type Mail struct {
	Id          string
	AddrTo      string
	AddrFrom    string
	Subject     string
	Date        time.Time
	Text        string
	Attachments []string
}

func (m *Mail) String() string {
	return fmt.Sprintf(
		"Mail %v on %v from %v to %v with subject %v and attachments %v",
		m.Id, m.Date, m.AddrFrom, m.AddrTo, m.Subject, m.Attachments,
	)
}

type MailFetcher struct {
	name        string
	config      *config.MailConfig
	client      *imapclient.Client
	isIdle      bool
	idleCommand *imapclient.IdleCommand
}

func NewMailFetcher(name string, cfg *config.MailConfig) *MailFetcher {
	mailfetcher := &MailFetcher{name: name, config: cfg}
	return mailfetcher
}

func (mf *MailFetcher) FetchMessages() []*Mail {
	mf.RevokeIdle()
	defer mf.Idle()

	msgCount := mf.client.Mailbox().NumMessages
	log.Infof("Fetching %v messages from %v...", msgCount, mf.name)

	options := &imap.FetchOptions{
		Flags:       true,
		Envelope:    true,
		BodySection: []*imap.FetchItemBodySection{{}},
	}

	var mails []*Mail
	for i := 0; i < int(msgCount); i++ {
		seqSet := imap.SeqSetNum(uint32(i + 1))
		fetch := mf.client.Fetch(seqSet, options)
		msg := fetch.Next()
		if msg == nil {
			log.Errorf("Fetched invalid message for %v with num %v", mf.name, i)
			continue
		}
		mail := mf.parseMessage(msg)
		if mail != nil {
			log.Infof("MailFetcher %v fetched: %v", mf.name, mail)
			mails = append(mails, mail)
		}
		fetch.Close()
	}
	log.Infof("Done fetching %v messages from %v", len(mails), mf.name)
	return mails
}

func (mf *MailFetcher) parseMessage(msg *imapclient.FetchMessageData) *Mail {
	var bodySection imapclient.FetchItemDataBodySection
	ok := false
	for {
		item := msg.Next()
		if item == nil {
			break
		}
		bodySection, ok = item.(imapclient.FetchItemDataBodySection)
		if ok {
			break
		}
	}
	if !ok {
		log.Errorf("FETCH command for %v did not return body section", mf.name)
		return nil
	}

	var envelope *enmime.Envelope
	envelope, err := enmime.ReadEnvelope(bodySection.Literal)
	if err != nil {
		log.Errorf("Failed to parse mail for %v: %v", mf.name, err)
		return nil
	}
	var date time.Time
	if date, err = envelope.Date(); err != nil {
		log.Errorf("Failed to parse date of mail for %v: %v", mf.name, err)
		return nil
	}
	var attachments []string
	for _, att := range envelope.Attachments {
		attachments = append(attachments, att.FileName)
	}
	parsedMail := &Mail{
		Id:          envelope.GetHeader("Message-ID"),
		AddrTo:      envelope.GetHeader("To"),
		AddrFrom:    envelope.GetHeader("From"),
		Subject:     envelope.GetHeader("Subject"),
		Date:        date,
		Text:        envelope.Text,
		Attachments: attachments,
	}
	if parsedMail.Id == "" || parsedMail.Text == "" {
		log.Errorf("Skipping invalid mail for %v: %v", mf.name, parsedMail)
		return nil
	}
	return parsedMail
}

func (mf *MailFetcher) Idle() {
	if !mf.isIdle {
		cmd, err := mf.client.Idle()
		if err != nil {
			log.Fatalf("Error going idle with MailFetcher %v: %v", mf.name, err)
		}
		mf.idleCommand = cmd
		log.Infof("MailFetcher %v is now in idle", mf.name)
	}
	mf.isIdle = true
}

func (mf *MailFetcher) RevokeIdle() {
	if mf.isIdle {
		mf.idleCommand.Close()
	}
	mf.isIdle = false
}

func (mf *MailFetcher) Login() {
	options := &imapclient.Options{}
	client, err := imapclient.DialTLS(
		fmt.Sprintf("%s:%d", mf.config.Hostname, mf.config.Port),
		options,
	)
	if err != nil {
		log.Fatalf("Failed to create imap client: %v", err)
	}
	err = client.Login(mf.config.Username, mf.config.Password).Wait()
	if err != nil {
		log.Fatalf("Failed to login to mailbox %v: %v", mf.name, err)
	}
	mf.client = client

	_, err = client.Select("INBOX", nil).Wait()
	if err != nil {
		log.Fatalf("Failed to select inbox for %v: %v", mf.name, err)
	}
	mf.Idle()
	log.Infof("MailFetcher %v logged in", mf.name)
}

func (mf *MailFetcher) Logout() {
	mf.RevokeIdle()
	err := mf.client.Logout().Wait()
	if err != nil {
		log.Errorf("Error logging out MailFetcher %v: %v", mf.name, err)
	}
	log.Infof("MailFetcher %v logged out", mf.name)
}
