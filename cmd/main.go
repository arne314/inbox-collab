package main

import (
	"os"
	"os/signal"
	"sync"

	log "github.com/sirupsen/logrus"

	"github.com/arne314/inbox-collab/internal/app"
	cfg "github.com/arne314/inbox-collab/internal/config"
	"github.com/arne314/inbox-collab/internal/db"
	"github.com/arne314/inbox-collab/internal/mail"
	"github.com/arne314/inbox-collab/internal/matrix"
)

var (
	waitGroup     *sync.WaitGroup = &sync.WaitGroup{}
	config        *cfg.Config     = &cfg.Config{}
	dbHandler     *db.DbHandler
	inboxCollab   *app.InboxCollab
	matrixHandler *matrix.MatrixHandler
	mailHandler   *mail.MailHandler
)

func main() {
	log.Info("Starting inbox-collab...")
	config.Load()
	dbHandler = &db.DbHandler{Config: config}
	inboxCollab = &app.InboxCollab{Config: config}
	mailHandler = &mail.MailHandler{Config: config.Mail}
	matrixHandler = &matrix.MatrixHandler{Config: config.Matrix}
	dbHandler.Setup()
	inboxCollab.Setup(dbHandler, mailHandler, matrixHandler)

	go mailHandler.Run()
	go inboxCollab.Run()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	log.Info("Startup complete")
	<-stop
	log.Info("Shutting down inbox-collab...")
	defer shutdown()
}

func shutdown() {
	waitGroup.Add(4)
	go dbHandler.Stop(waitGroup)
	go mailHandler.Stop(waitGroup)
	go matrixHandler.Stop(waitGroup)
	go inboxCollab.Stop(waitGroup)
	waitGroup.Wait()
	log.Info("Shutdown successful")
}
