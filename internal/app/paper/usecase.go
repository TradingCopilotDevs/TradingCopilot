package paper

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/market"
	domainpaper "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/paper"

	"github.com/shopspring/decimal"
)

type Usecase struct {
	repo    Repository
	service Service
	tx      Transactor
}

func NewUsecase(repo Repository, service Service, tx ...Transactor) Usecase {
	u := Usecase{repo: repo, service: service}
	if len(tx) > 0 {
		u.tx = tx[0]
	}
	return u
}

var ErrHighRiskApprovalRequiresConfirmation = errors.New("high risk order approval requires explicit confirmation")

var orderApprovalNearLimitThreshold = decimal.RequireFromString("0.80")

type Repository interface {
	ListRiskConfigs(ctx context.Context) ([]domainpaper.RiskConfig, error)
	FindRiskConfig(ctx context.Context, id uint) (*domainpaper.RiskConfig, bool, error)
	CreateRiskConfig(ctx context.Context, row *domainpaper.RiskConfig) error
	SaveRiskConfig(ctx context.Context, row *domainpaper.RiskConfig) error
	DeleteRiskConfig(ctx context.Context, id uint) error
	RiskConfigAccountIDs(ctx context.Context, id uint) ([]uint, error)
	ReplaceRiskConfigAccounts(ctx context.Context, id uint, accountIDs []uint) error
	UnbindRiskConfigAccounts(ctx context.Context, id uint) ([]uint, error)
	ListAccounts(ctx context.Context) ([]domainpaper.Account, error)
	FindAccount(ctx context.Context, id uint) (*domainpaper.Account, bool, error)
	AccountResearchTeam(ctx context.Context, accountID uint) (*AccountTeam, bool, error)
	SaveAccount(ctx context.Context, row *domainpaper.Account) error
	DeleteAccount(ctx context.Context, id uint) error
	ListPositions(ctx context.Context, accountID uint) ([]domainpaper.Position, error)
	ListAccountOrders(ctx context.Context, accountID uint, status string, limit int, cursorID uint64) ([]domainpaper.Order, error)
	ListFills(ctx context.Context, accountID uint, limit int, cursorID uint64) ([]domainpaper.Fill, error)
	ListCorporateActions(ctx context.Context, accountID uint, limit int, cursorID uint64) ([]domainpaper.CorporateAction, error)
	ListOrders(ctx context.Context, limit int, cursorID uint64) ([]domainpaper.Order, error)
	FindOrder(ctx context.Context, id uint) (*domainpaper.Order, bool, error)
	SaveOrder(ctx context.Context, row *domainpaper.Order) error
	DeleteOrder(ctx context.Context, id uint) error
	FindPosition(ctx context.Context, accountID uint, code string) (*domainpaper.Position, bool, error)
	CreatePosition(ctx context.Context, row *domainpaper.Position) error
	SavePosition(ctx context.Context, row *domainpaper.Position) error
	DeletePosition(ctx context.Context, id uint) error
	CreateFill(ctx context.Context, row *domainpaper.Fill) error
	CreateEquitySnapshot(ctx context.Context, row *domainpaper.EquitySnapshot) error
	CreateCorporateAction(ctx context.Context, row *domainpaper.CorporateAction) error
	DailyBarsForBacktest(ctx context.Context, code string, start time.Time, end time.Time, limit int) ([]domainmarket.DailyBar, error)
}

type Service interface {
	Overview(ctx context.Context) (map[string]any, error)
	CreateAccount(ctx context.Context, input domainpaper.AccountInput) (*domainpaper.Account, error)
	AccountPublic(ctx context.Context, account domainpaper.Account) map[string]any
	PositionsPublic(ctx context.Context, rows []domainpaper.Position) []map[string]any
	FillsPublic(ctx context.Context, rows []domainpaper.Fill) []map[string]any
	Performance(ctx context.Context, account domainpaper.Account) map[string]any
	CreateOrder(ctx context.Context, input domainpaper.OrderInput) (*domainpaper.Order, error)
	SubmitOrder(ctx context.Context, row *domainpaper.Order) error
	FillOrder(ctx context.Context, row *domainpaper.Order, input domainpaper.OrderFillInput) error
	OrdersPublic(ctx context.Context, rows []domainpaper.Order) []map[string]any
	RunMaintenanceOnce(ctx context.Context) map[string]any
	EnsureDefaultSetup(ctx context.Context) error
}

type Transactor interface {
	WithTx(ctx context.Context, fn func(Repository, Service) error) error
}

type AccountRow struct {
	Account domainpaper.Account
	Public  map[string]any
	Team    *AccountTeam
}

type AccountTeam struct {
	ID   uint
	Name string
}

type RiskConfigRow struct {
	Config     domainpaper.RiskConfig
	AccountIDs []uint
}

type RiskConfigDeletion struct {
	ID                uint
	UnboundAccountIDs []uint
}

type OrderRow struct {
	Order  domainpaper.Order
	Public map[string]any
}

type OrderApproveInput struct {
	ConfirmHighRisk bool
}

type FillRow struct {
	Public map[string]any
}

type OrderRejectInput struct {
	Reason string
}

type Page struct {
	Limit  int
	Cursor string
}

type OrderList struct {
	Rows       []OrderRow
	NextCursor string
}

type OrderApprovalReview struct {
	RiskLevel       string
	ReviewRequired  bool
	ConfirmRequired bool
	Reasons         []string
	Metrics         map[string]any
}

type FillList struct {
	Rows       []FillRow
	NextCursor string
}

type CorporateActionRow struct {
	Action domainpaper.CorporateAction
	Public map[string]any
}

type CorporateActionList struct {
	Rows       []CorporateActionRow
	NextCursor string
}

type SetupStatus struct {
	AccountCount    int
	RiskConfigCount int
}

type ReplayEvent struct {
	ID         string
	Type       string
	Time       time.Time
	Code       string
	Summary    string
	Attributes map[string]any
}

type ReplayReport struct {
	AccountID   uint
	GeneratedAt time.Time
	ModelPolicy map[string]any
	Summary     map[string]any
	Events      []ReplayEvent
}

type BacktestInput struct {
	AccountID        uint
	Code             string
	StartDate        time.Time
	EndDate          time.Time
	InitialCash      decimal.Decimal
	BuyThresholdPct  decimal.Decimal
	SellThresholdPct decimal.Decimal
	OrderPct         decimal.Decimal
	SlippageBps      decimal.Decimal
}

type BacktestPolicy struct {
	ExecutionModel    string
	RiskModel         string
	BrokerIntegration string
	DataSource        string
	LimitBandPct      decimal.Decimal
	SlippageBps       decimal.Decimal
	LotSize           int
	Rules             []string
}

type BacktestSummary struct {
	BarCount         int
	TradeCount       int
	RejectedCount    int
	InitialCash      decimal.Decimal
	FinalCash        decimal.Decimal
	FinalMarketValue decimal.Decimal
	FinalEquity      decimal.Decimal
	TotalReturnPct   decimal.Decimal
	MaxDrawdownPct   decimal.Decimal
}

type BacktestPoint struct {
	TradeDate      time.Time
	Close          decimal.Decimal
	SignalPct      decimal.Decimal
	Cash           decimal.Decimal
	Quantity       int
	MarketValue    decimal.Decimal
	TotalEquity    decimal.Decimal
	DailyReturnPct decimal.Decimal
	DrawdownPct    decimal.Decimal
}

type BacktestOrder struct {
	ID             string
	TradeDate      time.Time
	Code           string
	Side           string
	Quantity       int
	SignalPct      decimal.Decimal
	ReferencePrice decimal.Decimal
	FilledPrice    decimal.Decimal
	Status         string
	Reason         string
	GrossAmount    decimal.Decimal
	Fees           decimal.Decimal
	CashAfter      decimal.Decimal
	PositionAfter  int
}

type BacktestReport struct {
	AccountID   uint
	GeneratedAt time.Time
	Input       BacktestInput
	Policy      BacktestPolicy
	Summary     BacktestSummary
	Series      []BacktestPoint
	Orders      []BacktestOrder
}

