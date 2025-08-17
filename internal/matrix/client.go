package matrix

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	// "github.com/rs/zerolog"
	log "github.com/sirupsen/logrus"
	"go.mau.fi/util/dbutil"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/cryptohelper"
	"maunium.net/go/mautrix/crypto/verificationhelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
	"maunium.net/go/mautrix/sqlstatestore"

	config "github.com/arne314/inbox-collab/internal/config"
)

type MatrixClient struct {
	Config             *config.MatrixConfig
	client             *mautrix.Client
	ctx                context.Context
	cancelSync         context.CancelFunc
	cryptoHelper       *cryptohelper.CryptoHelper
	verificationHelper *verificationhelper.VerificationHelper
	autoVerifySession  bool
	commandHandler     *CommandHandler
}

const matrixTimeout = 100 * time.Second

func (mc *MatrixClient) defaultContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(mc.ctx, matrixTimeout)
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

func (mc *MatrixClient) VerificationReady(
	ctx context.Context, txnID id.VerificationTransactionID, otherDeviceID id.DeviceID,
	supportsSAS, supportsScanQRCode bool, qrCode *verificationhelper.QRCode,
) {
	log.Infof(
		"Verification %v has been accepted by both parties (other device id: %v)",
		txnID, otherDeviceID,
	)
}

func (mc *MatrixClient) VerificationCancelled(
	ctx context.Context, txnID id.VerificationTransactionID,
	code event.VerificationCancelCode, reason string,
) {
	log.Warnf(
		"Verification %v cancelled with code %v for reason: %v",
		txnID, code, reason,
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
		err := mc.verificationHelper.ConfirmSAS(ctx, txnID)
		if err == nil {
			log.Infof("\"Confirmed\" SAS are the same")
		} else {
			log.Errorf("Error confirming SAS: %s", err)
		}
	}()
}

func (mc *MatrixClient) Login(ctx context.Context, actions Actions) {
	client, err := mautrix.NewClient(mc.Config.HomeServer, "", "")
	client.DefaultHTTPRetries = 3
	// client.Log = zerolog.New(os.Stdout).With().Timestamp().Logger()
	if err != nil {
		log.Fatalf("Invalid matrix config: %v", err)
	}
	mc.client = client
	mc.ctx = ctx
	mc.commandHandler = NewCommandHandler(mc.Config, actions, mc)
	syncer := client.Syncer.(*mautrix.DefaultSyncer)

	// listen for messages
	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		if sender := evt.Sender.String(); sender != mc.Config.Username {
			log.Infof("Received message\nSender: %s\nType: %s\nID: %s\nBody: %s\n",
				sender, evt.Type.String(), evt.ID.String(), evt.Content.AsMessage().Body,
			)
			mc.commandHandler.ProcessMessage(ctx, evt)
		}
	})

	syncer.OnEventType(event.StateMember, func(ctx context.Context, evt *event.Event) {
		// accept room invites
		if evt.GetStateKey() == client.UserID.String() &&
			evt.Content.AsMember().Membership == event.MembershipInvite {
			validRoom := slices.Contains(mc.Config.AllRooms(), evt.RoomID.String())
			if !validRoom {
				log.Warnf("Rejecting invite to room not mentioned in the config: %v", evt.RoomID)
				return
			}
			_, err := client.JoinRoomByID(ctx, evt.RoomID)
			if err != nil {
				log.Errorf("Error joining room: %v", err)
			}
		}

		// resend overview on member join
		if evt.GetStateKey() != client.UserID.String() &&
			evt.Content.AsMember().Membership == event.MembershipJoin {
			mc.commandHandler.Actions.ResendThreadOverview(ctx, string(evt.RoomID))
		}
	})

	// session login
	getDb := func(name string) (*dbutil.Database, error) {
		db, err := dbutil.NewFromConfig(fmt.Sprintf("inbox-collab-%v", name), dbutil.Config{
			PoolConfig: dbutil.PoolConfig{
				Type:         "sqlite3-fk-wal",
				URI:          fmt.Sprintf("file:data/%v.db?_txlock=immediate", name),
				MaxOpenConns: 5,
				MaxIdleConns: 1,
			},
		}, dbutil.ZeroLogger(mc.client.Log))
		if err == nil && db.Owner == "" {
			db.Owner = "inbox-collab"
		}
		return db, err
	}
	pickleKey := []byte("meow")
	sessionDb, err := getDb("session")
	if err != nil {
		log.Fatalf("Error creating matrix session database: %v", err)
	}

	stateStore := sqlstatestore.NewSQLStateStore(sessionDb, dbutil.ZeroLogger(mc.client.Log), false)
	err = stateStore.Upgrade(ctx)
	if err != nil {
		log.Fatalf("Error upgrading state store db")
	}
	mc.client.StateStore = stateStore

	cryptoHelper, err := cryptohelper.NewCryptoHelper(client, pickleKey, sessionDb)
	if err != nil {
		log.Fatalf("Error setting up cryptohelper: %v", err)
	}
	mc.cryptoHelper = cryptoHelper
	cryptoHelper.LoginAs = &mautrix.ReqLogin{
		Type: mautrix.AuthTypePassword,
		Identifier: mautrix.UserIdentifier{
			Type: mautrix.IdentifierTypeUser,
			User: mc.Config.Username,
		},
		Password:                 mc.Config.Password,
		InitialDeviceDisplayName: "inbox-collab",
	}
	err = cryptoHelper.Init(ctx)
	if err != nil {
		log.Fatalf("Error initializing cryptohelper: %v", err)
	}
	client.Crypto = cryptoHelper

	// session verification
	verificationHelper := verificationhelper.NewVerificationHelper(
		client, cryptoHelper.Machine(), nil, mc, false, false, true,
	)
	err = verificationHelper.Init(ctx)
	if err != nil {
		log.Fatalf("Error setting up verification helper: %v", err)
	}
	mc.verificationHelper = verificationHelper
	mc.autoVerifySession = mc.Config.VerifySession
	log.Info("Logged into matrix")
}

