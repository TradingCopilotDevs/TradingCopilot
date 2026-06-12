package messaging

import (
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
)

const (
	ProviderTelegramChannel = "telegram_channel"
	ProviderTelegramBot     = "telegram_bot"
	ProviderRSSFeed         = "rss_feed"
)

const (
	FilterStatusUnfiltered = "unfiltered"
	FilterStatusFiltering  = "filtering"
	FilterStatusFiltered   = "filtered"
	FilterStatusFailed     = "failed"
)

const (
	FeedbackHelpful       = "helpful"
	FeedbackNoise         = "noise"
	FeedbackMisclassified = "misclassified"
	FeedbackNeutral       = "neutral"
)

type MessageSubscription struct {
	ID                  uint
	Provider            string
	Title               string
	SourceRef           string
	Enabled             bool
	FilterID            uint
	Filter              *MessageSubscriptionFilter
	TeamIDs             []uint
	Assignments         []MessageSubscriptionAssignment
	BackfillLimit       int
	PollIntervalSeconds int
	CollectFrom         time.Time
	LastCollectedAt     *time.Time
	NextCollectAt       *time.Time
	LastCollectError    *string
	Config              kernel.JSON
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

type MessageSubscriptionAssignment struct {
	ID             uint
	SubscriptionID uint
	FilterID       uint
	Filter         *MessageSubscriptionFilter
	ResearchTeamID uint
	Enabled        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type MessageSubscriptionFilter struct {
	ID             uint
	Name           string
	Description    string
	PromptTemplate string
	ProviderID     *uint
	Enabled        bool
	IsDefault      bool
	Model          *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type IngestedMessage struct {
	ID              uint
	SubscriptionID  uint
	Subscription    *MessageSubscription
	Provider        string
	SourceMessageID string
	MessageTime     time.Time
	Text            string
	Raw             kernel.JSON
	FilterDecision  *kernel.NewsDecision
	FilterReason    *string
	FilterStatus    string
	RelatedSymbols  kernel.JSON
	FilteredAt      *time.Time
	FilterID        *uint
	FilterResults   []IngestedMessageFilterResult
	FeedbackLabel   *string
	FeedbackComment *string
	FeedbackAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type IngestedMessageFilterResult struct {
	ID             uint
	MessageID      uint
	SubscriptionID uint
	AssignmentID   *uint
	FilterID       uint
	Filter         *MessageSubscriptionFilter
	ResearchTeamID uint
	FilterDecision *kernel.NewsDecision
	FilterReason   *string
	FilterStatus   string
	RelatedSymbols kernel.JSON
	FilteredAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type PlatformAdapter struct {
	ID          uint
	Provider    string
	DisplayName string
	Enabled     bool
	Config      kernel.JSON
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
