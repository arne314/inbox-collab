package app

import (
	"slices"
	"sync"

	"github.com/jackc/pgx/v5/pgtype"
	log "github.com/sirupsen/logrus"

	cfg "github.com/arne314/inbox-collab/internal/config"
	"github.com/arne314/inbox-collab/internal/db"
	model "github.com/arne314/inbox-collab/internal/db/generated"
	"github.com/arne314/inbox-collab/internal/mail"
	"github.com/arne314/inbox-collab/internal/matrix"
)

var (
	waitGroup               *sync.WaitGroup = &sync.WaitGroup{}
	MessageExtractionStage  *PipelineStage
	ThreadSortingStage      *PipelineStage
	MatrixNotificationStage *PipelineStage
	MatrixOverviewStages    map[string]*PipelineStage
)

type InboxCollab struct {
	config        *cfg.Config
	dbHandler     *db.DbHandler
	matrixHandler *matrix.MatrixHandler
	mailHandler   *mail.MailHandler
	llm           *LLM

	fetchedMails chan []*mail.Mail
}

type FetcherStateStorageImpl struct {
	getState  func(id string) (uint32, uint32)
	saveState func(id string, uidLast uint32, uidValidity uint32)
}

func (f FetcherStateStorageImpl) GetState(id string) (uint32, uint32) {
	return f.getState(id)
}

func (f FetcherStateStorageImpl) SaveState(id string, uidLast uint32, uidValidity uint32) {
	f.saveState(id, uidLast, uidValidity)
}

func (ic *InboxCollab) Setup(
	config *cfg.Config,
	dbHandler *db.DbHandler,
	mailHandler *mail.MailHandler,
	matrixHandler *matrix.MatrixHandler,
) {
	ic.config = config
	ic.dbHandler = dbHandler
	ic.mailHandler = mailHandler
	ic.matrixHandler = matrixHandler
	ic.llm = &LLM{config: config.LLM}
	ic.fetchedMails = make(chan []*mail.Mail, 100)
	if !config.Matrix.VerifySession {
		waitGroup.Add(1)
		go mailHandler.Setup(config.Mail, waitGroup, ic.fetchedMails, FetcherStateStorageImpl{
			getState:  dbHandler.GetMailFetcherState,
			saveState: dbHandler.UpdateMailFetcherState,
		})
	}
	waitGroup.Add(1)
	go matrixHandler.Setup(config, ic, waitGroup)
	waitGroup.Wait()
	ic.setupMessageExtractionStage()
	ic.setupThreadSortingStage()
	ic.setupMatrixNotificationsStage()
	ic.setupMatrixOverviewStage()
}

func (ic *InboxCollab) storeMails() {
	initial := true
	for chunk := range ic.fetchedMails {
		modelled := make([]*model.Mail, len(chunk))
		for i, mail := range chunk {
			modelled[i] = &model.Mail{
				Fetcher:          pgtype.Text{String: mail.Fetcher, Valid: true},
				HeaderID:         mail.MessageId,
				HeaderInReplyTo:  mail.InReplyTo,
				HeaderReferences: mail.References,
				Subject:          mail.Subject,
				Timestamp:        pgtype.Timestamp{Time: mail.Date, Valid: true},
				Attachments:      mail.Attachments,
				NameFrom:         mail.NameFrom,
				AddrFrom:         mail.AddrFrom,
				AddrTo:           mail.AddrTo,
				Body:             &mail.Text,
			}
		}
		nFetched := ic.dbHandler.AddMails(modelled)
		if nFetched > 0 || initial {
			log.Infof("Added %v new messages to db", nFetched)
			MessageExtractionStage.QueueWork()
			initial = false
		}
	}
}

func (ic *InboxCollab) OpenThread(roomId string, threadId string) bool {
	ok := ic.dbHandler.UpdateThreadEnabled(roomId, threadId, true, false)
	if ok {
		ic.QueueMatrixOverviewUpdate([]string{roomId})
	}
	return ok
}

func (ic *InboxCollab) CloseThread(roomId string, threadId string) bool {
	ok := ic.dbHandler.UpdateThreadEnabled(
		roomId, threadId, false,
		false, // force close boolean is ignored internally
	)
	if ok {
		ic.QueueMatrixOverviewUpdate([]string{roomId})
	}
	return ok
}

func (ic *InboxCollab) ForceCloseThread(roomId string, threadId string) bool {
	ok := ic.dbHandler.UpdateThreadEnabled(roomId, threadId, false, true)
	if ok {
		ic.QueueMatrixOverviewUpdate([]string{roomId})
	}
	return ok
}

func (ic *InboxCollab) ResendThreadOverview(roomId string) bool {
	ok := false
	if !slices.Contains(ic.config.Matrix.AllOverviewRooms(), roomId) {
		return false
	}
	room := ic.dbHandler.GetRoom(roomId)
	if room != nil {
		ok = ic.matrixHandler.RemoveThreadOverview(roomId, room.OverviewMessageID.String)
		if ok {
			MatrixOverviewStages[roomId].QueueWork()
		}
	}
	return ok
}

func (ic *InboxCollab) ResendThreadOverviewAll() bool {
	ok := true
	rooms := ic.config.Matrix.AllOverviewRooms()
	for _, roomId := range rooms {
		room := ic.dbHandler.GetRoom(roomId)
		if room != nil {
			if ic.matrixHandler.RemoveThreadOverview(roomId, room.OverviewMessageID.String) {
				MatrixOverviewStages[roomId].QueueWork()
			} else {
				ok = false
			}
		}
	}
	return ok
}

func (ic *InboxCollab) Run() {
	if ic.config.Mail.ListMailboxes || ic.config.Matrix.VerifySession {
		return
	}
	go ic.storeMails()
	go MessageExtractionStage.Run()
	go ThreadSortingStage.Run()
	go MatrixNotificationStage.Run()
	for _, stage := range MatrixOverviewStages {
		go stage.Run()
	}
}

func (ic *InboxCollab) Stop(waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
}
