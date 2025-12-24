package textprocessor

import (
	"context"
	"slices"
	"strings"

	model "github.com/arne314/inbox-collab/internal/db/generated"
	db "github.com/arne314/inbox-collab/internal/db/sqlc"
)

type MessageExtractor struct {
	llm           LLM
	mail          *model.Mail
	threadHistory []*model.Mail
	result        *db.ExtractedMessages

	messageRemoved   map[*model.Mail]bool
	messageSimilar   map[*model.Mail]bool
	oldMessageChunks map[*model.Mail]*message
}

// create a new MessageExtractor for an email
func NewMessageExtractor(apiUrl string, mail model.Mail, threadHistory []*model.Mail) *MessageExtractor {
	// mail is not a pointer as we want to modify its content
	var llm LLM
	if apiUrl == "passthrough" {
		llm = &LLMPassthrough{}
	} else {
		llm = &LLMPython{apiUrl: apiUrl}
	}
	body := strings.Clone(*mail.Body)
	mail.Body = &body
	extractor := &MessageExtractor{
		llm: llm, mail: &mail, threadHistory: threadHistory,
		messageRemoved:   make(map[*model.Mail]bool),
		messageSimilar:   make(map[*model.Mail]bool),
		oldMessageChunks: make(map[*model.Mail]*message),
	}
	return extractor
}

// wrapper for all extraction operations
func (me *MessageExtractor) ExtractMessages(ctx context.Context) *db.ExtractedMessages {
	// handle empty messages
	if NormalizeText(*me.mail.Body, true) == "" {
		content := ""
		me.mail.Body = &content
		return PassthroughExtraction(me.mail)
	}

	// replace old messages
	me.replaceOldMessages()

	// send through llm
	me.extractLLM(ctx)

	// fixup
	me.postExtraction()
	return me.result
}

// replaces messages from threadHistory found in mail.Body with placeholder of llm
func (me *MessageExtractor) replaceOldMessages() {
	baseChunks := computeMessageChunks(me.mail.Body)
	for i := len(me.threadHistory) - 1; i >= 0; i-- { // check latest ones first
		old := me.threadHistory[i]
		if old.Messages != nil {
			var replaced *string
			oldChunks := computeMessageChunks(old.Body)
			me.oldMessageChunks[old] = computeMessageChunks(old.Messages.Messages[0].Content)
			me.messageRemoved[old] = false

			// we might have identical messages so we ignore this one
			if levenshtein(baseChunks, oldChunks) >= 0.8 {
				me.messageSimilar[old] = true
				continue
			}
			replaced, me.messageRemoved[old] = replaceBestBlockMatch(
				baseChunks, oldChunks, me.llm.GetPlaceholder(),
			)
			if me.messageRemoved[old] {
				me.mail.Body = replaced
				baseChunks = computeMessageChunks(replaced)
			}
		}
	}
}

// invoke exraction process of llm
func (me *MessageExtractor) extractLLM(ctx context.Context) {
	me.result = me.llm.ExtractMessages(ctx, me.mail)
}

// fix potential llm extraction issues and process placeholders
func (me *MessageExtractor) postExtraction() {
	if me.result == nil {
		return
	}

	// we might need to restore a message (though restoring more than one is not implemented)
	if me.result.Forwarded {
	iterresult:
		for _, ext := range me.result.Messages {
			if me.llm.IsPlaceholder(*ext.Content) {
				for mail, removed := range me.messageRemoved {
					if removed && mail.Messages != nil {
						ext.Content = mail.Messages.Messages[0].Content
						break iterresult
					}
				}
			}
		}
	}
	// placeholders can be ignored from now on
	me.result.Messages = slices.DeleteFunc(me.result.Messages, func(m *db.Message) bool {
		return me.llm.IsPlaceholder(*m.Content)
	})

	// detect new and known messsages
	messageKnown := make(map[*db.Message]bool)
	oldMessageKnown := make(map[*model.Mail]bool)
	knownCount := 0
detectKnown:
	for _, ext := range me.result.Messages {
		extractedChunks := computeMessageChunks(ext.Content)
		for old, removed := range me.messageRemoved {
			if removed || me.messageSimilar[old] || oldMessageKnown[old] {
				continue
			}
			if similarity := levenshtein(extractedChunks, me.oldMessageChunks[old]); similarity >= 0.9 {
				oldMessageKnown[old] = true
				messageKnown[ext] = true
				knownCount++
				continue detectKnown
			}
		}
		messageKnown[ext] = false
	}

	// make sure the first message is new
	if messageKnown[me.result.Messages[0]] {
		if knownCount != len(me.result.Messages) {
			for _, ext := range me.result.Messages {
				// swap first with an unknown message
				if !messageKnown[ext] {
					tmp := me.result.Messages[0].Content
					tmp2 := me.result.Messages[0].Author
					me.result.Messages[0].Content = ext.Content
					me.result.Messages[0].Author = ext.Author
					ext.Content = tmp
					ext.Author = tmp2
					break
				}
			}
		}
	}
}

// smith waterman wrapper
func replaceBestBlockMatch(base *message, block *message, placeholder string) (result *string, replaced bool) {
	similarity, start, end := smithWaterman(base, block)
	if similarity >= 0.85 {
		res := ((*base.content)[:start] + placeholder + (*base.content)[end:])
		return &res, true
	}
	return base.content, false
}
