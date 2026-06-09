package paper

import (
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	"github.com/shopspring/decimal"
)

type RiskConfig struct {
	ID              uint
	Name            string
	InitialCash     decimal.Decimal
	MaxPositionPct  decimal.Decimal
	MaxOrderPct     decimal.Decimal
	AllowShort      bool
	AllowMargin     bool
	AllowSHMain     bool
	AllowSZMain     bool
	AllowBJ         bool
	AllowSTAR       bool
	AllowChiNext    bool `gorm:"column:allow_chinext"`
	AllowETFLOF     bool `gorm:"column:allow_etf_lof"`
	CommissionRate  decimal.Decimal
	MinCommission   decimal.Decimal
	StampDutyRate   decimal.Decimal
	TransferFeeRate decimal.Decimal
	Enabled         bool
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type RiskConfigInput struct {
	Name            *string
	InitialCash     *decimal.Decimal
	MaxPositionPct  *decimal.Decimal
	MaxOrderPct     *decimal.Decimal
	AllowShort      *bool
	AllowMargin     *bool
	AllowSHMain     *bool
	AllowSZMain     *bool
	AllowBJ         *bool
	AllowSTAR       *bool
	AllowChiNext    *bool
	AllowETFLOF     *bool
	CommissionRate  *decimal.Decimal
	MinCommission   *decimal.Decimal
	StampDutyRate   *decimal.Decimal
	TransferFeeRate *decimal.Decimal
	Enabled         *bool
	AccountIDs      []uint
	AccountIDsSet   bool
}

func (r RiskConfig) GetAllowSHMain() bool  { return r.AllowSHMain }
func (r RiskConfig) GetAllowSZMain() bool  { return r.AllowSZMain }
func (r RiskConfig) GetAllowBJ() bool      { return r.AllowBJ }
func (r RiskConfig) GetAllowSTAR() bool    { return r.AllowSTAR }
func (r RiskConfig) GetAllowChiNext() bool { return r.AllowChiNext }
func (r RiskConfig) GetAllowETFLOF() bool  { return r.AllowETFLOF }

type Account struct {
	ID           uint
	Name         string
	InitialCash  decimal.Decimal
	Cash         decimal.Decimal
	RiskConfigID *uint
	RiskConfig   *RiskConfig
	Active       bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type AccountInput struct {
	Name         string
	Cash         decimal.Decimal
	InitialCash  decimal.Decimal
	RiskConfigID *uint
	Active       *bool
}

type Order struct {
	ID                   uint
	AccountID            uint
	Account              *Account
	MeetingID            *uint
	Meeting              *domainmeeting.Meeting
	Code                 string
	Side                 kernel.OrderSide
	Quantity             int
	Status               kernel.OrderStatus
	SuggestedPrice       decimal.Decimal
	FilledPrice          decimal.Decimal
	Reason               *string
	SubmittedAt          *time.Time
	ExecuteAfter         *time.Time
	ExpireAt             *time.Time
	SourceMeetingEventID *uint
	SourceMeetingEvent   *domainmeeting.Event
	ExecutionNote        *string
	Commission           decimal.Decimal
	StampDuty            decimal.Decimal
	TransferFee          decimal.Decimal
	NetAmount            decimal.Decimal
	CreatedAt            time.Time
	FilledAt             *time.Time
}

type OrderInput struct {
	AccountID            uint
	MeetingID            *uint
	SourceMeetingEventID *uint
	Code                 string
	Side                 kernel.OrderSide
	Quantity             int
	SuggestedPrice       decimal.Decimal
	Reason               *string
	ExecuteAfter         *time.Time
	ExpireAt             *time.Time
	PositionPct          decimal.Decimal
	AllocationPct        decimal.Decimal
	MeetingSummary       string
	MeetingConclusion    string
}

type OrderFillInput struct {
	Price    decimal.Decimal
	Quantity int
}

type Position struct {
	ID            uint
	AccountID     uint
	Account       *Account
	Code          string
	Quantity      int
	AvgCost       decimal.Decimal
	CostAmount    decimal.Decimal
	LastPrice     decimal.Decimal
	MarketValue   decimal.Decimal
	UnrealizedPNL decimal.Decimal
	RealizedPNL   decimal.Decimal
	UpdatedAt     time.Time
}

type Fill struct {
	ID          uint
	OrderID     uint
	Order       *Order
	AccountID   uint
	Account     *Account
	Code        string
	Side        kernel.OrderSide
	Quantity    int
	Price       decimal.Decimal
	GrossAmount decimal.Decimal
	Commission  decimal.Decimal
	StampDuty   decimal.Decimal
	TransferFee decimal.Decimal
	NetAmount   decimal.Decimal
	RealizedPNL decimal.Decimal
	FilledAt    time.Time
}

type EquitySnapshot struct {
	ID            uint
	AccountID     uint
	Account       *Account
	SnapshotTime  time.Time
	Cash          decimal.Decimal
	MarketValue   decimal.Decimal
	TotalEquity   decimal.Decimal
	UnrealizedPNL decimal.Decimal
	RealizedPNL   decimal.Decimal
	DailyPNL      decimal.Decimal
}

type CorporateAction struct {
	ID             uint
	AccountID      uint
	Account        *Account
	Code           string
	ActionType     string
	ExDate         time.Time
	CashPerShare   decimal.Decimal
	ShareRatio     decimal.Decimal
	AffectedShares int
	CashAmount     decimal.Decimal
	Status         string
	Note           *string
	AppliedAt      *time.Time
	CreatedAt      time.Time
}

type CorporateActionInput struct {
	AccountID    uint
	Code         string
	ActionType   string
	ExDate       time.Time
	CashPerShare decimal.Decimal
	ShareRatio   decimal.Decimal
	Note         *string
}