type backtestRisk struct {
	ConfigID        *uint
	Source          string
	MaxOrderPct     decimal.Decimal
	MaxPositionPct  decimal.Decimal
	CommissionRate  decimal.Decimal
	MinCommission   decimal.Decimal
	StampDutyRate   decimal.Decimal
	TransferFeeRate decimal.Decimal
}

func (u Usecase) Overview(ctx context.Context) (map[string]any, error) {
	if err := u.ensureDefaultSetup(ctx); err != nil {
		return nil, err
	}
	return u.service.Overview(ctx)
}

func (u Usecase) RunMaintenanceOnce(ctx context.Context) map[string]any {
	result := map[string]any{}
	_ = u.withTx(ctx, func(_ Repository, service Service) error {
		result = service.RunMaintenanceOnce(ctx)
		return nil
	})
	return result
}

func (u Usecase) ListRiskConfigs(ctx context.Context) ([]RiskConfigRow, error) {
	if err := u.ensureDefaultSetup(ctx); err != nil {
		return nil, err
	}
	rows, err := u.repo.ListRiskConfigs(ctx)
	if err != nil {
		return nil, err
	}
	return u.riskConfigRows(ctx, rows)
}

func (u Usecase) CreateRiskConfig(ctx context.Context, input domainpaper.RiskConfigInput) (*RiskConfigRow, error) {
	var row domainpaper.RiskConfig
	if err := applyRiskConfigInput(&row, input, true); err != nil {
		return nil, err
	}
	if err := u.withTx(ctx, func(repo Repository, _ Service) error {
		if err := repo.CreateRiskConfig(ctx, &row); err != nil {
			return err
		}
		if input.AccountIDsSet {
			return repo.ReplaceRiskConfigAccounts(ctx, row.ID, input.AccountIDs)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	accountIDs := []uint{}
	if input.AccountIDsSet {
		accountIDs = uniqueUintIDs(input.AccountIDs)
	}
	return &RiskConfigRow{Config: row, AccountIDs: accountIDs}, nil
}

func (u Usecase) UpdateRiskConfig(ctx context.Context, id uint, input domainpaper.RiskConfigInput) (*RiskConfigRow, bool, error) {
	var row *domainpaper.RiskConfig
	found := false
	if err := u.withTx(ctx, func(repo Repository, _ Service) error {
		var err error
		row, found, err = repo.FindRiskConfig(ctx, id)
		if err != nil || !found {
			return err
		}
		if err := applyRiskConfigInput(row, input, false); err != nil {
			return err
		}
		row.ID = id
		if err := repo.SaveRiskConfig(ctx, row); err != nil {
			return err
		}
		if input.AccountIDsSet {
			return repo.ReplaceRiskConfigAccounts(ctx, row.ID, input.AccountIDs)
		}
		return nil
	}); err != nil {
		return nil, found, err
	}
	if !found {
		return nil, false, nil
	}
	accountIDs := uniqueUintIDs(input.AccountIDs)
	if !input.AccountIDsSet {
		var err error
		accountIDs, err = u.repo.RiskConfigAccountIDs(ctx, row.ID)
		if err != nil {
			return nil, true, err
		}
	}
	return &RiskConfigRow{Config: *row, AccountIDs: accountIDs}, true, nil
}

func (u Usecase) DeleteRiskConfig(ctx context.Context, id uint) (*RiskConfigDeletion, bool, error) {
	deleted := false
	result := &RiskConfigDeletion{ID: id}
	if err := u.withTx(ctx, func(repo Repository, _ Service) error {
		_, found, err := repo.FindRiskConfig(ctx, id)
		if err != nil || !found {
			deleted = false
			return err
		}
		unbound, err := repo.UnbindRiskConfigAccounts(ctx, id)
		if err != nil {
			return err
		}
		if err := repo.DeleteRiskConfig(ctx, id); err != nil {
			return err
		}
		result.UnboundAccountIDs = unbound
		deleted = true
		return nil
	}); err != nil {
		return nil, deleted, err
	}
	return result, deleted, nil
}

func (u Usecase) ListAccounts(ctx context.Context) ([]AccountRow, error) {
	if err := u.ensureDefaultSetup(ctx); err != nil {
		return nil, err
	}
	rows, err := u.repo.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]AccountRow, 0, len(rows))
	for _, row := range rows {
		team, _, _ := u.repo.AccountResearchTeam(ctx, row.ID)
		out = append(out, AccountRow{Account: row, Public: u.service.AccountPublic(ctx, row), Team: team})
	}
	return out, nil
}

func (u Usecase) SetupStatus(ctx context.Context) (SetupStatus, error) {
	accounts, err := u.repo.ListAccounts(ctx)
	if err != nil {
		return SetupStatus{}, err
	}
	riskConfigs, err := u.repo.ListRiskConfigs(ctx)
	if err != nil {
		return SetupStatus{}, err
	}
	return SetupStatus{AccountCount: len(accounts), RiskConfigCount: len(riskConfigs)}, nil
}

func (u Usecase) CreateAccount(ctx context.Context, input domainpaper.AccountInput) (*AccountRow, error) {
	var row *domainpaper.Account
	if err := u.withTx(ctx, func(_ Repository, service Service) error {
		var err error
		row, err = service.CreateAccount(ctx, input)
		return err
	}); err != nil {
		return nil, err
	}
	team, _, _ := u.repo.AccountResearchTeam(ctx, row.ID)
	return &AccountRow{Account: *row, Public: u.service.AccountPublic(ctx, *row), Team: team}, nil
}

func (u Usecase) UpdateAccount(ctx context.Context, id uint, attrs map[string]any) (*AccountRow, bool, error) {
	body, _ := json.Marshal(attrs)
	var row *domainpaper.Account
	found := false
	if err := u.withTx(ctx, func(repo Repository, _ Service) error {
		var err error
		row, found, err = repo.FindAccount(ctx, id)
		if err != nil || !found {
			return err
		}
		if err := json.Unmarshal(body, &row); err != nil {
			return err
		}
		row.ID = id
		return repo.SaveAccount(ctx, row)
	}); err != nil {
		return nil, found, err
	}
	if !found {
		return nil, false, nil
	}
	team, _, _ := u.repo.AccountResearchTeam(ctx, row.ID)
	return &AccountRow{Account: *row, Public: u.service.AccountPublic(ctx, *row), Team: team}, true, nil
}

func (u Usecase) SetAccountActive(ctx context.Context, id uint, active bool) (*AccountRow, bool, error) {
	var row *domainpaper.Account
	found := false
	if err := u.withTx(ctx, func(repo Repository, _ Service) error {
		var err error
		row, found, err = repo.FindAccount(ctx, id)
		if err != nil || !found {
			return err
		}
		row.Active = active
		return repo.SaveAccount(ctx, row)
	}); err != nil {
		return nil, found, err
	}
	if !found {
		return nil, false, nil
	}
	team, _, _ := u.repo.AccountResearchTeam(ctx, row.ID)
	return &AccountRow{Account: *row, Public: u.service.AccountPublic(ctx, *row), Team: team}, true, nil
}

func (u Usecase) DeleteAccount(ctx context.Context, id uint) error {
	return u.withTx(ctx, func(repo Repository, _ Service) error {
		if _, found, err := repo.AccountResearchTeam(ctx, id); err != nil {
			return err
		} else if found {
			return errors.New("paper account is bound to a research team")
		}
		return repo.DeleteAccount(ctx, id)
	})
}

func (u Usecase) ListPositions(ctx context.Context, accountID uint) ([]map[string]any, error) {
	rows, err := u.repo.ListPositions(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return u.service.PositionsPublic(ctx, rows), nil
}

func (u Usecase) ListAccountOrders(ctx context.Context, accountID uint, status string, page Page) (OrderList, error) {
	page = normalizePage(page)
	rows, err := u.repo.ListAccountOrders(ctx, accountID, status, page.Limit+1, cursorID(page.Cursor))
	if err != nil {
		return OrderList{}, err
	}
	nextCursor := ""
	if len(rows) > page.Limit {
		nextCursor = strconv.FormatUint(uint64(rows[page.Limit-1].ID), 10)
		rows = rows[:page.Limit]
	}
	return OrderList{Rows: u.orderRows(ctx, rows), NextCursor: nextCursor}, nil
}

func (u Usecase) ListFills(ctx context.Context, accountID uint, page Page) (FillList, error) {
	page = normalizePage(page)
	rows, err := u.repo.ListFills(ctx, accountID, page.Limit+1, cursorID(page.Cursor))
	if err != nil {
		return FillList{}, err
	}
	nextCursor := ""
	if len(rows) > page.Limit {
		nextCursor = strconv.FormatUint(uint64(rows[page.Limit-1].ID), 10)
		rows = rows[:page.Limit]
	}
	return FillList{Rows: fillRows(u.service.FillsPublic(ctx, rows)), NextCursor: nextCursor}, nil
}

func (u Usecase) ListCorporateActions(ctx context.Context, accountID uint, page Page) (CorporateActionList, error) {
	page = normalizePage(page)
	rows, err := u.repo.ListCorporateActions(ctx, accountID, page.Limit+1, cursorID(page.Cursor))
	if err != nil {
		return CorporateActionList{}, err
	}
	nextCursor := ""
	if len(rows) > page.Limit {
		nextCursor = strconv.FormatUint(uint64(rows[page.Limit-1].ID), 10)
		rows = rows[:page.Limit]
	}
	return CorporateActionList{Rows: corporateActionRows(rows), NextCursor: nextCursor}, nil
}

func (u Usecase) CreateCorporateAction(ctx context.Context, input domainpaper.CorporateActionInput) (*CorporateActionRow, error) {
	var action domainpaper.CorporateAction
	if err := u.withTx(ctx, func(repo Repository, _ Service) error {
		account, found, err := repo.FindAccount(ctx, input.AccountID)
		if err != nil || !found {
			if err != nil {
				return err
			}
			return errors.New("paper account not found")
		}
		position, found, err := repo.FindPosition(ctx, input.AccountID, strings.ToUpper(strings.TrimSpace(input.Code)))
		if err != nil || !found {
			if err != nil {
				return err
			}
			return errors.New("paper position not found for corporate action")
		}
		created, err := applyCorporateActionInput(input, account, position)
		if err != nil {
			return err
		}
		action = created
		switch action.ActionType {
		case "cash_dividend":
			account.Cash = account.Cash.Add(action.CashAmount)
			if err := repo.SaveAccount(ctx, account); err != nil {
				return err
			}
		case "bonus_share", "split":
			position.Quantity += action.AffectedShares
			if position.Quantity <= 0 {
				return errors.New("corporate action would leave a non-positive position quantity")
			}
			position.AvgCost = position.CostAmount.Div(decimal.NewFromInt(int64(position.Quantity)))
			position.MarketValue = position.LastPrice.Mul(decimal.NewFromInt(int64(position.Quantity)))
			position.UnrealizedPNL = position.MarketValue.Sub(position.CostAmount)
			position.UpdatedAt = time.Now()
			if err := repo.SavePosition(ctx, position); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unsupported corporate action type %q", action.ActionType)
		}
		return repo.CreateCorporateAction(ctx, &action)
	}); err != nil {
		return nil, err
	}
	row := corporateActionRows([]domainpaper.CorporateAction{action})
	return &row[0], nil
}

func (u Usecase) Performance(ctx context.Context, accountID uint) (map[string]any, bool, error) {
	account, found, err := u.repo.FindAccount(ctx, accountID)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	return u.service.Performance(ctx, *account), true, nil
}

func (u Usecase) Replay(ctx context.Context, accountID uint) (*ReplayReport, bool, error) {
	account, found, err := u.repo.FindAccount(ctx, accountID)
	if err != nil || !found {
		return nil, found, err
	}
	fills, err := u.repo.ListFills(ctx, accountID, 200, 0)
	if err != nil {
		return nil, true, err
	}
	orders, err := u.repo.ListAccountOrders(ctx, accountID, "", 200, 0)
	if err != nil {
		return nil, true, err
	}
	actions, err := u.repo.ListCorporateActions(ctx, accountID, 200, 0)
	if err != nil {
		return nil, true, err
	}
	events := replayEvents(fills, orders, actions)
	return &ReplayReport{
		AccountID:   account.ID,
		GeneratedAt: time.Now(),
		ModelPolicy: map[string]any{
			"executionModel":        "paper_fills_orders_corporate_actions",
			"riskModel":             "uses_current_paper_risk_config_for_new_orders",
			"brokerIntegration":     "disabled",
			"corporateActionStatus": "cash_dividend_bonus_share_split_applied",
		},
		Summary: map[string]any{
			"fillCount":            len(fills),
			"orderCount":           len(orders),
			"corporateActionCount": len(actions),
			"eventCount":           len(events),
		},
		Events: events,
	}, true, nil
}

func (u Usecase) Backtest(ctx context.Context, input BacktestInput) (*BacktestReport, bool, error) {
	account, found, err := u.repo.FindAccount(ctx, input.AccountID)
	if err != nil || !found {
		return nil, found, err
	}
	normalized, err := normalizeBacktestInput(input, *account)
	if err != nil {
		return nil, true, err
	}
	risk, err := u.backtestRisk(ctx, *account)
	if err != nil {
		return nil, true, err
	}
	bars, err := u.repo.DailyBarsForBacktest(ctx, normalized.Code, normalized.StartDate, normalized.EndDate, 5000)
	if err != nil {
		return nil, true, err
	}
	if len(bars) < 2 {
		return nil, true, errors.New("backtest requires at least two daily bars in the requested range")
	}
	report := runDailyBacktest(normalized, risk, bars)
	report.AccountID = account.ID
	return report, true, nil
}

func (u Usecase) backtestRisk(ctx context.Context, account domainpaper.Account) (backtestRisk, error) {
	risk := defaultBacktestRisk()
	if account.RiskConfigID == nil {
		return risk, nil
	}
	cfg, found, err := u.repo.FindRiskConfig(ctx, *account.RiskConfigID)
	if err != nil || !found || cfg == nil || !cfg.Enabled {
		return risk, err
	}
	risk.ConfigID = &cfg.ID
	risk.Source = "account_risk_config"
	if cfg.MaxOrderPct.IsPositive() {
		risk.MaxOrderPct = cfg.MaxOrderPct
	}
	if cfg.MaxPositionPct.IsPositive() {
		risk.MaxPositionPct = cfg.MaxPositionPct
	}
	if !cfg.CommissionRate.IsNegative() {
		risk.CommissionRate = cfg.CommissionRate
	}
	if !cfg.MinCommission.IsNegative() {
		risk.MinCommission = cfg.MinCommission
	}
	if !cfg.StampDutyRate.IsNegative() {
		risk.StampDutyRate = cfg.StampDutyRate
	}
	if !cfg.TransferFeeRate.IsNegative() {
		risk.TransferFeeRate = cfg.TransferFeeRate
	}
	return risk, nil
}

func (u Usecase) ListOrders(ctx context.Context, page Page) (OrderList, error) {
	page = normalizePage(page)
	rows, err := u.repo.ListOrders(ctx, page.Limit+1, cursorID(page.Cursor))
	if err != nil {
		return OrderList{}, err
	}
	nextCursor := ""
	if len(rows) > page.Limit {
		nextCursor = strconv.FormatUint(uint64(rows[page.Limit-1].ID), 10)
		rows = rows[:page.Limit]
	}
	return OrderList{Rows: u.orderRows(ctx, rows), NextCursor: nextCursor}, nil
}

func (u Usecase) CreateOrder(ctx context.Context, input domainpaper.OrderInput) (*OrderRow, error) {
	var row *domainpaper.Order
	if err := u.withTx(ctx, func(_ Repository, service Service) error {
		var err error
		row, err = service.CreateOrder(ctx, input)
		return err
	}); err != nil {
		return nil, err
	}
	return u.orderRow(ctx, *row), nil
}

func (u Usecase) ApproveOrder(ctx context.Context, id uint, input OrderApproveInput) (*OrderRow, bool, error) {
	var row *domainpaper.Order
	found := false
	if err := u.withTx(ctx, func(repo Repository, service Service) error {
		var err error
		row, found, err = repo.FindOrder(ctx, id)
		if err != nil || !found {
			return err
		}
		if row.Status != domainkernel.OrderSuggested {
			return errors.New("only suggested orders can be approved")
		}
		review := orderApprovalReviewWithRepo(ctx, repo, *row)
		if review.ConfirmRequired && !input.ConfirmHighRisk {
			return fmt.Errorf("%w: %s", ErrHighRiskApprovalRequiresConfirmation, strings.Join(review.Reasons, "; "))
		}
		return service.SubmitOrder(ctx, row)
	}); err != nil {
		return nil, found, err
	}
	if !found {
		return nil, false, nil
	}
	return u.orderRow(ctx, *row), true, nil
}

func (u Usecase) RejectOrder(ctx context.Context, id uint, input OrderRejectInput) (*OrderRow, bool, error) {
	var row *domainpaper.Order
	found := false
	if err := u.withTx(ctx, func(repo Repository, _ Service) error {
		var err error
		row, found, err = repo.FindOrder(ctx, id)
		if err != nil || !found {
			return err
		}
		if row.Status != domainkernel.OrderSuggested {
			return errors.New("only suggested orders can be rejected")
		}
		row.Status = domainkernel.OrderRejected
		note := strings.TrimSpace(input.Reason)
		if note == "" {
			note = "Rejected by manual approval."
		}
		row.ExecutionNote = &note
		return repo.SaveOrder(ctx, row)
	}); err != nil {
		return nil, found, err
	}
	if !found {
		return nil, false, nil
	}
	return u.orderRow(ctx, *row), true, nil
}

func (u Usecase) CancelOrder(ctx context.Context, id uint) (*OrderRow, bool, error) {
	var row *domainpaper.Order
	found := false
	if err := u.withTx(ctx, func(repo Repository, service Service) error {
		var err error
		row, found, err = repo.FindOrder(ctx, id)
		if err != nil || !found {
			return err
		}
		if row.Status != domainkernel.OrderPending && row.Status != domainkernel.OrderSuggested {
			return errors.New("only pending or suggested orders can be cancelled")
		}
		row.Status = domainkernel.OrderCancelled
		note := "Cancelled by user."
		row.ExecutionNote = &note
		return repo.SaveOrder(ctx, row)
	}); err != nil {
		return nil, found, err
	}
	if !found {
		return nil, false, nil
	}
	return u.orderRow(ctx, *row), true, nil
}

func (u Usecase) FillOrder(ctx context.Context, id uint, input domainpaper.OrderFillInput) (*OrderRow, bool, error) {
	var row *domainpaper.Order
	found := false
	if err := u.withTx(ctx, func(repo Repository, service Service) error {
		var err error
		row, found, err = repo.FindOrder(ctx, id)
		if err != nil || !found {
			return err
		}
		return service.FillOrder(ctx, row, input)
	}); err != nil {
		return nil, found, err
	}
	if !found {
		return nil, false, nil
	}
	return u.orderRow(ctx, *row), true, nil
}

func (u Usecase) DeleteOrder(ctx context.Context, id uint) (bool, error) {
	found := false
	if err := u.withTx(ctx, func(repo Repository, _ Service) error {
		row, ok, err := repo.FindOrder(ctx, id)
		if err != nil || !ok {
			found = ok
			return err
		}
		found = true
		if row.Status != domainkernel.OrderCancelled && row.Status != domainkernel.OrderRejected {
			return errors.New("only cancelled or rejected orders can be deleted")
		}
		return repo.DeleteOrder(ctx, row.ID)
	}); err != nil {
		return found, err
	}
	return found, nil
}

func (u Usecase) orderRows(ctx context.Context, rows []domainpaper.Order) []OrderRow {
	out := make([]OrderRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, *u.orderRow(ctx, row))
	}
	return out
}

func (u Usecase) orderRow(ctx context.Context, row domainpaper.Order) *OrderRow {
	public := u.service.OrdersPublic(ctx, []domainpaper.Order{row})
	attrs := map[string]any{}
	if len(public) > 0 {
		attrs = public[0]
	}
	applyOrderApprovalReview(attrs, u.orderApprovalReview(ctx, row))
	return &OrderRow{Order: row, Public: attrs}
}

func (u Usecase) orderApprovalReview(ctx context.Context, row domainpaper.Order) OrderApprovalReview {
	return orderApprovalReviewWithRepo(ctx, u.repo, row)
}

func applyOrderApprovalReview(attrs map[string]any, review OrderApprovalReview) {
	attrs["approval_risk_level"] = review.RiskLevel
	attrs["approval_review_required"] = review.ReviewRequired
	attrs["approval_confirm_required"] = review.ConfirmRequired
	attrs["approval_risk_reasons"] = review.Reasons
	attrs["approval_risk_metrics"] = review.Metrics
}

func orderApprovalReviewWithRepo(ctx context.Context, repo Repository, row domainpaper.Order) OrderApprovalReview {
	review := newOrderApprovalReview(row)
	if row.Status != domainkernel.OrderSuggested {
		return finalizeOrderApprovalReview(row, review)
	}
	if repo == nil {
		review.addRiskReason("high", "审批复核上下文不可用")
		return finalizeOrderApprovalReview(row, review)
	}
	if row.MeetingID == nil {
		review.addRiskReason("medium", "缺少来源会议 ID")
	}
	if row.SourceMeetingEventID == nil {
		review.addRiskReason("high", "缺少会议事件来源，无法回溯证据链")
	}
	switch row.Side {
	case domainkernel.OrderBuy, domainkernel.OrderSell:
	default:
		review.addRiskReason("high", "订单方向无效")
	}
	if row.Quantity <= 0 {
		review.addRiskReason("high", "订单数量必须为正数")
	}
	if !row.SuggestedPrice.IsPositive() {
		review.addRiskReason("high", "缺少有效建议价格，无法计算订单金额")
	}
	if row.Quantity > 0 && row.SuggestedPrice.IsPositive() {
		review.Metrics["notional"] = row.SuggestedPrice.Mul(decimal.NewFromInt(int64(row.Quantity))).Round(2)
	}

	account, found, err := repo.FindAccount(ctx, row.AccountID)
	if err != nil {
		review.addRiskReason("high", "账户复核失败: "+err.Error())
		return finalizeOrderApprovalReview(row, review)
	}
	if !found || account == nil {
		review.addRiskReason("high", "找不到模拟盘账户")
		return finalizeOrderApprovalReview(row, review)
	}
	review.Metrics["account_id"] = account.ID
	review.Metrics["cash"] = account.Cash.Round(2)
	if !account.Active {
		review.addRiskReason("high", "模拟盘账户已停用")
	}
	if account.RiskConfigID == nil {
		review.addRiskReason("high", "账户未绑定风控配置")
		return finalizeOrderApprovalReview(row, review)
	}

	cfg, found, err := repo.FindRiskConfig(ctx, *account.RiskConfigID)
	if err != nil {
		review.addRiskReason("high", "风控配置复核失败: "+err.Error())
		return finalizeOrderApprovalReview(row, review)
	}
	if !found || cfg == nil {
		review.addRiskReason("high", "找不到账户绑定的风控配置")
		return finalizeOrderApprovalReview(row, review)
	}
	review.Metrics["risk_config_id"] = cfg.ID
	review.Metrics["max_order_pct"] = cfg.MaxOrderPct
	review.Metrics["max_position_pct"] = cfg.MaxPositionPct
	if !cfg.Enabled {
		review.addRiskReason("high", "账户绑定的风控配置已停用")
		return finalizeOrderApprovalReview(row, review)
	}

	positions, err := repo.ListPositions(ctx, account.ID)
	if err != nil {
		review.addRiskReason("medium", "持仓复核失败: "+err.Error())
		positions = nil
	}
	totalEquity := approvalTotalEquity(*account, positions)
	review.Metrics["total_equity"] = totalEquity
	if !totalEquity.IsPositive() {
		review.addRiskReason("high", "账户总权益无法计算或不为正")
		return finalizeOrderApprovalReview(row, review)
	}

	notional := decimal.Zero
	if value, ok := review.Metrics["notional"].(decimal.Decimal); ok {
		notional = value
	}
	if notional.IsPositive() {
		orderPct := notional.Div(totalEquity).Round(6)
		review.Metrics["order_pct"] = orderPct
		if cfg.MaxOrderPct.IsPositive() {
			if orderPct.GreaterThanOrEqual(cfg.MaxOrderPct) {
				review.addRiskReason("high", "订单金额占权益 "+formatApprovalPct(orderPct)+"，达到或超过单笔上限 "+formatApprovalPct(cfg.MaxOrderPct))
			} else if orderPct.GreaterThanOrEqual(cfg.MaxOrderPct.Mul(orderApprovalNearLimitThreshold)) {
				review.addRiskReason("high", "订单金额占权益 "+formatApprovalPct(orderPct)+"，接近单笔上限 "+formatApprovalPct(cfg.MaxOrderPct))
			}
		}
	}

	currentPositionValue := approvalCurrentPositionValue(positions, row.Code, row.SuggestedPrice)
	review.Metrics["current_position_value"] = currentPositionValue
	review.Metrics["current_position_quantity"] = approvalHeldQuantity(positions, row.Code)
	if row.Side == domainkernel.OrderBuy && notional.IsPositive() {
		cashPct := decimal.Zero
		if account.Cash.IsPositive() {
			cashPct = notional.Div(account.Cash).Round(6)
			review.Metrics["cash_pct"] = cashPct
		}
		if account.Cash.LessThan(notional) {
			review.addRiskReason("high", "预计订单金额超过账户现金")
		} else if cashPct.GreaterThanOrEqual(orderApprovalNearLimitThreshold) {
			review.addRiskReason("high", "预计占用现金 "+formatApprovalPct(cashPct)+"，需要人工复核流动性")
		}
		postTradePositionValue := currentPositionValue.Add(notional)
		review.Metrics["post_trade_position_value"] = postTradePositionValue.Round(2)
		positionPct := postTradePositionValue.Div(totalEquity).Round(6)
		review.Metrics["post_trade_position_pct"] = positionPct
		if cfg.MaxPositionPct.IsPositive() {
			if positionPct.GreaterThanOrEqual(cfg.MaxPositionPct) {
				review.addRiskReason("high", "买后单标的仓位 "+formatApprovalPct(positionPct)+"，达到或超过仓位上限 "+formatApprovalPct(cfg.MaxPositionPct))
			} else if positionPct.GreaterThanOrEqual(cfg.MaxPositionPct.Mul(orderApprovalNearLimitThreshold)) {
				review.addRiskReason("high", "买后单标的仓位 "+formatApprovalPct(positionPct)+"，接近仓位上限 "+formatApprovalPct(cfg.MaxPositionPct))
			}
		}
	}
	if row.Side == domainkernel.OrderSell && row.Quantity > 0 {
		held := approvalHeldQuantity(positions, row.Code)
		if held < row.Quantity {
			review.addRiskReason("high", fmt.Sprintf("卖出数量 %d 超过当前持仓 %d", row.Quantity, held))
		}
	}
	return finalizeOrderApprovalReview(row, review)
}

func newOrderApprovalReview(row domainpaper.Order) OrderApprovalReview {
	return OrderApprovalReview{
		RiskLevel: "low",
		Reasons:   []string{},
		Metrics: map[string]any{
			"status": row.Status,
		},
	}
}

func finalizeOrderApprovalReview(row domainpaper.Order, review OrderApprovalReview) OrderApprovalReview {
	if review.RiskLevel == "" {
		review.RiskLevel = "low"
	}
	if review.Reasons == nil {
		review.Reasons = []string{}
	}
	if review.Metrics == nil {
		review.Metrics = map[string]any{}
	}
	review.ReviewRequired = row.Status == domainkernel.OrderSuggested && review.RiskLevel != "low"
	review.ConfirmRequired = row.Status == domainkernel.OrderSuggested && review.RiskLevel == "high"
	return review
}

func (review *OrderApprovalReview) addRiskReason(level string, reason string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return
	}
	if approvalRiskRank(level) > approvalRiskRank(review.RiskLevel) {
		review.RiskLevel = level
	}
	for _, existing := range review.Reasons {
		if existing == reason {
			return
		}
	}
	review.Reasons = append(review.Reasons, reason)
}

