package db

import (
	"sync"

	config "github.com/arne314/inbox-collab/internal/config"
)

type DbHandler struct{}

func (dh *DbHandler) Setup(cfg *config.Config) {
}

func (dh *DbHandler) Stop(waitGroup *sync.WaitGroup) {
	waitGroup.Done()
}
