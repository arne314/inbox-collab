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
	// handle empty messages
	if NormalizeText(*mail.Body, true) == "" {
		content := ""
		return &db.ExtractedMessages{
			Forwarded:   false,
			ForwardedBy: "",
			Messages: []*db.Message{
				{
					Author:    mail.NameFrom,
					Content:   &content,
					Timestamp: &mail.Timestamp.Time,
				},
			},
		}
	}

	// find and remove old contained messages (check latest ones first)
	baseChunks := computeMessageChunks(mail.Body)
	messageRemoved := make(map[*model.Mail]bool)
	messageSimilar := make(map[*model.Mail]bool)
	oldMessageChunks := make(map[*model.Mail]*message)
	for i := len(threadHistory) - 1; i >= 0; i-- { // keep at least one past message for content
		old := threadHistory[i]
		if old.Messages != nil {
			var replaced *string
			oldChunks := computeMessageChunks(old.Body)
			oldMessageChunks[old] = computeMessageChunks(old.Messages.Messages[0].Content)
			messageRemoved[old] = false
			// we might have identical messages so we ignore them
			if levenshtein(baseChunks, oldChunks) >= 0.8 {
				messageSimilar[old] = true
				continue
			}
			replaced, messageRemoved[old] = replaceBestBlockMatch(baseChunks, oldChunks)
			if messageRemoved[old] {
				mail.Body = replaced
				baseChunks = computeMessageChunks(replaced)
			}
		}
	}

	// send through llm
	extracted := me.llm.extractMessages(ctx, &mail)
	if extracted == nil {
		return nil
	}

	// detect new and known messsages
	messageKnown := make(map[*db.Message]bool)
	oldMessageKnown := make(map[*model.Mail]bool)
	knownCount := 0
detectKnown:
	for _, ext := range extracted.Messages {
		extractedChunks := computeMessageChunks(ext.Content)
		for old, removed := range messageRemoved {
			if removed || messageSimilar[old] || oldMessageKnown[old] {
				continue
			}
			if similarity := levenshtein(extractedChunks, oldMessageChunks[old]); similarity >= 0.9 {
				oldMessageKnown[old] = true
				messageKnown[ext] = true
				knownCount++
				continue detectKnown
			}
		}
		messageKnown[ext] = false
	}

	// make sure the first message is new
	if messageKnown[extracted.Messages[0]] {
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
