package app

import (
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

type Stage struct {
	name            string
	setup           func()
	work            func() bool
	done            atomic.Bool
	launch          chan struct{}
	queuedTime      time.Time
	queuedTimeMutex sync.RWMutex
	isWorking       atomic.Bool
	IsFirstWork     bool
}

func NewStage(name string, setup func(), work func() bool, initialQueue bool) *Stage {
	if setup == nil {
		setup = func() {}
	}
	stage := &Stage{
		name: name, setup: setup, work: work,
		IsFirstWork: true, launch: make(chan struct{}, 1),
	}
	stage.done.Store(true)
	if initialQueue {
		stage.QueueWork()
	}
	return stage
}

func (s *Stage) QueueWork() { // ensures that work is queued at most once
	queue := s.done.CompareAndSwap(true, false)
	if queue {
		log.Infof("Queued stage '%s'", s.name)
		s.launch <- struct{}{}
		s.queuedTimeMutex.Lock()
		s.queuedTime = time.Now()
		s.queuedTimeMutex.Unlock()
	}
}

func (s *Stage) Run() {
	s.setup()
	for range s.launch {
		log.Infof("Executing stage '%s'...", s.name)
		s.done.Store(true)
		s.isWorking.Store(true)
		retry := true
		for retry {
			retry = !s.work()
		}
		s.isWorking.Store(false)
		s.IsFirstWork = false
		log.Infof("Done executing stage '%s'", s.name)
	}
}

func (s *Stage) Working() bool {
	return s.isWorking.Load()
}

func (s *Stage) TimeSinceQueued() time.Duration {
	s.queuedTimeMutex.RLock()
	defer s.queuedTimeMutex.RUnlock()
	return time.Now().Sub(s.queuedTime)
}
