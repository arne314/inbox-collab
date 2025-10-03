package textprocessor

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var whitespacesRegex = regexp.MustCompile(`\s+`)

func NormalizeText(text string, allowPunctuation bool) string {
	// only allow letters, numbers, punctuation and spaces
	decomposed := norm.NFKD.String(text)
	sb := strings.Builder{}
	for _, r := range decomposed {
		if unicode.IsLetter(r) || unicode.IsSpace(r) || unicode.IsNumber(r) || (allowPunctuation && unicode.IsPunct(r)) {
			sb.WriteRune(r)
		}
	}
	text = sb.String()

	// lowercase
	text = strings.ToLower(text)

	// remove unnecessary spaces
	text = whitespacesRegex.ReplaceAllLiteralString(text, " ")
	text = strings.TrimSpace(text)
	return text
}

// group of non whitespace adjacent characters
type chunk struct {
	start     int
	end       int
	norm      string // normalized content of this chunk
	normPunct string // same as norm but including punctuation
}

// collection of chunks
type message struct {
	content *string
	chunks  []*chunk
}

func chunkToString(message *message, chunk *chunk) string {
	return (*message.content)[chunk.start:chunk.end]
}

func computeMessageChunks(s *string) *message {
	chunks := make([]*chunk, 0, 10)
	result := &message{content: s}
	start := 0
	space := false
	prevSpace := true

	for i, r := range *s + " " {
		space = unicode.IsSpace(r)
		if prevSpace {
			start = i
		} else if space {
			add := &chunk{start: start, end: i}
			content := chunkToString(result, add)
			add.norm = NormalizeText(content, false)
			add.normPunct = NormalizeText(content, true)
			chunks = append(chunks, add)
		}
		prevSpace = space
	}
	result.chunks = chunks
	return result
}
