package db

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	config "github.com/arne314/inbox-collab/internal/config"
	db "github.com/arne314/inbox-collab/internal/db/generated"
	log "github.com/sirupsen/logrus"
)

type DbHandler struct {
	Config  *config.Config
	ctx     context.Context
	pool    *pgxpool.Pool
	queries *db.Queries
}

func (dh *DbHandler) Setup() {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dh.Config.DatabaseUrl)
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

const dbTimeout = 200 * time.Second

func defaultContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, dbTimeout)
}

func (dh *DbHandler) AddMails(ctx context.Context, mails []*db.Mail) int {
	count := 0
	for _, mail := range mails {
		ctxAdd, cancelAdd := defaultContext(ctx)
		defer cancelAdd()
		inserted, err := dh.queries.AddMail(ctxAdd, db.AddMailParams{
			Fetcher:          mail.Fetcher,
			HeaderID:         mail.HeaderID,
			HeaderInReplyTo:  mail.HeaderInReplyTo,
			HeaderReferences: mail.HeaderReferences,
			Timestamp:        mail.Timestamp,
			Attachments:      mail.Attachments,
			NameFrom:         mail.NameFrom,
			AddrFrom:         mail.AddrFrom,
			AddrTo:           mail.AddrTo,
			Subject:          mail.Subject,
			Body:             mail.Body,
		})
		if err == nil {
			count += len(inserted)
		} else {
			log.Errorf("Error adding mail to db: %v", err)
		}
	}
	return count
}

func (dh *DbHandler) GetMailById(ctx context.Context, id int64) *db.GetMailRow {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	mail, err := dh.queries.GetMail(ctx, id)
	if err != nil {
		log.Errorf("Failed to fetch mail by id %v: %v", id, err)
		return nil
	}
	return mail
}

type getMailsQuery func(ctx context.Context) ([]*db.Mail, error)

func getMails(ctx context.Context, query getMailsQuery, mailTypeLogMsg string) []*db.Mail {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	mails, err := query(ctx)
	if err != nil {
		log.Errorf("Error fetching mails %v: %v", mailTypeLogMsg, err)
		return []*db.Mail{}
	}
	log.Infof("Loaded %v mails %v from db", len(mails), mailTypeLogMsg)
	return mails
}

func (dh *DbHandler) GetMailsRequiringMessageExtraction(ctx context.Context) []*db.Mail {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	return getMails(ctx, dh.queries.GetMailsRequiringMessageExtraction, "requiring message extraction")
}

func (dh *DbHandler) GetMailsRequiringSorting(ctx context.Context) []*db.Mail {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	return getMails(ctx, dh.queries.GetMailsRequiringSorting, "requiring sorting")
}

