package app

import (
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

type PipelineStage struct {
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

func NewStage(name string, setup func(), work func() bool, initialQueue bool) *PipelineStage {
	if setup == nil {
		setup = func() {}
	}
	stage := &PipelineStage{
		name: name, setup: setup, work: work,
		IsFirstWork: true, launch: make(chan struct{}, 1),
	}
	stage.done.Store(true)
	if initialQueue {
		stage.QueueWork()
	}
	return stage
}

func (s *PipelineStage) QueueWork() { // ensures that work is queued at most once
	queue := s.done.CompareAndSwap(true, false)
	if queue {
		log.Infof("Queued pipeline stage '%s'", s.name)
		s.launch <- struct{}{}
		s.queuedTimeMutex.Lock()
		s.queuedTime = time.Now()
		s.queuedTimeMutex.Unlock()
	}
}

func (s *PipelineStage) Run() {
	s.setup()
	for range s.launch {
		log.Infof("Executing pipeline stage '%s'...", s.name)
		s.done.Store(true)
		s.isWorking.Store(true)
		retry := true
		for retry {
			retry = !s.work()
		}
		s.isWorking.Store(false)
		s.IsFirstWork = false
		log.Infof("Done executing pipeline stage '%s'", s.name)
	}
}

func (s *PipelineStage) Working() bool {
	return s.isWorking.Load()
}

func (s *PipelineStage) TimeSinceQueued() time.Duration {
	s.queuedTimeMutex.RLock()
	defer s.queuedTimeMutex.RUnlock()
	return time.Now().Sub(s.queuedTime)
}
