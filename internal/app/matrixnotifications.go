package app

import (
	"context"
	"time"

	log "github.com/sirupsen/logrus"
)

func (ic *InboxCollab) setupMatrixNotificationsStage() {
	setup := func(ctx context.Context) {
		ic.dbHandler.AddAllRooms(ctx)
		if !ic.Config.Matrix.VerifySession {
			ic.matrixHandler.WaitForRoomJoins()
		}

		go func(ctx context.Context) {
		roomnameupdateloop:
			for {
				for _, roomId := range ic.Config.Matrix.AllRooms() {
					ok, name := ic.matrixHandler.GetRoomName(roomId)
					if ok {
						ic.dbHandler.UpdateRoomName(ctx, roomId, name)
					}
				}
				select {
				case <-ctx.Done():
					break roomnameupdateloop
				case <-time.After(5 * time.Minute):
				}
			}
		}(ctx)
	}

	touchedRooms := []string{}

	work := func(ctx context.Context) bool {
		if ic.Config.Matrix.VerifySession {
			return true
		}
		if retry, ok := ctx.Value("retry").(bool); ok && !retry {
			touchedRooms = []string{}
		}

		// post new threads
		threads := ic.dbHandler.GetMatrixReadyThreads(ctx)
		for _, thread := range threads {
			ok, roomId, messageId := ic.matrixHandler.CreateThread(
				thread.Fetcher.String, thread.AddrFrom, thread.AddrTo,
				thread.NameFrom, thread.Subject, thread.MatrixRoomID.String,
			)
			if !ok {
				return false
			}
			ic.dbHandler.UpdateThreadMatrixIds(ctx, thread.ID, roomId, messageId)
			touchedRooms = append(touchedRooms, roomId)

			if v, ok := recreatedThreads.Load(thread.ID); ok {
				if head, ok := v.(*recreatedThreadHead); ok {
					if ic.matrixHandler.NotifyRecreation(head.roomId, head.threadId, roomId, messageId, head.intentional) {
						recreatedThreads.Delete(thread.ID)
					}
				} else {
					log.Errorf("Invalid type for recreated thread %v", v)
				}
			}
		}

		// add messages to threads
		mails := ic.dbHandler.GetMatrixReadyMails(ctx)
		for _, mail := range mails {
			ok, redacted, matrixId := ic.matrixHandler.AddReply(
				mail.RootMatrixRoomID.String, mail.RootMatrixID.String, mail.NameFrom,
				mail.Subject, mail.Timestamp.Time, mail.Attachments,
				*mail.Messages, mail.IsFirst,
			)
			if redacted && ic.dbHandler.RemoveMatrixMessageIdsOfThread(ctx, mail.Thread.Int64) {
				log.Infof("Thread head of mail %v has been redacted, queueing recreation...", mail.ID)
				recreatedThreads.Store(mail.Thread.Int64, &recreatedThreadHead{
					roomId: mail.RootMatrixRoomID.String, threadId: mail.RootMatrixID.String,
				})
			}
			if !ok {
				return false
			}
			ic.dbHandler.UpdateMailMatrixId(ctx, mail.ID, matrixId)
			touchedRooms = append(touchedRooms, mail.RootMatrixRoomID.String)
		}
		updateOverview := len(threads) > 0 || len(mails) > 0
		if updateOverview {
			ic.QueueMatrixOverviewUpdate(touchedRooms, false)
		}
		return true
	}
	MatrixNotificationStage = NewStage("MatrixNotification", setup, work, true)
}