func approvalRiskRank(level string) int {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

func approvalTotalEquity(account domainpaper.Account, positions []domainpaper.Position) decimal.Decimal {
	total := account.Cash
	for _, position := range positions {
		total = total.Add(approvalPositionValue(position, decimal.Zero))
	}
	return total.Round(2)
}

func approvalCurrentPositionValue(positions []domainpaper.Position, code string, referencePrice decimal.Decimal) decimal.Decimal {
	for _, position := range positions {
		if strings.EqualFold(position.Code, code) {
			return approvalPositionValue(position, referencePrice)
		}
	}
	return decimal.Zero
}

func approvalPositionValue(position domainpaper.Position, referencePrice decimal.Decimal) decimal.Decimal {
	if position.Quantity <= 0 {
		return decimal.Zero
	}
	if referencePrice.IsPositive() {
		return referencePrice.Mul(decimal.NewFromInt(int64(position.Quantity))).Round(2)
	}
	if position.MarketValue.IsPositive() {
		return position.MarketValue.Round(2)
	}
	if position.LastPrice.IsPositive() {
		return position.LastPrice.Mul(decimal.NewFromInt(int64(position.Quantity))).Round(2)
	}
	if position.CostAmount.IsPositive() {
		return position.CostAmount.Round(2)
	}
	return decimal.Zero
}

func approvalHeldQuantity(positions []domainpaper.Position, code string) int {
	for _, position := range positions {
		if strings.EqualFold(position.Code, code) {
			return position.Quantity
		}
	}
	return 0
}

func formatApprovalPct(value decimal.Decimal) string {
	return value.Mul(decimal.NewFromInt(100)).Round(2).String() + "%"
}

func (u Usecase) riskConfigRows(ctx context.Context, rows []domainpaper.RiskConfig) ([]RiskConfigRow, error) {
	out := make([]RiskConfigRow, 0, len(rows))
	for _, row := range rows {
		accountIDs, err := u.repo.RiskConfigAccountIDs(ctx, row.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, RiskConfigRow{Config: row, AccountIDs: accountIDs})
	}
	return out, nil
}

func (u Usecase) withTx(ctx context.Context, fn func(Repository, Service) error) error {
	if u.tx != nil {
		return u.tx.WithTx(ctx, fn)
	}
	return errors.New("paper unit of work is not configured")
}

func (u Usecase) ensureDefaultSetup(ctx context.Context) error {
	return u.service.EnsureDefaultSetup(ctx)
}

func fillRows(public []map[string]any) []FillRow {
	out := make([]FillRow, 0, len(public))
	for _, item := range public {
		out = append(out, FillRow{Public: item})
	}
	return out
}

func corporateActionRows(rows []domainpaper.CorporateAction) []CorporateActionRow {
	out := make([]CorporateActionRow, 0, len(rows))
	for _, row := range rows {
		public := map[string]any{
			"id":             row.ID,
			"accountId":      row.AccountID,
			"code":           row.Code,
			"actionType":     row.ActionType,
			"exDate":         row.ExDate,
			"cashPerShare":   row.CashPerShare,
			"shareRatio":     row.ShareRatio,
			"affectedShares": row.AffectedShares,
			"cashAmount":     row.CashAmount,
			"status":         row.Status,
			"note":           row.Note,
			"appliedAt":      row.AppliedAt,
			"createdAt":      row.CreatedAt,
		}
		out = append(out, CorporateActionRow{Action: row, Public: public})
	}
	return out
}

func applyCorporateActionInput(input domainpaper.CorporateActionInput, account *domainpaper.Account, position *domainpaper.Position) (domainpaper.CorporateAction, error) {
	if account == nil || account.ID == 0 {
		return domainpaper.CorporateAction{}, errors.New("paper account is required")
	}
	if position == nil || position.ID == 0 {
		return domainpaper.CorporateAction{}, errors.New("paper position is required")
	}
	code := strings.ToUpper(strings.TrimSpace(input.Code))
	actionType := strings.ToLower(strings.TrimSpace(input.ActionType))
	if code == "" {
		return domainpaper.CorporateAction{}, errors.New("corporate action code is required")
	}
	if position.AccountID != account.ID || strings.ToUpper(strings.TrimSpace(position.Code)) != code {
		return domainpaper.CorporateAction{}, errors.New("corporate action position does not match account and code")
	}
	if position.Quantity <= 0 {
		return domainpaper.CorporateAction{}, errors.New("corporate action requires a positive holding quantity")
	}
	exDate := input.ExDate
	now := time.Now()
	if exDate.IsZero() {
		exDate = now
	}
	note := input.Note
	action := domainpaper.CorporateAction{
		AccountID:      account.ID,
		Code:           code,
		ActionType:     actionType,
		ExDate:         exDate,
		CashPerShare:   input.CashPerShare,
		ShareRatio:     input.ShareRatio,
		AffectedShares: position.Quantity,
		Status:         "applied",
		Note:           note,
		AppliedAt:      &now,
	}
	switch actionType {
	case "cash_dividend":
		if !input.CashPerShare.IsPositive() {
			return domainpaper.CorporateAction{}, errors.New("cash dividend requires a positive cashPerShare")
		}
		action.ShareRatio = decimal.Zero
		action.AffectedShares = position.Quantity
		action.CashAmount = input.CashPerShare.Mul(decimal.NewFromInt(int64(position.Quantity)))
	case "bonus_share":
		if !input.ShareRatio.IsPositive() {
			return domainpaper.CorporateAction{}, errors.New("bonus share requires a positive shareRatio")
		}
		action.CashPerShare = decimal.Zero
		action.CashAmount = decimal.Zero
		action.AffectedShares = int(decimal.NewFromInt(int64(position.Quantity)).Mul(input.ShareRatio).IntPart())
	case "split":
		if input.ShareRatio.Cmp(decimal.NewFromInt(1)) <= 0 {
			return domainpaper.CorporateAction{}, errors.New("split requires shareRatio greater than 1")
		}
		action.CashPerShare = decimal.Zero
		action.CashAmount = decimal.Zero
		additionalRatio := input.ShareRatio.Sub(decimal.NewFromInt(1))
		action.AffectedShares = int(decimal.NewFromInt(int64(position.Quantity)).Mul(additionalRatio).IntPart())
	default:
		return domainpaper.CorporateAction{}, fmt.Errorf("unsupported corporate action type %q", actionType)
	}
	if action.ActionType != "cash_dividend" && action.AffectedShares <= 0 {
		return domainpaper.CorporateAction{}, errors.New("corporate action produces no whole shares")
	}
	return action, nil
}

func replayEvents(fills []domainpaper.Fill, orders []domainpaper.Order, actions []domainpaper.CorporateAction) []ReplayEvent {
	events := make([]ReplayEvent, 0, len(fills)+len(orders)+len(actions))
	for _, row := range orders {
		when := row.CreatedAt
		if row.SubmittedAt != nil {
			when = *row.SubmittedAt
		}
		events = append(events, ReplayEvent{
			ID:      fmt.Sprintf("order:%d", row.ID),
			Type:    "order",
			Time:    when,
			Code:    row.Code,
			Summary: fmt.Sprintf("%s %s %d shares, status %s", row.Code, row.Side, row.Quantity, row.Status),
			Attributes: map[string]any{
				"accountId":            row.AccountID,
				"meetingId":            row.MeetingID,
				"sourceMeetingEventId": row.SourceMeetingEventID,
				"side":                 row.Side,
				"quantity":             row.Quantity,
				"status":               row.Status,
				"suggestedPrice":       row.SuggestedPrice,
				"filledPrice":          row.FilledPrice,
				"executeAfter":         row.ExecuteAfter,
				"expireAt":             row.ExpireAt,
				"createdAt":            row.CreatedAt,
			},
		})
	}
	for _, row := range fills {
		events = append(events, ReplayEvent{
			ID:      fmt.Sprintf("fill:%d", row.ID),
			Type:    "fill",
			Time:    row.FilledAt,
			Code:    row.Code,
			Summary: fmt.Sprintf("%s %s %d shares at %s", row.Code, row.Side, row.Quantity, row.Price.String()),
			Attributes: map[string]any{
				"orderId":     row.OrderID,
				"accountId":   row.AccountID,
				"side":        row.Side,
				"quantity":    row.Quantity,
				"price":       row.Price,
				"grossAmount": row.GrossAmount,
				"commission":  row.Commission,
				"stampDuty":   row.StampDuty,
				"transferFee": row.TransferFee,
				"netAmount":   row.NetAmount,
				"realizedPnl": row.RealizedPNL,
				"filledAt":    row.FilledAt,
			},
		})
	}
	for _, row := range actions {
		when := row.ExDate
		if row.AppliedAt != nil {
			when = *row.AppliedAt
		}
		events = append(events, ReplayEvent{
			ID:      fmt.Sprintf("corporate_action:%d", row.ID),
			Type:    "corporate_action",
			Time:    when,
			Code:    row.Code,
			Summary: corporateActionSummary(row),
			Attributes: map[string]any{
				"accountId":      row.AccountID,
				"actionType":     row.ActionType,
				"exDate":         row.ExDate,
				"cashPerShare":   row.CashPerShare,
				"shareRatio":     row.ShareRatio,
				"affectedShares": row.AffectedShares,
				"cashAmount":     row.CashAmount,
				"status":         row.Status,
				"note":           row.Note,
				"appliedAt":      row.AppliedAt,
				"createdAt":      row.CreatedAt,
			},
		})
	}
	sort.SliceStable(events, func(i, j int) bool {
		left, right := events[i], events[j]
		if !left.Time.Equal(right.Time) {
			return left.Time.Before(right.Time)
		}
		return left.ID < right.ID
	})
	return events
}

func corporateActionSummary(row domainpaper.CorporateAction) string {
	switch row.ActionType {
	case "cash_dividend":
		return fmt.Sprintf("%s cash dividend %s per share, cash +%s", row.Code, row.CashPerShare.String(), row.CashAmount.String())
	case "bonus_share":
		return fmt.Sprintf("%s bonus shares +%d at ratio %s", row.Code, row.AffectedShares, row.ShareRatio.String())
	case "split":
		return fmt.Sprintf("%s split ratio %s, shares +%d", row.Code, row.ShareRatio.String(), row.AffectedShares)
	default:
		return fmt.Sprintf("%s corporate action %s", row.Code, row.ActionType)
	}
}

func normalizeBacktestInput(input BacktestInput, account domainpaper.Account) (BacktestInput, error) {
	input.Code = strings.ToUpper(strings.TrimSpace(input.Code))
	if input.Code == "" {
		return input, errors.New("backtest code is required")
	}
	now := time.Now()
	if input.EndDate.IsZero() {
		input.EndDate = now
	}
	if input.StartDate.IsZero() {
		input.StartDate = input.EndDate.AddDate(0, 0, -180)
	}
	input.StartDate = dateOnly(input.StartDate)
	input.EndDate = dateOnly(input.EndDate)
	if input.EndDate.Before(input.StartDate) {
		return input, errors.New("backtest endDate must be on or after startDate")
	}
	if input.EndDate.Sub(input.StartDate) > 5000*24*time.Hour {
		return input, errors.New("backtest date range cannot exceed 5000 days")
	}
	if !input.InitialCash.IsPositive() {
		input.InitialCash = firstPositiveDecimal(account.InitialCash, account.Cash, decimal.NewFromInt(1000000))
	}
	if input.BuyThresholdPct.IsZero() {
		input.BuyThresholdPct = decimal.NewFromInt(-3)
	}
	if input.SellThresholdPct.IsZero() {
		input.SellThresholdPct = decimal.NewFromInt(3)
	}
	if input.BuyThresholdPct.GreaterThan(decimal.Zero) {
		return input, errors.New("buyThresholdPct must be zero or negative")
	}
	if !input.SellThresholdPct.IsPositive() {
		return input, errors.New("sellThresholdPct must be positive")
	}
	if input.OrderPct.IsZero() {
		input.OrderPct = decimal.RequireFromString("0.10")
	}
	if !input.OrderPct.IsPositive() || input.OrderPct.GreaterThan(decimal.NewFromInt(1)) {
		return input, errors.New("orderPct must be greater than 0 and no greater than 1")
	}
	if input.SlippageBps.IsZero() {
		input.SlippageBps = decimal.NewFromInt(5)
	}
	if input.SlippageBps.IsNegative() || input.SlippageBps.GreaterThan(decimal.NewFromInt(1000)) {
		return input, errors.New("slippageBps must be between 0 and 1000")
	}
	return input, nil
}

func runDailyBacktest(input BacktestInput, risk backtestRisk, bars []domainmarket.DailyBar) *BacktestReport {
	sort.SliceStable(bars, func(i, j int) bool {
		return bars[i].TradeDate.Before(bars[j].TradeDate)
	})
	const lotSize = 100
	cash := input.InitialCash
	quantity := 0
	avgCost := decimal.Zero
	prevClose := decimal.Zero
	prevEquity := input.InitialCash
	peakEquity := input.InitialCash
	maxDrawdown := decimal.Zero
	tradeCount := 0
	rejectedCount := 0
	series := make([]BacktestPoint, 0, len(bars))
	orders := []BacktestOrder{}
	var lastBuyDate time.Time

	for _, bar := range bars {
		if !bar.Close.IsPositive() {
			continue
		}
		tradeDate := dateOnly(bar.TradeDate)
		signalPct := decimal.Zero
		if prevClose.IsPositive() {
			signalPct = bar.Close.Sub(prevClose).Div(prevClose).Mul(decimal.NewFromInt(100))
			order, filled := evaluateBacktestSignal(input, risk, bar, prevClose, signalPct, cash, quantity, avgCost, lastBuyDate, lotSize)
			if order != nil {
				if filled {
					tradeCount++
					if order.Side == string(domainkernel.OrderBuy) {
						cash = cash.Sub(order.GrossAmount).Sub(order.Fees)
						avgCost = avgCost.Mul(decimal.NewFromInt(int64(quantity))).Add(order.GrossAmount).Add(order.Fees).
							Div(decimal.NewFromInt(int64(quantity + order.Quantity)))
						quantity += order.Quantity
						lastBuyDate = tradeDate
					} else {
						cash = cash.Add(order.GrossAmount).Sub(order.Fees)
						quantity -= order.Quantity
						if quantity <= 0 {
							quantity = 0
							avgCost = decimal.Zero
						}
					}
				} else {
					rejectedCount++
				}
				order.CashAfter = cash.Round(2)
				order.PositionAfter = quantity
				orders = append(orders, *order)
			}
		}
		marketValue := bar.Close.Mul(decimal.NewFromInt(int64(quantity))).Round(2)
		totalEquity := cash.Add(marketValue).Round(2)
		dailyReturn := decimal.Zero
		if prevEquity.IsPositive() {
			dailyReturn = totalEquity.Sub(prevEquity).Div(prevEquity).Mul(decimal.NewFromInt(100))
		}
		if totalEquity.GreaterThan(peakEquity) {
			peakEquity = totalEquity
		}
		drawdown := decimal.Zero
		if peakEquity.IsPositive() {
			drawdown = peakEquity.Sub(totalEquity).Div(peakEquity).Mul(decimal.NewFromInt(100))
			if drawdown.GreaterThan(maxDrawdown) {
				maxDrawdown = drawdown
			}
		}
		series = append(series, BacktestPoint{
			TradeDate:      tradeDate,
			Close:          bar.Close,
			SignalPct:      signalPct,
			Cash:           cash.Round(2),
			Quantity:       quantity,
			MarketValue:    marketValue,
			TotalEquity:    totalEquity,
			DailyReturnPct: dailyReturn,
			DrawdownPct:    drawdown,
		})
		prevEquity = totalEquity
		prevClose = bar.Close
	}

	finalCash := cash.Round(2)
	finalMarketValue := decimal.Zero
	finalEquity := finalCash
	if len(series) > 0 {
		last := series[len(series)-1]
		finalMarketValue = last.MarketValue
		finalEquity = last.TotalEquity
	}
	totalReturn := decimal.Zero
	if input.InitialCash.IsPositive() {
		totalReturn = finalEquity.Sub(input.InitialCash).Div(input.InitialCash).Mul(decimal.NewFromInt(100))
	}
	return &BacktestReport{
		GeneratedAt: time.Now(),
		Input:       input,
		Policy: BacktestPolicy{
			ExecutionModel:    "daily_close_threshold",
			RiskModel:         risk.Source,
			BrokerIntegration: "disabled",
			DataSource:        "daily_bars",
			LimitBandPct:      decimal.NewFromInt(10),
			SlippageBps:       input.SlippageBps,
			LotSize:           lotSize,
			Rules: []string{
				"A-share lot size is 100 shares",
				"buy signals use close-to-previous-close pct <= buyThresholdPct",
				"sell signals use close-to-previous-close pct >= sellThresholdPct",
				"zero-volume bars are treated as suspended",
				"T+1 blocks same-day sells after a simulated buy",
				"daily limit band uses a conservative 10% default",
				"orders are simulated and never mutate paper accounts",
			},
		},
		Summary: BacktestSummary{
			BarCount:         len(series),
			TradeCount:       tradeCount,
			RejectedCount:    rejectedCount,
			InitialCash:      input.InitialCash,
			FinalCash:        finalCash,
			FinalMarketValue: finalMarketValue,
			FinalEquity:      finalEquity,
			TotalReturnPct:   totalReturn,
			MaxDrawdownPct:   maxDrawdown,
		},
		Series: series,
		Orders: orders,
	}
}

func evaluateBacktestSignal(input BacktestInput, risk backtestRisk, bar domainmarket.DailyBar, prevClose decimal.Decimal, signalPct decimal.Decimal, cash decimal.Decimal, quantity int, avgCost decimal.Decimal, lastBuyDate time.Time, lotSize int) (*BacktestOrder, bool) {
	tradeDate := dateOnly(bar.TradeDate)
	if signalPct.GreaterThanOrEqual(input.SellThresholdPct) && quantity > 0 {
		order := newBacktestOrder(input, bar, signalPct, domainkernel.OrderSell)
		if !lastBuyDate.IsZero() && dateOnly(lastBuyDate).Equal(tradeDate) {
			order.Status = "rejected"
			order.Reason = "blocked_by_t_plus_one"
			return order, false
		}
		if isBacktestSuspended(bar) {
			order.Status = "rejected"
			order.Reason = "blocked_by_zero_volume"
			return order, false
		}
		if isBacktestDownLimit(bar.Close, prevClose) {
			order.Status = "rejected"
			order.Reason = "blocked_by_daily_limit_down"
			return order, false
		}
		qty := floorToLot(quantity, lotSize)
		if qty <= 0 {
			order.Status = "rejected"
			order.Reason = "quantity_below_lot"
			return order, false
		}
		order.Quantity = qty
		order.FilledPrice = backtestExecutionPrice(bar.Close, domainkernel.OrderSell, input.SlippageBps)
		order.GrossAmount = order.FilledPrice.Mul(decimal.NewFromInt(int64(qty))).Round(2)
		order.Fees = backtestFees(order.GrossAmount, domainkernel.OrderSell, risk)
		if avgCost.IsPositive() {
			order.Reason = "filled; estimated realized pnl " + order.FilledPrice.Sub(avgCost).Mul(decimal.NewFromInt(int64(qty))).Sub(order.Fees).Round(2).String()
		} else {
			order.Reason = "filled"
		}
		order.Status = "filled"
		return order, true
	}
	if signalPct.LessThanOrEqual(input.BuyThresholdPct) {
		order := newBacktestOrder(input, bar, signalPct, domainkernel.OrderBuy)
		if isBacktestSuspended(bar) {
			order.Status = "rejected"
			order.Reason = "blocked_by_zero_volume"
			return order, false
		}
		if isBacktestUpLimit(bar.Close, prevClose) {
			order.Status = "rejected"
			order.Reason = "blocked_by_daily_limit_up"
			return order, false
		}
		equity := cash.Add(bar.Close.Mul(decimal.NewFromInt(int64(quantity))))
		orderBudget := equity.Mul(decimalMin(input.OrderPct, risk.MaxOrderPct))
		positionRoom := equity.Mul(risk.MaxPositionPct).Sub(bar.Close.Mul(decimal.NewFromInt(int64(quantity))))
		budget := decimalMin(orderBudget, cash, positionRoom)
		if !budget.IsPositive() {
			order.Status = "rejected"
			order.Reason = "blocked_by_cash_or_position_limit"
			return order, false
		}
		order.FilledPrice = backtestExecutionPrice(bar.Close, domainkernel.OrderBuy, input.SlippageBps)
		qty := floorToLot(int(budget.Div(order.FilledPrice).IntPart()), lotSize)
		for qty > 0 {
			gross := order.FilledPrice.Mul(decimal.NewFromInt(int64(qty))).Round(2)
			fees := backtestFees(gross, domainkernel.OrderBuy, risk)
			totalCost := gross.Add(fees)
			if totalCost.LessThanOrEqual(cash) && totalCost.LessThanOrEqual(positionRoom) && totalCost.LessThanOrEqual(orderBudget) {
				order.Quantity = qty
				order.GrossAmount = gross
				order.Fees = fees
				order.Status = "filled"
				order.Reason = "filled"
				return order, true
			}
			qty -= lotSize
		}
		order.Status = "rejected"
		order.Reason = "quantity_below_lot_after_fees"
		return order, false
	}
	return nil, false
}

func newBacktestOrder(input BacktestInput, bar domainmarket.DailyBar, signalPct decimal.Decimal, side domainkernel.OrderSide) *BacktestOrder {
	tradeDate := dateOnly(bar.TradeDate)
	return &BacktestOrder{
		ID:             fmt.Sprintf("%s:%s:%s", input.Code, tradeDate.Format("20060102"), side),
		TradeDate:      tradeDate,
		Code:           input.Code,
		Side:           string(side),
		SignalPct:      signalPct,
		ReferencePrice: bar.Close,
		FilledPrice:    decimal.Zero,
		Status:         "suggested",
		Reason:         "signal",
	}
}

func defaultBacktestRisk() backtestRisk {
	return backtestRisk{
		Source:          "default_paper_risk",
		MaxOrderPct:     decimal.RequireFromString("0.20"),
		MaxPositionPct:  decimal.RequireFromString("0.30"),
		CommissionRate:  decimal.RequireFromString("0.0001"),
		MinCommission:   decimal.NewFromInt(5),
		StampDutyRate:   decimal.RequireFromString("0.001"),
		TransferFeeRate: decimal.RequireFromString("0.00001"),
	}
}

func backtestExecutionPrice(price decimal.Decimal, side domainkernel.OrderSide, slippageBps decimal.Decimal) decimal.Decimal {
	rate := slippageBps.Div(decimal.NewFromInt(10000))
	if side == domainkernel.OrderSell {
		return price.Mul(decimal.NewFromInt(1).Sub(rate)).Round(4)
	}
	return price.Mul(decimal.NewFromInt(1).Add(rate)).Round(4)
}

func backtestFees(gross decimal.Decimal, side domainkernel.OrderSide, risk backtestRisk) decimal.Decimal {
	commission := gross.Mul(risk.CommissionRate)
	if risk.MinCommission.IsPositive() && commission.LessThan(risk.MinCommission) {
		commission = risk.MinCommission
	}
	stampDuty := decimal.Zero
	if side == domainkernel.OrderSell {
		stampDuty = gross.Mul(risk.StampDutyRate)
	}
	return commission.Add(stampDuty).Add(gross.Mul(risk.TransferFeeRate)).Round(2)
}

func floorToLot(quantity int, lotSize int) int {
	if lotSize <= 0 || quantity <= 0 {
		return 0
	}
	return quantity / lotSize * lotSize
}

func isBacktestSuspended(bar domainmarket.DailyBar) bool {
	return bar.Volume.IsZero()
}

func isBacktestUpLimit(close decimal.Decimal, prevClose decimal.Decimal) bool {
	return prevClose.IsPositive() && close.GreaterThanOrEqual(prevClose.Mul(decimal.RequireFromString("1.10")).Round(2))
}

func isBacktestDownLimit(close decimal.Decimal, prevClose decimal.Decimal) bool {
	return prevClose.IsPositive() && close.LessThanOrEqual(prevClose.Mul(decimal.RequireFromString("0.90")).Round(2))
}

func dateOnly(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, value.Location())
}

