package meeting

import (
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"gorm.io/gorm"
)

func RecoverQueuedMeetings(db *gorm.DB, settings config.Settings, limit int, olderThan time.Duration, runner string, dispatch func(*domainmeeting.Meeting) error) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	if olderThan < 0 {
		olderThan = 0
	}
	recovered := 0
	threshold := time.Now().Add(-olderThan)
	var queuedRows []persistmodel.Meeting
	if err := db.Where("status = ? AND started_at IS NULL AND created_at <= ?", domainkernel.MeetingQueued, threshold).Order("created_at").Limit(limit).Find(&queuedRows).Error; err != nil {
		return recovered, err
	}
	queued := make([]domainmeeting.Meeting, 0, len(queuedRows))
	for _, row := range queuedRows {
		queued = append(queued, meetingFromModel(row))
	}
	for i := range queued {
		if dispatch != nil {
			if err := dispatch(&queued[i]); err != nil {
				return recovered, err
			}
		}
		_, _ = AppendEvent(db, queued[i].ID, domainkernel.EventSystem, nil, recoveryQueuedMessage(runner), map[string]any{"status": "queued_recovered", "runner": runner})
		recovered++
	}

	remaining := limit - recovered
	if remaining <= 0 {
		return recovered, nil
	}
	staleAfter := settings.MeetingStaleAfter
	if staleAfter <= 0 {
		staleAfter = 30 * time.Minute
	}
	staleThreshold := time.Now().Add(-staleAfter)
	var staleRows []persistmodel.Meeting
	if err := db.Where("status = ? AND completed_at IS NULL AND (heartbeat_at <= ? OR heartbeat_at IS NULL)", domainkernel.MeetingRunning, staleThreshold).Order("started_at").Limit(remaining).Find(&staleRows).Error; err != nil {
		return recovered, err
	}
	stale := make([]domainmeeting.Meeting, 0, len(staleRows))
	for _, row := range staleRows {
		stale = append(stale, meetingFromModel(row))
	}
	for i := range stale {
		meeting := &stale[i]
		if meeting.AutoRequeueCount >= settings.MeetingAutoRequeueLimit {
			now := time.Now()
			conclusion := recoveryFailedMessage(runner)
			meeting.Status = domainkernel.MeetingFailed
			meeting.CompletedAt = &now
			meeting.HeartbeatAt = &now
			meeting.RunID = nil
			meeting.Conclusion = &conclusion
			if err := saveMeeting(db, meeting); err != nil {
				return recovered, err
			}
			_, _ = AppendEvent(db, meeting.ID, domainkernel.EventError, nil, conclusion, map[string]any{"status": "stale_failed", "auto_requeue_count": meeting.AutoRequeueCount, "runner": runner})
			recovered++
			continue
		}
		meeting.Status = domainkernel.MeetingQueued
		meeting.StartedAt = nil
		meeting.CompletedAt = nil
		meeting.RunID = nil
		meeting.HeartbeatAt = nil
		meeting.AutoRequeueCount++
		if err := saveMeeting(db, meeting); err != nil {
			return recovered, err
		}
		if dispatch != nil {
			if err := dispatch(meeting); err != nil {
				return recovered, err
			}
		}
		_, _ = AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, recoveryStaleMessage(runner), map[string]any{"status": "stale_requeued", "runner": runner, "auto_requeue_count": meeting.AutoRequeueCount})
		recovered++
	}
	return recovered, nil
}

func recoveryQueuedMessage(runner string) string {
	if runner == "redis" {
		return "Recovered a queued meeting and rescheduled execution."
	}
	return "Recovered a queued meeting and scheduled local execution."
}

func recoveryStaleMessage(runner string) string {
	if runner == "redis" {
		return "Recovered a stale running meeting and rescheduled execution."
	}
	return "Recovered a stale running meeting and scheduled local execution."
}

func recoveryFailedMessage(runner string) string {
	if runner == "redis" {
		return "Meeting stopped after repeated stale running recoveries. Please review the worker logs and restart manually if needed."
	}
	return "Meeting stopped after repeated stale running recoveries. Please review the local process logs and restart manually if needed."
}
