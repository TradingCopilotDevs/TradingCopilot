package paper

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
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
}

type Service interface {
	Overview(ctx context.Context) (map[string]any, error)
	CreateAccount(ctx context.Context, input domainpaper.AccountInput) (*domainpaper.Account, error)
	AccountPublic(ctx context.Context, account domainpaper.Account) map[string]any
	PositionsPublic(ctx context.Context, rows []domainpaper.Position) []map[string]any
	FillsPublic(ctx context.Context, rows []domainpaper.Fill) []map[string]any
	Performance(ctx context.Context, account domainpaper.Account) map[string]any
	CreateOrder(ctx context.Context, input domainpaper.OrderInput) (*domainpaper.Order, error)
	FillOrder(ctx context.Context, row *domainpaper.Order, price decimal.Decimal) error
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

type FillRow struct {
	Public map[string]any
}

type Page struct {
	Limit  int
	Cursor string
}

type OrderList struct {
	Rows       []OrderRow
	NextCursor string
}

type FillList struct {
	Rows       []FillRow
	NextCursor string
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

func (u Usecase) FillOrder(ctx context.Context, id uint, price decimal.Decimal) (*OrderRow, bool, error) {
	var row *domainpaper.Order
	found := false
	if err := u.withTx(ctx, func(repo Repository, service Service) error {
		var err error
		row, found, err = repo.FindOrder(ctx, id)
		if err != nil || !found {
			return err
		}
		return service.FillOrder(ctx, row, price)
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
	return &OrderRow{Order: row, Public: attrs}
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
