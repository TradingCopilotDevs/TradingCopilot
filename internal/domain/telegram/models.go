package telegram

import (
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
)

type Channel struct {
	ID            uint
	Title         string
	ChannelRef    string
	Enabled       bool
	BackfillLimit int
	CollectFrom   time.Time
	CreatedAt     time.Time
}

type Message struct {
	ID                 uint
	ChannelID          uint
	Channel            *Channel
	MessageID          int64
	MessageTime        time.Time
	Text               string
	Raw                kernel.JSON
	FilterDecision     *kernel.NewsDecision
	FilterReason       *string
	RelatedSymbols     kernel.JSON
	FilteredAt         *time.Time
	FilterModelRoleKey *string
	CreatedAt          time.Time
	UpdatedAt          time.Time
}
