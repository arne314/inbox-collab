package app

import (
	"context"
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	model "github.com/arne314/inbox-collab/internal/db/generated"
)

func (ic *InboxCollab) performMessageExtraction(ctx context.Context, mail *model.Mail) {
	ic.llm.ExtractMessages(ctx, mail)
	if mail.Messages == nil || mail.Messages.Messages == nil {
		log.Errorf("Error extracting messages for mail %v", mail.ID)
	} else {
		ic.dbHandler.UpdateExtractedMessages(ctx, mail)
	}
}

func (ic *InboxCollab) setupMessageExtractionStage() {
	var wg sync.WaitGroup
	work := func(ctx context.Context) bool {
		mails := ic.dbHandler.GetMailsRequiringMessageExtraction(ctx)
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
				ic.performMessageExtraction(ctx, m)
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
	work := func(ctx context.Context) bool {
		timeSinceMailboxUpdate := time.Since(ic.mailHandler.GetLastMailboxUpdate()).Seconds()
		timeSinceSortRequest := ThreadSortingStage.TimeSinceQueued().Seconds()
		waitForCompleteData := timeSinceMailboxUpdate < 10 && timeSinceSortRequest < 120 // timeout
		if MessageExtractionStage.Working() || waitForCompleteData {
			log.Infof("Waiting for complete data to sort threads...")
			time.Sleep(2 * time.Second)
			return false
		}

		ic.dbHandler.AutoUpdateMailSorting(ctx)
		mails := ic.dbHandler.GetMailsRequiringSorting(ctx)
		if len(mails) == 0 {
			return true
		}
		log.Infof("Sorting %v mails...", len(mails))
		for _, mail := range mails {
			var threadId int64
			if mail.ReplyTo.Valid {
				if m := ic.dbHandler.GetMailById(ctx, mail.ReplyTo.Int64); m.Thread.Valid &&
					!m.ForceClose.Bool {
					threadId = m.Thread.Int64
				}
			}
			if threadId == 0 {
				if m := ic.dbHandler.GetReferencedThreadParent(ctx, mail); m != nil {
					if t := m.Thread; t.Valid {
						threadId = t.Int64
					}
				}
			}
			if threadId != 0 {
				ic.dbHandler.AddMailToThread(ctx, mail, threadId)
				continue
			}
			headAllowed := true
			for _, regex := range ic.Config.Matrix.HeadBlacklistRegex {
				if regex.MatchString(mail.AddrFrom) {
					headAllowed = false
					log.Infof("Ignoring mail as thread head from %v", mail.AddrFrom)
					break
				}
			}
			if headAllowed {
				ic.dbHandler.CreateThread(ctx, mail)
			} else {
				ic.dbHandler.MarkMailAsSorted(ctx, mail)
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

func (ic *InboxCollab) setupMatrixNotificationsStage() {
	setup := func(ctx context.Context) {
		ic.dbHandler.AddAllRooms(ctx)
		if !ic.Config.Matrix.VerifySession {
			ic.matrixHandler.WaitForRoomJoins()
		}
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
				thread.NameFrom, thread.Subject,
			)
			if ok {
				ic.dbHandler.UpdateThreadMatrixIds(ctx, thread.ID, roomId, messageId)
				touchedRooms = append(touchedRooms, roomId)
			} else {
				return false
			}
		}
		// add messages to threads
		mails := ic.dbHandler.GetMatrixReadyMails(ctx)
		for _, mail := range mails {
			ok, matrixId := ic.matrixHandler.AddReply(
				mail.RootMatrixRoomID.String, mail.RootMatrixID.String, mail.NameFrom,
				mail.Subject, mail.Timestamp.Time, mail.Attachments,
				*mail.Messages, mail.IsFirst,
			)
			if ok {
				ic.dbHandler.UpdateMailMatrixId(ctx, mail.ID, matrixId)
				touchedRooms = append(touchedRooms, mail.RootMatrixRoomID.String)
			} else {
				return false
			}
		}
		updateOverview := len(threads) > 0 || len(mails) > 0
		if updateOverview {
			ic.QueueMatrixOverviewUpdate(touchedRooms)
		}
		return true
	}
	MatrixNotificationStage = NewStage("MatrixNotification", setup, work, true)
}

// touchedRooms: rooms that have been updated (overview rooms will be determined by this function)
func (ic *InboxCollab) QueueMatrixOverviewUpdate(touchedRooms []string) {
	notify := make(map[string]bool)
	for _, target := range touchedRooms {
		for _, room := range ic.Config.Matrix.GetOverviewRooms(target) {
			if _, ok := notify[room]; !ok {
				notify[room] = true
			}
		}
	}
	for room := range notify {
		if stage, ok := MatrixOverviewStages[room]; ok {
			stage.QueueWork()
		}
	}
}

func (ic *InboxCollab) setupMatrixOverviewStage() {
	genWork := func(roomId string) func(context.Context) bool {
		return func(ctx context.Context) bool {
			if ic.Config.Matrix.VerifySession {
				return true
			}
			messageId, authors, subjects, rooms, threadMsgs := ic.dbHandler.GetOverviewThreads(
				ctx, roomId,
			)
			ok, messageId := ic.matrixHandler.UpdateThreadOverview(
				roomId, messageId, authors, subjects, rooms, threadMsgs,
			)
			if ok {
				ic.dbHandler.OverviewMessageUpdated(ctx, roomId, messageId)
			} else {
				return false
			}
			return true
		}
	}
	MatrixOverviewStages = make(map[string]*PipelineStage)
	for _, overviewRoom := range ic.Config.Matrix.AllOverviewRooms() {
		MatrixOverviewStages[overviewRoom] = NewStage(
			fmt.Sprintf("MatrixOverview[%s]", ic.Config.Matrix.AliasOfRoom(overviewRoom)),
			nil, genWork(overviewRoom), true,
		)
	}
}
