package app

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	log "github.com/sirupsen/logrus"

	cfg "github.com/arne314/inbox-collab/internal/config"
	"github.com/arne314/inbox-collab/internal/db"
	model "github.com/arne314/inbox-collab/internal/db/generated"
	"github.com/arne314/inbox-collab/internal/mail"
	"github.com/arne314/inbox-collab/internal/matrix"
)

var waitGroup *sync.WaitGroup = &sync.WaitGroup{}

type InboxCollab struct {
	config        *cfg.Config
	dbHandler     *db.DbHandler
	matrixHandler *matrix.MatrixHandler
	mailHandler   *mail.MailHandler
	llm           *LLM

	doneStoring        chan struct{}
	doneExtracting     chan struct{}
	extractionRequired atomic.Bool
	extractionDone     atomic.Bool
	sortingRequired    chan struct{}
	matrixRequired     chan struct{}
	fetchedMails       chan []*mail.Mail
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
	ic.doneStoring = make(chan struct{}, 1)
	ic.sortingRequired = make(chan struct{}, 1)
	ic.matrixRequired = make(chan struct{}, 100)
	ic.extractionRequired = atomic.Bool{}
	ic.extractionRequired.Store(true)
	ic.extractionDone = atomic.Bool{}
	ic.extractionDone.Store(true)

	waitGroup.Add(2)
	go mailHandler.Setup(config, waitGroup, ic.fetchedMails, FetcherStateStorageImpl{
		getState:  dbHandler.GetMailFetcherState,
		saveState: dbHandler.UpdateMailFetcherState,
	})
	go matrixHandler.Setup(config, waitGroup)
	waitGroup.Wait()
}

func (ic *InboxCollab) storeMails() {
	initial := true
	for chunk := range ic.fetchedMails {
		var modelled []*model.Mail
		for _, mail := range chunk {
			modelled = append(modelled, &model.Mail{
				Fetcher:          pgtype.Text{String: mail.Fetcher, Valid: true},
				HeaderID:         mail.MessageId,
				HeaderInReplyTo:  pgtype.Text{String: mail.InReplyTo, Valid: true},
				HeaderReferences: mail.References,
				Subject:          mail.Subject,
				Timestamp:        pgtype.Timestamp{Time: mail.Date, Valid: true},
				NameFrom:         pgtype.Text{String: mail.NameFrom, Valid: true},
				AddrFrom:         pgtype.Text{String: mail.AddrFrom, Valid: true},
				AddrTo:           mail.AddrTo,
				Body:             &pgtype.Text{String: mail.Text, Valid: true},
			})
		}
		if ic.dbHandler.AddMails(modelled) > 0 || initial {
			ic.extractionRequired.Store(true)
			ic.doneStoring <- struct{}{}
			initial = false
		}
	}
	log.Info("Added fetched messages to db")
}

func (ic *InboxCollab) performMessageExtraction(mail *model.Mail) {
	ic.llm.ExtractMessages(mail)
	if mail.Messages == nil || mail.Messages.Messages == nil {
		log.Errorf("Error extracting messages for mail %v", mail.ID)
	} else {
		ic.dbHandler.UpdateExtractedMessages(mail)
	}
}

func (ic *InboxCollab) extractMessages() {
	var wg sync.WaitGroup
	initial := true
	for range ic.doneStoring {
		if !ic.extractionRequired.Load() {
			continue // drains the channel
		}
		ic.extractionRequired.Store(false)
		ic.extractionDone.Store(false)
		mails := ic.dbHandler.GetMailsRequiringMessageExtraction()
		if len(mails) == 0 && !initial {
			continue
		}
		initial = false
		log.Infof("Extracting messages from %v mails...", len(mails))
		wg.Add(len(mails))
		for _, mail := range mails {
			go func() {
				defer wg.Done()
				ic.performMessageExtraction(mail)
			}()
		}
		ic.extractionDone.Store(true)
		ic.sortingRequired <- struct{}{}
		wg.Wait()
		log.Infof("Done extracting messages from %v mails", len(mails))
	}
}

func (ic *InboxCollab) sortMails() {
	var sortRequestTime time.Time
	var sortingRequested atomic.Bool
	go func() {
		for range ic.sortingRequired {
			if !sortingRequested.Swap(true) {
				sortRequestTime = time.Now()
			}
		}
	}()

	for true {
		timeSinceMailboxUpdate := time.Now().Sub(ic.mailHandler.GetLastMailboxUpdate()).Seconds()
		timeSinceSortRequest := time.Now().Sub(sortRequestTime).Seconds()
		waitForCompleteData := timeSinceMailboxUpdate < 10 && timeSinceSortRequest < 120 // timeout
		if !ic.extractionDone.Load() || !sortingRequested.Load() || waitForCompleteData {
			time.Sleep(1 * time.Second)
			continue
		}
		sortingRequested.Store(false)
		ic.dbHandler.AutoUpdateMailSorting()
		mails := ic.dbHandler.GetMailsRequiringSorting()
		if len(mails) == 0 {
			continue
		}
		log.Infof("Sorting %v mails...", len(mails))
		for _, mail := range mails {
			var threadId int64
			if mail.ReplyTo.Valid {
				if t := ic.dbHandler.GetMailById(mail.ReplyTo.Int64).Thread; t.Valid {
					threadId = t.Int64
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
				if regex.MatchString(mail.AddrFrom.String) {
					headAllowed = false
					break
				}
			}
			if headAllowed {
				ic.dbHandler.CreateThread(mail)
			} else {
				ic.dbHandler.MarkMailAsSorted(mail)
			}
		}
		ic.matrixRequired <- struct{}{}
		log.Infof("Done sorting %v mails", len(mails))
	}
}

func (ic *InboxCollab) notifyMatrix() {
	ic.matrixRequired <- struct{}{} // initial update
	for range ic.matrixRequired {
		threads := ic.dbHandler.GetMatrixReadyThreads()
		for _, thread := range threads {
			ok, roomId, messageId := ic.matrixHandler.CreateThread(
				thread.Fetcher.String, thread.AddrFrom.String, thread.AddrTo,
				thread.NameFrom.String, thread.Subject,
			)
			if ok {
				ic.dbHandler.UpdateThreadMatrixIds(thread.ID, roomId, messageId)
			} else {
				ic.matrixRequired <- struct{}{}
				break
			}
		}
		mails := ic.dbHandler.GetMatrixReadyMails()
		for _, mail := range mails {
			ok, matrixId := ic.matrixHandler.AddReply(
				mail.RootMatrixRoomID.String, mail.RootMatrixID.String, mail.NameFrom.String,
				mail.Subject, mail.Timestamp.Time, *mail.Messages.Messages[0].Content, mail.IsFirst,
			)
			if ok {
				ic.dbHandler.UpdateMailMatrixId(mail.ID, matrixId)
			} else {
				ic.matrixRequired <- struct{}{}
				break
			}
		}
	}
}

func (ic *InboxCollab) Run() {
	go ic.storeMails()
	go ic.extractMessages()
	go ic.sortMails()
	go ic.notifyMatrix()
}

func (ic *InboxCollab) Stop(waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
}
