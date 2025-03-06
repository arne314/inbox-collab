package app

import (
	"sync"

	"github.com/jackc/pgx/v5/pgtype"
	log "github.com/sirupsen/logrus"

	cfg "github.com/arne314/inbox-collab/internal/config"
	"github.com/arne314/inbox-collab/internal/db"
	model "github.com/arne314/inbox-collab/internal/db/generated"
	"github.com/arne314/inbox-collab/internal/mail"
	"github.com/arne314/inbox-collab/internal/matrix"
)

type InboxCollab struct {
	config        *cfg.Config
	dbHandler     *db.DbHandler
	matrixHandler *matrix.MatrixHandler
	mailHandler   *mail.MailHandler
	llm           *LLM
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
}

func (ic *InboxCollab) fetchMessages() {
	inboxes := make(chan []*mail.Mail, 10)
	ic.mailHandler.FetchMessages(inboxes)
	for mails := range inboxes {
		var modelled []*model.Mail
		for _, mail := range mails {
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
	}
	log.Info("Added fetched messages to db")
}

func (ic *InboxCollab) extractMessages(mail *model.Mail) {
	ic.llm.ExtractMessages(mail)
	if mail.Messages == nil || mail.Messages.Messages == nil {
		log.Errorf("Error extracting messages for mail %v", mail.ID)
	} else {
		ic.dbHandler.UpdateExtractedMessages(mail)
	}
}

func (ic *InboxCollab) extractFetchedMessages() {
	var wg sync.WaitGroup
	mails := ic.dbHandler.GetMailsRequiringMessageExtraction()
	if len(mails) == 0 {
		return
	}
	log.Infof("Extracting messages from %v mails...", len(mails))
	wg.Add(len(mails))
	for _, mail := range mails {
		go func() {
			ic.extractMessages(mail)
			wg.Done()
		}()
	}
	wg.Wait()
	log.Infof("Done extracting messages from %v mails", len(mails))
}

func (ic *InboxCollab) processExtractedMessages() {
	ic.dbHandler.AutoUpdateMailReplyTo()
	mails := ic.dbHandler.GetMailsRequiringSorting()
	if len(mails) == 0 {
		return
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

func (ic *InboxCollab) Run() {
	ic.fetchMessages()
	ic.extractFetchedMessages()
	ic.processExtractedMessages()
}

func (ic *InboxCollab) Stop(waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
}
