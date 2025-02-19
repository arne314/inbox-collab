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
}

func (ic *InboxCollab) Run() {
	inboxes := make(chan []*mail.Mail, 10)
	ic.mailHandler.FetchMessages(inboxes)
	for mails := range inboxes {
		var modelled []*model.Mail
		for _, mail := range mails {
			modelled = append(modelled, &model.Mail{
				MailID:   pgtype.Text{String: mail.Id, Valid: true},
				Date:     pgtype.Timestamp{Time: mail.Date, Valid: true},
				AddrFrom: pgtype.Text{String: mail.AddrFrom, Valid: true},
				AddrTo:   pgtype.Text{String: mail.AddrTo, Valid: true},
				Body:     pgtype.Text{String: mail.Text, Valid: true},
			})
		}
		ic.dbHandler.AddMails(modelled)
	}
	log.Info("Added fetched messages to db")
}

func (ic *InboxCollab) Stop(waitGroup *sync.WaitGroup) {
	waitGroup.Done()
}
