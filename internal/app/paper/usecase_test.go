package paper

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/market"
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

func TestApplyCorporateActionInputCalculatesCashDividendBonusAndSplit(t *testing.T) {
	account := &domainpaper.Account{ID: 7}
	position := &domainpaper.Position{
		ID:         9,
		AccountID:  7,
		Code:       "600519",
		Quantity:   100,
		CostAmount: decimal.NewFromInt(10000),
	}

	dividend, err := applyCorporateActionInput(domainpaper.CorporateActionInput{
		Code:         "600519",
		ActionType:   "cash_dividend",
		CashPerShare: decimal.RequireFromString("2.30"),
	}, account, position)
	if err != nil {
		t.Fatal(err)
	}
	if dividend.AffectedShares != 100 || !dividend.CashAmount.Equal(decimal.NewFromInt(230)) {
		t.Fatalf("cash dividend mismatch: %+v", dividend)
	}

	bonus, err := applyCorporateActionInput(domainpaper.CorporateActionInput{
		Code:       "600519",
		ActionType: "bonus_share",
		ShareRatio: decimal.RequireFromString("0.20"),
	}, account, position)
	if err != nil {
		t.Fatal(err)
	}
	if bonus.AffectedShares != 20 || !bonus.CashAmount.IsZero() {
		t.Fatalf("bonus share mismatch: %+v", bonus)
	}

	split, err := applyCorporateActionInput(domainpaper.CorporateActionInput{
		Code:       "600519",
		ActionType: "split",
		ShareRatio: decimal.RequireFromString("2"),
	}, account, position)
	if err != nil {
		t.Fatal(err)
	}
	if split.AffectedShares != 100 {
		t.Fatalf("split affected shares = %d, want 100", split.AffectedShares)
	}
}

func TestOrderApprovalReviewFlagsHighRiskNearOrderLimit(t *testing.T) {
	meetingID := uint(2)
	eventID := uint(3)
	repo := &fakePaperRepo{
		account:    &domainpaper.Account{ID: 1, Cash: decimal.NewFromInt(100000), RiskConfigID: uintPtr(7), Active: true},
		riskConfig: &domainpaper.RiskConfig{ID: 7, MaxOrderPct: decimal.RequireFromString("0.20"), MaxPositionPct: decimal.RequireFromString("0.30"), Enabled: true},
	}

	review := orderApprovalReviewWithRepo(context.Background(), repo, domainpaper.Order{
		ID:                   9,
		AccountID:            1,
		MeetingID:            &meetingID,
		SourceMeetingEventID: &eventID,
		Code:                 "600519",
		Side:                 domainkernel.OrderBuy,
		Quantity:             1700,
		SuggestedPrice:       decimal.NewFromInt(10),
		Status:               domainkernel.OrderSuggested,
	})

	if review.RiskLevel != "high" || !review.ReviewRequired || !review.ConfirmRequired {
		t.Fatalf("review = %+v, want high risk with confirmation", review)
	}
	if !strings.Contains(strings.Join(review.Reasons, ";"), "单笔上限") {
		t.Fatalf("review reasons = %v, want max order limit reason", review.Reasons)
	}
	if _, ok := review.Metrics["order_pct"]; !ok {
		t.Fatalf("review metrics = %#v, want order_pct", review.Metrics)
	}
}

func TestApproveOrderRequiresHighRiskConfirmation(t *testing.T) {
	meetingID := uint(2)
	eventID := uint(3)
	order := &domainpaper.Order{
		ID:                   9,
		AccountID:            1,
		MeetingID:            &meetingID,
		SourceMeetingEventID: &eventID,
		Code:                 "600519",
		Side:                 domainkernel.OrderBuy,
		Quantity:             1700,
		SuggestedPrice:       decimal.NewFromInt(10),
		Status:               domainkernel.OrderSuggested,
	}
	repo := &fakePaperRepo{
		account:    &domainpaper.Account{ID: 1, Cash: decimal.NewFromInt(100000), RiskConfigID: uintPtr(7), Active: true},
		riskConfig: &domainpaper.RiskConfig{ID: 7, MaxOrderPct: decimal.RequireFromString("0.20"), MaxPositionPct: decimal.RequireFromString("0.30"), Enabled: true},
		order:      order,
	}
	service := &approvalPaperService{}
	u := NewUsecase(repo, service, passthroughPaperTx{repo: repo, service: service})

	_, _, err := u.ApproveOrder(context.Background(), order.ID, OrderApproveInput{})
	if !errors.Is(err, ErrHighRiskApprovalRequiresConfirmation) {
		t.Fatalf("ApproveOrder error = %v, want high risk confirmation error", err)
	}
	if service.submitted {
		t.Fatalf("SubmitOrder should not be called before high risk confirmation")
	}

	row, found, err := u.ApproveOrder(context.Background(), order.ID, OrderApproveInput{ConfirmHighRisk: true})
	if err != nil || !found {
		t.Fatalf("ApproveOrder confirmed = row:%+v found:%v err:%v", row, found, err)
	}
	if !service.submitted || row.Order.Status != domainkernel.OrderPending {
		t.Fatalf("confirmed order = %+v submitted=%v, want pending submitted", row.Order, service.submitted)
	}
}

