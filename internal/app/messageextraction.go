package app

import (
	"context"
	"sync"

	log "github.com/sirupsen/logrus"

	model "github.com/arne314/inbox-collab/internal/db/generated"
)

func (ic *InboxCollab) performMessageExtraction(ctx context.Context, mail *model.Mail) {
	history := ic.dbHandler.GetMailsByThread(ctx, mail.Thread)
	extracted := ic.messageExtractor.ExtractMessages(ctx, mail, history)
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
		for _, mail := range mails {
			go func(m *model.Mail) {
				defer wg.Done()
				ic.performMessageExtraction(ctx, m)
			}(mail)
		}
		wg.Wait()
		log.Infof("Done extracting messages from %v mails", len(mails))
		MatrixNotificationStage.QueueWork()
		return true
	}
	MessageExtractionStage = NewStage("MessageExtraction", nil, work, true)
}
