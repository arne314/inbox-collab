package textprocessor

import (
	"context"
	"fmt"
	"math/rand"
	"slices"
	"strings"

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
	// find and replace old contained messages (larger ones first)
	slices.SortFunc(threadHistory, func(m1, m2 *model.Mail) int {
		len1, len2 := 0, 0
		if m1.Messages != nil {
			len1 = len(*m1.Messages.Messages[0].Content)
		}
		if m2.Messages != nil {
			len2 = len(*m2.Messages.Messages[0].Content)
		}
		return len2 - len1
	})
	replaceMap := make(map[string]*string)
	for _, m := range threadHistory {
		if m.Messages != nil {
			mail.Body = replaceBestBlockMatch(mail.Body, m.Messages.Messages[0].Content, replaceMap)
		}
	}

	// send through llm
	extracted := me.llm.extractMessages(ctx, &mail)
	if extracted == nil {
		return nil
	}

	// replace with original content
	for _, msg := range extracted.Messages {
		for key, block := range replaceMap {
			if strings.Contains(*msg.Content, key) || strings.Contains(*msg.Content, key[2:len(key)-2]) {
				msg.Content = block
			}
		}
	}
	return extracted
}

// replace occurrence of block in base string
func replaceBestBlockMatch(base *string, block *string, replaceMap map[string]*string) *string {
	key := fmt.Sprintf("I got number [[KEEP%v]]", rand.Intn(90000)+10000)
	similarity, start, end := smithWaterman(computeMessageChunks(base), computeMessageChunks(block))
	if similarity >= 0.85 {
		replaceMap[key] = block
		res := ((*base)[:start] + key + (*base)[end:])
		return &res
	}
	return base
}