func (dh *DbHandler) UpdateExtractedMessages(ctx context.Context, mail *db.Mail) {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	err := dh.queries.UpdateExtractedMessages(
		ctx,
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

func (dh *DbHandler) AutoUpdateMailSorting(ctx context.Context) {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	count, err := dh.queries.AutoUpdateMailReplyTo(ctx)
	if err != nil {
		log.Errorf("Error auto updating reply_to columns: %v", err)
		return
	}
	log.Infof("Auto updated %v mail reply_to columns", count)
}

func (dh *DbHandler) GetReferencedThreadParent(ctx context.Context, mail *db.Mail) *db.GetReferencedThreadParentRow {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	rows, err := dh.queries.GetReferencedThreadParent(ctx, mail.HeaderReferences)
	if err != nil {
		log.Errorf("Error getting referenced thread parent for mail %v: %v", mail.ID, err)
		return nil
	}
	if len(rows) == 0 {
		return nil
	}
	return rows[0]
}

func (dh *DbHandler) CreateThread(ctx context.Context, mail *db.Mail) {
	ctx1, cancel1 := defaultContext(ctx)
	defer cancel1()
	thread, err := dh.queries.AddThread(
		ctx1,
		pgtype.Int8{Int64: mail.ID, Valid: true},
	)
	if err != nil {
		log.Errorf("Error creating new thread for mail %v: %v", mail.ID, err)
		return
	}
	ctx2, cancel2 := defaultContext(ctx)
	defer cancel2()
	err = dh.queries.UpdateMailSorting(ctx2, db.UpdateMailSortingParams{
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

func (dh *DbHandler) AddMailToThread(ctx context.Context, mail *db.Mail, threadId int64) {
	ctx1, cancel1 := defaultContext(ctx)
	defer cancel1()
	err := dh.queries.UpdateMailSorting(ctx1, db.UpdateMailSortingParams{
		ID:      mail.ID,
		Thread:  pgtype.Int8{Int64: threadId, Valid: true},
		ReplyTo: mail.ReplyTo,
	})
	if err != nil {
		log.Errorf("Error setting thread of mail %v to %v: %v", mail.ID, threadId, err)
		return
	}
	ctx2, cancel2 := defaultContext(ctx)
	defer cancel2()
	err = dh.queries.UpdateThreadLastMail(ctx2, db.UpdateThreadLastMailParams{
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

func (dh *DbHandler) MarkMailAsSorted(ctx context.Context, mail *db.Mail) {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	err := dh.queries.UpdateMailMarkSorted(ctx, mail.ID)
	if err != nil {
		log.Errorf("Error marking mail %v as sorted: %v", mail.ID, err)
	}
}

func (dh *DbHandler) GetMailFetcherState(ctx context.Context, id string) (uint32, uint32) {
	ctxGet, cancelGet := defaultContext(ctx)
	defer cancelGet()
	state, err := dh.queries.GetFetcherState(ctxGet, id)
	if err != nil {
		log.Errorf("Error getting mail fetcher state: %v", err)
	}
	if len(state) == 0 {
		ctxSet, cancelSet := defaultContext(ctx)
		defer cancelSet()
		err = dh.queries.AddFetcher(ctxSet, id)
		if err != nil {
			log.Errorf("Error creating mail fetcher state: %v", err)
		}
		ctxGet, cancelGet = defaultContext(ctx)
		defer cancelGet()
		return dh.GetMailFetcherState(ctxGet, id)
	}
	return uint32(state[0].UidLast), uint32(state[0].UidValidity)
}

func (dh *DbHandler) UpdateMailFetcherState(ctx context.Context, id string, uidLast uint32, uidValidity uint32) {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	err := dh.queries.UpdateFetcherState(ctx, db.UpdateFetcherStateParams{
		ID:          id,
		UidLast:     int32(uidLast),
		UidValidity: int32(uidValidity),
	})
	if err != nil {
		log.Errorf("Error updating mail fetcher state: %v", err)
	}
}

var emailRegex = regexp.MustCompile("^([^@]+)@.*$")

func displayName(name string, email string) string {
	if strings.TrimSpace(name) == "" {
		parsed := emailRegex.FindStringSubmatch(email)
		if len(parsed) >= 2 {
			return parsed[1]
		}
		return email
	}
	return name
}

func (dh *DbHandler) GetMatrixReadyThreads(ctx context.Context) []*db.GetMatrixReadyThreadsRow {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	threads, err := dh.queries.GetMatrixReadyThreads(ctx)
	if err != nil {
		log.Errorf("Error getting matrix ready threads from db: %v", err)
		return []*db.GetMatrixReadyThreadsRow{}
	}
	for _, thread := range threads {
		thread.NameFrom = displayName(thread.NameFrom, thread.AddrFrom)
	}
	log.Infof("Fetched %v threads ready to be associated with a matrix message", len(threads))
	return threads
}

func (dh *DbHandler) GetMatrixReadyMails(ctx context.Context) []*db.GetMatrixReadyMailsRow {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	mails, err := dh.queries.GetMatrixReadyMails(ctx)
	if err != nil {
		log.Errorf("Error getting matrix ready mails from db: %v", err)
		return []*db.GetMatrixReadyMailsRow{}
	}
	for _, mail := range mails {
		mail.NameFrom = displayName(mail.NameFrom, mail.AddrFrom)
	}
	log.Infof("Fetched %v mails ready to be associated with a matrix message", len(mails))
	return mails
}

func (dh *DbHandler) UpdateThreadMatrixIds(ctx context.Context, threadId int64, roomId string, messageId string) {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	err := dh.queries.UpdateThreadMatrixIds(ctx, db.UpdateThreadMatrixIdsParams{
		ID:           threadId,
		MatrixID:     pgtype.Text{String: messageId, Valid: true},
		MatrixRoomID: pgtype.Text{String: roomId, Valid: true},
	})
	if err != nil {
		log.Errorf("Error updating thread matrix id: %v", err)
	}
}

func (dh *DbHandler) UpdateMailMatrixId(ctx context.Context, mailId int64, matrixId string) {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	err := dh.queries.UpdateMailMatrixId(ctx, db.UpdateMailMatrixIdParams{
		ID:       mailId,
		MatrixID: pgtype.Text{String: matrixId, Valid: true},
	})
	if err != nil {
		log.Errorf("Error updating mail matrix id: %v", err)
	}
}

func (dh *DbHandler) UpdateThreadEnabled(ctx context.Context,
	roomId string, messageId string, enabled bool, forceClose bool,
) bool {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	params := db.UpdateThreadEnabledParams{
		Enabled:      enabled,
		MatrixID:     pgtype.Text{String: messageId, Valid: true},
		MatrixRoomID: pgtype.Text{String: roomId, Valid: true},

		// ignore in case of normal thread close
		ForceClose: pgtype.Bool{Bool: forceClose, Valid: enabled || forceClose},
	}

	count, err := dh.queries.UpdateThreadEnabled(ctx, params)
	if err != nil {
		log.Errorf(
			"Error enabled column of thread in room %v with message %v to %v: %v",
			roomId, messageId, enabled, err,
		)
		return false
	}
	return count == 1
}

func (dh *DbHandler) AddAllRooms(ctx context.Context) {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	for _, room := range dh.Config.Matrix.AllRooms() {
		err := dh.queries.AddRoom(ctx, room)
		if err != nil {
			log.Errorf("Error adding room to db: %v", err)
		}
	}
}

func (dh *DbHandler) GetRoom(ctx context.Context, overviewRoom string) *db.Room {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	room, err := dh.queries.GetRoom(ctx, overviewRoom)
	if err != nil {
		log.Errorf("Error fetching room: %v", err)
		return nil
	}
	return room
}

func (dh *DbHandler) UpdateRoomOverviewMessage(ctx context.Context, roomId string, messageId string) {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	err := dh.queries.UpdateRoomOverviewMessage(
		ctx,
		db.UpdateRoomOverviewMessageParams{
			ID:                roomId,
			OverviewMessageID: pgtype.Text{String: messageId, Valid: true},
		},
	)
	if err != nil {
		log.Errorf("Error updating room overview message id: %v", err)
	}
}

func (dh *DbHandler) GetOverviewThreads(ctx context.Context,
	overviewRoom string,
) (messageId string, authors []string, subjects []string, rooms []string, threadMsgs []string) {
	// load room
	ctxRoom, cancelRoom := defaultContext(ctx)
	defer cancelRoom()
	room, err := dh.queries.GetRoom(ctxRoom, overviewRoom)
	if err != nil {
		log.Errorf("Error reading overview room %v from db: %v", overviewRoom, err)
		return "", []string{}, []string{}, []string{}, []string{}
	}
	messageId = room.OverviewMessageID.String

	// load threads
	targets := dh.Config.Matrix.GetOverviewRoomTargets(overviewRoom)
	ctxThreads, cancelThreads := defaultContext(ctx)
	defer cancelThreads()
	threads, err := dh.queries.GetOverviewThreads(ctxThreads, targets)
	if err != nil {
		log.Errorf("Error reading overview room %v from db: %v", overviewRoom, err)
		return "", []string{}, []string{}, []string{}, []string{}
	}
	log.Infof("Fetched %v threads for overview room %v from db", len(threads), overviewRoom)
	authors = make([]string, len(threads))
	subjects = make([]string, len(threads))
	rooms = make([]string, len(threads))
	threadMsgs = make([]string, len(threads))
	for i, thread := range threads {
		authors[i] = displayName(thread.NameFrom, thread.AddrFrom)
		subjects[i] = thread.Subject
		rooms[i] = thread.MatrixRoomID.String
		threadMsgs[i] = thread.MessageID.String
	}
	return
}

func (dh *DbHandler) OverviewMessageUpdated(ctx context.Context, roomId string, messageId string) {
	ctx, cancel := defaultContext(ctx)
	defer cancel()
	err := dh.queries.UpdateRoomOverviewMessage(
		ctx,
		db.UpdateRoomOverviewMessageParams{
			ID:                roomId,
			OverviewMessageID: pgtype.Text{String: messageId, Valid: true},
		},
	)
	if err != nil {
		log.Errorf("Error updating overview message: %v", err)
	}
}

func (dh *DbHandler) Stop() {
	dh.pool.Close()
	log.Info("Closed db connection")
}
