package main

import (
	"flag"
	"os"
	"os/signal"
	"sync"

	log "github.com/sirupsen/logrus"

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
	mailHandler.Setup(config)
	matrixHandler.Setup(config, *verifyMatrixSession)

	go mailHandler.Run()
	go matrixHandler.Run()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	log.Info("Startup complete")
	<-stop
	log.Info("Shutting down inbox-collab...")
	defer shutdown()
}

func shutdown() {
	waitGroup.Add(3)
	dbHandler.Stop(waitGroup)
	mailHandler.Stop(waitGroup)
	matrixHandler.Stop(waitGroup)
	waitGroup.Wait()
	log.Info("Shutdown successful")
}
