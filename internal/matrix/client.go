package matrix

import (
	"context"
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
	client             *mautrix.Client
	cryptoHelper       *cryptohelper.CryptoHelper
	verificationHelper *verificationhelper.VerificationHelper
	autoVerifySession  bool
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

func (mc *MatrixClient) Login(cfg *config.Config) {
	client, err := mautrix.NewClient(cfg.Matrix.HomeServer, "", "")
	mc.client = client
	if err != nil {
		log.Fatalf("Invalid matrix config: %v", err)
	}
	syncer := client.Syncer.(*mautrix.DefaultSyncer)

	// listen for messages
	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		log.Infof("Received message\nSender: %s\nType: %s\nID: %s\nBody: %s\n",
			evt.Sender.String(),
			evt.Type.String(),
			evt.ID.String(),
			evt.Content.AsMessage().Body)
	})

	// accept room invites
	syncer.OnEventType(event.StateMember, func(ctx context.Context, evt *event.Event) {
		if evt.GetStateKey() == client.UserID.String() &&
			evt.Content.AsMember().Membership == event.MembershipInvite {
			_, err := client.JoinRoomByID(ctx, evt.RoomID)
			if err != nil {
				log.Errorf("Error joining room: %v", err)
			}
		}
	})

	// session login
	cryptoHelper, err := cryptohelper.NewCryptoHelper(client, []byte("meow"), "session.db")
	mc.cryptoHelper = cryptoHelper
	if err != nil {
		log.Fatalf("Error setting up cryptohelper: %v", err)
	}
	cryptoHelper.LoginAs = &mautrix.ReqLogin{
		Type: mautrix.AuthTypePassword,
		Identifier: mautrix.UserIdentifier{
			Type: mautrix.IdentifierTypeUser,
			User: cfg.Matrix.Username,
		},
		Password: cfg.Matrix.Password,
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
	mc.autoVerifySession = cfg.Matrix.VerifySession
	log.Info("Logged into matrix")
}

func (mc *MatrixClient) SendRoomMessage(roomId string, text string) (bool, string) {
	resp, err := mc.client.SendText(context.Background(), id.RoomID(roomId), text)
	if err != nil {
		log.Errorf("Error sending message to matrix: %v", err)
		return false, ""
	}
	return true, resp.EventID.String()
}

func (mc *MatrixClient) SendThreadMessage(
	roomId string, threadId string, text string,
) (bool, string) {
	resp, err := mc.client.SendMessageEvent(
		context.Background(),
		id.RoomID(roomId),
		event.EventMessage,
		&event.MessageEventContent{
			Body:    text,
			MsgType: event.MsgText,
			RelatesTo: &event.RelatesTo{
				EventID: id.EventID(threadId),
				Type:    event.RelThread,
			},
		},
	)
	if err != nil {
		log.Errorf("Error responding to thread on matrix: %v", err)
		return false, ""
	}
	return true, resp.EventID.String()
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
