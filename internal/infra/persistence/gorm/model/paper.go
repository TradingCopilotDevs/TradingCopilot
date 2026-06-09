package model

import (
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	"time"

	"github.com/shopspring/decimal"
)

type RiskConfig struct {
	ID              uint            `gorm:"primaryKey"`
	Name            string          `gorm:"size:128;uniqueIndex"`
	InitialCash     decimal.Decimal `gorm:"type:numeric(18,2)"`
	MaxPositionPct  decimal.Decimal `gorm:"type:numeric(8,4)"`
	MaxOrderPct     decimal.Decimal `gorm:"type:numeric(8,4)"`
	AllowShort      bool            `gorm:"default:false"`
	AllowMargin     bool            `gorm:"default:false"`
	AllowSHMain     bool            `gorm:"default:true"`
	AllowSZMain     bool            `gorm:"default:true"`
	AllowBJ         bool            `gorm:"default:false"`
	AllowSTAR       bool            `gorm:"default:false"`
	AllowChiNext    bool            `gorm:"column:allow_chinext;default:false"`
	AllowETFLOF     bool            `gorm:"column:allow_etf_lof;default:true"`
	CommissionRate  decimal.Decimal `gorm:"type:numeric(10,6)"`
	MinCommission   decimal.Decimal `gorm:"type:numeric(18,2)"`
	StampDutyRate   decimal.Decimal `gorm:"type:numeric(10,6)"`
	TransferFeeRate decimal.Decimal `gorm:"type:numeric(10,6)"`
	Enabled         bool            `gorm:"default:false"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (RiskConfig) TableName() string { return "risk_configs" }

type PaperAccount struct {
	ID           uint            `gorm:"primaryKey"`
	Name         string          `gorm:"size:128;uniqueIndex"`
	InitialCash  decimal.Decimal `gorm:"type:numeric(18,2)"`
	Cash         decimal.Decimal `gorm:"type:numeric(18,2)"`
	RiskConfigID *uint
	RiskConfig   *RiskConfig `gorm:"foreignKey:RiskConfigID;constraint:OnDelete:SET NULL"`
	Active       bool        `gorm:"default:true"`
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (PaperAccount) TableName() string { return "paper_accounts" }

type PaperOrder struct {
	ID                   uint          `gorm:"primaryKey"`
	AccountID            uint          `gorm:"index"`
	Account              *PaperAccount `gorm:"foreignKey:AccountID;constraint:OnDelete:CASCADE"`
	MeetingID            *uint
	Meeting              *Meeting               `gorm:"foreignKey:MeetingID;constraint:OnDelete:SET NULL"`
	Code                 string                 `gorm:"size:16;index"`
	Side                 domainkernel.OrderSide `gorm:"size:16"`
	Quantity             int
	Status               domainkernel.OrderStatus `gorm:"size:32;default:suggested"`
	SuggestedPrice       decimal.Decimal          `gorm:"type:numeric(18,4)"`
	FilledPrice          decimal.Decimal          `gorm:"type:numeric(18,4)"`
	Reason               *string                  `gorm:"type:text"`
	SubmittedAt          *time.Time
	ExecuteAfter         *time.Time `gorm:"index"`
	ExpireAt             *time.Time
	SourceMeetingEventID *uint
	SourceMeetingEvent   *MeetingEvent   `gorm:"foreignKey:SourceMeetingEventID;constraint:OnDelete:SET NULL"`
	ExecutionNote        *string         `gorm:"type:text"`
	Commission           decimal.Decimal `gorm:"type:numeric(18,4)"`
	StampDuty            decimal.Decimal `gorm:"type:numeric(18,4)"`
	TransferFee          decimal.Decimal `gorm:"type:numeric(18,4)"`
	NetAmount            decimal.Decimal `gorm:"type:numeric(18,4)"`
	CreatedAt            time.Time
	FilledAt             *time.Time
}

func (PaperOrder) TableName() string { return "paper_orders" }

type PaperPosition struct {
	ID            uint          `gorm:"primaryKey"`
	AccountID     uint          `gorm:"index;uniqueIndex:uq_position_account_code"`
	Account       *PaperAccount `gorm:"foreignKey:AccountID;constraint:OnDelete:CASCADE"`
	Code          string        `gorm:"size:16;index;uniqueIndex:uq_position_account_code"`
	Quantity      int
	AvgCost       decimal.Decimal `gorm:"type:numeric(18,4)"`
	CostAmount    decimal.Decimal `gorm:"type:numeric(18,4)"`
	LastPrice     decimal.Decimal `gorm:"type:numeric(18,4)"`
	MarketValue   decimal.Decimal `gorm:"type:numeric(18,4)"`
	UnrealizedPNL decimal.Decimal `gorm:"type:numeric(18,4)"`
	RealizedPNL   decimal.Decimal `gorm:"type:numeric(18,4)"`
	UpdatedAt     time.Time
}

func (PaperPosition) TableName() string { return "paper_positions" }

type PaperFill struct {
	ID          uint                   `gorm:"primaryKey"`
	OrderID     uint                   `gorm:"index"`
	Order       *PaperOrder            `gorm:"foreignKey:OrderID;constraint:OnDelete:CASCADE"`
	AccountID   uint                   `gorm:"index"`
	Account     *PaperAccount          `gorm:"foreignKey:AccountID;constraint:OnDelete:CASCADE"`
	Code        string                 `gorm:"size:16;index"`
	Side        domainkernel.OrderSide `gorm:"size:16"`
	Quantity    int
	Price       decimal.Decimal `gorm:"type:numeric(18,4)"`
	GrossAmount decimal.Decimal `gorm:"type:numeric(18,4)"`
	Commission  decimal.Decimal `gorm:"type:numeric(18,4)"`
	StampDuty   decimal.Decimal `gorm:"type:numeric(18,4)"`
	TransferFee decimal.Decimal `gorm:"type:numeric(18,4)"`
	NetAmount   decimal.Decimal `gorm:"type:numeric(18,4)"`
	RealizedPNL decimal.Decimal `gorm:"type:numeric(18,4)"`
	FilledAt    time.Time
}

func (PaperFill) TableName() string { return "paper_fills" }

type PaperEquitySnapshot struct {
	ID            uint          `gorm:"primaryKey"`
	AccountID     uint          `gorm:"index"`
	Account       *PaperAccount `gorm:"foreignKey:AccountID;constraint:OnDelete:CASCADE"`
	SnapshotTime  time.Time
	Cash          decimal.Decimal `gorm:"type:numeric(18,4)"`
	MarketValue   decimal.Decimal `gorm:"type:numeric(18,4)"`
	TotalEquity   decimal.Decimal `gorm:"type:numeric(18,4)"`
	UnrealizedPNL decimal.Decimal `gorm:"type:numeric(18,4)"`
	RealizedPNL   decimal.Decimal `gorm:"type:numeric(18,4)"`
	DailyPNL      decimal.Decimal `gorm:"type:numeric(18,4)"`
}

func (PaperEquitySnapshot) TableName() string { return "paper_equity_snapshots" }

type PaperCorporateAction struct {
	ID             uint            `gorm:"primaryKey"`
	AccountID      uint            `gorm:"index"`
	Account        *PaperAccount   `gorm:"foreignKey:AccountID;constraint:OnDelete:CASCADE"`
	Code           string          `gorm:"size:16;index"`
	ActionType     string          `gorm:"size:32;index"`
	ExDate         time.Time       `gorm:"index"`
	CashPerShare   decimal.Decimal `gorm:"type:numeric(18,6)"`
	ShareRatio     decimal.Decimal `gorm:"type:numeric(18,6)"`
	AffectedShares int
	CashAmount     decimal.Decimal `gorm:"type:numeric(18,4)"`
	Status         string          `gorm:"size:32;index;default:applied"`
	Note           *string         `gorm:"type:text"`
	AppliedAt      *time.Time
	CreatedAt      time.Time
}

func (PaperCorporateAction) TableName() string { return "paper_corporate_actions" }
