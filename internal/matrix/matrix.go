package matrix

import (
	"context"
	"sync"
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

type MatrixHandler struct {
	client             *mautrix.Client
	cryptoHelper       *cryptohelper.CryptoHelper
	verificationHelper *verificationhelper.VerificationHelper
	autoVerifySession  bool
}

func (mh MatrixHandler) VerificationRequested(
	ctx context.Context, txnID id.VerificationTransactionID,
	from id.UserID, fromDevice id.DeviceID,
) {
	log.Infof(
		"Verification requested from user %v and device %v with transaction id %v",
		from, fromDevice, txnID,
	)
	if mh.autoVerifySession {
		mh.verificationHelper.AcceptVerification(ctx, txnID)
	} else {
		time.Sleep(time.Second)
		mh.verificationHelper.DismissVerification(ctx, txnID)
		log.Warnf(
			"Session verification is currently disabled, use the --verify-matrix flag to automatically verify sessions",
		)
	}
}

func (mh MatrixHandler) VerificationCancelled(
	ctx context.Context, txnID id.VerificationTransactionID,
	code event.VerificationCancelCode, reason string,
) {
	log.Warnf(
		"Verification cancelled with code %v and transaction id %v for reason: %v",
		code, txnID, reason,
	)
}

func (mh MatrixHandler) VerificationDone(
	ctx context.Context, txnID id.VerificationTransactionID,
) {
	log.Infof("Verification done with transaction id %v", txnID)
}

func (mh MatrixHandler) ShowSAS(
	ctx context.Context, txnID id.VerificationTransactionID, emojis []rune,
	emojiDescriptions []string, decimals []int,
) {
	log.Infof(
		"VERIFICATION CODE:\nemojis: %v\nemoji descriptions: %v\ndecimals: %v",
		string(emojis), emojiDescriptions, decimals,
	)
	go func() {
		time.Sleep(5 * time.Second)
		mh.verificationHelper.ConfirmSAS(context.Background(), txnID)
		log.Infof("\"Confirmed\" SAS are the same")
	}()
}

func (mh *MatrixHandler) Setup(cfg *config.Config, wg *sync.WaitGroup) {
	defer wg.Done()
	client, err := mautrix.NewClient(cfg.MatrixHomeServer, "", "")
	mh.client = client
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
	mh.cryptoHelper = cryptoHelper
	if err != nil {
		log.Fatalf("Error setting up cryptohelper: %v", err)
	}
	cryptoHelper.LoginAs = &mautrix.ReqLogin{
		Type: mautrix.AuthTypePassword,
		Identifier: mautrix.UserIdentifier{
			Type: mautrix.IdentifierTypeUser,
			User: cfg.MatrixUsername,
		},
		Password: cfg.MatrixPassword,
	}
	err = cryptoHelper.Init(context.Background())
	if err != nil {
		log.Fatalf("Error setting up cryptohelper: %v", err)
	}
	client.Crypto = cryptoHelper

	// session verification
	verificationHelper := verificationhelper.NewVerificationHelper(
		client, cryptoHelper.Machine(), nil, mh, false,
	)
	err = verificationHelper.Init(context.Background())
	if err != nil {
		log.Fatalf("Error setting up verification helper: %v", err)
	}
	mh.verificationHelper = verificationHelper
	mh.autoVerifySession = cfg.VerifyMatrixSession
	log.Info("Logged into matrix")
}

func (mh *MatrixHandler) Run() {
	err := mh.client.Sync()
	if err != nil {
		log.Fatalf("Error syncing with matrix server: %v", err)
	}
}

func (mh *MatrixHandler) Stop(waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
	mh.client.StopSync()
	log.Info("Stopped matrix sync")
}
