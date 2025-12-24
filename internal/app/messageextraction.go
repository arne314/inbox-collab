package app

import (
	"context"
	"slices"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	model "github.com/arne314/inbox-collab/internal/db/generated"
	textprocessor "github.com/arne314/inbox-collab/internal/textprocessor"
)

func (ic *InboxCollab) performMessageExtraction(ctx context.Context, mail *model.Mail) bool {
	history := ic.dbHandler.GetMailsByThread(ctx, mail.Thread.Int64)
	history = slices.DeleteFunc(history, func(m *model.Mail) bool {
		return m.ID == mail.ID || m.Timestamp.Time.After(mail.Timestamp.Time)
	})
	extractor := textprocessor.NewMessageExtractor(ic.Config.LLM.ApiUrl, *mail, history)
	extracted := extractor.ExtractMessages(ctx)
	if extracted == nil || extracted.Messages == nil {
		log.Errorf("Error extracting messages for mail %v", mail.ID)
		return false
	} else {
		mail.Messages = extracted
		ic.dbHandler.UpdateExtractedMessages(ctx, mail)
		return true
	}
}

func (ic *InboxCollab) setupMessageExtractionStage() {
	var wg sync.WaitGroup
	work := func(ctx context.Context) bool {
		mails := ic.dbHandler.GetMailsRequiringMessageExtraction(ctx)
		if len(mails) == 0 {
			if MessageExtractionStage.IsFirstWork {
				MatrixNotificationStage.QueueWork()
			}
			return true
		}
		log.Infof("Extracting messages from %v mails...", len(mails))
		wg.Add(len(mails))
		// extract messages in batches per thread (mails are ordered by thread, timestamp)
		extractBatch := func(b []*model.Mail) {
			success := true
			for _, m := range b {
				defer wg.Done()
				if success { // cancel entire batch on error
					success = success && ic.performMessageExtraction(ctx, m)
				}
			}
			if !success {
				time.Sleep(5 * time.Second)
				MessageExtractionStage.QueueWork()
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
	MessageExtractionStage = NewStage("MessageExtraction", nil, work, false) // initial extraction happens in sorting
}
