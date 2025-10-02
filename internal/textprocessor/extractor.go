package textprocessor

import (
	"context"

	cfg "github.com/arne314/inbox-collab/internal/config"
	model "github.com/arne314/inbox-collab/internal/db/generated"
	db "github.com/arne314/inbox-collab/internal/db/sqlc"
)

type MessageExtractor struct {
	llm *LLM
}

func NewMessageExtractor(llmConfig *cfg.LLMConfig) *MessageExtractor {
	llm := &LLM{Config: llmConfig}
	extractor := &MessageExtractor{llm: llm}
	return extractor
}

func (me *MessageExtractor) ExtractMessages(ctx context.Context, mail model.Mail, threadHistory []*model.Mail) *db.ExtractedMessages {
	// find and remove old contained messages (check latest ones first)
	baseChunks := computeMessageChunks(mail.Body)
	messageRemoved := make(map[*model.Mail]bool)
	messageChunks := make(map[*model.Mail]*message)
	for i := len(threadHistory) - 2; i >= 0; i-- { // we keep at least one reply to avoid removing too much (messages could have identical content)
		old := threadHistory[i]
		if old.Messages != nil {
			var replaced *string
			replaced, messageRemoved[old] = replaceBestBlockMatch(baseChunks, computeMessageChunks(old.Body))
			if !messageRemoved[old] {
				messageChunks[old] = computeMessageChunks(old.Messages.Messages[0].Content)
			} else {
				replacedChunks := computeMessageChunks(mail.Body)
				baseChunks = replacedChunks
				mail.Body = replaced
			}
		}
	}

	// send through llm
	extracted := me.llm.extractMessages(ctx, &mail)
	if extracted == nil {
		return nil
	}

	// make sure the new message was not previously extracted
	messageKnown := make(map[*db.Message]bool)
	visited := make(map[*model.Mail]bool)
	knownCount := 0
detectKnown:
	for _, ext := range extracted.Messages {
		extractedMessage := computeMessageChunks(ext.Content)
		for old, removed := range messageRemoved {
			if removed || visited[old] {
				continue
			}
			visited[old] = true
			if similarity, _, _ := smithWaterman(extractedMessage, messageChunks[old]); similarity >= 0.9 {
				messageKnown[ext] = true
				knownCount++
				continue detectKnown
			}
		}
		messageKnown[ext] = false
	}
	if messageKnown[extracted.Messages[0]] { // this is a problem
		if knownCount != len(extracted.Messages) {
			for _, ext := range extracted.Messages {
				// swap first with an unknown message
				if !messageKnown[ext] {
					tmp := extracted.Messages[0].Content
					tmp2 := extracted.Messages[0].Author
					extracted.Messages[0].Content = ext.Content
					extracted.Messages[0].Author = ext.Author
					ext.Content = tmp
					ext.Author = tmp2
					break
				}
			}
		}
	}
	return extracted
}

// replace occurrence of block in base string
func replaceBestBlockMatch(base *message, block *message) (result *string, replaced bool) {
	similarity, start, end := smithWaterman(base, block)
	if similarity >= 0.85 {
		res := ((*base.content)[:start] + "\n\n=== PLACEHOLDER ===\n\n" + (*base.content)[end:])
		return &res, true
	}
	return base.content, false
}
