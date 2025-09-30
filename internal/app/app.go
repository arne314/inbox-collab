package app

import (
	"context"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	log "github.com/sirupsen/logrus"

	cfg "github.com/arne314/inbox-collab/internal/config"
	"github.com/arne314/inbox-collab/internal/db"
	model "github.com/arne314/inbox-collab/internal/db/generated"
	"github.com/arne314/inbox-collab/internal/mail"
	"github.com/arne314/inbox-collab/internal/matrix"
	textprocessor "github.com/arne314/inbox-collab/internal/textprocessor"
)

var (
	MessageExtractionStage  *PipelineStage
	ThreadSortingStage      *PipelineStage
	MatrixNotificationStage *PipelineStage
	MatrixOverviewStages    map[string]*PipelineStage
	recreatedThreads        sync.Map
)

type InboxCollab struct {
	Config           *cfg.Config
	dbHandler        *db.DbHandler
	matrixHandler    *matrix.MatrixHandler
	mailHandler      *mail.MailHandler
	messageExtractor *textprocessor.MessageExtractor

	fetchedMails chan []*mail.Mail
}

type recreatedThreadHead struct {
	roomId      string
	threadId    string
	intentional bool
}

type FetcherStateStorageImpl struct {
	getState  func(ctx context.Context, id string) (uint32, uint32)
	saveState func(ctx context.Context, id string, uidLast uint32, uidValidity uint32)
}

func (f FetcherStateStorageImpl) GetState(ctx context.Context, id string) (uint32, uint32) {
	return f.getState(ctx, id)
}

func (f FetcherStateStorageImpl) SaveState(ctx context.Context, id string, uidLast uint32, uidValidity uint32) {
	f.saveState(ctx, id, uidLast, uidValidity)
}

func (ic *InboxCollab) Setup(
	dbHandler *db.DbHandler,
	mailHandler *mail.MailHandler,
	matrixHandler *matrix.MatrixHandler,
) {
	ic.dbHandler = dbHandler
	ic.mailHandler = mailHandler
	ic.matrixHandler = matrixHandler
	ic.messageExtractor = textprocessor.NewMessageExtractor(ic.Config.LLM)
	ic.fetchedMails = make(chan []*mail.Mail, 100)

	waitGroup := &sync.WaitGroup{}
	if !ic.Config.Matrix.VerifySession {
		waitGroup.Add(1)
		go mailHandler.Setup(waitGroup, ic.fetchedMails, FetcherStateStorageImpl{
			getState:  dbHandler.GetMailFetcherState,
			saveState: dbHandler.UpdateMailFetcherState,
		})
	}
	waitGroup.Add(1)
	go matrixHandler.Setup(ic, waitGroup)
	waitGroup.Wait()
	ic.setupMessageExtractionStage()
	ic.setupThreadSortingStage()
	ic.setupMatrixNotificationsStage()
	ic.setupMatrixOverviewStage()
}

func (ic *InboxCollab) storeMails(waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
	ctx := context.Background()
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
		nFetched := ic.dbHandler.AddMails(ctx, modelled)
		if nFetched > 0 || initial {
			log.Infof("Added %v new messages to db", nFetched)
			ThreadSortingStage.QueueWork()
			initial = false
		}
	}
}

func (ic *InboxCollab) OpenThread(ctx context.Context, roomId string, threadId string) bool {
	ok := ic.dbHandler.UpdateThreadEnabled(ctx, roomId, threadId, true, false)
	if ok {
		ic.QueueMatrixOverviewUpdate([]string{roomId}, true)
	}
	return ok
}

func (ic *InboxCollab) CloseThread(ctx context.Context, roomId string, threadId string) bool {
	ok := ic.dbHandler.UpdateThreadEnabled(
		ctx, roomId, threadId, false,
		false, // force close boolean is ignored internally
	)
	if ok {
		ic.QueueMatrixOverviewUpdate([]string{roomId}, true)
	}
	return ok
}

