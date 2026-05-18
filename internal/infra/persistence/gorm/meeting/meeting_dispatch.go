package meeting

import (
	"context"
	"sync"

	"gorm.io/gorm"
)

var localMeetingRuns = struct {
	sync.Mutex
	cancel map[uint]context.CancelFunc
}{cancel: map[uint]context.CancelFunc{}}

func StartLocalMeetingRun(db *gorm.DB, meetingID uint) bool {
	localMeetingRuns.Lock()
	if _, exists := localMeetingRuns.cancel[meetingID]; exists {
		localMeetingRuns.Unlock()
		return false
	}
	ctx, cancel := context.WithCancel(dbContext(db))
	localMeetingRuns.cancel[meetingID] = cancel
	localMeetingRuns.Unlock()

	go func() {
		defer func() {
			localMeetingRuns.Lock()
			if current := localMeetingRuns.cancel[meetingID]; current != nil {
				delete(localMeetingRuns.cancel, meetingID)
			}
			localMeetingRuns.Unlock()
		}()
		_ = RunMeetingOnceWithContext(ctx, db, meetingID)
	}()
	return true
}

func CancelLocalMeetingRun(meetingID uint) bool {
	localMeetingRuns.Lock()
	cancel := localMeetingRuns.cancel[meetingID]
	if cancel != nil {
		delete(localMeetingRuns.cancel, meetingID)
	}
	localMeetingRuns.Unlock()
	if cancel == nil {
		return false
	}
	cancel()
	return true
}

func LocalMeetingRunActive(meetingID uint) bool {
	localMeetingRuns.Lock()
	defer localMeetingRuns.Unlock()
	return localMeetingRuns.cancel[meetingID] != nil
}
