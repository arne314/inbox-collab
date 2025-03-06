package mail

import (
	"sync"

	config "github.com/arne314/inbox-collab/internal/config"
	log "github.com/sirupsen/logrus"
)

type MailHandler struct {
	fetchers []*MailFetcher
}

func (mh *MailHandler) Setup(cfg *config.Config, wg *sync.WaitGroup) {
	defer wg.Done()
	var waitGroup sync.WaitGroup
	for name, c := range cfg.Mail {
		fetcher := NewMailFetcher(name, c)
		waitGroup.Add(1)
		go func() {
			fetcher.Login()
			waitGroup.Done()
		}()
		mh.fetchers = append(mh.fetchers, fetcher)
	}
	waitGroup.Wait()
	log.Infof("Setup MailHandler")
}

func (mh *MailHandler) Run() {
}

func (mh *MailHandler) FetchMessages(mails chan []*Mail) {
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
