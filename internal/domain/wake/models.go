package wake

import (
	"time"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/meeting"
)

type Plan struct {
	ID                   uint
	ResearchTeamID       uint
	MeetingID            *uint
	Meeting              *domainmeeting.Meeting
	TriggerType          kernel.WakeTriggerType
	TriggerConfig        kernel.JSON
	Reason               string
	SourceMeetingEventID *uint
	SourceMeetingEvent   *domainmeeting.Event
	SourceRoleKey        *string
	Status               kernel.WakePlanStatus
	NextCheckAt          *time.Time
	FiredAt              *time.Time
	LastRunAt            *time.Time
	ResultSummary        *string
	CreatedAt            time.Time
}