func TestReplayEventsSortsOrdersFillsAndCorporateActions(t *testing.T) {
	base := time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC)
	appliedAt := base.Add(time.Hour)
	events := replayEvents(
		[]domainpaper.Fill{{ID: 2, AccountID: 1, OrderID: 1, Code: "600519", Side: domainkernel.OrderBuy, Quantity: 100, Price: decimal.NewFromInt(10), FilledAt: base.Add(30 * time.Minute)}},
		[]domainpaper.Order{{ID: 1, AccountID: 1, Code: "600519", Side: domainkernel.OrderBuy, Quantity: 100, Status: domainkernel.OrderFilled, CreatedAt: base}},
		[]domainpaper.CorporateAction{{ID: 3, AccountID: 1, Code: "600519", ActionType: "cash_dividend", CashPerShare: decimal.NewFromInt(1), CashAmount: decimal.NewFromInt(100), AffectedShares: 100, AppliedAt: &appliedAt}},
	)
	if len(events) != 3 {
		t.Fatalf("event count = %d, want 3", len(events))
	}
	got := []string{events[0].ID, events[1].ID, events[2].ID}
	want := []string{"order:1", "fill:2", "corporate_action:3"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events sorted as %v, want %v", got, want)
		}
	}
}

func TestRunDailyBacktestAppliesLotsFeesAndSuspensionRejections(t *testing.T) {
	input := BacktestInput{
		AccountID:        1,
		Code:             "600519",
		StartDate:        time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		EndDate:          time.Date(2026, 6, 4, 0, 0, 0, 0, time.UTC),
		InitialCash:      decimal.NewFromInt(100000),
		BuyThresholdPct:  decimal.NewFromInt(-3),
		SellThresholdPct: decimal.NewFromInt(3),
		OrderPct:         decimal.RequireFromString("0.10"),
		SlippageBps:      decimal.Zero,
	}
	bars := []domainmarket.DailyBar{
		backtestBar("600519", "2026-06-01", "10.00", "1000"),
		backtestBar("600519", "2026-06-02", "9.60", "1000"),
		backtestBar("600519", "2026-06-03", "10.10", "1000"),
		backtestBar("600519", "2026-06-04", "9.50", "0"),
	}

	report := runDailyBacktest(input, defaultBacktestRisk(), bars)

	if report.Summary.TradeCount != 2 || report.Summary.RejectedCount != 1 {
		t.Fatalf("summary trade/rejected = %d/%d, want 2/1", report.Summary.TradeCount, report.Summary.RejectedCount)
	}
	if len(report.Orders) != 3 {
		t.Fatalf("order count = %d, want 3", len(report.Orders))
	}
	if report.Orders[0].Side != "buy" || report.Orders[0].Quantity != 1000 || report.Orders[0].Status != "filled" {
		t.Fatalf("unexpected buy order: %+v", report.Orders[0])
	}
	if report.Orders[1].Side != "sell" || report.Orders[1].Quantity != 1000 || report.Orders[1].Status != "filled" {
		t.Fatalf("unexpected sell order: %+v", report.Orders[1])
	}
	if report.Orders[2].Status != "rejected" || report.Orders[2].Reason != "blocked_by_zero_volume" {
		t.Fatalf("unexpected rejected order: %+v", report.Orders[2])
	}
	if !report.Summary.FinalEquity.GreaterThan(input.InitialCash) {
		t.Fatalf("final equity = %s, want above initial cash %s", report.Summary.FinalEquity, input.InitialCash)
	}
}

func backtestBar(code string, date string, close string, volume string) domainmarket.DailyBar {
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		panic(err)
	}
	return domainmarket.DailyBar{
		Code:      code,
		TradeDate: parsed,
		Open:      decimal.RequireFromString(close),
		High:      decimal.RequireFromString(close),
		Low:       decimal.RequireFromString(close),
		Close:     decimal.RequireFromString(close),
		Volume:    decimal.RequireFromString(volume),
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
func (fakePaperService) SubmitOrder(context.Context, *domainpaper.Order) error {
	return nil
}
func (fakePaperService) FillOrder(context.Context, *domainpaper.Order, domainpaper.OrderFillInput) error {
	return nil
}
func (fakePaperService) OrdersPublic(context.Context, []domainpaper.Order) []map[string]any {
	return nil
}
func (fakePaperService) RunMaintenanceOnce(context.Context) map[string]any { return nil }
func (fakePaperService) EnsureDefaultSetup(context.Context) error          { return nil }

type passthroughPaperTx struct {
	repo    Repository
	service Service
}

func (tx passthroughPaperTx) WithTx(ctx context.Context, fn func(Repository, Service) error) error {
	return fn(tx.repo, tx.service)
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

type approvalPaperService struct {
	fakePaperService
	submitted bool
}

func (s *approvalPaperService) SubmitOrder(_ context.Context, row *domainpaper.Order) error {
	s.submitted = true
	row.Status = domainkernel.OrderPending
	return nil
}

func (s *approvalPaperService) OrdersPublic(_ context.Context, rows []domainpaper.Order) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{"id": row.ID, "accountId": row.AccountID})
	}
	return out
}

