package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	log "github.com/sirupsen/logrus"

	cfg "github.com/arne314/inbox-collab/internal/config"
	model "github.com/arne314/inbox-collab/internal/db/generated"
	"github.com/arne314/inbox-collab/internal/db/sqlc"
)

type LLM struct {
	config *cfg.LLMConfig
}

func (llm *LLM) apiRequest(endpoint string, body []byte) ([]byte, error) {
	resp, err := http.Post(
		llm.config.ApiUrl+"/"+endpoint,
		"application/json",
		bytes.NewBuffer(body),
	)
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

func (llm *LLM) ExtractMessages(mail *model.Mail) {
	data := map[string]string{
		"conversation": *mail.Body,
		"subject":      mail.Subject,
		"timestamp":    mail.Timestamp.Time.Format("2006-01-02T15:04"),
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		log.Errorf("Error enconding json: %v", err)
		return
	}
	response, err := llm.apiRequest("parse_messages", encoded)
	if err != nil {
		log.Errorf("Error requesting llm api: %v", err)
		return
	}
	result := &db.ExtractedMessages{}
	json.Unmarshal(response, result)
	mail.Messages = result
}
