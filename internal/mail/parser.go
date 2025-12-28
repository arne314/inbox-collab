package mail

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/jhillyerd/enmime/v2"
	log "github.com/sirupsen/logrus"
)

var (
	nameFromRegex = regexp.MustCompile(
		`(?i)\"?\s*([^<>\" ][^<>\"]+[^<>\" ])\"?\s*<`)
	addressRegex = regexp.MustCompile(
		`(?i)<?(([a-zA-Z0-9%+-][a-zA-Z0-9.+-_{}\(\)\[\]'"\\#\$%\^\?/=&!\*\|~]*)@([a-zA-Z0-9.-]+\.[a-zA-Z]{2,}))>?`)
	idRegex = regexp.MustCompile(`(?i)<([^@ ]+@[^@ ]+)>`)
)

func parseHeaderRegex(
	regex *regexp.Regexp,
	header string,
	allowEmpty bool,
	lower bool,
) []string {
	parsed := regex.FindAllStringSubmatch(header, -1)
	if !allowEmpty && len(parsed) == 0 {
		return []string{""}
	}
	matches := make([]string, len(parsed))
	for i, match := range parsed {
		if lower {
			matches[i] = strings.ToLower(match[1])
		} else {
			matches[i] = match[1]
		}
	}
	slices.Sort(matches)
	return slices.Compact(matches)
}

func parseAddresses(header string, allowEmpty bool) []string {
	return parseHeaderRegex(addressRegex, header, allowEmpty, true)
}

func parseIds(header string, allowEmpty bool) []string {
	return parseHeaderRegex(idRegex, header, allowEmpty, false)
}

func parseNameFrom(header string) string {
	matches := nameFromRegex.FindStringSubmatch(header)
	if matches != nil {
		return matches[1]
	}
	return ""
}

func parseDomain(header string) string {
	matches := addressRegex.FindStringSubmatch(header)
	if matches != nil {
		return matches[3]
	}
	return ""
}

func (mf *MailFetcher) parseMessage(msg *imapclient.FetchMessageData) *Mail {
	var bodySection imapclient.FetchItemDataBodySection
	ok := false
	for {
		item := msg.Next()
		if item == nil {
			break
		}
		bodySection, ok = item.(imapclient.FetchItemDataBodySection)
		if ok {
			break
		}
	}
	if !ok {
		log.Errorf("FETCH command for %v did not return body section", mf.name)
		return nil
	}

	var envelope *enmime.Envelope
	envelope, err := enmime.ReadEnvelope(bodySection.Literal)
	if err != nil {
		log.Errorf("Failed to parse mail for %v: %v", mf.name, err)
		return nil
	}
	var date time.Time
	if date, err = envelope.Date(); err != nil {
		log.Errorf("Failed to parse date of mail for %v: %v", mf.name, err)
		return nil
	}
	attachments := make([]string, len(envelope.Attachments))
	for i, att := range envelope.Attachments {
		attachments[i] = att.FileName
	}
	parsedMail := &Mail{
		Fetcher:     mf.name,
		MessageId:   parseIds(envelope.GetHeader("Message-ID"), false)[0],
		InReplyTo:   parseIds(envelope.GetHeader("In-Reply-To"), false)[0],
		References:  parseIds(envelope.GetHeader("References"), true),
		NameFrom:    parseNameFrom(envelope.GetHeader("From")),
		AddrFrom:    parseAddresses(envelope.GetHeader("From"), false)[0],
		AddrTo:      parseAddresses(envelope.GetHeader("To"), true),
		Subject:     envelope.GetHeader("Subject"),
		Date:        date.UTC(),
		Text:        envelope.Text,
		Attachments: attachments,
	}
	if parsedMail.MessageId == "" {
		log.Errorf("Skipping invalid mail from fetcher %v: %v", mf.name, parsedMail)
		return nil
	}
	return parsedMail
}
