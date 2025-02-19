package db

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5"

	config "github.com/arne314/inbox-collab/internal/config"
	db "github.com/arne314/inbox-collab/internal/db/generated"
	log "github.com/sirupsen/logrus"
)

type DbHandler struct {
	connection *pgx.Conn
	queries    *db.Queries
}

func (dh *DbHandler) Setup(cfg *config.Config) {
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, cfg.DatabaseUrl)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
		return
	}
	dh.connection = conn
	dh.queries = db.New(conn)
	mailCount, err := dh.queries.MailCount(ctx)
	if err != nil {
		log.Errorf("Error counting mails: %v", err)
		mailCount = -1
	}
	log.Infof(
		"Connected to database on %v with %v mails",
		dh.connection.Config().Host,
		mailCount,
	)
}

func (dh *DbHandler) AddMails(mails []*db.Mail) {
	for _, mail := range mails {
		err := dh.queries.AddMail(context.Background(), db.AddMailParams{
			MailID:   mail.MailID,
			Date:     mail.Date,
			AddrFrom: mail.AddrFrom,
			AddrTo:   mail.AddrTo,
			Body:     mail.Body,
		})
		if err != nil {
			log.Errorf("Error adding mail to db: %v", err)
			return
		}
	}
}

func (dh *DbHandler) Stop(waitGroup *sync.WaitGroup) {
	err := dh.connection.Close(context.Background())
	if err != nil {
		log.Errorf("Failed to close database connection: %v", err)
	}
	waitGroup.Done()
}
