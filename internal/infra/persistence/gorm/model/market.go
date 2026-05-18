package model

import (
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/datatypes"
)

type MarketSymbol struct {
	Code       string     `gorm:"size:16;primaryKey"`
	Name       string     `gorm:"size:64;index"`
	Exchange   string     `gorm:"size:16;index"`
	Industry   *string    `gorm:"size:128"`
	ListedDate *time.Time `gorm:"type:date"`
	Active     bool       `gorm:"default:true"`
	UpdatedAt  time.Time
}

func (MarketSymbol) TableName() string { return "market_symbols" }

type DailyBar struct {
	ID        uint            `gorm:"primaryKey"`
	Code      string          `gorm:"size:16;index;uniqueIndex:uq_daily_bar_code_date"`
	TradeDate time.Time       `gorm:"type:date;not null;index;uniqueIndex:uq_daily_bar_code_date"`
	Open      decimal.Decimal `gorm:"type:numeric(18,4)"`
	High      decimal.Decimal `gorm:"type:numeric(18,4)"`
	Low       decimal.Decimal `gorm:"type:numeric(18,4)"`
	Close     decimal.Decimal `gorm:"type:numeric(18,4)"`
	Volume    decimal.Decimal `gorm:"type:numeric(24,4)"`
	Amount    decimal.Decimal `gorm:"type:numeric(24,4)"`
	Provider  string          `gorm:"size:64;default:adata"`
}

func (DailyBar) TableName() string { return "daily_bars" }

type RealtimeQuote struct {
	ID        uint            `gorm:"primaryKey"`
	Code      string          `gorm:"size:16;index"`
	QuoteTime time.Time       `gorm:"not null;index"`
	Price     decimal.Decimal `gorm:"type:numeric(18,4)"`
	ChangePct decimal.Decimal `gorm:"type:numeric(10,4)"`
	Volume    decimal.Decimal `gorm:"type:numeric(24,4)"`
	Amount    decimal.Decimal `gorm:"type:numeric(24,4)"`
	Raw       datatypes.JSON  `gorm:"type:json;default:'{}'"`
	Provider  string          `gorm:"size:64;default:adata"`
}

func (RealtimeQuote) TableName() string { return "realtime_quotes" }

type WatchlistItem struct {
	ID             uint          `gorm:"primaryKey"`
	ResearchTeamID uint          `gorm:"not null;index;uniqueIndex:uq_watchlist_team_code"`
	ResearchTeam   *ResearchTeam `gorm:"foreignKey:ResearchTeamID;constraint:OnDelete:CASCADE"`
	Code           string        `gorm:"size:16;index;uniqueIndex:uq_watchlist_team_code"`
	Note           *string       `gorm:"type:text"`
	Active         bool          `gorm:"default:true"`
	CreatedAt      time.Time
}

func (WatchlistItem) TableName() string { return "watchlist_items" }
