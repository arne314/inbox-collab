package matrix

import (
	"context"
	"fmt"
	"strings"
	"time"

	config "github.com/arne314/inbox-collab/internal/config"
	_ "github.com/mattn/go-sqlite3"
	log "github.com/sirupsen/logrus"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/cryptohelper"
	"maunium.net/go/mautrix/crypto/verificationhelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

type MatrixClient struct {
	config             *config.MatrixConfig
	client             *mautrix.Client
	cryptoHelper       *cryptohelper.CryptoHelper
	verificationHelper *verificationhelper.VerificationHelper
	autoVerifySession  bool
	commandHandler     *CommandHandler
}

// implement mautrix auth callbacks
func (mc *MatrixClient) VerificationRequested(
	ctx context.Context, txnID id.VerificationTransactionID,
	from id.UserID, fromDevice id.DeviceID,
) {
	log.Infof(
		"Verification requested from user %v and device %v with transaction id %v",
		from, fromDevice, txnID,
	)
	if mc.autoVerifySession {
		mc.verificationHelper.AcceptVerification(ctx, txnID)
	} else {
		time.Sleep(time.Second)
		mc.verificationHelper.DismissVerification(ctx, txnID)
		log.Warnf(
			"Session verification is currently disabled, use the --verify-matrix flag to automatically verify sessions",
		)
	}
}

func (mc *MatrixClient) VerificationCancelled(
	ctx context.Context, txnID id.VerificationTransactionID,
	code event.VerificationCancelCode, reason string,
) {
	log.Warnf(
		"Verification cancelled with code %v and transaction id %v for reason: %v",
		code, txnID, reason,
	)
}

func (mc *MatrixClient) VerificationDone(
	ctx context.Context, txnID id.VerificationTransactionID,
) {
	log.Infof("Verification done with transaction id %v", txnID)
}

func (mc *MatrixClient) ShowSAS(
	ctx context.Context, txnID id.VerificationTransactionID, emojis []rune,
	emojiDescriptions []string, decimals []int,
) {
	log.Infof(
		"VERIFICATION CODE:\nemojis: %v\nemoji descriptions: %v\ndecimals: %v",
		string(emojis), emojiDescriptions, decimals,
	)
	go func() {
		time.Sleep(5 * time.Second)
		mc.verificationHelper.ConfirmSAS(context.Background(), txnID)
		log.Infof("\"Confirmed\" SAS are the same")
	}()
}

func (mc *MatrixClient) Login(cfg *config.Config, actions Actions) {
	mc.config = cfg.Matrix
	client, err := mautrix.NewClient(mc.config.HomeServer, "", "")
	client.DefaultHTTPRetries = 3
	if err != nil {
		log.Fatalf("Invalid matrix config: %v", err)
	}
	mc.client = client
	mc.commandHandler = &CommandHandler{actions: actions, client: mc}
	syncer := client.Syncer.(*mautrix.DefaultSyncer)

	// listen for messages
	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		if sender := evt.Sender.String(); sender != mc.config.Username {
			log.Infof("Received message\nSender: %s\nType: %s\nID: %s\nBody: %s\n",
				sender, evt.Type.String(), evt.ID.String(), evt.Content.AsMessage().Body,
			)
			mc.commandHandler.ProcessMessage(evt)
		}
	})

	// accept room invites
	syncer.OnEventType(event.StateMember, func(ctx context.Context, evt *event.Event) {
		if evt.GetStateKey() == client.UserID.String() &&
			evt.Content.AsMember().Membership == event.MembershipInvite {
			validRoom := false
			for _, room := range mc.config.AllRooms {
				if room == evt.RoomID.String() {
					validRoom = true
					break
				}
			}
			if !validRoom {
				log.Warnf("Rejecting invite to room not mentioned in the config: %v", evt.RoomID)
				return
			}
			_, err := client.JoinRoomByID(ctx, evt.RoomID)
			if err != nil {
				log.Errorf("Error joining room: %v", err)
			}
		}
	})

	// session login
	cryptoHelper, err := cryptohelper.NewCryptoHelper(client, []byte("meow"), "data/session.db")
	mc.cryptoHelper = cryptoHelper
	if err != nil {
		log.Fatalf("Error setting up cryptohelper: %v", err)
	}
	cryptoHelper.LoginAs = &mautrix.ReqLogin{
		Type: mautrix.AuthTypePassword,
		Identifier: mautrix.UserIdentifier{
			Type: mautrix.IdentifierTypeUser,
			User: mc.config.Username,
		},
		Password: mc.config.Password,
	}
	err = cryptoHelper.Init(context.Background())
	if err != nil {
		log.Fatalf("Error setting up cryptohelper: %v", err)
	}
	client.Crypto = cryptoHelper

	// session verification
	verificationHelper := verificationhelper.NewVerificationHelper(
		client, cryptoHelper.Machine(), nil, mc, false,
	)
	err = verificationHelper.Init(context.Background())
	if err != nil {
		log.Fatalf("Error setting up verification helper: %v", err)
	}
	mc.verificationHelper = verificationHelper
	mc.autoVerifySession = mc.config.VerifySession
	log.Info("Logged into matrix")
}

