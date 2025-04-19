package app

import (
	"fmt"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	model "github.com/arne314/inbox-collab/internal/db/generated"
)

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
		touchedRooms := []string{}

		// post new threads
		threads := ic.dbHandler.GetMatrixReadyThreads()
		for _, thread := range threads {
			ok, roomId, messageId := ic.matrixHandler.CreateThread(
				thread.Fetcher.String, thread.AddrFrom, thread.AddrTo,
				thread.NameFrom, thread.Subject,
			)
			if ok {
				ic.dbHandler.UpdateThreadMatrixIds(thread.ID, roomId, messageId)
				touchedRooms = append(touchedRooms, roomId)
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
		for _, room := range ic.config.Matrix.GetOverviewRooms(target) {
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
	genWork := func(roomId string) func() bool {
		return func() bool {
			if ic.config.Matrix.VerifySession {
				return true
			}
			messageId, authors, subjects, rooms, threadMsgs := ic.dbHandler.GetOverviewThreads(
				roomId,
			)
			ok, messageId := ic.matrixHandler.UpdateThreadOverview(
				roomId, messageId, authors, subjects, rooms, threadMsgs,
			)
			if ok {
				ic.dbHandler.OverviewMessageUpdated(roomId, messageId)
			} else {
				return false
			}
			return true
		}
	}
	MatrixOverviewStages = make(map[string]*PipelineStage)
	for _, overviewRoom := range ic.config.Matrix.AllOverviewRooms() {
		MatrixOverviewStages[overviewRoom] = NewStage(
			fmt.Sprintf("MatrixOverview[%s]", ic.config.Matrix.AliasOfRoom(overviewRoom)),
			nil, genWork(overviewRoom), true,
		)
	}
}
