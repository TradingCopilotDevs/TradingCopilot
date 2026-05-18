package paper

import (
	"context"
	domainpaper "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/paper"
	"time"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/marketdata/ashare"
	persistmodel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/model"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	DefaultPaperRiskConfigName = "默认A股模拟盘风控"
	DefaultPaperAccountName    = "默认模拟账户"
)

var legacyPaperRiskConfigNames = []string{
	"default",
	"默认A股模拟盘风控",
}

var legacyPaperAccountNames = []string{
	"Default paper account",
	"默认模拟账户",
}

var appTZ = time.FixedZone("Asia/Shanghai", 8*3600)
var paperNow = time.Now

var boardSubstitutes = map[string]struct {
	Code     string
	Name     string
	Exchange string
}{
	ashare.BoardSTAR:    {Code: "588000", Name: "科创50ETF", Exchange: "SH"},
	ashare.BoardChiNext: {Code: "159915", Name: "创业板ETF", Exchange: "SZ"},
	ashare.BoardSHMain:  {Code: "510300", Name: "沪深300ETF", Exchange: "SH"},
	ashare.BoardSZMain:  {Code: "159919", Name: "沪深300ETF", Exchange: "SZ"},
}

func q4(value decimal.Decimal) decimal.Decimal { return value.Round(4) }

func dbContext(db *gorm.DB) context.Context {
	if db != nil && db.Statement != nil && db.Statement.Context != nil {
		return db.Statement.Context
	}
	return context.Background()
}
func q2(value decimal.Decimal) decimal.Decimal { return value.Round(2) }
func strPtr(v string) *string                  { return &v }

func IsTradingTime(moment time.Time) bool {
	t := moment.In(appTZ)
	if t.Weekday() == time.Saturday || t.Weekday() == time.Sunday {
		return false
	}
	hm := t.Hour()*100 + t.Minute()
	return (hm >= 930 && hm <= 1130) || (hm >= 1300 && hm <= 1500)
}

func NextTradingSessionStart(moment time.Time) time.Time {
	t := moment.In(appTZ)
	day := t.YearDay()
	sessions := []time.Time{
		time.Date(t.Year(), t.Month(), t.Day(), 9, 30, 0, 0, appTZ),
		time.Date(t.Year(), t.Month(), t.Day(), 13, 0, 0, 0, appTZ),
	}
	for _, start := range sessions {
		if !t.After(start) {
			return start
		}
	}
	next := t.AddDate(0, 0, 1)
	for next.Weekday() == time.Saturday || next.Weekday() == time.Sunday || next.YearDay() == day && next.Day() == t.Day() {
		next = next.AddDate(0, 0, 1)
	}
	return time.Date(next.Year(), next.Month(), next.Day(), 9, 30, 0, 0, appTZ)
}

func CreateAccountRecord(db *gorm.DB, payload map[string]any) (*domainpaper.Account, error) {
	cash := decimalFromPayload(payload["cash"])
	if cash.IsZero() {
		cash = decimalFromPayload(payload["initial_cash"])
	}
	account := domainpaper.Account{
		Name:        stringFromPayload(payload["name"]),
		InitialCash: cash,
		Cash:        cash,
		Active:      boolFromPayload(payload["active"], true),
	}
	if account.Name == "" {
		account.Name = DefaultPaperAccountName
	}
	if id, ok := uintFromPayload(payload["risk_config_id"]); ok {
		account.RiskConfigID = &id
	}
	if err := gormrepo.NewPaperRepository(db).SaveAccount(dbContext(db), &account); err != nil {
		return nil, err
	}
	return &account, nil
}

func RiskEnabled(db *gorm.DB, account domainpaper.Account) (*domainpaper.RiskConfig, error) {
	if account.RiskConfigID == nil {
		return nil, nil
	}
	var row persistmodel.RiskConfig
	if err := db.First(&row, *account.RiskConfigID).Error; err != nil {
		return nil, nil
	}
	cfg := riskConfigModelToDomain(row)
	if !cfg.Enabled {
		return nil, nil
	}
	return &cfg, nil
}

func DefaultPaperRiskConfig() domainpaper.RiskConfig {
	return domainpaper.RiskConfig{
		Name:            DefaultPaperRiskConfigName,
		InitialCash:     decimal.NewFromInt(100000),
		MaxPositionPct:  decimal.RequireFromString("0.30"),
		MaxOrderPct:     decimal.RequireFromString("0.20"),
		AllowShort:      false,
		AllowMargin:     false,
		AllowSHMain:     true,
		AllowSZMain:     true,
		AllowBJ:         false,
		AllowSTAR:       false,
		AllowChiNext:    false,
		AllowETFLOF:     true,
		CommissionRate:  decimal.RequireFromString("0.0001"),
		MinCommission:   decimal.RequireFromString("5.00"),
		StampDutyRate:   decimal.RequireFromString("0.001"),
		TransferFeeRate: decimal.RequireFromString("0.00001"),
		Enabled:         true,
	}
}

func firstRiskConfigByName(db *gorm.DB, name string) (domainpaper.RiskConfig, error) {
	var row persistmodel.RiskConfig
	err := db.Where("name = ?", name).First(&row).Error
	return riskConfigModelToDomain(row), err
}

func createRiskConfigRecord(db *gorm.DB, cfg *domainpaper.RiskConfig) error {
	return gormrepo.NewPaperRepository(db).CreateRiskConfig(dbContext(db), cfg)
}

func saveRiskConfigRecord(db *gorm.DB, cfg *domainpaper.RiskConfig) error {
	return gormrepo.NewPaperRepository(db).SaveRiskConfig(dbContext(db), cfg)
}