func (mc *MatrixClient) ValidateRooms() (ok bool, missing string) {
	joined, err := mc.client.JoinedRooms(context.Background())
	if err != nil {
		log.Errorf("Error fetching joined rooms: %v", err)
		return true, ""
	}
	for _, room := range mc.config.AllRooms {
		member := false
		for _, j := range joined.JoinedRooms {
			if j.String() == room {
				member = true
				break
			}
		}
		if !member {
			return false, room
		}
	}
	return true, ""
}

func (mc *MatrixClient) SleepOnRateLimit(err error) {
	if strings.Contains(err.Error(), "M_LIMIT_EXCEEDED") {
		time.Sleep(time.Second * 5)
	}
}

func (mc *MatrixClient) SendRoomMessage(roomId string, text string, html string) (bool, string) {
	resp, err := mc.client.SendMessageEvent(
		context.Background(),
		id.RoomID(roomId),
		event.EventMessage,
		&event.MessageEventContent{
			MsgType:       event.MsgText,
			Body:          text,
			Format:        event.FormatHTML,
			FormattedBody: html,
		},
	)
	if err != nil {
		log.Errorf("Error sending message to matrix: %v", err)
		mc.SleepOnRateLimit(err)
		return false, ""
	}
	return true, resp.EventID.String()
}

func (mc *MatrixClient) SendThreadMessage(
	roomId string, threadId string, text string, html string,
) (bool, string) {
	resp, err := mc.client.SendMessageEvent(
		context.Background(),
		id.RoomID(roomId),
		event.EventMessage,
		&event.MessageEventContent{
			MsgType:       event.MsgText,
			Body:          text,
			Format:        event.FormatHTML,
			FormattedBody: html,
			RelatesTo: &event.RelatesTo{
				EventID: id.EventID(threadId),
				Type:    event.RelThread,
			},
		},
	)
	if err != nil {
		log.Errorf("Error responding to thread on matrix: %v", err)
		mc.SleepOnRateLimit(err)
		return false, ""
	}
	return true, resp.EventID.String()
}

func (mc *MatrixClient) MessageRedacted(roomId string, messageId string) bool {
	evt, err := mc.client.GetEvent(context.Background(), id.RoomID(roomId), id.EventID(messageId))
	if err != nil {
		log.Errorf("Error fetching matrix event: %v", err)
		return true
	}
	return len(evt.Content.Raw) == 0
}

func (mc *MatrixClient) EditRoomMessage(
	roomId string, messageId string, text string, html string,
) (bool, string) {
	var err error
	ctx := context.Background()
	if mc.MessageRedacted(roomId, messageId) {
		return mc.SendRoomMessage(roomId, text, html)
	}
	_, err = mc.client.SendMessageEvent(
		ctx,
		id.RoomID(roomId),
		event.EventMessage,
		&event.MessageEventContent{
			MsgType:       event.MsgText,
			Body:          fmt.Sprintf("* %s", text),
			Format:        event.FormatHTML,
			FormattedBody: fmt.Sprintf("* %s", html),
			NewContent: &event.MessageEventContent{
				MsgType:       event.MsgText,
				Body:          text,
				Format:        event.FormatHTML,
				FormattedBody: html,
			},
			RelatesTo: &event.RelatesTo{
				EventID: id.EventID(messageId),
				Type:    event.RelReplace,
			},
		},
	)
	if err != nil {
		if strings.Contains(err.Error(), "M_TOO_LARGE") {
			_, err = mc.client.RedactEvent(ctx, id.RoomID(roomId), id.EventID(messageId))
			if err != nil {
				log.Errorf("Error redacting message event: %v", err)
				mc.SleepOnRateLimit(err)
				return false, ""
			}
			return mc.SendRoomMessage(roomId, text, html)
		}
		log.Errorf("Error updating message on matrix: %v", err)
		mc.SleepOnRateLimit(err)
		return false, ""
	}
	return true, messageId
}

func (mc *MatrixClient) ReactToMessage(roomId string, messageId string, reaction string) {
	_, err := mc.client.SendReaction(
		context.Background(), id.RoomID(roomId), id.EventID(messageId), reaction,
	)
	if err != nil {
		log.Errorf("Error reacting to message: %v", err)
	}
}

func (mc *MatrixClient) Run() {
	err := mc.client.Sync()
	if err != nil {
		log.Fatalf("Error syncing with matrix server: %v", err)
	}
}

func (mc *MatrixClient) Stop() {
	mc.client.StopSync()
	log.Info("Stopped matrix sync")
}
