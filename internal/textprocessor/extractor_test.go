package textprocessor

import (
	"testing"

	model "github.com/arne314/inbox-collab/internal/db/generated"
	db "github.com/arne314/inbox-collab/internal/db/sqlc"
)

func TestMessageExtractor_replaceOldMessages(t *testing.T) {
	tests := []struct {
		name    string
		mail    string
		history []string
		wanted  string
	}{
		{
			"empty",
			`Hi, thanks for your message
			Reply to:`,
			[]string{
				``,
			},
			`Hi, thanks for your message
			Reply to:`,
		},
		{
			"identical",
			`I love go! :)`,
			[]string{
				`I love go! :)`,
			},
			`I love go! :)`,
		},
		{
			"simple",
			`Hi, thanks for your message.
			Reply to: I love go! :)`,
			[]string{
				`I love go! :)`,
			},
			`Hi, thanks for your message.
			Reply to:`,
		},
		{
			"simple",
			`Hi, thanks for your message.
			Reply to: I love go! :)
			Reply to: I really love go!`,
			[]string{
				`I love go! :)`,
				`I really love go`,
			},
			`Hi, thanks for your message.
			Reply to:
			Reply to:`,
		},
		{
			"simple",
			`Hi, thanks for your message.
			Reply to: I love go! :)
			Reply to: I really love go!
			-- end of message`,
			[]string{
				`I love go! :)`,
				`I really love go`,
			},
			`Hi, thanks for your message.
			Reply to:
			Reply to:
			-- end of message`,
		},
		{
			"complex",
			`Hi, thanks for your message.
			Reply to:
			> I love go! :)
			> Reply to:
			> > I really love go!`,
			[]string{
				`I love go! :)
				Reply to:
				> I really love go!`,
			},
			`Hi, thanks for your message.
			Reply to:
			> `,
		},
		{
			"missing",
			`Hi, thanks for your message.
			Reply to:
			> This message is missing in the history
			> Reply to:
			> > I really love go!`,
			[]string{
				`This message is just different from the one given`,
				`I really love go!`,
			},
			`Hi, thanks for your message.
			Reply to:
			> This message is missing in the history
			> Reply to:
			> > `,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mail := model.Mail{Body: &tt.mail}
			me := NewMessageExtractor("passthrough_test", mail, arrayToHistory(tt.history))
			me.replaceOldMessages()
			result := *me.mail.Body
			if NormalizeText(result, true) != NormalizeText(tt.wanted, true) {
				t.Errorf("replaceOldMessages() = %v, want %v", result, tt.wanted)
			}
		})
	}
}

func TestMessageExtractor_postExtraction(t *testing.T) {
	tests := []struct {
		name      string
		mail      string
		forwarded bool
		history   []string
		extracted []string
		want      []string
	}{
		{
			name:      "one",
			mail:      "just one",
			extracted: []string{"just one"},
			want:      []string{"just one"},
		},
		{
			name:      "remove_placeholder",
			mail:      "Sample mail",
			history:   []string{"old1", "old2"},
			extracted: []string{"Sample mail", "== PLACEHOLDER =="},
			want:      []string{"Sample mail"},
		},
		{
			name:      "keep_simple",
			mail:      "Sample mail",
			history:   []string{"old1", "old2"},
			extracted: []string{"Sample mail", "old1"},
			want:      []string{"Sample mail", "old1"},
		},

		// extracted messages "old1" and "old2" are known while "Sample mail" one is not
		// -> should move "Sample mail" to the front
		{
			name:      "swap_new",
			mail:      "Sample mail",
			history:   []string{"old1", "old2"},
			extracted: []string{"old1", "old2", "Sample mail"},
			want:      []string{"Sample mail", "old1", "old2"},
		},

		// "Sample mail" (mail text) is similar (identical) to a past message
		// -> still treat it as new and move it to the front
		{
			name:      "swap_similar",
			mail:      "Sample mail",
			history:   []string{"old1", "old2", "Sample mail"},
			extracted: []string{"old1", "old2", "Sample mail"},
			want:      []string{"Sample mail", "old1", "old2"},
		},

		// all extracted messages are known -> can't do anything about it
		{
			name:      "keep_known",
			mail:      "Sample mail",
			history:   []string{"old1", "old2"},
			extracted: []string{"old1", "old2"},
			want:      []string{"old1", "old2"},
		},

		// in case of forwarded conversations we don't want to discard placeholders
		{
			name:      "restore_placeholder_forwarded",
			forwarded: true,
			mail:      "This is interesting: Some forwarded message",
			history:   []string{"Some forwarded message"},
			extracted: []string{"This is interesting", "== PLACEHOLDER =="},
			want:      []string{"This is interesting", "Some forwarded message"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mail := model.Mail{Body: &tt.mail}
			me := NewMessageExtractor("passthrough_test", mail, arrayToHistory(tt.history))
			me.replaceOldMessages() // fill maps used in postExtraction()
			me.result = arrayToExtracted(tt.extracted)
			me.result.Forwarded = tt.forwarded
			me.postExtraction()

			// validation
			ok := true
			if len(me.result.Messages) != len(tt.want) {
				t.Errorf("postExtraction got length %d, want length %d", len(me.result.Messages), len(tt.want))
				ok = false
			}
			if ok {
				for i, msg := range me.result.Messages {
					if *msg.Content != tt.want[i] {
						t.Errorf("postExtraction() = %v, want %v", *msg.Content, tt.want[i])
						break
					}
				}
			}
			if !ok {
				full := make([]string, len(me.result.Messages))
				for j, m := range me.result.Messages {
					full[j] = *m.Content
				}
				t.Errorf("full result is %v, want %v", full, tt.want)
			}
		})
	}
}

func arrayToExtracted(arr []string) *db.ExtractedMessages {
	messages := make([]*db.Message, len(arr))
	for i, m := range arr {
		messages[i] = &db.Message{
			Content: &m,
		}
	}
	return &db.ExtractedMessages{
		Messages: messages,
	}
}

func arrayToHistory(arr []string) []*model.Mail {
	history := make([]*model.Mail, len(arr))
	for i, msg := range arr {
		history[i] = &model.Mail{
			Body:     &msg,
			Messages: arrayToExtracted([]string{msg}),
		}
	}
	return history
}
