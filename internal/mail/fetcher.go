package mail

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
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
	GetState(ctx context.Context, id string) (uint32, uint32)
	SaveState(ctx context.Context, id string, uidLast uint32, uidValidity uint32)
}

type MailFetcher struct {
	name           string
	mailbox        string
	config         *config.MailSourceConfig
	globalConfig   *config.MailConfig
	client         *imapclient.Client
	idleCommand    *imapclient.IdleCommand
	isIdle         bool
	idleMutex      sync.Mutex
	isReconnecting atomic.Bool
	nameFromRegex  *regexp.Regexp
	addressRegex   *regexp.Regexp
	idRegex        *regexp.Regexp

	uidLast     uint32
	uidValidity uint32
	mailHandler *MailHandler

	ctx              context.Context
	cancel           context.CancelFunc
	closed           chan struct{}
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
	ctx, cancel := context.WithCancel(context.Background())
	mailfetcher := &MailFetcher{
		name:         name,
		mailbox:      mailbox,
		mailHandler:  mailHandler,
		config:       config,
		globalConfig: globalConfig,

		fetchingRequired: make(chan struct{}, 1),
		fetchedMails:     fetchedMails,
		ctx:              ctx,
		cancel:           cancel,
		closed:           make(chan struct{}, 1),

		nameFromRegex: regexp.MustCompile(`(?i)\"?\s*([^<>\" ][^<>\"]+[^<>\" ])\"?\s*<`),
		addressRegex: regexp.MustCompile(
			`(?i)<?([a-zA-Z0-9%+-][a-zA-Z0-9.+-_{}\(\)\[\]'"\\#\$%\^\?/=&!\*\|~]*@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,})>?`,
		),
		idRegex: regexp.MustCompile(`(?i)<([^@ ]+@[^@ ]+)>`),
	}
	mailfetcher.loadState()
	return mailfetcher
}

func (mf *MailFetcher) fetchMessages() []*Mail {
	searchCriteria := &imap.SearchCriteria{
		SentSince: time.Now().UTC().AddDate(0, 0, -mf.globalConfig.MaxAge),
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
		mf.mailHandler.MailboxUpdated()
	}
	mf.saveState()
	log.Infof("Done fetching %v messages from %v", len(mails), mf.name)
	return mails
}

func (mf *MailFetcher) idle() bool {
	if mf.isIdle {
		return true
	}
	mf.idleMutex.Lock()
	defer mf.idleMutex.Unlock()
	cmd, err := mf.client.Idle()
	if err != nil {
		log.Errorf("Error going idle with MailFetcher %v: %v", mf.name, err)
		mf.isIdle = false
		time.Sleep(3 * time.Second)
		mf.reconnect()
		return false
	}
	mf.idleCommand = cmd
	log.Infof("MailFetcher %v is now in idle", mf.name)
	mf.isIdle = true
	return true
}

func (mf *MailFetcher) revokeIdle() bool {
	if !mf.isIdle {
		return true
	}
	mf.isIdle = false
	if err := mf.idleCommand.Close(); err != nil {
		log.Errorf("Error stopping idle of MailFetcher %v, retrying in 5s: %v", mf.name, err)
		time.Sleep(3 * time.Second)
		if err = mf.idleCommand.Close(); err != nil {
			log.Errorf("Error stopping idle of MailFetcher %v, reconnecting: %v", mf.name, err)
			mf.reconnect()
			return false
		}
	}
	log.Infof("MailFetcher %v stopped idle", mf.name)
	return true
}

func runWithHardTimeout(timeout time.Duration, fn func() error) error {
	done := make(chan error, 1)
	go func() {
		done <- fn()
	}()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		return fmt.Errorf("timed out after %v", timeout)
	}
}

