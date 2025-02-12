package mail

import (
	"sync"

	config "github.com/arne314/inbox-collab/internal/config"
)

type MailHandler struct{}

func (dh *MailHandler) Setup(cfg *config.Config) {
}

func (mh *MailHandler) Run() {
}

func (mh *MailHandler) Stop(waitGroup *sync.WaitGroup) {
	waitGroup.Done()
}
