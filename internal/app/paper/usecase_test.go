package paper

import (
	"context"
	"strings"
	"testing"

	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainpaper "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/paper"
	"github.com/shopspring/decimal"
)

func TestCreateOrderRequiresConfiguredUnitOfWork(t *testing.T) {
	u := NewUsecase(nil, fakePaperService{})
	_, err := u.CreateOrder(context.Background(), domainpaper.OrderInput{Code: "600519"})
	if err == nil || !strings.Contains(err.Error(), "unit of work") {
		t.Fatalf("CreateOrder error = %v, want unit of work error", err)
	}
}

func TestCreateOrderPassesTypedInputToPaperService(t *testing.T) {
	service := &capturingPaperService{}
	u := NewUsecase(nil, service, passthroughPaperTx{service: service})

	meetingID := uint(2)
	eventID := uint(3)
	_, err := u.CreateOrder(context.Background(), domainpaper.OrderInput{
		AccountID:            1,
		MeetingID:            &meetingID,
		SourceMeetingEventID: &eventID,
		Code:                 "600519",
		Side:                 domainkernel.OrderBuy,
		SuggestedPrice:       decimal.RequireFromString("12.34"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if service.input.AccountID != 1 {
		t.Fatalf("CreateOrder input account = %d, want 1", service.input.AccountID)
	}
	if service.input.SourceMeetingEventID == nil || *service.input.SourceMeetingEventID != 3 {
		t.Fatalf("CreateOrder input source event = %#v, want 3", service.input.SourceMeetingEventID)
	}
	if !service.input.SuggestedPrice.Equal(decimal.RequireFromString("12.34")) {
		t.Fatalf("CreateOrder input suggested price = %s, want 12.34", service.input.SuggestedPrice)
	}
}

func TestRiskConfigInputAppliesExplicitFalseValues(t *testing.T) {
	name := "strict"
	allowETFLOF := false
	allowSHMain := false
	allowSZMain := true
	var row domainpaper.RiskConfig
	if err := applyRiskConfigInput(&row, domainpaper.RiskConfigInput{
		Name:          &name,
		AllowETFLOF:   &allowETFLOF,
		AllowSHMain:   &allowSHMain,
		AllowSZMain:   &allowSZMain,
		AccountIDsSet: true,
	}, true); err != nil {
		t.Fatal(err)
	}
	if row.AllowETFLOF || row.AllowSHMain || !row.AllowSZMain {
		t.Fatalf("explicit bool values were not preserved: %+v", row)
	}
}

func TestApplyPythonRiskConfigCreateDefaultsMatchesSchemaDefaults(t *testing.T) {
	name := "created"
	var row domainpaper.RiskConfig
	if err := applyRiskConfigInput(&row, domainpaper.RiskConfigInput{Name: &name}, true); err != nil {
		t.Fatal(err)
	}

	if row.AllowShort || row.AllowMargin || row.AllowBJ || row.AllowSTAR || row.AllowChiNext {
		t.Fatalf("unexpected enabled risk flags: %+v", row)
	}
	if !row.AllowSHMain || !row.AllowSZMain || !row.AllowETFLOF {
		t.Fatalf("expected SH/SZ main and ETF/LOF defaults enabled: %+v", row)
	}
	if !row.CommissionRate.Equal(decimal.RequireFromString("0.00005")) ||
		!row.MinCommission.Equal(decimal.RequireFromString("5.00")) ||
		!row.StampDutyRate.Equal(decimal.RequireFromString("0.001")) ||
		!row.TransferFeeRate.Equal(decimal.RequireFromString("0.00001")) {
		t.Fatalf("fee defaults mismatch: %+v", row)
	}
	if row.Enabled {
		t.Fatalf("newly created user risk config should default disabled: %+v", row)
	}
}

type fakePaperService struct{}

func (fakePaperService) Overview(context.Context) (map[string]any, error) { return nil, nil }
func (fakePaperService) CreateAccount(context.Context, domainpaper.AccountInput) (*domainpaper.Account, error) {
	return nil, nil
}
func (fakePaperService) AccountPublic(context.Context, domainpaper.Account) map[string]any {
	return nil
}
func (fakePaperService) PositionsPublic(context.Context, []domainpaper.Position) []map[string]any {
	return nil
}
func (fakePaperService) FillsPublic(context.Context, []domainpaper.Fill) []map[string]any {
	return nil
}
func (fakePaperService) Performance(context.Context, domainpaper.Account) map[string]any {
	return nil
}
func (fakePaperService) CreateOrder(context.Context, domainpaper.OrderInput) (*domainpaper.Order, error) {
	return nil, nil
}
func (fakePaperService) FillOrder(context.Context, *domainpaper.Order, decimal.Decimal) error {
	return nil
}
func (fakePaperService) OrdersPublic(context.Context, []domainpaper.Order) []map[string]any {
	return nil
}
func (fakePaperService) RunMaintenanceOnce(context.Context) map[string]any { return nil }
func (fakePaperService) EnsureDefaultSetup(context.Context) error          { return nil }

type passthroughPaperTx struct {
	service Service
}

func (tx passthroughPaperTx) WithTx(ctx context.Context, fn func(Repository, Service) error) error {
	return fn(nil, tx.service)
}

type capturingPaperService struct {
	fakePaperService
	input domainpaper.OrderInput
}

func (s *capturingPaperService) CreateOrder(_ context.Context, input domainpaper.OrderInput) (*domainpaper.Order, error) {
	s.input = input
	return &domainpaper.Order{ID: 1, AccountID: 1}, nil
}

func (s *capturingPaperService) OrdersPublic(context.Context, []domainpaper.Order) []map[string]any {
	return []map[string]any{{"id": 1, "accountId": 1}}
}
