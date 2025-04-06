package app

import (
	"sync"
	"time"

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
	MessageExtractionStage  *Stage
	ThreadSortingStage      *Stage
	MatrixNotificationStage *Stage
	MatrixOverviewStage     *Stage
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
	waitGroup.Add(2)
	go mailHandler.Setup(config.Mail, waitGroup, ic.fetchedMails, FetcherStateStorageImpl{
		getState:  dbHandler.GetMailFetcherState,
		saveState: dbHandler.UpdateMailFetcherState,
	})
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

func (ic *InboxCollab) performMessageExtraction(mail *model.Mail) {
	ic.llm.ExtractMessages(mail)
	if mail.Messages == nil || mail.Messages.Messages == nil {
		log.Errorf("Error extracting messages for mail %v", mail.ID)
	} else {
		ic.dbHandler.UpdateExtractedMessages(mail)
	}
}

func (ic *InboxCollab) setupMessageExtractionStage() {
	var wg sync.WaitGroup
	work := func() bool {
		mails := ic.dbHandler.GetMailsRequiringMessageExtraction()
		if len(mails) == 0 {
			if MessageExtractionStage.IsFirstWork {
				ThreadSortingStage.QueueWork()
			}
			return true
		}
		log.Infof("Extracting messages from %v mails...", len(mails))
		wg.Add(len(mails))
		for _, mail := range mails {
			go func(m *model.Mail) {
				defer wg.Done()
				ic.performMessageExtraction(m)
			}(mail)
		}
		wg.Wait()
		log.Infof("Done extracting messages from %v mails", len(mails))
		ThreadSortingStage.QueueWork()
		return true
	}
	MessageExtractionStage = NewStage(
		"MessageExtraction", nil, work,
		false, // initial queueing happens in storeMails()
	)
}

func (ic *InboxCollab) setupThreadSortingStage() {
	work := func() bool {
		timeSinceMailboxUpdate := time.Now().Sub(ic.mailHandler.GetLastMailboxUpdate()).Seconds()
		timeSinceSortRequest := ThreadSortingStage.TimeSinceQueued().Seconds()
		waitForCompleteData := timeSinceMailboxUpdate < 10 && timeSinceSortRequest < 120 // timeout
		if MessageExtractionStage.Working() || waitForCompleteData {
			log.Infof("Waiting for complete data to sort threads...")
			time.Sleep(2 * time.Second)
			return false
		}

		ic.dbHandler.AutoUpdateMailSorting()
		mails := ic.dbHandler.GetMailsRequiringSorting()
		if len(mails) == 0 {
			return true
		}
		log.Infof("Sorting %v mails...", len(mails))
		for _, mail := range mails {
			var threadId int64
			if mail.ReplyTo.Valid {
				if m := ic.dbHandler.GetMailById(mail.ReplyTo.Int64); m.Thread.Valid &&
					!m.ForceClose.Bool {
					threadId = m.Thread.Int64
				}
			}
			if threadId == 0 {
				if m := ic.dbHandler.GetReferencedThreadParent(mail); m != nil {
					if t := m.Thread; t.Valid {
						threadId = t.Int64
					}
				}
			}
			if threadId != 0 {
				ic.dbHandler.AddMailToThread(mail, threadId)
				continue
			}
			headAllowed := true
			for _, regex := range ic.config.Matrix.HeadBlacklistRegex {
				if regex.MatchString(mail.AddrFrom) {
					headAllowed = false
					log.Infof("Ignoring mail as thread head from %v", mail.AddrFrom)
					break
				}
			}
			if headAllowed {
				ic.dbHandler.CreateThread(mail)
			} else {
				ic.dbHandler.MarkMailAsSorted(mail)
			}
		}
		log.Infof("Done sorting %v mails", len(mails))
		MatrixNotificationStage.QueueWork()
		return true
	}
	ThreadSortingStage = NewStage(
		"ThreadSorting", nil, work,
		false, // initial queueing happens in message extraction stage
	)
}

func (ic *InboxCollab) OpenThread(roomId string, threadId string) bool {
	ok := ic.dbHandler.UpdateThreadEnabled(roomId, threadId, true, false)
	if ok {
		MatrixNotificationStage.QueueWork()
	}
	return ok
}

func (ic *InboxCollab) CloseThread(roomId string, threadId string) bool {
	// force close boolean is ignored internally
	ok := ic.dbHandler.UpdateThreadEnabled(roomId, threadId, false, false)
	if ok {
		MatrixNotificationStage.QueueWork()
	}
	return ok
}

func (ic *InboxCollab) ForceCloseThread(roomId string, threadId string) bool {
	ok := ic.dbHandler.UpdateThreadEnabled(roomId, threadId, false, true)
	if ok {
		MatrixNotificationStage.QueueWork()
	}
	return ok
}

func (ic *InboxCollab) setupMatrixNotificationsStage() {
	setup := func() {
		ic.dbHandler.AddAllRooms()
		if !ic.config.Matrix.VerifySession {
			ic.matrixHandler.WaitForRoomJoins()
		}
	}
	work := func() bool {
		if ic.config.Matrix.VerifySession {
			return true
		}
		// post new threads
		threads := ic.dbHandler.GetMatrixReadyThreads()
		for _, thread := range threads {
			ok, roomId, messageId := ic.matrixHandler.CreateThread(
				thread.Fetcher.String, thread.AddrFrom, thread.AddrTo,
				thread.NameFrom, thread.Subject,
			)
			if ok {
				ic.dbHandler.UpdateThreadMatrixIds(thread.ID, roomId, messageId)
			} else {
				return false
			}
		}
		// add messages to threads
		mails := ic.dbHandler.GetMatrixReadyMails()
		for _, mail := range mails {
			ok, matrixId := ic.matrixHandler.AddReply(
				mail.RootMatrixRoomID.String, mail.RootMatrixID.String, mail.NameFrom,
				mail.Subject, mail.Timestamp.Time, mail.Attachments,
				*mail.Messages.Messages[0].Content, mail.IsFirst,
			)
			if ok {
				ic.dbHandler.UpdateMailMatrixId(mail.ID, matrixId)
			} else {
				return false
			}
		}
		updateOverview := len(threads) > 0 || len(mails) > 0
		if updateOverview {
			MatrixOverviewStage.QueueWork()
		}
		return true
	}
	MatrixNotificationStage = NewStage("MatrixNotification", setup, work, true)
}

func (ic *InboxCollab) setupMatrixOverviewStage() {
	work := func() bool {
		if ic.config.Matrix.VerifySession {
			return true
		}
		for overviewRoom := range ic.config.Matrix.RoomsOverview {
			messageId, authors, subjects, rooms, threadMsgs := ic.dbHandler.GetOverviewThreads(
				overviewRoom,
			)
			ok, messageId := ic.matrixHandler.UpdateThreadOverview(
				overviewRoom, messageId, authors, subjects, rooms, threadMsgs,
			)
			if ok {
				ic.dbHandler.OverviewMessageUpdated(overviewRoom, messageId)
			} else {
				return false
			}
		}
		return true
	}
	MatrixOverviewStage = NewStage("MatrixOverview", nil, work, true)
}

func (ic *InboxCollab) Run() {
	go ic.storeMails()
	go MessageExtractionStage.Run()
	go ThreadSortingStage.Run()
	go MatrixNotificationStage.Run()
	go MatrixOverviewStage.Run()
}

func (ic *InboxCollab) Stop(waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
}