func (ic *InboxCollab) ForceCloseThread(ctx context.Context, roomId string, threadId string) bool {
	ok := ic.dbHandler.UpdateThreadEnabled(ctx, roomId, threadId, false, true)
	if ok {
		ic.QueueMatrixOverviewUpdate([]string{roomId}, true)
	}
	return ok
}

func (ic *InboxCollab) MoveThread(ctx context.Context, roomId string, threadId string, query string) bool {
	var targetRoom string
	query = strings.ToLower(query)
	roomIds := ic.Config.Matrix.AllTargetRooms()
	for _, r := range ic.dbHandler.GetRooms(ctx, roomIds) {
		if strings.Contains(strings.ToLower(r.Name.String), query) {
			if targetRoom != "" { // allow excactly one match
				return false
			}
			targetRoom = r.ID
		}
	}
	if targetRoom == "" || targetRoom == roomId {
		return false
	}
	thread := ic.dbHandler.GetThreadByMatrixId(ctx, threadId)
	if thread == nil {
		return false
	}
	ok := ic.dbHandler.RemoveMatrixMessageIdsOfThread(ctx, thread.ID)
	ok = ic.dbHandler.UpdateThreadMatrixIds(ctx, thread.ID, targetRoom, "") || ok
	if !ok {
		return false
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		ic.QueueMatrixOverviewUpdate([]string{roomId}, true)
		wg.Done()
	}()
	recreatedThreads.Store(thread.ID, &recreatedThreadHead{ // to link new thread once created
		roomId:      roomId,
		threadId:    threadId,
		intentional: true,
	})
	go func() {
		MatrixNotificationStage.QueueWorkBlocking()
		time.Sleep(2 * time.Second)
		wg.Done()
	}()
	wg.Wait()
	return true
}

func (ic *InboxCollab) ResendThreadOverview(ctx context.Context, roomId string) bool {
	ok := false
	if !slices.Contains(ic.Config.Matrix.AllOverviewRooms(), roomId) {
		return false
	}
	room := ic.dbHandler.GetRoom(ctx, roomId)
	if room != nil {
		ok = ic.matrixHandler.RemoveThreadOverview(roomId, room.OverviewMessageID.String)
		if ok {
			MatrixOverviewStages[roomId].QueueWorkBlocking()
		}
	}
	return ok
}

func (ic *InboxCollab) ResendThreadOverviewAll(ctx context.Context) bool {
	ok := true
	roomIds := ic.Config.Matrix.AllOverviewRooms()
	for _, room := range ic.dbHandler.GetRooms(ctx, roomIds) {
		if !ic.matrixHandler.RemoveThreadOverview(room.ID, room.OverviewMessageID.String) {
			ok = false
		}
	}
	var wg sync.WaitGroup
	wg.Add(len(roomIds))
	for _, roomId := range roomIds {
		go func(roomId string) {
			MatrixOverviewStages[roomId].QueueWorkBlocking()
			wg.Done()
		}(roomId)
	}
	wg.Wait()
	return ok
}

func (ic *InboxCollab) Run(waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
	if ic.Config.Mail.ListMailboxes || ic.Config.Matrix.VerifySession {
		return
	}
	wg := &sync.WaitGroup{}
	wg.Add(1)
	go ic.storeMails(wg)
	wg.Add(3)
	go MessageExtractionStage.Run(wg)
	go ThreadSortingStage.Run(wg)
	go MatrixNotificationStage.Run(wg)
	wg.Add(len(MatrixOverviewStages))
	for _, stage := range MatrixOverviewStages {
		go stage.Run(wg)
	}
	wg.Wait()
}

func (ic *InboxCollab) Stop() {
	ThreadSortingStage.Stop()
	MessageExtractionStage.ForceStop()
	MatrixNotificationStage.ForceStop()
	for _, stage := range MatrixOverviewStages {
		stage.ForceStop()
	}
	close(ic.fetchedMails)
}
