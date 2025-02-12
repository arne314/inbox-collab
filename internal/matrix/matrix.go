package matrix

import (
	"sync"

	config "github.com/arne314/inbox-collab/internal/config"
)

type MatrixHandler struct{}

func (mh *MatrixHandler) Setup(cfg *config.Config) {
}

func (mh *MatrixHandler) Run() {
}

func (mh *MatrixHandler) Stop(waitGroup *sync.WaitGroup) {
}
