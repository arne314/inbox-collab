package mail

import (
	"fmt"
	"sync"
	"time"

	config "github.com/arne314/inbox-collab/internal/config"
	log "github.com/sirupsen/logrus"
)

var mailboxUpdateMutex sync.RWMutex

type MailHandler struct {
	fetchers          []*MailFetcher
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
					log.Panicf("Unable to connect MailFetcher %v", name)
				}
				waitGroup.Done()
			}(fetcher)
			mh.fetchers = append(mh.fetchers, fetcher)
			if mh.Config.ListMailboxes { // only one fetcher per mail server
				break
			}
		}
	}
	waitGroup.Wait()
	log.Infof("Setup MailHandler")
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
	if mh.Config.ListMailboxes {
		return
	}
	for _, fetcher := range mh.fetchers {
		go fetcher.StartFetching()
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