type fakePaperRepo struct {
	account    *domainpaper.Account
	riskConfig *domainpaper.RiskConfig
	positions  []domainpaper.Position
	order      *domainpaper.Order
}

func (r *fakePaperRepo) ListRiskConfigs(context.Context) ([]domainpaper.RiskConfig, error) {
	return nil, nil
}

func (r *fakePaperRepo) FindRiskConfig(_ context.Context, id uint) (*domainpaper.RiskConfig, bool, error) {
	if r.riskConfig == nil || r.riskConfig.ID != id {
		return nil, false, nil
	}
	return r.riskConfig, true, nil
}

func (r *fakePaperRepo) CreateRiskConfig(context.Context, *domainpaper.RiskConfig) error {
	return nil
}

func (r *fakePaperRepo) SaveRiskConfig(context.Context, *domainpaper.RiskConfig) error {
	return nil
}

func (r *fakePaperRepo) DeleteRiskConfig(context.Context, uint) error {
	return nil
}

func (r *fakePaperRepo) RiskConfigAccountIDs(context.Context, uint) ([]uint, error) {
	return nil, nil
}

func (r *fakePaperRepo) ReplaceRiskConfigAccounts(context.Context, uint, []uint) error {
	return nil
}

func (r *fakePaperRepo) UnbindRiskConfigAccounts(context.Context, uint) ([]uint, error) {
	return nil, nil
}

func (r *fakePaperRepo) ListAccounts(context.Context) ([]domainpaper.Account, error) {
	return nil, nil
}

func (r *fakePaperRepo) FindAccount(_ context.Context, id uint) (*domainpaper.Account, bool, error) {
	if r.account == nil || r.account.ID != id {
		return nil, false, nil
	}
	return r.account, true, nil
}

func (r *fakePaperRepo) AccountResearchTeam(context.Context, uint) (*AccountTeam, bool, error) {
	return nil, false, nil
}

func (r *fakePaperRepo) SaveAccount(context.Context, *domainpaper.Account) error {
	return nil
}

func (r *fakePaperRepo) DeleteAccount(context.Context, uint) error {
	return nil
}

func (r *fakePaperRepo) ListPositions(_ context.Context, accountID uint) ([]domainpaper.Position, error) {
	if r.account != nil && r.account.ID != accountID {
		return nil, nil
	}
	return r.positions, nil
}

func (r *fakePaperRepo) ListAccountOrders(context.Context, uint, string, int, uint64) ([]domainpaper.Order, error) {
	return nil, nil
}

func (r *fakePaperRepo) ListFills(context.Context, uint, int, uint64) ([]domainpaper.Fill, error) {
	return nil, nil
}

func (r *fakePaperRepo) ListCorporateActions(context.Context, uint, int, uint64) ([]domainpaper.CorporateAction, error) {
	return nil, nil
}

func (r *fakePaperRepo) ListOrders(context.Context, int, uint64) ([]domainpaper.Order, error) {
	return nil, nil
}

func (r *fakePaperRepo) FindOrder(_ context.Context, id uint) (*domainpaper.Order, bool, error) {
	if r.order == nil || r.order.ID != id {
		return nil, false, nil
	}
	return r.order, true, nil
}

func (r *fakePaperRepo) SaveOrder(_ context.Context, row *domainpaper.Order) error {
	r.order = row
	return nil
}

func (r *fakePaperRepo) DeleteOrder(context.Context, uint) error {
	return nil
}

func (r *fakePaperRepo) FindPosition(context.Context, uint, string) (*domainpaper.Position, bool, error) {
	return nil, false, nil
}

func (r *fakePaperRepo) CreatePosition(context.Context, *domainpaper.Position) error {
	return nil
}

func (r *fakePaperRepo) SavePosition(context.Context, *domainpaper.Position) error {
	return nil
}

func (r *fakePaperRepo) DeletePosition(context.Context, uint) error {
	return nil
}

func (r *fakePaperRepo) CreateFill(context.Context, *domainpaper.Fill) error {
	return nil
}

func (r *fakePaperRepo) CreateEquitySnapshot(context.Context, *domainpaper.EquitySnapshot) error {
	return nil
}

func (r *fakePaperRepo) CreateCorporateAction(context.Context, *domainpaper.CorporateAction) error {
	return nil
}

func (r *fakePaperRepo) DailyBarsForBacktest(context.Context, string, time.Time, time.Time, int) ([]domainmarket.DailyBar, error) {
	return nil, nil
}

func uintPtr(value uint) *uint {
	return &value
}
