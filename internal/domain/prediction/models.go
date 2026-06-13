package prediction

import (
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	"github.com/shopspring/decimal"
)

const ProviderPolymarket = "polymarket"

type Event struct {
	ID              uint
	Provider        string
	ExternalEventID string
	Slug            string
	Title           string
	Description     string
	Category        *string
	Tags            kernel.JSON
	Active          bool
	Closed          bool
	EndDate         *time.Time
	Volume          decimal.Decimal
	Liquidity       decimal.Decimal
	OpenInterest    decimal.Decimal
	Raw             kernel.JSON
	UpdatedAt       time.Time
	CreatedAt       time.Time
}

type Market struct {
	ID                   uint
	EventID              *uint
	EventExternalEventID string
	EventSlug            string
	EventTitle           string
	Provider             string
	ExternalMarketID     string
	ConditionID          string
	Question             string
	Slug                 string
	Description          string
	Outcomes             kernel.JSON
	OutcomePrices        kernel.JSON
	CLOBTokenIDs         kernel.JSON
	EnableOrderBook      bool
	BestBid              decimal.Decimal
	BestAsk              decimal.Decimal
	LastTradePrice       decimal.Decimal
	Spread               decimal.Decimal
	Volume               decimal.Decimal
	Liquidity            decimal.Decimal
	Active               bool
	Closed               bool
	Restricted           bool
	EndDate              *time.Time
	Raw                  kernel.JSON
	UpdatedAt            time.Time
	CreatedAt            time.Time
}

type Quote struct {
	ID        uint
	MarketID  uint
	TokenID   string
	Outcome   string
	QuoteTime time.Time
	BestBid   decimal.Decimal
	BestAsk   decimal.Decimal
	Midpoint  decimal.Decimal
	LastPrice decimal.Decimal
	Spread    decimal.Decimal
	Raw       kernel.JSON
	Provider  string
}

type Match struct {
	ID                uint
	MessageID         *uint
	MarketID          uint
	Query             string
	NewsSnippet       string
	CandidateSnapshot kernel.JSON
	Score             decimal.Decimal
	ScoreBreakdown    kernel.JSON
	Status            string
	Reason            string
	ReviewedBy        *string
	ReviewedAt        *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

type WatchlistItem struct {
	ID                   uint
	ResearchTeamID       uint
	MarketID             uint
	Note                 *string
	Active               bool
	SourceMeetingID      *uint
	SourceMeetingEventID *uint
	SourceRoleKey        *string
	CreatedAt            time.Time
}