func (mf *MailFetcher) reconnect() {
	if !mf.isReconnecting.CompareAndSwap(false, true) {
		return
	}
	defer mf.isReconnecting.Store(false)
	for {
		if err := mf.ctx.Err(); err != nil {
			return
		}
		log.Infof("Reconnecting MailFetcher %v", mf.name)
		if !mf.logout() {
			err := mf.client.Close()
			if err != nil {
				log.Errorf("Error closing connection of MailFetcher %v: %v", mf.name, err)
			}
		}
		err := runWithHardTimeout(30*time.Second, func() error {
			if !mf.login() {
				return fmt.Errorf("Error logging in MailFetcher %v", mf.name)
			}
			return nil
		})
		if err != nil {
			log.Infof("Error reconnecting, retrying in 5s...")
			time.Sleep(5 * time.Second)
		} else {
			mf.queueFetch()
			time.Sleep(5 * time.Second)
			break
		}
	}
}

func (mf *MailFetcher) ensureConnected(activeValidation bool) {
	state := mf.client.State()
	if state != imap.ConnStateSelected && state != imap.ConnStateAuthenticated {
		mf.reconnect()
	}
	if activeValidation {
		if mf.revokeIdle() {
			defer mf.idle()
			err := runWithHardTimeout(10*time.Second, func() error { return mf.client.Noop().Wait() })
			if err != nil {
				log.Errorf("Error validating connection of MailFetcher %v: %v", mf.name, err)
				mf.reconnect()
			}
		}
	}
}

func (mf *MailFetcher) Setup() bool {
	loginSuccess := mf.login()
	startConnectionWatcher := func(active bool, interval time.Duration) {
		for {
			select {
			case <-mf.ctx.Done():
				return
			case <-time.After(interval):
				mf.ensureConnected(active)
			}
		}
	}
	if loginSuccess {
		if !mf.globalConfig.ListMailboxes {
			go startConnectionWatcher(false, time.Minute)
			go startConnectionWatcher(true, 20*time.Minute)
		}
		log.Infof("MailFetcher %v setup successfully", mf.name)
	}
	return loginSuccess
}

func (mf *MailFetcher) login() bool {
	options := &imapclient.Options{
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Mailbox: func(data *imapclient.UnilateralDataMailbox) {
				if mf.ctx.Err() != nil {
					return
				}
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
		log.Errorf("Failed to create imap client: %v", err)
		return false
	}
	err = client.Login(mf.config.Username, mf.config.Password).Wait()
	if err != nil {
		log.Errorf("Failed to login to mailbox %v: %v", mf.name, err)
		return false
	}
	mf.client = client
	if mf.globalConfig.ListMailboxes {
		mf.listMailboxes()
		return true
	}
	_, err = mf.uidsValid() // try to select inbox
	if err != nil {
		return false
	}
	mf.idle()
	mf.saveState()
	log.Infof("MailFetcher %v logged in successfully", mf.name)
	return true
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
	uidLast, uidValidity := mf.mailHandler.StateStorage.GetState(mf.ctx, mf.name)
	mf.uidLast, mf.uidValidity = uidLast, uidValidity
}

func (mf *MailFetcher) saveState() {
	mf.mailHandler.StateStorage.SaveState(mf.ctx, mf.name, mf.uidLast, mf.uidValidity)
}

func (mf *MailFetcher) queueFetch() {
	if len(mf.fetchingRequired) == 0 {
		mf.fetchingRequired <- struct{}{}
	}
	mf.mailHandler.MailboxUpdated()
}

func (mf *MailFetcher) StartFetching() {
	mf.queueFetch() // initial fetch

fetchloop:
	for {
		select {
		case <-mf.ctx.Done():
			break fetchloop
		case <-mf.fetchingRequired:
			mf.revokeIdle()
			mails := mf.fetchMessages()
			mf.fetchedMails <- mails
			if err := mf.ctx.Err(); err == nil {
				mf.idle()
			}
		}
	}
	mf.closed <- struct{}{}
}

func (mf *MailFetcher) logout() bool {
	mf.revokeIdle()
	err := runWithHardTimeout(5*time.Second, func() error { return mf.client.Logout().Wait() })
	if err != nil {
		log.Errorf("Error logging out MailFetcher %v: %v", mf.name, err)
		return false
	}
	log.Infof("MailFetcher %v logged out", mf.name)
	return true
}

func (mf *MailFetcher) Shutdown() {
	mf.cancel()
	mf.logout()
	<-mf.closed
}