func (mc *MatrixClient) ValidateRooms() (ok bool, missing string) {
	ctx, cancel := mc.defaultContext()
	defer cancel()
	joined, err := mc.client.JoinedRooms(ctx)
	if err != nil {
		log.Errorf("Error fetching joined rooms: %v", err)
		return true, ""
	}
	for _, room := range mc.Config.AllRooms() {
		member := false
		for _, j := range joined.JoinedRooms {
			if j.String() == room {
				member = true
				ctx, cancel := mc.defaultContext()
				defer cancel()
				_, err := mc.client.State(ctx, j)
				if err != nil {
					log.Errorf("Error getting state of joined room: %v", err)
				}
				break
			}
		}
		if !member {
			return false, room
		}
	}
	return true, ""
}

type roomNameContent struct {
	Name string `json:"name"`
}

func (mc *MatrixClient) GetRoomName(roomId string) (bool, string) {
	ctx, cancel := mc.defaultContext()
	defer cancel()
	var content roomNameContent
	err := mc.client.StateEvent(ctx, id.RoomID(roomId), event.StateRoomName, "", &content)
	if err != nil {
		log.Errorf("Error getting room state: %v", err)
		return false, ""
	}
	return true, content.Name
}

func (mc *MatrixClient) SleepOnRateLimit(err error) {
	if strings.Contains(err.Error(), "M_LIMIT_EXCEEDED") {
		time.Sleep(time.Second * 5)
	}
}

func (mc *MatrixClient) SendRoomMessage(roomId string, text string, html string) (bool, string) {
	ctx, cancel := mc.defaultContext()
	defer cancel()
	resp, err := mc.client.SendMessageEvent(
		ctx,
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
	roomId string, threadId string, text string, html string, ignoreRedacted bool,
) (bool, bool, string) {
	if !ignoreRedacted && mc.MessageRedacted(roomId, threadId) {
		return false, true, ""
	}
	ctx, cancel := mc.defaultContext()
	defer cancel()
	resp, err := mc.client.SendMessageEvent(
		ctx,
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
		return false, false, ""
	}
	return true, false, resp.EventID.String()
}

func (mc *MatrixClient) RedactMessage(roomId, messageId string) bool {
	ctx, cancel := mc.defaultContext()
	defer cancel()
	_, err := mc.client.RedactEvent(ctx, id.RoomID(roomId), id.EventID(messageId))
	if err != nil {
		log.Errorf("Error redacting message event: %v", err)
		mc.SleepOnRateLimit(err)
		return false
	}
	return true
}

func (mc *MatrixClient) MessageRedacted(roomId string, messageId string) bool {
	ctx, cancel := mc.defaultContext()
	defer cancel()
	evt, err := mc.client.GetEvent(ctx, id.RoomID(roomId), id.EventID(messageId))
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
	ctx, cancel := mc.defaultContext()
	defer cancel()
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
			if !mc.RedactMessage(roomId, messageId) {
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

func (mc *MatrixClient) ReactToMessage(roomId string, messageId string, reaction string) string {
	ctx, cancel := mc.defaultContext()
	defer cancel()
	resp, err := mc.client.SendReaction(
		ctx, id.RoomID(roomId), id.EventID(messageId), reaction,
	)
	if err != nil {
		log.Errorf("Error reacting to message: %v", err)
		return ""
	}
	return resp.EventID.String()
}

func (mc *MatrixClient) Sync() {
	syncCtx, cancelSync := context.WithCancel(mc.ctx)
	mc.cancelSync = cancelSync
	err := mc.client.SyncWithContext(syncCtx)
	if err != nil && !errors.Is(err, context.Canceled) {
		log.Fatalf("Error syncing with matrix server: %v", err)
	}
}

func (mc *MatrixClient) Stop() {
	mc.cancelSync()
	mc.cryptoHelper.Close()
	log.Info("Stopped matrix sync")
}
