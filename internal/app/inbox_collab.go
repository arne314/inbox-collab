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
	sortingRequired    atomic.Bool
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
	ic.extractionRequired = atomic.Bool{}
	ic.extractionRequired.Store(true)
	ic.extractionDone = atomic.Bool{}
	ic.extractionDone.Store(true)
	ic.sortingRequired = atomic.Bool{}
	ic.sortingRequired.Store(true)

	waitGroup.Add(2)
	go mailHandler.Setup(config, waitGroup, ic.fetchedMails, FetcherStateStorageImpl{
		getState:  dbHandler.GetMailFetcherState,
		saveState: dbHandler.UpdateMailFetcherState,
	})
	go matrixHandler.Setup(config, waitGroup)
	waitGroup.Wait()
}

func (ic *InboxCollab) storeMails() {
	for chunk := range ic.fetchedMails {
		var modelled []*model.Mail
		for _, mail := range chunk {
			modelled = append(modelled, &model.Mail{
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
		ic.dbHandler.AddMails(modelled)
		ic.doneStoring <- struct{}{}
		ic.extractionRequired.Store(true)
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
	for range ic.doneStoring {
		if !ic.extractionRequired.Load() {
			continue // drains the channel
		}
		ic.extractionRequired.Store(false)
		ic.extractionDone.Store(false)
		mails := ic.dbHandler.GetMailsRequiringMessageExtraction()
		if len(mails) == 0 {
			continue
		}
		log.Infof("Extracting messages from %v mails...", len(mails))
		wg.Add(len(mails))
		for _, mail := range mails {
			go func() {
				defer wg.Done()
				ic.performMessageExtraction(mail)
			}()
		}
		ic.extractionDone.Store(true)
		ic.sortingRequired.Store(true)
		wg.Wait()
		log.Infof("Done extracting messages from %v mails", len(mails))
	}
}

func (ic *InboxCollab) sortMails() {
	var lastSort time.Time
	for true {
		timeSinceMailboxUpdate := time.Now().Sub(ic.mailHandler.GetLastMailboxUpdate()).Seconds()
		timeSinceLastSort := time.Now().Sub(lastSort).Seconds()
		waitForCompleteData := timeSinceMailboxUpdate < 10 && timeSinceLastSort < 120 // timeout
		if !ic.extractionDone.Load() || !ic.sortingRequired.Load() || waitForCompleteData {
			time.Sleep(1 * time.Second)
			continue
		}
		ic.sortingRequired.Store(false)
		ic.dbHandler.AutoUpdateMailReplyTo()
		lastSort = time.Now()
		mails := ic.dbHandler.GetMailsRequiringSorting()
		if len(mails) == 0 {
			continue
		}
		log.Infof("Sorting %v mails...", len(mails))
		for _, mail := range mails {
			var threadParent *model.Mail
			if mail.ReplyTo.Valid {
				threadParent = ic.dbHandler.GetMailById(mail.ReplyTo.Int64)
			}
			if threadParent == nil {
				threadParent = ic.dbHandler.GetReferencedThreadParent(mail)
			}
			if threadParent != nil && threadParent.Thread.Valid {
				ic.dbHandler.AddMailToThread(mail, threadParent.Thread.Int64)
				continue
			}
			ic.dbHandler.CreateThread(mail)
		}
		log.Infof("Done sorting %v mails", len(mails))
	}
}

func (ic *InboxCollab) Run() {
	go ic.storeMails()
	go ic.extractMessages()
	go ic.sortMails()
}

func (ic *InboxCollab) Stop(waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
}
