package db

import (
	"time"
)

type Message struct {
	Author    string     `json:"author"`
	Timestamp *time.Time `json:"timestamp"`
	Content   *string    `json:"content"`
}

type ExtractedMessages struct {
	Messages    []*Message `json:"messages"`
	Forwarded   bool       `json:"forwarded"`
	ForwardedBy *string    `json:"forwarded_by"`
}
