package mail

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	log "github.com/sirupsen/logrus"

	"github.com/arne314/inbox-collab/internal/config"
)

type Mail struct {
	Fetcher     string
	NameFrom    string
	AddrFrom    string
	Subject     string
	Date        time.Time
	Text        string
	MessageId   string
	InReplyTo   string
	References  []string
	AddrTo      []string
	Attachments []string
}

func (m *Mail) String() string {
	return fmt.Sprintf(
		"Mail %v on %v from %v to %v with subject %v and attachments %v",
		m.MessageId, m.Date, m.AddrFrom, m.AddrTo, m.Subject, m.Attachments,
	)
}

type FetcherStateStorage interface {
	GetState(id string) (uint32, uint32)
	SaveState(id string, uidLast uint32, uidValidity uint32)
}

type MailFetcher struct {
	name          string
	mailbox       string
	config        *config.MailSourceConfig
	globalConfig  *config.MailConfig
	client        *imapclient.Client
	idleCommand   *imapclient.IdleCommand
	isIdle        bool
	nameFromRegex *regexp.Regexp
	addressRegex  *regexp.Regexp
	idRegex       *regexp.Regexp

	uidLast     uint32
	uidValidity uint32
	mailHandler *MailHandler

	fetchingRequired chan struct{}
	fetchedMails     chan []*Mail
}

func NewMailFetcher(
	name string,
	mailbox string,
	config *config.MailSourceConfig,
	globalConfig *config.MailConfig,
	mailHandler *MailHandler,
	fetchedMails chan []*Mail,
) *MailFetcher {
	mailfetcher := &MailFetcher{
		name:          name,
		mailbox:       mailbox,
		config:        config,
		globalConfig:  globalConfig,
		mailHandler:   mailHandler,
		nameFromRegex: regexp.MustCompile(`(?i)\"?\s*([^<>\" ][^<>\"]+[^<>\" ])\"?\s*<`),
		addressRegex: regexp.MustCompile(
			`(?i)<?([a-zA-Z0-9%+-][a-zA-Z0-9.+-_{}\(\)\[\]'"\\#\$%\^\?/=&!\*\|~]*@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})>?`,
		),
		idRegex:          regexp.MustCompile(`(?i)<([^@ ]+@[^@ ]+)>`),
		fetchingRequired: make(chan struct{}, 1),
		fetchedMails:     fetchedMails,
	}
	mailfetcher.loadState()
	return mailfetcher
}

func (mf *MailFetcher) FetchMessages() []*Mail {
	mf.RevokeIdle()
	defer mf.Idle()

	searchCriteria := &imap.SearchCriteria{
		SentSince: time.Now().AddDate(0, 0, -mf.globalConfig.MaxAge),
	}
	if ok, _ := mf.uidsValid(); ok {
		uid := imap.UIDSet{}
		uid.AddRange(imap.UID(mf.uidLast+1), 0) // 0 means no upper limit
		searchCriteria.UID = []imap.UIDSet{uid}
	}
	searchOptions := &imap.SearchOptions{
		ReturnAll:   true,
		ReturnCount: true,
	}
	fetchOptions := &imap.FetchOptions{
		UID:         true,
		Flags:       true,
		Envelope:    true,
		BodySection: []*imap.FetchItemBodySection{{}},
	}

	// search for mails
	log.Infof("Searching for new mails for %v", mf.name)
	search, err := mf.client.UIDSearch(searchCriteria, searchOptions).Wait()
	if err != nil {
		log.Errorf("Error searching for mails in %v: %v", mf.name, err)
		return []*Mail{}
	}
	uids := search.AllUIDs()
	msgCount := int(search.Count)
	mails := make([]*Mail, 0, msgCount)
	if msgCount == 0 {
		log.Infof("No new mails to fetch for %v", mf.name)
		return mails
	}

	// fetch mails
	log.Infof("Fetching %v messages from %v...", msgCount, mf.name)
	fetch := mf.client.Fetch(search.All, fetchOptions)
	defer fetch.Close()

	for i := range msgCount {
		msg := fetch.Next()
		if msg == nil {
			log.Errorf("Fetched invalid message for %v from id %v", mf.name, uids[i])
			continue
		}
		mail := mf.parseMessage(msg)
		if mail != nil {
			log.Infof("MailFetcher %v fetched: %v", mf.name, mail)
			mails = append(mails, mail)
			mf.uidLast = max(mf.uidLast, uint32(uids[i]))
		}
	}
	mf.saveState()
	log.Infof("Done fetching %v messages from %v", len(mails), mf.name)
	return mails
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
		if err := mf.idleCommand.Close(); err != nil {
			log.Errorf("Error stopping idle of MailFetcher %v, retrying in 5s...", mf.name)
			time.Sleep(5 * time.Second)
			mf.RevokeIdle()
		}
	}
	mf.isIdle = false
	log.Infof("MailFetcher %v stopped idle", mf.name)
}

