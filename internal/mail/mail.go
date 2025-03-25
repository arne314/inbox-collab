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
	config            *config.Config
	fetchedMails      chan []*Mail
	lastMailboxUpdate time.Time
	StateStorage      FetcherStateStorage
}

func (mh *MailHandler) Setup(
	globalCfg *config.Config, wg *sync.WaitGroup,
	fetchedMails chan []*Mail, stateStorage FetcherStateStorage,
) {
	defer wg.Done()
	var waitGroup sync.WaitGroup
	mh.config = globalCfg
	mh.fetchedMails = fetchedMails
	mh.StateStorage = stateStorage

	for name, cfg := range globalCfg.Mail {
		for _, mailbox := range cfg.Mailboxes {
			var fetcherName string
			if globalCfg.ListMailboxes {
				fetcherName = name
			} else {
				fetcherName = fmt.Sprintf("%s:%s", name, mailbox)
			}
			fetcher := NewMailFetcher(
				fetcherName, mailbox, cfg,
				globalCfg, mh, fetchedMails,
			)
			waitGroup.Add(1)
			go func() {
				fetcher.Login()
				waitGroup.Done()
			}()
			mh.fetchers = append(mh.fetchers, fetcher)
			if globalCfg.ListMailboxes { // only one fetcher per mail server
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
	mh.lastMailboxUpdate = time.Now()
}

func (mh *MailHandler) GetLastMailboxUpdate() time.Time {
	return mh.lastMailboxUpdate
}

func (mh *MailHandler) Run() {
	if mh.config.ListMailboxes {
		return
	}
	for _, fetcher := range mh.fetchers {
		go fetcher.StartFetching()
	}
}

func (mh *MailHandler) Stop(wg *sync.WaitGroup) {
	defer wg.Done()
	var waitGroup sync.WaitGroup
	waitGroup.Add(len(mh.fetchers))
	for _, fetcher := range mh.fetchers {
		go func() {
			fetcher.Logout()
			waitGroup.Done()
		}()
	}
	waitGroup.Wait()
}
