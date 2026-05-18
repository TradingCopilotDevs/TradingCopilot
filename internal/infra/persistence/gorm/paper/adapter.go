package paper

import (
	"context"
	domainpaper "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/paper"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type Service struct {
	db *gorm.DB
}

func NewService(db *gorm.DB) Service {
	return Service{db: db}
}

func (s Service) Overview(ctx context.Context) (map[string]any, error) {
	return Overview(s.db.WithContext(ctx))
}

func (s Service) CreateAccount(ctx context.Context, input domainpaper.AccountInput) (*domainpaper.Account, error) {
	return CreateAccountRecord(s.db.WithContext(ctx), accountInputPayload(input))
}

func (s Service) AccountPublic(ctx context.Context, account domainpaper.Account) map[string]any {
	metrics := ComputeAccountMetrics(s.db.WithContext(ctx), account, false)
	return map[string]any{
		"id": account.ID, "name": account.Name, "initial_cash": account.InitialCash, "cash": account.Cash,
		"risk_config_id": account.RiskConfigID, "active": account.Active, "market_value": metrics["market_value"],
		"total_equity": metrics["total_equity"], "unrealized_pnl": metrics["unrealized_pnl"], "realized_pnl": metrics["realized_pnl"],
		"total_return_pct": metrics["total_return_pct"], "position_count": metrics["position_count"], "pending_order_count": metrics["pending_order_count"],
	}
}

func (s Service) PositionsPublic(ctx context.Context, rows []domainpaper.Position) []map[string]any {
	return PositionsPublic(s.db.WithContext(ctx), rows)
}

func (s Service) FillsPublic(ctx context.Context, rows []domainpaper.Fill) []map[string]any {
	return FillsPublic(s.db.WithContext(ctx), rows)
}

func (s Service) Performance(ctx context.Context, account domainpaper.Account) map[string]any {
	return Performance(s.db.WithContext(ctx), account)
}

func (s Service) CreateOrder(ctx context.Context, input domainpaper.OrderInput) (*domainpaper.Order, error) {
	return CreateOrderFromSpec(s.db.WithContext(ctx), orderInputSpec(input))
}

func (s Service) FillOrder(ctx context.Context, row *domainpaper.Order, price decimal.Decimal) error {
	return FillOrder(s.db.WithContext(ctx), row, price)
}

func (s Service) OrdersPublic(ctx context.Context, rows []domainpaper.Order) []map[string]any {
	return OrdersPublic(s.db.WithContext(ctx), rows)
}

func (s Service) RunMaintenanceOnce(ctx context.Context) map[string]any {
	return RunPaperMaintenanceOnce(s.db.WithContext(ctx))
}

func (s Service) EnsureDefaultSetup(ctx context.Context) error {
	_, _, err := EnsureDefaultPaperSetup(s.db.WithContext(ctx))
	return err
}

func Overview(db *gorm.DB) (map[string]any, error) { return PaperOverview(db) }
func PositionsPublic(db *gorm.DB, rows []domainpaper.Position) []map[string]any {
	return PaperPositionsPublic(db, rows)
}
func FillsPublic(db *gorm.DB, rows []domainpaper.Fill) []map[string]any {
	return PaperFillsPublic(db, rows)
}
func Performance(db *gorm.DB, account domainpaper.Account) map[string]any {
	return PaperPerformance(db, account)
}
func OrdersPublic(db *gorm.DB, rows []domainpaper.Order) []map[string]any {
	return PaperOrdersPublic(db, rows)
}
func EnsureDefaultSetup(db *gorm.DB) (*domainpaper.RiskConfig, []domainpaper.Account, error) {
	return EnsureDefaultPaperSetup(db)
}
func RunMaintenanceOnce(db *gorm.DB) map[string]any { return RunPaperMaintenanceOnce(db) }
func RunMaintenanceLoop(ctx context.Context, db *gorm.DB, interval time.Duration) {
	RunPaperMaintenanceLoop(ctx, db, interval)
}

func accountInputPayload(input domainpaper.AccountInput) map[string]any {
	payload := map[string]any{
		"name":         input.Name,
		"cash":         input.Cash,
		"initial_cash": input.InitialCash,
	}
	if input.RiskConfigID != nil {
		payload["risk_config_id"] = *input.RiskConfigID
	}
	if input.Active != nil {
		payload["active"] = *input.Active
	}
	return payload
}

func orderInputSpec(input domainpaper.OrderInput) map[string]any {
	spec := map[string]any{
		"account_id":         input.AccountID,
		"code":               input.Code,
		"side":               input.Side,
		"quantity":           input.Quantity,
		"suggested_price":    input.SuggestedPrice,
		"position_pct":       input.PositionPct,
		"allocation_pct":     input.AllocationPct,
		"meeting_summary":    input.MeetingSummary,
		"meeting_conclusion": input.MeetingConclusion,
	}
	if input.Reason != nil {
		spec["reason"] = *input.Reason
	}
	if input.MeetingID != nil {
		spec["meeting_id"] = *input.MeetingID
	}
	if input.SourceMeetingEventID != nil {
		spec["source_meeting_event_id"] = *input.SourceMeetingEventID
	}
	if input.ExecuteAfter != nil {
		spec["execute_after"] = *input.ExecuteAfter
	}
	if input.ExpireAt != nil {
		spec["expire_at"] = *input.ExpireAt
	}
	return spec
}
