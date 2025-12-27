package app

import (
	"context"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

var threadSortingMutex sync.Mutex

func (ic *InboxCollab) LockThreadSorting() {
	threadSortingMutex.Lock()
}

func (ic *InboxCollab) UnlockThreadSorting() {
	threadSortingMutex.Unlock()
}

func (ic *InboxCollab) setupThreadSortingStage() {
	work := func(ctx context.Context) bool {
		threadSortingMutex.Lock()
		defer threadSortingMutex.Unlock()

		timeSinceMailboxUpdate := time.Since(ic.mailHandler.GetLastMailboxUpdate()).Seconds()
		timeSinceSortRequest := ThreadSortingStage.TimeSinceQueued().Seconds()
		waitForCompleteData := timeSinceMailboxUpdate < 5 && timeSinceSortRequest < 120 // timeout
		if waitForCompleteData {
			log.Infof("Waiting for complete data to sort threads...")
			time.Sleep(2 * time.Second)
			return false
		}

		ic.dbHandler.AutoUpdateMailSorting(ctx)
		mails := ic.dbHandler.GetMailsRequiringSorting(ctx)
		if len(mails) == 0 {
			if ThreadSortingStage.IsFirstWork {
				MessageExtractionStage.QueueWork()
			}
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
		MessageExtractionStage.QueueWork()
		return true
	}
	ThreadSortingStage = NewStage(
		"ThreadSorting", nil, work,
		false, // initial queueing happens in storeMails()
	)
}
