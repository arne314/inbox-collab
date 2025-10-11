package app

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

type PipelineStage struct {
	name            string
	setup           func(context.Context)
	work            func(context.Context) bool
	done            atomic.Bool
	launch          chan struct{}
	queuedTime      time.Time
	queuedTimeMutex sync.RWMutex
	isWorking       atomic.Bool
	IsFirstWork     bool

	ctx            context.Context
	cancelFunc     context.CancelFunc
	active         bool
	activeMutex    sync.Mutex // to safely close the launch channel
	blockings      []chan struct{}
	blockingsMutex sync.Mutex
}

func NewStage(name string, setup func(context.Context), work func(context.Context) bool, initialQueue bool) *PipelineStage {
	if setup == nil {
		setup = func(context.Context) {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	stage := &PipelineStage{
		name: name, setup: setup, work: work,
		IsFirstWork: true, launch: make(chan struct{}, 1),
		ctx: ctx, cancelFunc: cancel, active: true,
	}
	stage.done.Store(true)
	if initialQueue {
		stage.QueueWork()
	}
	return stage
}

func (s *PipelineStage) QueueWork() { // ensures that work is queued at most once
	s.activeMutex.Lock()
	defer s.activeMutex.Unlock()
	if !s.active {
		return
	}
	queue := s.done.CompareAndSwap(true, false)
	if queue {
		log.Infof("Queued pipeline stage '%s'", s.name)
		s.launch <- struct{}{}
		s.queuedTimeMutex.Lock()
		s.queuedTime = time.Now().UTC()
		s.queuedTimeMutex.Unlock()
	}
}

func (s *PipelineStage) QueueWorkBlocking() {
	done := make(chan struct{})
	s.blockingsMutex.Lock()
	s.blockings = append(s.blockings, done)
	s.blockingsMutex.Unlock()
	s.QueueWork()
	<-done
}

func (s *PipelineStage) Run(waitGroup *sync.WaitGroup) {
	defer waitGroup.Done()
	s.setup(s.ctx)
	for range s.launch {
		log.Infof("Executing pipeline stage '%s'...", s.name)
		s.done.Store(true)
		s.isWorking.Store(true)
		first := true
		retry := false
		for first || retry {
			ctx := context.WithValue(s.ctx, "retry", retry)
			first = false
			retry = !s.work(ctx) && ctx.Err() == nil
		}
		s.isWorking.Store(false)
		s.IsFirstWork = false
		s.blockingsMutex.Lock()
		if len(s.launch) == 0 { // actually done
			for _, c := range s.blockings {
				close(c)
			}
			s.blockings = []chan struct{}{}
		}
		s.blockingsMutex.Unlock()
		log.Infof("Done executing pipeline stage '%s'", s.name)
	}
}

func (s *PipelineStage) Working() bool {
	return s.isWorking.Load()
}

func (s *PipelineStage) TimeSinceQueued() time.Duration {
	s.queuedTimeMutex.RLock()
	defer s.queuedTimeMutex.RUnlock()
	return time.Since(s.queuedTime)
}

func (s *PipelineStage) close() {
	s.activeMutex.Lock()
	defer s.activeMutex.Unlock()
	s.active = false
	close(s.launch)
}

func (s *PipelineStage) Stop() {
	s.close()
}

func (s *PipelineStage) ForceStop() {
	s.close()
	s.cancelFunc()
}
