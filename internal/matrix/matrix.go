package matrix

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"

	config "github.com/arne314/inbox-collab/internal/config"
	_ "github.com/mattn/go-sqlite3"
)

type MatrixHandler struct {
	client *MatrixClient
	config *config.MatrixConfig
}

func (mh *MatrixHandler) Setup(cfg *config.Config, wg *sync.WaitGroup) {
	defer wg.Done()
	mh.client = &MatrixClient{}
	mh.client.Login(cfg)
	mh.config = cfg.Matrix
}

func (mh *MatrixHandler) Run() {
	mh.client.Run()
}

func formatHtml(text string) string {
	return strings.ReplaceAll(text, "\n", "<br>")
}

func formatTime(timestamp time.Time) string {
	var formatTime string
	age := time.Now().Sub(timestamp)
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

func (mh *MatrixHandler) matchRoomsRegexps(regexps map[*regexp.Regexp]string, s string) string {
	for regex, room := range regexps {
		if regex.MatchString(s) {
			log.Infof("Using matrix room %v for: %v", room, s)
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
	return mh.config.DefaultRoom
}

func (mh *MatrixHandler) CreateThread(
	fetcher string, addrFrom string, addrTo []string, author string, subject string,
) (bool, string, string) {
	textMessage := fmt.Sprintf("%s: %s", author, subject)
	htmlMessage := fmt.Sprintf("<strong>%s</strong>: %s", author, subject)
	roomId := mh.determineMatrixRoom(fetcher, addrFrom, addrTo)
	ok, messageId := mh.client.SendRoomMessage(roomId, textMessage, htmlMessage)
	return ok, roomId, messageId
}

func (mh *MatrixHandler) AddReply(
	roomId string, threadId string, author string,
	subject string, timestamp time.Time, message string, isFirst bool,
) (bool, string) {
	var textMessage, htmlMessage string
	time := formatTime(timestamp)

	if isFirst { // subject and author are already given in thread head
		if time != "" {
			textMessage = fmt.Sprintf("%s\n%s", time, message)
			htmlMessage = fmt.Sprintf(
				"<strong>%s</strong><br><br>%s",
				time, formatHtml(message),
			)
		} else {
			textMessage = message
			htmlMessage = formatHtml(message)
		}
	} else {
		if time != "" {
			textMessage = fmt.Sprintf("%s: %s\n%s\n\n%s", author, subject, time, message)
			htmlMessage = fmt.Sprintf(
				"<strong>%s</strong>: %s<br><strong>%s</strong><br><br>%s",
				author, subject, time, formatHtml(message),
			)
		} else {
			textMessage = fmt.Sprintf("%s: %s\n\n%s", author, subject, message)
			htmlMessage = fmt.Sprintf(
				"<strong>%s</strong>: %s<br><br>%s",
				author, subject, formatHtml(message),
			)
		}
	}
	return mh.client.SendThreadMessage(roomId, threadId, textMessage, htmlMessage)
}

func (mh *MatrixHandler) Stop(waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
	mh.client.Stop()
}
