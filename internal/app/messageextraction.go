package app

import (
	"context"
	"slices"
	"sync"

	log "github.com/sirupsen/logrus"

	model "github.com/arne314/inbox-collab/internal/db/generated"
)

func (ic *InboxCollab) performMessageExtraction(ctx context.Context, mail *model.Mail) {
	history := ic.dbHandler.GetMailsByThread(ctx, mail.Thread.Int64)
	history = slices.DeleteFunc(history, func(m *model.Mail) bool {
		return m.ID == mail.ID || m.Timestamp.Time.After(mail.Timestamp.Time)
	})
	extracted := ic.messageExtractor.ExtractMessages(ctx, *mail, history)
	if extracted == nil || extracted.Messages == nil {
		log.Errorf("Error extracting messages for mail %v", mail.ID)
	} else {
		mail.Messages = extracted
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
		// extract messages in batches per thread (mails are ordered by thread, timestamp)
		extractBatch := func(b []*model.Mail) {
			for _, m := range b {
				defer wg.Done()
				ic.performMessageExtraction(ctx, m)
			}
		}
		batch := make([]*model.Mail, 0, len(mails))
		var prevThread int64 = -1
		for _, mail := range mails {
			if len(batch) > 0 && mail.Thread.Int64 != int64(prevThread) {
				go extractBatch(batch)
				batch = make([]*model.Mail, 0, len(batch))
			}
			batch = append(batch, mail)
			prevThread = mail.Thread.Int64
		}
		extractBatch(batch)
		wg.Wait()
		log.Infof("Done extracting messages from %v mails", len(mails))
		MatrixNotificationStage.QueueWork()
		return true
	}
	MessageExtractionStage = NewStage("MessageExtraction", nil, work, true)
}