func (mf *MailFetcher) Login() {
	options := &imapclient.Options{
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Mailbox: func(data *imapclient.UnilateralDataMailbox) {
				if data.NumMessages != nil {
					log.Infof(
						"MailFetcher %v received a mailbox update, now %v messages",
						mf.name, *data.NumMessages,
					)
					mf.queueFetch()
				}
			},
		},
	}
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
	if mf.globalConfig.ListMailboxes {
		mf.listMailboxes()
		return
	}
	_, err = mf.uidsValid() // try to select inbox
	if err != nil {
		return
	}
	mf.Idle()
	mf.saveState()
	log.Infof("MailFetcher %v setup successfully", mf.name)
}

func (mf *MailFetcher) uidsValid() (bool, error) {
	mailbox, err := mf.client.Select(mf.mailbox, nil).Wait()
	if err != nil {
		log.Errorf("Failed to select inbox for %v: %v", mf.name, err)
		return false, err
	}
	if mf.uidValidity == mailbox.UIDValidity {
		return true, nil
	}
	if mf.uidValidity != 0 {
		log.Infof("UIDs for %v have been invalidated, all mails need to be refetched", mf.name)
	}
	mf.uidValidity = mailbox.UIDValidity
	return false, nil
}

func (mf *MailFetcher) listMailboxes() {
	listCmd := mf.client.List("", "*", nil)
	mailboxes, err := listCmd.Collect()
	mbs := make([]string, len(mailboxes))
	if err != nil {
		log.Errorf("Error listing mailboxes for %v: %v", mf.name, err)
		return
	}
	for i, m := range mailboxes {
		mbs[i] = m.Mailbox
	}
	log.Infof("Available mailboxes for %v: %v", mf.name, strings.Join(mbs, ", "))
}

func (mf *MailFetcher) loadState() {
	uidLast, uidValidity := mf.mailHandler.StateStorage.GetState(mf.name)
	mf.uidLast, mf.uidValidity = uidLast, uidValidity
}

func (mf *MailFetcher) saveState() {
	mf.mailHandler.StateStorage.SaveState(mf.name, mf.uidLast, mf.uidValidity)
}

func (mf *MailFetcher) queueFetch() {
	if len(mf.fetchingRequired) == 0 {
		mf.fetchingRequired <- struct{}{}
	}
	mf.mailHandler.MailboxUpdated()
}

func (mf *MailFetcher) StartFetching() {
	mf.queueFetch() // initial fetch
	for range mf.fetchingRequired {
		mf.fetchedMails <- mf.FetchMessages()
	}
}

func (mf *MailFetcher) Logout() {
	mf.RevokeIdle()
	err := mf.client.Logout().Wait()
	if err != nil {
		log.Errorf("Error logging out MailFetcher %v: %v", mf.name, err)
	}
	log.Infof("MailFetcher %v logged out", mf.name)
}
