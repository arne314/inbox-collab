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

func (mh *MatrixHandler) CreateThread(roomId string, subject string) (bool, string) {
	return mh.client.SendRoomMessage(roomId, subject)
}

func (mh *MatrixHandler) AddReply(roomId string, threadId string, message string) (bool, string) {
	return mh.client.SendThreadMessage(roomId, threadId, message)
}

func (mh *MatrixHandler) Stop(waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
	mh.client.Stop()
}
