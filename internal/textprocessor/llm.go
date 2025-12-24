package textprocessor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"

	log "github.com/sirupsen/logrus"

	model "github.com/arne314/inbox-collab/internal/db/generated"
	"github.com/arne314/inbox-collab/internal/db/sqlc"
)

var placeholderRegex *regexp.Regexp = regexp.MustCompile(`==\s*PLACEHOLDER\s*==`)

type LLM interface {
	GetPlaceholder() string
	IsPlaceholder(msg string) bool
	ExtractMessages(ctx context.Context, mail *model.Mail) *db.ExtractedMessages
}

type LLMPassthrough struct{}

func (llm *LLMPassthrough) GetPlaceholder() string {
	return "\n"
}

func (llm *LLMPassthrough) IsPlaceholder(msg string) bool {
	return false
}

func (llm *LLMPassthrough) ExtractMessages(ctx context.Context, mail *model.Mail) *db.ExtractedMessages {
	return PassthroughExtraction(mail)
}

func PassthroughExtraction(mail *model.Mail) *db.ExtractedMessages {
	return &db.ExtractedMessages{
		Forwarded:   false,
		ForwardedBy: "",
		Messages: []*db.Message{
			{
				Author:    mail.NameFrom,
				Content:   mail.Body,
				Timestamp: &mail.Timestamp.Time,
			},
		},
	}
}

type LLMPython struct {
	apiUrl string
}

type ParseMessagesRequest struct {
	Author           string `json:"author"`
	Conversation     string `json:"conversation"`
	Subject          string `json:"subject"`
	Timestamp        string `json:"timestamp"`
	ReplyCandidate   bool   `json:"reply_candidate"`
	ForwardCandidate bool   `json:"forward_candidate"`
}

func (llm *LLMPython) GetPlaceholder() string {
	return "\n\n=== PLACEHOLDER ===\n\n"
}

func (llm *LLMPython) IsPlaceholder(msg string) bool {
	return placeholderRegex.FindStringIndex(msg) != nil
}

func (llm *LLMPython) apiRequest(ctx context.Context, endpoint string, body []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		llm.apiUrl+"/"+endpoint,
		bytes.NewBuffer(body),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if code := resp.StatusCode; code != 200 {
		return nil, fmt.Errorf("Received http status %v from llm api: %v", code, string(data))
	}
	return data, nil
}

func (llm *LLMPython) ExtractMessages(ctx context.Context, mail *model.Mail) *db.ExtractedMessages {
	data := ParseMessagesRequest{
		Author:           mail.NameFrom,
		Conversation:     *mail.Body,
		Subject:          mail.Subject,
		Timestamp:        mail.Timestamp.Time.Format("2006-01-02T15:04"),
		ReplyCandidate:   mail.HeaderInReplyTo != "",
		ForwardCandidate: len(mail.HeaderReferences) != 0,
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		log.Errorf("Error enconding json: %v", err)
		return nil
	}
	response, err := llm.apiRequest(ctx, "parse_messages", encoded)
	if err != nil {
		log.Errorf("Error requesting llm api: %v", err)
		return nil
	}
	result := &db.ExtractedMessages{}
	json.Unmarshal(response, result)
	return result
}
