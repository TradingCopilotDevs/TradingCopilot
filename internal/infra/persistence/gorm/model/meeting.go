package model

import (
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	"time"

	"gorm.io/datatypes"
)

type Meeting struct {
	ID               uint                       `gorm:"primaryKey"`
	ResearchTeamID   uint                       `gorm:"not null;index"`
	ResearchTeam     *ResearchTeam              `gorm:"foreignKey:ResearchTeamID;constraint:OnDelete:CASCADE"`
	Topic            string                     `gorm:"size:240"`
	Status           domainkernel.MeetingStatus `gorm:"size:32;default:queued"`
	TriggerSource    string                     `gorm:"size:64;default:manual"`
	Summary          *string                    `gorm:"type:text"`
	Conclusion       *string                    `gorm:"type:text"`
	Tags             datatypes.JSON             `gorm:"type:json;default:'[]'"`
	RecapStatus      *string                    `gorm:"size:32"`
	RecapUpdatedAt   *time.Time
	TokenBudget      int     `gorm:"default:0"`
	RunID            *string `gorm:"size:64"`
	RunAttempt       int     `gorm:"default:0"`
	HeartbeatAt      *time.Time
	AutoRequeueCount int `gorm:"default:0"`
	StartedAt        *time.Time
	CompletedAt      *time.Time
	CreatedAt        time.Time
}

func (Meeting) TableName() string { return "meetings" }

type MeetingEvent struct {
	ID        uint     `gorm:"primaryKey"`
	MeetingID uint     `gorm:"index"`
	Meeting   *Meeting `gorm:"foreignKey:MeetingID;constraint:OnDelete:CASCADE"`
	Sequence  int
	Type      domainkernel.MeetingEventType `gorm:"size:32"`
	RoleKey   *string                       `gorm:"size:64"`
	Content   string                        `gorm:"type:text"`
	Payload   datatypes.JSON                `gorm:"type:json;default:'{}'"`
	CreatedAt time.Time
}

func (MeetingEvent) TableName() string { return "meeting_events" }

type MeetingReference struct {
	ID                    uint     `gorm:"primaryKey"`
	SourceMeetingID       uint     `gorm:"index"`
	SourceMeeting         *Meeting `gorm:"foreignKey:SourceMeetingID;constraint:OnDelete:CASCADE"`
	TargetMeetingID       *uint    `gorm:"index"`
	TargetMeeting         *Meeting `gorm:"foreignKey:TargetMeetingID;constraint:OnDelete:SET NULL"`
	ReferenceType         string   `gorm:"size:32;default:meeting"`
	Note                  *string  `gorm:"type:text"`
	TargetTopicSnapshot   string   `gorm:"size:240"`
	TargetSummarySnapshot *string  `gorm:"type:text"`
	TargetDeleted         bool     `gorm:"default:false"`
	ExternalRef           *string  `gorm:"size:128"`
	CreatedAt             time.Time
}

func (MeetingReference) TableName() string { return "meeting_references" }

type ToolCallLog struct {
	ID            uint           `gorm:"primaryKey"`
	MeetingID     *uint          `gorm:"index"`
	Meeting       *Meeting       `gorm:"foreignKey:MeetingID;constraint:OnDelete:CASCADE"`
	RoleKey       *string        `gorm:"size:64"`
	ToolName      string         `gorm:"size:128"`
	Arguments     datatypes.JSON `gorm:"type:json;default:'{}'"`
	ResultPreview datatypes.JSON `gorm:"type:json;default:'{}'"`
	CreatedAt     time.Time
}

func (ToolCallLog) TableName() string { return "tool_call_logs" }
