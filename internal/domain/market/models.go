package market

import (
	"time"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	"github.com/shopspring/decimal"
)

type Symbol struct {
	Code       string
	Name       string
	Exchange   string
	Industry   *string
	ListedDate *time.Time
	Active     bool
	UpdatedAt  time.Time
}

type DailyBar struct {
	ID        uint
	Code      string
	TradeDate time.Time
	Open      decimal.Decimal
	High      decimal.Decimal
	Low       decimal.Decimal
	Close     decimal.Decimal
	Volume    decimal.Decimal
	Amount    decimal.Decimal
	Provider  string
}

type RealtimeQuote struct {
	ID        uint
	Code      string
	QuoteTime time.Time
	Price     decimal.Decimal
	ChangePct decimal.Decimal
	Volume    decimal.Decimal
	Amount    decimal.Decimal
	Raw       kernel.JSON
	Provider  string
}

type WatchlistItem struct {
	ID             uint
	ResearchTeamID uint
	Code           string
	Note           *string
	Active         bool
	CreatedAt      time.Time
}
