package db

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	config "github.com/arne314/inbox-collab/internal/config"
	db "github.com/arne314/inbox-collab/internal/db/generated"
	log "github.com/sirupsen/logrus"
)

type DbHandler struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

func (dh *DbHandler) Setup(cfg *config.Config) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseUrl)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
		return
	}
	dh.pool = pool
	dh.queries = db.New(pool)
	mailCount, err := dh.queries.MailCount(ctx)
	if err != nil {
		log.Errorf("Error counting mails: %v", err)
		mailCount = -1
	}
	log.Infof(
		"Connected to database on %v with %v mails",
		dh.pool.Config().ConnConfig.Host,
		mailCount,
	)
}

func (dh *DbHandler) AddMails(mails []*db.Mail) int {
	count := 0
	for _, mail := range mails {
		inserted, err := dh.queries.AddMail(context.Background(), db.AddMailParams{
			HeaderID:         mail.HeaderID,
			HeaderInReplyTo:  mail.HeaderInReplyTo,
			HeaderReferences: mail.HeaderReferences,
			Timestamp:        mail.Timestamp,
			NameFrom:         mail.NameFrom,
			AddrFrom:         mail.AddrFrom,
			AddrTo:           mail.AddrTo,
			Subject:          mail.Subject,
			Body:             mail.Body,
		})
		if len(inserted) != 0 {
			count++
		}
		if err != nil {
			log.Errorf("Error adding mail to db: %v", err)
			break
		}
	}
	return count
}

func (dh *DbHandler) GetMailById(id int64) *db.Mail {
	mail, err := dh.queries.GetMail(context.Background(), id)
	if err != nil {
		log.Errorf("Failed to fetch mail by id %v: %v", id, err)
		return nil
	}
	return mail
}

type getMailsQuery func(ctx context.Context) ([]*db.Mail, error)

func getMails(query getMailsQuery, mailTypeLogMsg string) []*db.Mail {
	mails, err := query(context.Background())
	if err != nil {
		log.Errorf("Error fetching mails %v: %v", mailTypeLogMsg, err)
		return []*db.Mail{}
	}
	log.Infof("Loaded %v mails %v from db", len(mails), mailTypeLogMsg)
	return mails
}

func (dh *DbHandler) GetMailsRequiringMessageExtraction() []*db.Mail {
	return getMails(dh.queries.GetMailsRequiringMessageExtraction, "requiring message extraction")
}

func (dh *DbHandler) GetMailsRequiringSorting() []*db.Mail {
	return getMails(dh.queries.GetMailsRequiringSorting, "requiring sorting")
}

func (dh *DbHandler) UpdateExtractedMessages(mail *db.Mail) {
	err := dh.queries.UpdateExtractedMessages(
		context.Background(),
		db.UpdateExtractedMessagesParams{
			ID:       mail.ID,
			Messages: mail.Messages,
		},
	)
	if err != nil {
		log.Errorf("Error updating extracted messages: %v", err)
		return
	}
	log.Infof("Updated extracted messages of mail %v", mail.ID)
}

func (dh *DbHandler) AutoUpdateMailReplyTo() {
	count, err := dh.queries.AutoUpdateMailReplyTo(context.Background())
	if err != nil {
		log.Errorf("Error auto updating reply_to columns: %v", err)
		return
	}
	log.Infof("Auto updated %v mail reply_to columns", count)
}

func (dh *DbHandler) GetReferencedThreadParent(mail *db.Mail) *db.Mail {
	rows, err := dh.queries.GetReferencedThreadParent(context.Background(), mail.HeaderReferences)
	if err != nil {
		log.Errorf("Error getting referenced thread parent for mail %v: %v", mail.ID, err)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

func (dh *DbHandler) CreateThread(mail *db.Mail) {
	thread, err := dh.queries.AddThread(
		context.Background(),
		pgtype.Int8{Int64: mail.ID, Valid: true},
	)
	if err != nil {
		log.Errorf("Error creating new thread for mail %v: %v", mail.ID, err)
		return
	}
	err = dh.queries.UpdateMailSorting(context.Background(), db.UpdateMailSortingParams{
		ID:      mail.ID,
		Thread:  pgtype.Int8{Int64: thread.ID, Valid: true},
		ReplyTo: mail.ReplyTo,
	})
	if err != nil {
		log.Errorf("Error setting thread of mail %v to %v: %v", mail.ID, thread.ID, err)
		return
	}
	log.Infof("Created new thread with mail %v", mail.ID)
}

func (dh *DbHandler) AddMailToThread(mail *db.Mail, threadId int64) {
	err := dh.queries.UpdateMailSorting(context.Background(), db.UpdateMailSortingParams{
		ID:      mail.ID,
		Thread:  pgtype.Int8{Int64: threadId, Valid: true},
		ReplyTo: mail.ReplyTo,
	})
	if err != nil {
		log.Errorf("Error setting thread of mail %v to %v: %v", mail.ID, threadId, err)
		return
	}
	err = dh.queries.UpdateThreadLastMail(context.Background(), db.UpdateThreadLastMailParams{
		ID:          threadId,
		LastMail:    pgtype.Int8{Int64: mail.ID, Valid: true},
		LastMessage: pgtype.Timestamp{Time: mail.Timestamp.Time, Valid: true},
	})
	if err != nil {
		log.Errorf("Error setting last mail of thread %v to %v: %v", threadId, mail.ID, err)
		return
	}
	log.Infof("Added mail %v to thread %v", mail.ID, threadId)
}

func (dh *DbHandler) GetMailFetcherState(id string) (uint32, uint32) {
	ctx := context.Background()
	state, err := dh.queries.GetFetcherState(ctx, id)
	if err != nil {
		log.Panicf("Error getting mail fetcher state: %v", err)
	}
	if len(state) == 0 {
		err = dh.queries.AddFetcher(ctx, id)
		if err != nil {
			log.Panicf("Error creating mail fetcher state: %v", err)
		}
		return dh.GetMailFetcherState(id)
	}
	return uint32(state[0].UidLast), uint32(state[0].UidValidity)
}

func (dh *DbHandler) UpdateMailFetcherState(id string, uidLast uint32, uidValidity uint32) {
	err := dh.queries.UpdateFetcherState(context.Background(), db.UpdateFetcherStateParams{
		ID:          id,
		UidLast:     int32(uidLast),
		UidValidity: int32(uidValidity),
	})
	if err != nil {
		log.Panicf("Error updating mail fetcher state: %v", err)
	}
}

func (dh *DbHandler) Stop(waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
	dh.pool.Close()
}
