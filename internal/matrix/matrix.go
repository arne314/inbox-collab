package matrix

import (
	"sync"

	config "github.com/arne314/inbox-collab/internal/config"
	_ "github.com/mattn/go-sqlite3"
)

type MatrixHandler struct {
	client *MatrixClient
}

func (mh *MatrixHandler) Setup(cfg *config.Config, wg *sync.WaitGroup) {
	defer wg.Done()
	mh.client = &MatrixClient{}
	mh.client.Login(cfg)
}

func (mh *MatrixHandler) Run() {
	mh.client.Run()
}

func (mh *MatrixHandler) Stop(waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
	mh.client.Stop()
}
