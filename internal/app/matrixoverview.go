package app

import (
	"context"
	"fmt"
	"sync"
)

// touchedRooms: rooms that have been updated (overview rooms will be determined by this function)
func (ic *InboxCollab) QueueMatrixOverviewUpdate(touchedRooms []string, blocking bool) {
	notify := make(map[string]bool)
	for _, target := range touchedRooms {
		for _, room := range ic.Config.Matrix.GetOverviewRooms(target) {
			if _, ok := notify[room]; !ok {
				notify[room] = true
			}
		}
	}
	var wg sync.WaitGroup
	for room := range notify {
		if stage, ok := MatrixOverviewStages[room]; ok {
			if blocking {
				wg.Add(1)
				go func(stage *PipelineStage) {
					stage.QueueWorkBlocking()
					wg.Done()
				}(stage)
			} else {
				stage.QueueWork()
			}
		}
	}
	wg.Wait()
}

func (ic *InboxCollab) setupMatrixOverviewStage() {
	genWork := func(roomId string) func(context.Context) bool {
		return func(ctx context.Context) bool {
			if ic.Config.Matrix.VerifySession {
				return true
			}
			messageId, authors, subjects, rooms, threadMsgs := ic.dbHandler.GetOverviewThreads(
				ctx, roomId,
			)
			ok, messageId := ic.matrixHandler.UpdateThreadOverview(
				roomId, messageId, authors, subjects, rooms, threadMsgs,
			)
			if ok {
				ic.dbHandler.OverviewMessageUpdated(ctx, roomId, messageId)
			} else {
				return false
			}
			return true
		}
	}
	MatrixOverviewStages = make(map[string]*PipelineStage)
	for _, overviewRoom := range ic.Config.Matrix.AllOverviewRooms() {
		MatrixOverviewStages[overviewRoom] = NewStage(
			fmt.Sprintf("MatrixOverview[%s]", ic.Config.Matrix.AliasOfRoom(overviewRoom)),
			nil, genWork(overviewRoom), true,
		)
	}
}
