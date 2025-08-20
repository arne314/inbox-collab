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

func (mf *MailFetcher) parseHeaderRegex(
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
		matches[i] = match[1]
		if lower {
			matches[i] = strings.ToLower(matches[i])
		}
	}
	slices.Sort(matches)
	return slices.Compact(matches)
}

func (mf *MailFetcher) parseAddresses(header string, allowEmpty bool) []string {
	return mf.parseHeaderRegex(mf.addressRegex, header, allowEmpty, true)
}

func (mf *MailFetcher) parseIds(header string, allowEmpty bool) []string {
	return mf.parseHeaderRegex(mf.idRegex, header, allowEmpty, false)
}

func (mf *MailFetcher) parseNameFrom(header string) string {
	matches := mf.nameFromRegex.FindStringSubmatch(header)
	if matches != nil {
		return matches[1]
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
	attachments := make([]string, 0)
	for _, att := range envelope.Attachments {
		attachments = append(attachments, att.FileName)
	}
	parsedMail := &Mail{
		Fetcher:     mf.name,
		MessageId:   mf.parseIds(envelope.GetHeader("Message-ID"), false)[0],
		InReplyTo:   mf.parseIds(envelope.GetHeader("In-Reply-To"), false)[0],
		References:  mf.parseIds(envelope.GetHeader("References"), true),
		NameFrom:    mf.parseNameFrom(envelope.GetHeader("From")),
		AddrFrom:    mf.parseAddresses(envelope.GetHeader("From"), false)[0],
		AddrTo:      mf.parseAddresses(envelope.GetHeader("To"), true),
		Subject:     envelope.GetHeader("Subject"),
		Date:        date,
		Text:        envelope.Text,
		Attachments: attachments,
	}
	if parsedMail.MessageId == "" || parsedMail.Text == "" {
		log.Errorf("Skipping invalid mail for %v: %v", mf.name, parsedMail)
		return nil
	}
	return parsedMail
}
