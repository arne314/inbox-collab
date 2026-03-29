package matrix

import (
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	textNewline = "\n"
	htmlNewline = "<br>"
)

type TextHtmlBuilder struct {
	textBuilder *strings.Builder
	htmlBuilder *strings.Builder
}

func NewTextHtmlBuilder() *TextHtmlBuilder {
	return &TextHtmlBuilder{
		textBuilder: new(strings.Builder),
		htmlBuilder: new(strings.Builder),
	}
}

func (t *TextHtmlBuilder) Write(text, html string) {
	t.textBuilder.WriteString(text)
	t.htmlBuilder.WriteString(html)
}

func (t *TextHtmlBuilder) WriteLine(text, html string) {
	t.Write(text, html)
	t.NewLine()
}

func (t *TextHtmlBuilder) NewLine() {
	t.Write(textNewline, htmlNewline)
}

func (t *TextHtmlBuilder) Len() (int, int) {
	return t.textBuilder.Len(), t.htmlBuilder.Len()
}

func (t *TextHtmlBuilder) MaxLen() int {
	return max(t.Len())
}

func (t *TextHtmlBuilder) String() (string, string) {
	return t.Text(), t.Html()
}

func (t *TextHtmlBuilder) Text() string {
	return t.textBuilder.String()
}

func (t *TextHtmlBuilder) Html() string {
	return t.htmlBuilder.String()
}

func wrapHtmlTag(s, tag string) string {
	return fmt.Sprintf("<%s>%s</%s>", tag, s, tag)
}

func wrapHtmlStrong(s string) string {
	return wrapHtmlTag(s, "strong")
}

func wrapHtmlItalic(s string) string {
	return wrapHtmlTag(s, "i")
}

func wrapHtmlCode(s string) string {
	return wrapHtmlTag(s, "code")
}

func formatAttribute(name string, value string) (string, string) {
	textMessage := fmt.Sprintf("%s: %s", name, value)
	htmlMessage := fmt.Sprintf("%s: %s", wrapHtmlStrong(name), value)
	return textMessage, htmlMessage
}

func formatBold(message string) (string, string) {
	return message, wrapHtmlStrong(message)
}

func formatItalic(message string) (string, string) {
	return message, wrapHtmlItalic(message)
}

func formatCode(message string) (string, string) {
	return message, wrapHtmlCode(message)
}

var mdCodeRegex *regexp.Regexp = regexp.MustCompile("`([^`]+)`")

// replace `code` with html code tags
func convertMdCode(message string) (string, string) {
	return mdCodeRegex.ReplaceAllString(message, "$1"),
		mdCodeRegex.ReplaceAllString(formatHtml(message), wrapHtmlCode("$1"))
}

func formatHtml(text string) string {
	return strings.ReplaceAll(html.EscapeString(text), textNewline, htmlNewline)
}

func formatTime(timestamp time.Time, timezone string) string {
	var formatTime string
	zone, _ := time.LoadLocation(timezone) // timezone has already been validated
	timestamp = timestamp.In(zone)
	age := time.Since(timestamp)
	if age.Hours() > 24*30 {
		formatTime = timestamp.Format("2 Jan 2006 15:04")
	} else if age.Hours() > 24*3 {
		formatTime = timestamp.Format("2 Jan 15:04")
	} else if age.Hours() > 10 {
		formatTime = timestamp.Format("Mon 15:04")
	} else if age.Minutes() > 3 {
		formatTime = timestamp.Format("15:04")
	}
	return formatTime
}

func formatMessageLink(roomId, messageId, homeServer string) string {
	parsedUrl, err := url.Parse(homeServer)
	if err == nil {
		homeServer = parsedUrl.Host + parsedUrl.RequestURI()
	}
	return fmt.Sprintf(
		"https://matrix.to/#/%s/%s?via=%s",
		roomId, messageId, homeServer,
	)
}

func truncateString(s string) string {
	runes := []rune(s)
	return string(runes[:len(runes)-max(1, len(runes)/10)])
}

func truncateChars(text, html string) (string, string) {
	return truncateString(text), truncateString(html)
}

func truncateSplits(s string, split string) string {
	lines := strings.Split(s, split)
	lines = lines[:len(lines)-max(1, len(lines)/10)]
	return strings.Join(lines, split)
}

func truncateLines(text, html string) (string, string) {
	return truncateSplits(text, textNewline), truncateSplits(html, htmlNewline)
}
