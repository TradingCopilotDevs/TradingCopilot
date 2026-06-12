package model

import (
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/datatypes"
)

type PredictionEvent struct {
	ID              uint            `gorm:"primaryKey"`
	Provider        string          `gorm:"size:32;not null;default:polymarket;uniqueIndex:uq_prediction_event_provider_external"`
	ExternalEventID string          `gorm:"size:64;not null;uniqueIndex:uq_prediction_event_provider_external"`
	Slug            string          `gorm:"size:240;index"`
	Title           string          `gorm:"size:512;index"`
	Description     string          `gorm:"type:text"`
	Category        *string         `gorm:"size:128;index"`
	Tags            datatypes.JSON  `gorm:"type:json;default:'[]'"`
	Active          bool            `gorm:"default:true;index"`
	Closed          bool            `gorm:"default:false;index"`
	EndDate         *time.Time      `gorm:"index"`
	Volume          decimal.Decimal `gorm:"type:numeric(24,8)"`
	Liquidity       decimal.Decimal `gorm:"type:numeric(24,8)"`
	OpenInterest    decimal.Decimal `gorm:"type:numeric(24,8)"`
	Raw             datatypes.JSON  `gorm:"type:json;default:'{}'"`
	UpdatedAt       time.Time
	CreatedAt       time.Time
}

func (PredictionEvent) TableName() string { return "prediction_events" }

type PredictionMarket struct {
	ID               uint             `gorm:"primaryKey"`
	EventID          *uint            `gorm:"index"`
	Event            *PredictionEvent `gorm:"foreignKey:EventID;constraint:OnDelete:SET NULL"`
	Provider         string           `gorm:"size:32;not null;default:polymarket;uniqueIndex:uq_prediction_market_provider_external"`
	ExternalMarketID string           `gorm:"size:64;not null;uniqueIndex:uq_prediction_market_provider_external"`
	ConditionID      string           `gorm:"size:128;index"`
	Question         string           `gorm:"size:512;index"`
	Slug             string           `gorm:"size:240;index"`
	Description      string           `gorm:"type:text"`
	Outcomes         datatypes.JSON   `gorm:"type:json;default:'[]'"`
	OutcomePrices    datatypes.JSON   `gorm:"type:json;default:'[]'"`
	CLOBTokenIDs     datatypes.JSON   `gorm:"type:json;default:'[]'"`
	EnableOrderBook  bool             `gorm:"default:false;index"`
	BestBid          decimal.Decimal  `gorm:"type:numeric(18,8)"`
	BestAsk          decimal.Decimal  `gorm:"type:numeric(18,8)"`
	LastTradePrice   decimal.Decimal  `gorm:"type:numeric(18,8)"`
	Spread           decimal.Decimal  `gorm:"type:numeric(18,8)"`
	Volume           decimal.Decimal  `gorm:"type:numeric(24,8)"`
	Liquidity        decimal.Decimal  `gorm:"type:numeric(24,8)"`
	Active           bool             `gorm:"default:true;index"`
	Closed           bool             `gorm:"default:false;index"`
	Restricted       bool             `gorm:"default:false;index"`
	EndDate          *time.Time       `gorm:"index"`
	Raw              datatypes.JSON   `gorm:"type:json;default:'{}'"`
	UpdatedAt        time.Time
	CreatedAt        time.Time
}

func (PredictionMarket) TableName() string { return "prediction_markets" }

type PredictionMarketQuote struct {
	ID        uint              `gorm:"primaryKey"`
	MarketID  uint              `gorm:"not null;index"`
	Market    *PredictionMarket `gorm:"foreignKey:MarketID;constraint:OnDelete:CASCADE"`
	TokenID   string            `gorm:"size:128;index"`
	Outcome   string            `gorm:"size:64"`
	QuoteTime time.Time         `gorm:"not null;index"`
	BestBid   decimal.Decimal   `gorm:"type:numeric(18,8)"`
	BestAsk   decimal.Decimal   `gorm:"type:numeric(18,8)"`
	Midpoint  decimal.Decimal   `gorm:"type:numeric(18,8)"`
	LastPrice decimal.Decimal   `gorm:"type:numeric(18,8)"`
	Spread    decimal.Decimal   `gorm:"type:numeric(18,8)"`
	Raw       datatypes.JSON    `gorm:"type:json;default:'{}'"`
	Provider  string            `gorm:"size:32;default:polymarket"`
}

func (PredictionMarketQuote) TableName() string { return "prediction_market_quotes" }

type PredictionMarketMatch struct {
	ID                uint              `gorm:"primaryKey"`
	MessageID         *uint             `gorm:"index"`
	Message           *IngestedMessage  `gorm:"foreignKey:MessageID;constraint:OnDelete:SET NULL"`
	MarketID          uint              `gorm:"not null;index"`
	Market            *PredictionMarket `gorm:"foreignKey:MarketID;constraint:OnDelete:CASCADE"`
	Query             string            `gorm:"size:512;index"`
	NewsSnippet       string            `gorm:"type:text"`
	CandidateSnapshot datatypes.JSON    `gorm:"type:json;default:'{}'"`
	Score             decimal.Decimal   `gorm:"type:numeric(10,6);index"`
	ScoreBreakdown    datatypes.JSON    `gorm:"type:json;default:'{}'"`
	Status            string            `gorm:"size:32;default:candidate;index"`
	Reason            string            `gorm:"type:text"`
	ReviewedBy        *string           `gorm:"size:128"`
	ReviewedAt        *time.Time
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

func (PredictionMarketMatch) TableName() string { return "prediction_market_matches" }

type PredictionWatchlistItem struct {
	ID             uint              `gorm:"primaryKey"`
	ResearchTeamID uint              `gorm:"not null;index;uniqueIndex:uq_prediction_watchlist_team_market"`
	ResearchTeam   *ResearchTeam     `gorm:"foreignKey:ResearchTeamID;constraint:OnDelete:CASCADE"`
	MarketID       uint              `gorm:"not null;index;uniqueIndex:uq_prediction_watchlist_team_market"`
	Market         *PredictionMarket `gorm:"foreignKey:MarketID;constraint:OnDelete:CASCADE"`
	Note           *string           `gorm:"type:text"`
	Active         bool              `gorm:"default:true"`
	CreatedAt      time.Time
}

func (PredictionWatchlistItem) TableName() string { return "prediction_watchlist_items" }
