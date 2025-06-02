package matrix

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	config "github.com/arne314/inbox-collab/internal/config"
)

type MatrixHandler struct {
	client *MatrixClient
	config *config.MatrixConfig
}

func (mh *MatrixHandler) Setup(cfg *config.Config, actions Actions, wg *sync.WaitGroup) {
	defer wg.Done()
	mh.config = cfg.Matrix
	mh.client = &MatrixClient{}
	mh.client.Login(cfg, actions)
	go mh.client.Sync()
}

func (mh *MatrixHandler) WaitForRoomJoins() {
	for {
		ok, missing := mh.client.ValidateRooms()
		if ok {
			break
		}
		log.Warnf("Not a member of configured room %v - please invite, retrying in 20s...", missing)
		time.Sleep(time.Second * 20)
	}
}

func (mh *MatrixHandler) matchRoomsRegexps(regexps map[*regexp.Regexp]string, s string) string {
	for regex, room := range regexps {
		if regex.MatchString(s) {
			log.Infof("Using matrix room %v for: %v", mh.config.AliasOfRoom(room), s)
			return room
		}
	}
	return ""
}

func (mh *MatrixHandler) determineMatrixRoom(
	fetcher string, addrFrom string, addrTo []string,
) string {
	// check criteria in order: to > fetcher > from
	for _, addr := range addrTo {
		if room := mh.matchRoomsRegexps(mh.config.RoomsAddrToRegex, addr); room != "" {
			return room
		}
	}
	if room := mh.matchRoomsRegexps(mh.config.RoomsMailboxRegex, fetcher); room != "" {
		return room
	}
	if room := mh.matchRoomsRegexps(mh.config.RoomsAddrFromRegex, addrFrom); room != "" {
		return room
	}
	log.Infof("Using default matrix room for: to=%v from=%v mailbox=%v", addrTo, addrFrom, fetcher)
	return mh.config.DefaultRoom.String()
}

func (mh *MatrixHandler) CreateThread(
	fetcher string, addrFrom string, addrTo []string, author string, subject string,
) (bool, string, string) {
	textMessage, htmlMessage := formatAttribute(author, subject)
	roomId := mh.determineMatrixRoom(fetcher, addrFrom, addrTo)
	ok, messageId := mh.client.SendRoomMessage(roomId, textMessage, htmlMessage)
	return ok, roomId, messageId
}

func (mh *MatrixHandler) AddReply(
	roomId string, threadId string, author string, subject string,
	timestamp time.Time, attachments []string, message string, isFirst bool,
) (bool, string) {
	builder := NewTextHtmlBuilder()
	hasHead := false
	if !isFirst {
		builder.WriteLine(formatAttribute(author, subject))
		hasHead = true
	}
	if time := formatTime(timestamp); time != "" {
		builder.WriteLine(time, wrapHtmlStrong(time))
		hasHead = true
	}
	if len(attachments) != 0 {
		builder.WriteLine(formatAttribute("Attachments", strings.Join(attachments, ", ")))
		hasHead = true
	}
	if hasHead {
		builder.NewLine()
	}
	builder.Write(message, formatHtml(message))
	return mh.client.SendThreadMessage(roomId, threadId, builder.Text(), builder.Html())
}

func (mh *MatrixHandler) UpdateThreadOverview(
	overviewRoomId string, overviewMessageId string, authors []string,
	subjects []string, rooms []string, threadMsgs []string,
) (bool, string) {
	builder := NewTextHtmlBuilder()
	builder.WriteLine("Overview", "<h2>Overview</h2>")
	nAuthors := len(authors)

	for i := range nAuthors {
		link := formatMessageLink(rooms[i], threadMsgs[i], mh.config.HomeServer)
		textTitle, htlmTitle := formatAttribute(authors[i], subjects[i])
		textLine := fmt.Sprintf("%s - %s", textTitle, link)
		htmlLine := fmt.Sprintf("%s - %s", htlmTitle, link)

		if builder.MaxLen()+len(htmlLine) > 10000 {
			warning := fmt.Sprintf("%v additional threads are not listed here.", len(authors)-i)
			builder.NewLine()
			builder.NewLine()
			builder.Write(warning, warning)
			break
		}
		builder.Write(textLine, htmlLine)
		if i < nAuthors-1 {
			builder.NewLine()
		}
	}

	textMessage, htmlMessage := builder.String()
	if overviewMessageId == "" {
		return mh.client.SendRoomMessage(overviewRoomId, textMessage, htmlMessage)
	} else {
		return mh.client.EditRoomMessage(overviewRoomId, overviewMessageId, textMessage, htmlMessage)
	}
}

func (mh *MatrixHandler) Stop(waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
	mh.client.Stop()
}
