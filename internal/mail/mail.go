package mail

import (
	"fmt"
	"sync"

	config "github.com/arne314/inbox-collab/internal/config"
	log "github.com/sirupsen/logrus"
)

type MailHandler struct {
	fetchers []*MailFetcher
	config   *config.Config
}

func (mh *MailHandler) Setup(
	globalCfg *config.Config, wg *sync.WaitGroup, stateStorage FetcherStateStorage,
) {
	defer wg.Done()
	mh.config = globalCfg
	var waitGroup sync.WaitGroup
	for name, cfg := range globalCfg.Mail {
		for _, mailbox := range cfg.Mailboxes {
			var fetcherName string
			if globalCfg.ListMailboxes {
				fetcherName = name
			} else {
				fetcherName = fmt.Sprintf("%s_%s", name, mailbox)
			}
			fetcher := NewMailFetcher(fetcherName, mailbox, cfg, globalCfg, stateStorage)
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

func (mh *MailHandler) Run() {
}

func (mh *MailHandler) FetchMessages(mails chan []*Mail) {
	if mh.config.ListMailboxes {
		close(mails)
		return
	}
	var wg sync.WaitGroup
	wg.Add(len(mh.fetchers))
	for _, fetcher := range mh.fetchers {
		go func() {
			mails <- fetcher.FetchMessages()
			wg.Done()
		}()
	}
	wg.Wait()
	close(mails)
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
