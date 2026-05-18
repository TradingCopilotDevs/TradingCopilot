package meeting

import (
	"time"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
)

type Meeting struct {
	ID               uint
	ResearchTeamID   uint
	Topic            string
	Status           kernel.MeetingStatus
	TriggerSource    string
	Summary          *string
	Conclusion       *string
	Tags             kernel.JSON
	RecapStatus      *string
	RecapUpdatedAt   *time.Time
	TokenBudget      int
	RunID            *string
	RunAttempt       int
	HeartbeatAt      *time.Time
	AutoRequeueCount int
	StartedAt        *time.Time
	CompletedAt      *time.Time
	CreatedAt        time.Time
}

type Event struct {
	ID        uint
	MeetingID uint
	Meeting   *Meeting
	Sequence  int
	Type      kernel.MeetingEventType
	RoleKey   *string
	Content   string
	Payload   kernel.JSON
	CreatedAt time.Time
}

type Reference struct {
	ID                    uint
	SourceMeetingID       uint
	SourceMeeting         *Meeting
	TargetMeetingID       *uint
	TargetMeeting         *Meeting
	ReferenceType         string
	Note                  *string
	TargetTopicSnapshot   string
	TargetSummarySnapshot *string
	TargetDeleted         bool
	ExternalRef           *string
	CreatedAt             time.Time
}

type ToolCallLog struct {
	ID            uint
	MeetingID     *uint
	Meeting       *Meeting
	RoleKey       *string
	ToolName      string
	Arguments     kernel.JSON
	ResultPreview kernel.JSON
	CreatedAt     time.Time
}
