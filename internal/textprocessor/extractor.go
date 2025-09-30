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

func (me *MessageExtractor) ExtractMessages(ctx context.Context, mail *model.Mail, threadHistory []*model.Mail) *db.ExtractedMessages {
	extracted := me.llm.extractMessages(ctx, mail)
	return extracted
}
