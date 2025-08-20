package app

import (
	"context"
	"sync"

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
