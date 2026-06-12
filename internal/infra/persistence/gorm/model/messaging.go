package model

import (
	"time"

	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	"gorm.io/datatypes"
)

type MessageSubscription struct {
	ID                  uint   `gorm:"primaryKey"`
	Provider            string `gorm:"size:64;index;uniqueIndex:uq_message_subscription_provider_ref"`
	Title               string `gorm:"size:160"`
	SourceRef           string `gorm:"size:255;index;uniqueIndex:uq_message_subscription_provider_ref"`
	Enabled             bool   `gorm:"default:true"`
	FilterID            uint   `gorm:"index"`
	Filter              *MessageSubscriptionFilter
	Assignments         []MessageSubscriptionAssignment `gorm:"foreignKey:SubscriptionID;constraint:OnDelete:CASCADE"`
	BackfillLimit       int                             `gorm:"default:0"`
	PollIntervalSeconds int                             `gorm:"default:0"`
	CollectFrom         time.Time
	LastCollectedAt     *time.Time
	NextCollectAt       *time.Time     `gorm:"index"`
	LastCollectError    *string        `gorm:"type:text"`
	Config              datatypes.JSON `gorm:"type:json;default:'{}'"`
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func (MessageSubscription) TableName() string { return "message_subscriptions" }

type MessageSubscriptionAssignment struct {
	ID             uint                 `gorm:"primaryKey"`
	SubscriptionID uint                 `gorm:"index;uniqueIndex:uq_message_subscription_assignment"`
	Subscription   *MessageSubscription `gorm:"foreignKey:SubscriptionID;constraint:OnDelete:CASCADE"`
	FilterID       uint                 `gorm:"index;uniqueIndex:uq_message_subscription_assignment"`
	Filter         *MessageSubscriptionFilter
	ResearchTeamID uint          `gorm:"index;uniqueIndex:uq_message_subscription_assignment"`
	ResearchTeam   *ResearchTeam `gorm:"foreignKey:ResearchTeamID;constraint:OnDelete:RESTRICT"`
	Enabled        bool          `gorm:"default:true;index"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (MessageSubscriptionAssignment) TableName() string {
	return "message_subscription_assignments"
}

type MessageSubscriptionFilter struct {
	ID             uint   `gorm:"primaryKey"`
	Name           string `gorm:"size:160"`
	Description    string `gorm:"type:text"`
	PromptTemplate string `gorm:"type:text"`
	ProviderID     *uint  `gorm:"index"`
	Provider       *AiProvider
	Model          *string `gorm:"size:255"`
	Enabled        bool    `gorm:"default:true;index"`
	IsDefault      bool    `gorm:"default:false;index"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (MessageSubscriptionFilter) TableName() string { return "message_subscription_filters" }

type IngestedMessage struct {
	ID              uint                       `gorm:"primaryKey"`
	SubscriptionID  uint                       `gorm:"index;uniqueIndex:uq_ingested_subscription_message"`
	Subscription    *MessageSubscription       `gorm:"foreignKey:SubscriptionID;constraint:OnDelete:CASCADE"`
	Provider        string                     `gorm:"size:64;index"`
	SourceMessageID string                     `gorm:"size:255;uniqueIndex:uq_ingested_subscription_message"`
	MessageTime     time.Time                  `gorm:"index"`
	Text            string                     `gorm:"type:text"`
	Raw             datatypes.JSON             `gorm:"type:json;default:'{}'"`
	FilterDecision  *domainkernel.NewsDecision `gorm:"size:32"`
	FilterReason    *string                    `gorm:"type:text"`
	FilterStatus    string                     `gorm:"size:32;default:unfiltered;index"`
	RelatedSymbols  datatypes.JSON             `gorm:"type:json;default:'[]'"`
	FilteredAt      *time.Time
	FilterID        *uint `gorm:"index"`
	Filter          *MessageSubscriptionFilter
	FilterResults   []IngestedMessageFilterResult `gorm:"foreignKey:MessageID;constraint:OnDelete:CASCADE"`
	FeedbackLabel   *string                       `gorm:"size:32;index"`
	FeedbackComment *string                       `gorm:"type:text"`
	FeedbackAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (IngestedMessage) TableName() string { return "ingested_messages" }

type IngestedMessageFilterResult struct {
	ID             uint                 `gorm:"primaryKey"`
	MessageID      uint                 `gorm:"index;uniqueIndex:uq_ingested_message_filter_result"`
	Message        *IngestedMessage     `gorm:"foreignKey:MessageID;constraint:OnDelete:CASCADE"`
	SubscriptionID uint                 `gorm:"index"`
	Subscription   *MessageSubscription `gorm:"foreignKey:SubscriptionID;constraint:OnDelete:CASCADE"`
	AssignmentID   *uint                `gorm:"index"`
	Assignment     *MessageSubscriptionAssignment
	FilterID       uint                       `gorm:"index;uniqueIndex:uq_ingested_message_filter_result"`
	Filter         *MessageSubscriptionFilter `gorm:"foreignKey:FilterID"`
	ResearchTeamID uint                       `gorm:"index;uniqueIndex:uq_ingested_message_filter_result"`
	ResearchTeam   *ResearchTeam              `gorm:"foreignKey:ResearchTeamID;constraint:OnDelete:RESTRICT"`
	FilterDecision *domainkernel.NewsDecision `gorm:"size:32"`
	FilterReason   *string                    `gorm:"type:text"`
	FilterStatus   string                     `gorm:"size:32;default:unfiltered;index"`
	RelatedSymbols datatypes.JSON             `gorm:"type:json;default:'[]'"`
	FilteredAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (IngestedMessageFilterResult) TableName() string {
	return "ingested_message_filter_results"
}

type PlatformAdapter struct {
	ID          uint           `gorm:"primaryKey"`
	Provider    string         `gorm:"size:64;index;uniqueIndex:uq_platform_adapter_provider_name"`
	DisplayName string         `gorm:"size:160;uniqueIndex:uq_platform_adapter_provider_name"`
	Enabled     bool           `gorm:"default:true"`
	Config      datatypes.JSON `gorm:"type:json;default:'{}'"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (PlatformAdapter) TableName() string { return "platform_adapters" }
