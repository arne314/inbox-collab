package mail

import (
	"sync"

	config "github.com/arne314/inbox-collab/internal/config"
	log "github.com/sirupsen/logrus"
)

type MailHandler struct {
	fetchers []*MailFetcher
}

func (mh *MailHandler) Setup(cfg *config.Config) {
	var wg sync.WaitGroup
	for name, c := range cfg.Mail {
		fetcher := NewMailFetcher(name, c)
		wg.Add(1)
		go func() {
			fetcher.Login()
			wg.Done()
		}()
		mh.fetchers = append(mh.fetchers, fetcher)
	}
	wg.Wait()
	log.Infof("Setup MailHandler")
}

func (mh *MailHandler) Run() {
}

func (mh *MailHandler) Stop(waitGroup *sync.WaitGroup) {
	waitGroup.Add(len(mh.fetchers))
	for _, fetcher := range mh.fetchers {
		go func() {
			fetcher.Logout()
			waitGroup.Done()
		}()
	}
	waitGroup.Done()
}
