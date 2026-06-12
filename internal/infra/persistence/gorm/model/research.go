package model

import (
	"time"

	"gorm.io/datatypes"
)

type ResearchTeam struct {
	ID             uint          `gorm:"primaryKey"`
	Name           string        `gorm:"size:128;uniqueIndex"`
	Description    string        `gorm:"type:text"`
	PaperAccountID *uint         `gorm:"uniqueIndex"`
	PaperAccount   *PaperAccount `gorm:"foreignKey:PaperAccountID;constraint:OnDelete:RESTRICT"`
	AssetClass     string        `gorm:"size:32;default:a_share;index"`
	Active         bool          `gorm:"default:true;index"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (ResearchTeam) TableName() string { return "research_teams" }

type ResearchTeamRole struct {
	ID             uint          `gorm:"primaryKey"`
	ResearchTeamID uint          `gorm:"index;uniqueIndex:uq_research_team_role"`
	ResearchTeam   *ResearchTeam `gorm:"foreignKey:ResearchTeamID;constraint:OnDelete:CASCADE"`
	Key            string        `gorm:"size:64;uniqueIndex:uq_research_team_role"`
	Name           string        `gorm:"size:128"`
	Responsibility string        `gorm:"type:text"`
	PromptTemplate string        `gorm:"type:text"`
	ProviderID     *uint
	Provider       *AiProvider    `gorm:"foreignKey:ProviderID"`
	Model          *string        `gorm:"size:128"`
	ToolNames      datatypes.JSON `gorm:"type:json;default:'[]'"`
	SkillNames     datatypes.JSON `gorm:"type:json;default:'[]'"`
	Enabled        bool           `gorm:"default:true"`
	SortOrder      int            `gorm:"default:100"`
}

func (ResearchTeamRole) TableName() string { return "research_team_roles" }

type MessageSubscriptionResearchTeam struct {
	MessageSubscriptionID uint                 `gorm:"primaryKey"`
	MessageSubscription   *MessageSubscription `gorm:"foreignKey:MessageSubscriptionID;constraint:OnDelete:CASCADE"`
	ResearchTeamID        uint                 `gorm:"primaryKey;index"`
	ResearchTeam          *ResearchTeam        `gorm:"foreignKey:ResearchTeamID;constraint:OnDelete:RESTRICT"`
	CreatedAt             time.Time
}

func (MessageSubscriptionResearchTeam) TableName() string {
	return "message_subscription_research_teams"
}
