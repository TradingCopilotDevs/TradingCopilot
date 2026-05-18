package model

import (
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	"time"

	"gorm.io/datatypes"
)

type WakePlan struct {
	ID                   uint          `gorm:"primaryKey"`
	ResearchTeamID       uint          `gorm:"not null;index"`
	ResearchTeam         *ResearchTeam `gorm:"foreignKey:ResearchTeamID;constraint:OnDelete:CASCADE"`
	MeetingID            *uint
	Meeting              *Meeting                     `gorm:"foreignKey:MeetingID;constraint:OnDelete:CASCADE"`
	TriggerType          domainkernel.WakeTriggerType `gorm:"size:32;index"`
	TriggerConfig        datatypes.JSON               `gorm:"type:json;default:'{}'"`
	Reason               string                       `gorm:"type:text"`
	SourceMeetingEventID *uint
	SourceMeetingEvent   *MeetingEvent               `gorm:"foreignKey:SourceMeetingEventID;constraint:OnDelete:SET NULL"`
	SourceRoleKey        *string                     `gorm:"size:64"`
	Status               domainkernel.WakePlanStatus `gorm:"size:32;default:active"`
	NextCheckAt          *time.Time                  `gorm:"index"`
	FiredAt              *time.Time
	LastRunAt            *time.Time
	ResultSummary        *string `gorm:"type:text"`
	CreatedAt            time.Time
}

func (WakePlan) TableName() string { return "wake_plans" }
