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
		"Mail %v at %v from %v to %v with subject %v and attachments %v",
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
				if !f.Setup(mh.Config.ListMailboxes) {
					log.Fatalf("Unable to connect MailFetcher %v", f.name)
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
		sender := NewMailSender(name, cfg, mh.Config)
		waitGroup.Add(1)
		go func(s *MailSender) {
			if !s.TestConnection() {
				log.Fatalf("Unable to use MailSender %v", s.Name)
			}
			for _, storer := range s.config.Storers {
				if !mh.GetMailStorerFetcher(storer).TestConnection() {
					log.Fatalf("Unable to use store configuration %s in MailSender %s", storer, s.Name)
				}
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

func (mh *MailHandler) GetMailStorerFetcher(storer config.Storer) (fetcher *MailFetcher) {
	name := fmt.Sprintf("%s:%s:store", storer.Source, storer.Mailbox)
	return NewMailFetcher(name, storer.Mailbox, mh.Config.Sources[storer.Source], mh.Config, mh, nil)
}

func (mh *MailHandler) StoreSentMail(sender string, mail *Mail, raw string) bool {
	var waitGroup sync.WaitGroup
	storers := mh.Config.Senders[sender].Storers
	resultChan := make(chan bool, len(storers))

	for _, storer := range storers {
		waitGroup.Add(1)
		go func(fetcher *MailFetcher) {
			defer waitGroup.Done()
			resultChan <- fetcher.Setup(true) && fetcher.StoreMail(mail, raw)
		}(mh.GetMailStorerFetcher(storer))
	}
	waitGroup.Wait()
	close(resultChan)
	for ok := range resultChan {
		if !ok {
			return false
		}
	}
	return true
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