func decimalMin(values ...decimal.Decimal) decimal.Decimal {
	if len(values) == 0 {
		return decimal.Zero
	}
	min := values[0]
	for _, value := range values[1:] {
		if value.LessThan(min) {
			min = value
		}
	}
	return min
}

func firstPositiveDecimal(values ...decimal.Decimal) decimal.Decimal {
	for _, value := range values {
		if value.IsPositive() {
			return value
		}
	}
	return decimal.Zero
}

func normalizePage(page Page) Page {
	if page.Limit <= 0 {
		page.Limit = 100
	}
	return page
}

func cursorID(cursor string) uint64 {
	if strings.TrimSpace(cursor) == "" {
		return 0
	}
	id, _ := strconv.ParseUint(cursor, 10, 64)
	return id
}

func applyRiskConfigInput(row *domainpaper.RiskConfig, input domainpaper.RiskConfigInput, create bool) error {
	if input.Name != nil {
		row.Name = strings.TrimSpace(*input.Name)
	}
	if input.InitialCash != nil {
		row.InitialCash = *input.InitialCash
	}
	if input.MaxPositionPct != nil {
		row.MaxPositionPct = *input.MaxPositionPct
	}
	if input.MaxOrderPct != nil {
		row.MaxOrderPct = *input.MaxOrderPct
	}
	if input.AllowShort != nil {
		row.AllowShort = *input.AllowShort
	} else if create {
		row.AllowShort = false
	}
	if input.AllowMargin != nil {
		row.AllowMargin = *input.AllowMargin
	} else if create {
		row.AllowMargin = false
	}
	if input.AllowSHMain != nil {
		row.AllowSHMain = *input.AllowSHMain
	} else if create {
		row.AllowSHMain = true
	}
	if input.AllowSZMain != nil {
		row.AllowSZMain = *input.AllowSZMain
	} else if create {
		row.AllowSZMain = true
	}
	if input.AllowBJ != nil {
		row.AllowBJ = *input.AllowBJ
	} else if create {
		row.AllowBJ = false
	}
	if input.AllowSTAR != nil {
		row.AllowSTAR = *input.AllowSTAR
	} else if create {
		row.AllowSTAR = false
	}
	if input.AllowChiNext != nil {
		row.AllowChiNext = *input.AllowChiNext
	} else if create {
		row.AllowChiNext = false
	}
	if input.AllowETFLOF != nil {
		row.AllowETFLOF = *input.AllowETFLOF
	} else if create {
		row.AllowETFLOF = true
	}
	if input.CommissionRate != nil {
		row.CommissionRate = *input.CommissionRate
	} else if create {
		row.CommissionRate = decimal.RequireFromString("0.00005")
	}
	if input.MinCommission != nil {
		row.MinCommission = *input.MinCommission
	} else if create {
		row.MinCommission = decimal.RequireFromString("5.00")
	}
	if input.StampDutyRate != nil {
		row.StampDutyRate = *input.StampDutyRate
	} else if create {
		row.StampDutyRate = decimal.RequireFromString("0.001")
	}
	if input.TransferFeeRate != nil {
		row.TransferFeeRate = *input.TransferFeeRate
	} else if create {
		row.TransferFeeRate = decimal.RequireFromString("0.00001")
	}
	if input.Enabled != nil {
		row.Enabled = *input.Enabled
	} else if create {
		row.Enabled = false
	}
	if strings.TrimSpace(row.Name) == "" {
		return errors.New("risk config name is required")
	}
	if !row.AllowSHMain && !row.AllowSZMain && !row.AllowBJ && !row.AllowSTAR && !row.AllowChiNext && !row.AllowETFLOF {
		return errors.New("risk config must allow at least one trading scope")
	}
	return nil
}

func uniqueUintIDs(values []uint) []uint {
	seen := map[uint]bool{}
	out := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
