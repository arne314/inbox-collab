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
