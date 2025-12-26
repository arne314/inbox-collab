package mail

import (
	"fmt"
	"sync"
	"time"

	config "github.com/arne314/inbox-collab/internal/config"
	log "github.com/sirupsen/logrus"
)

var mailboxUpdateMutex sync.RWMutex

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

type MailHandler struct {
	fetchers          []*MailFetcher
	senders           map[string]*MailSender
	Config            *config.MailConfig
	fetchedMails      chan []*Mail
	lastMailboxUpdate time.Time
	StateStorage      FetcherStateStorage
}

func (mh *MailHandler) Setup(
	wg *sync.WaitGroup,
	fetchedMails chan []*Mail, stateStorage FetcherStateStorage,
) {
	defer wg.Done()
	var waitGroup sync.WaitGroup
	mh.fetchedMails = fetchedMails
	mh.StateStorage = stateStorage
	mh.senders = make(map[string]*MailSender)

	for name, cfg := range mh.Config.Sources {
		for _, mailbox := range cfg.Mailboxes {
			var fetcherName string
			if mh.Config.ListMailboxes {
				fetcherName = name
			} else {
				fetcherName = fmt.Sprintf("%s:%s", name, mailbox)
			}
			fetcher := NewMailFetcher(
				fetcherName, mailbox, cfg,
				mh.Config, mh, fetchedMails,
			)
			waitGroup.Add(1)
			go func(f *MailFetcher) {
				if !f.Setup() {
					log.Panicf("Unable to connect MailFetcher %v", f.name)
				}
				waitGroup.Done()
			}(fetcher)
			mh.fetchers = append(mh.fetchers, fetcher)
			if mh.Config.ListMailboxes { // only one fetcher per mail server
				break
			}
		}
	}

	for name, cfg := range mh.Config.Senders {
		sender := NewMailSender(name, cfg)
		waitGroup.Add(1)
		go func(s *MailSender) {
			if !s.TestConnection() {
				log.Panicf("Unable to use MailSender %v", s.name)
			}
			waitGroup.Done()
		}(sender)
		mh.senders[name] = sender
	}
	waitGroup.Wait()
	log.Infof("Setup MailHandler")
}

func (mh *MailHandler) GetMailSender(name string) *MailSender {
	return mh.senders[name]
}

func (mh *MailHandler) MailboxUpdated() {
	mailboxUpdateMutex.Lock()
	defer mailboxUpdateMutex.Unlock()
	mh.lastMailboxUpdate = time.Now().UTC()
}

func (mh *MailHandler) GetLastMailboxUpdate() time.Time {
	return mh.lastMailboxUpdate
}

func (mh *MailHandler) Run(waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
	for _, fetcher := range mh.fetchers {
		if !mh.Config.ListMailboxes {
			go fetcher.StartFetching()
		} else {
			fetcher.closed <- struct{}{}
		}
	}
}

func (mh *MailHandler) Stop() {
	var wg sync.WaitGroup
	wg.Add(len(mh.fetchers))
	for _, fetcher := range mh.fetchers {
		go func(f *MailFetcher) {
			f.Shutdown()
			wg.Done()
		}(fetcher)
	}
	wg.Wait()
}
