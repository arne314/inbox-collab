package main

import (
	"flag"
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
	waitGroup     *sync.WaitGroup       = &sync.WaitGroup{}
	config        *cfg.Config           = &cfg.Config{}
	dbHandler     *db.DbHandler         = &db.DbHandler{}
	matrixHandler *matrix.MatrixHandler = &matrix.MatrixHandler{}
	mailHandler   *mail.MailHandler     = &mail.MailHandler{}
	inboxCollab   *app.InboxCollab      = &app.InboxCollab{}
)

func main() {
	log.Info("Starting inbox-collab...")
	verifyMatrixSession := flag.Bool(
		"verify-matrix", false,
		"Accept session verification requests and automatically confirm matching SAS",
	)
	flag.Parse()
	config.Load()
	dbHandler.Setup(config)
	inboxCollab.Setup(config, dbHandler, mailHandler, matrixHandler, *verifyMatrixSession)

	go mailHandler.Run()
	go matrixHandler.Run()
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
