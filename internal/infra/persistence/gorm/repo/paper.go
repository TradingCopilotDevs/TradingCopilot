package repo

import (
	"context"
	"errors"
	"fmt"
	apppaper "github.com/TradingCopilotDevs/TradingCopilot/internal/app/paper"
	domainpaper "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/paper"
	"time"

	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"gorm.io/gorm"
)

type PaperRepository struct {
	db *gorm.DB
}

func NewPaperRepository(db *gorm.DB) PaperRepository {
	return PaperRepository{db: db}
}

var riskConfigWritableFields = []string{
	"Name", "InitialCash", "MaxPositionPct", "MaxOrderPct",
	"AllowShort", "AllowMargin", "AllowSHMain", "AllowSZMain", "AllowBJ", "AllowSTAR", "AllowChiNext", "AllowETFLOF",
	"CommissionRate", "MinCommission", "StampDutyRate", "TransferFeeRate", "Enabled",
}

func (r PaperRepository) ListRiskConfigs(ctx context.Context) ([]domainpaper.RiskConfig, error) {
	var rows []persistmodel.RiskConfig
	if err := r.db.WithContext(ctx).Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	return riskConfigsToDomain(rows), nil
}

func (r PaperRepository) FindRiskConfig(ctx context.Context, id uint) (*domainpaper.RiskConfig, bool, error) {
	var row persistmodel.RiskConfig
	err := r.db.WithContext(ctx).First(&row, id).Error
	if err == nil {
		out := riskConfigFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r PaperRepository) CreateRiskConfig(ctx context.Context, row *domainpaper.RiskConfig) error {
	modelRow := riskConfigToModel(*row)
	db := r.db.WithContext(ctx)
	now := time.Now()
	if err := db.Model(&persistmodel.RiskConfig{}).Create(map[string]any{
		"name":              modelRow.Name,
		"initial_cash":      modelRow.InitialCash,
		"max_position_pct":  modelRow.MaxPositionPct,
		"max_order_pct":     modelRow.MaxOrderPct,
		"allow_short":       modelRow.AllowShort,
		"allow_margin":      modelRow.AllowMargin,
		"allow_sh_main":     modelRow.AllowSHMain,
		"allow_sz_main":     modelRow.AllowSZMain,
		"allow_bj":          modelRow.AllowBJ,
		"allow_star":        modelRow.AllowSTAR,
		"allow_chinext":     modelRow.AllowChiNext,
		"allow_etf_lof":     modelRow.AllowETFLOF,
		"commission_rate":   modelRow.CommissionRate,
		"min_commission":    modelRow.MinCommission,
		"stamp_duty_rate":   modelRow.StampDutyRate,
		"transfer_fee_rate": modelRow.TransferFeeRate,
		"enabled":           modelRow.Enabled,
		"created_at":        now,
		"updated_at":        now,
	}).Error; err != nil {
		return err
	}
	if err := db.Where("name = ?", modelRow.Name).First(&modelRow).Error; err != nil {
		return err
	}
	*row = riskConfigFromModel(modelRow)
	return nil
}

func (r PaperRepository) SaveRiskConfig(ctx context.Context, row *domainpaper.RiskConfig) error {
	modelRow := riskConfigToModel(*row)
	db := r.db.WithContext(ctx)
	if err := db.Model(&persistmodel.RiskConfig{ID: modelRow.ID}).Select(riskConfigWritableFields).Updates(&modelRow).Error; err != nil {
		return err
	}
	if err := db.First(&modelRow, modelRow.ID).Error; err != nil {
		return err
	}
	*row = riskConfigFromModel(modelRow)
	return nil
}

func (r PaperRepository) DeleteRiskConfig(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&persistmodel.RiskConfig{}, id).Error
}

func (r PaperRepository) ReassignRiskConfigAccounts(ctx context.Context, fromID uint, toID uint) error {
	return r.db.WithContext(ctx).Model(&persistmodel.PaperAccount{}).Where("risk_config_id = ?", fromID).Update("risk_config_id", toID).Error
}

func (r PaperRepository) RiskConfigAccountIDs(ctx context.Context, id uint) ([]uint, error) {
	var ids []uint
	if err := r.db.WithContext(ctx).Model(&persistmodel.PaperAccount{}).Where("risk_config_id = ?", id).Order("id").Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (r PaperRepository) ReplaceRiskConfigAccounts(ctx context.Context, id uint, accountIDs []uint) error {
	accountIDs = uniqueUintIDs(accountIDs)
	db := r.db.WithContext(ctx)
	if err := db.Model(&persistmodel.PaperAccount{}).Where("risk_config_id = ?", id).Update("risk_config_id", nil).Error; err != nil {
		return err
	}
	if len(accountIDs) == 0 {
		return nil
	}
	result := db.Model(&persistmodel.PaperAccount{}).Where("id IN ?", accountIDs).Update("risk_config_id", id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != int64(len(accountIDs)) {
		return fmt.Errorf("one or more paper accounts were not found")
	}
	return nil
}

func (r PaperRepository) UnbindRiskConfigAccounts(ctx context.Context, id uint) ([]uint, error) {
	ids, err := r.RiskConfigAccountIDs(ctx, id)
	if err != nil || len(ids) == 0 {
		return ids, err
	}
	return ids, r.db.WithContext(ctx).Model(&persistmodel.PaperAccount{}).Where("risk_config_id = ?", id).Update("risk_config_id", nil).Error
}

func (r PaperRepository) ListAccounts(ctx context.Context) ([]domainpaper.Account, error) {
	var rows []persistmodel.PaperAccount
	if err := r.db.WithContext(ctx).Order("name").Find(&rows).Error; err != nil {
		return nil, err
	}
	return paperAccountsToDomain(rows), nil
}

func (r PaperRepository) FindAccount(ctx context.Context, id uint) (*domainpaper.Account, bool, error) {
	var row persistmodel.PaperAccount
	err := r.db.WithContext(ctx).First(&row, id).Error
	if err == nil {
		out := paperAccountFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r PaperRepository) AccountResearchTeam(ctx context.Context, accountID uint) (*apppaper.AccountTeam, bool, error) {
	var row persistmodel.ResearchTeam
	err := r.db.WithContext(ctx).Where("paper_account_id = ?", accountID).First(&row).Error
	if err == nil {
		return &apppaper.AccountTeam{ID: row.ID, Name: row.Name}, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r PaperRepository) SaveAccount(ctx context.Context, row *domainpaper.Account) error {
	modelRow := paperAccountToModel(*row)
	if err := r.db.WithContext(ctx).Save(&modelRow).Error; err != nil {
		return err
	}
	*row = paperAccountFromModel(modelRow)
	return nil
}

func (r PaperRepository) DeleteAccount(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&persistmodel.PaperAccount{}, id).Error
}

func (r PaperRepository) ListPositions(ctx context.Context, accountID uint) ([]domainpaper.Position, error) {
	var rows []persistmodel.PaperPosition
	if err := r.db.WithContext(ctx).Where("account_id = ?", accountID).Order("code").Find(&rows).Error; err != nil {
		return nil, err
	}
	return paperPositionsToDomain(rows), nil
}

func (r PaperRepository) ListAccountOrders(ctx context.Context, accountID uint, status string, limit int, cursorID uint64) ([]domainpaper.Order, error) {
	var rows []persistmodel.PaperOrder
	q := r.db.WithContext(ctx).Where("account_id = ?", accountID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if cursorID > 0 {
		q = q.Where("id < ?", cursorID)
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Order("id desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return paperOrdersToDomain(rows), nil
}

func (r PaperRepository) ListFills(ctx context.Context, accountID uint, limit int, cursorID uint64) ([]domainpaper.Fill, error) {
	var rows []persistmodel.PaperFill
	q := r.db.WithContext(ctx).Where("account_id = ?", accountID)
	if cursorID > 0 {
		q = q.Where("id < ?", cursorID)
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Order("id desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return paperFillsToDomain(rows), nil
}

func (r PaperRepository) ListOrders(ctx context.Context, limit int, cursorID uint64) ([]domainpaper.Order, error) {
	var rows []persistmodel.PaperOrder
	q := r.db.WithContext(ctx)
	if cursorID > 0 {
		q = q.Where("id < ?", cursorID)
	}
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Order("id desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return paperOrdersToDomain(rows), nil
}

func (r PaperRepository) FindOrder(ctx context.Context, id uint) (*domainpaper.Order, bool, error) {
	var row persistmodel.PaperOrder
	err := r.db.WithContext(ctx).First(&row, id).Error
	if err == nil {
		out := paperOrderFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r PaperRepository) SaveOrder(ctx context.Context, row *domainpaper.Order) error {
	modelRow := paperOrderToModel(*row)
	if err := r.db.WithContext(ctx).Save(&modelRow).Error; err != nil {
		return err
	}
	*row = paperOrderFromModel(modelRow)
	return nil
}

func (r PaperRepository) DeleteOrder(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&persistmodel.PaperOrder{}, id).Error
}

func (r PaperRepository) FindPosition(ctx context.Context, accountID uint, code string) (*domainpaper.Position, bool, error) {
	var row persistmodel.PaperPosition
	err := r.db.WithContext(ctx).Where("account_id = ? AND code = ?", accountID, code).First(&row).Error
	if err == nil {
		out := paperPositionFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r PaperRepository) CreatePosition(ctx context.Context, row *domainpaper.Position) error {
	modelRow := paperPositionToModel(*row)
	if err := r.db.WithContext(ctx).Create(&modelRow).Error; err != nil {
		return err
	}
	*row = paperPositionFromModel(modelRow)
	return nil
}

func (r PaperRepository) SavePosition(ctx context.Context, row *domainpaper.Position) error {
	modelRow := paperPositionToModel(*row)
	if err := r.db.WithContext(ctx).Save(&modelRow).Error; err != nil {
		return err
	}
	*row = paperPositionFromModel(modelRow)
	return nil
}

func (r PaperRepository) DeletePosition(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&persistmodel.PaperPosition{}, id).Error
}

func (r PaperRepository) CreateFill(ctx context.Context, row *domainpaper.Fill) error {
	modelRow := paperFillToModel(*row)
	if err := r.db.WithContext(ctx).Create(&modelRow).Error; err != nil {
		return err
	}
	*row = paperFillFromModel(modelRow)
	return nil
}

func (r PaperRepository) CreateEquitySnapshot(ctx context.Context, row *domainpaper.EquitySnapshot) error {
	modelRow := paperEquitySnapshotToModel(*row)
	return r.db.WithContext(ctx).Create(&modelRow).Error
}

func riskConfigsToDomain(rows []persistmodel.RiskConfig) []domainpaper.RiskConfig {
	out := make([]domainpaper.RiskConfig, 0, len(rows))
	for _, row := range rows {
		out = append(out, riskConfigFromModel(row))
	}
	return out
}

func riskConfigFromModel(row persistmodel.RiskConfig) domainpaper.RiskConfig {
	return domainpaper.RiskConfig{
		ID:              row.ID,
		Name:            row.Name,
		InitialCash:     row.InitialCash,
		MaxPositionPct:  row.MaxPositionPct,
		MaxOrderPct:     row.MaxOrderPct,
		AllowShort:      row.AllowShort,
		AllowMargin:     row.AllowMargin,
		AllowSHMain:     row.AllowSHMain,
		AllowSZMain:     row.AllowSZMain,
		AllowBJ:         row.AllowBJ,
		AllowSTAR:       row.AllowSTAR,
		AllowChiNext:    row.AllowChiNext,
		AllowETFLOF:     row.AllowETFLOF,
		CommissionRate:  row.CommissionRate,
		MinCommission:   row.MinCommission,
		StampDutyRate:   row.StampDutyRate,
		TransferFeeRate: row.TransferFeeRate,
		Enabled:         row.Enabled,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func riskConfigToModel(row domainpaper.RiskConfig) persistmodel.RiskConfig {
	return persistmodel.RiskConfig{
		ID:              row.ID,
		Name:            row.Name,
		InitialCash:     row.InitialCash,
		MaxPositionPct:  row.MaxPositionPct,
		MaxOrderPct:     row.MaxOrderPct,
		AllowShort:      row.AllowShort,
		AllowMargin:     row.AllowMargin,
		AllowSHMain:     row.AllowSHMain,
		AllowSZMain:     row.AllowSZMain,
		AllowBJ:         row.AllowBJ,
		AllowSTAR:       row.AllowSTAR,
		AllowChiNext:    row.AllowChiNext,
		AllowETFLOF:     row.AllowETFLOF,
		CommissionRate:  row.CommissionRate,
		MinCommission:   row.MinCommission,
		StampDutyRate:   row.StampDutyRate,
		TransferFeeRate: row.TransferFeeRate,
		Enabled:         row.Enabled,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}

func paperAccountsToDomain(rows []persistmodel.PaperAccount) []domainpaper.Account {
	out := make([]domainpaper.Account, 0, len(rows))
	for _, row := range rows {
		out = append(out, paperAccountFromModel(row))
	}
	return out
}

func paperAccountFromModel(row persistmodel.PaperAccount) domainpaper.Account {
	return domainpaper.Account{
		ID:           row.ID,
		Name:         row.Name,
		InitialCash:  row.InitialCash,
		Cash:         row.Cash,
		RiskConfigID: row.RiskConfigID,
		Active:       row.Active,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

func paperAccountToModel(row domainpaper.Account) persistmodel.PaperAccount {
	return persistmodel.PaperAccount{
		ID:           row.ID,
		Name:         row.Name,
		InitialCash:  row.InitialCash,
		Cash:         row.Cash,
		RiskConfigID: row.RiskConfigID,
		Active:       row.Active,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
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

func paperPositionsToDomain(rows []persistmodel.PaperPosition) []domainpaper.Position {
	out := make([]domainpaper.Position, 0, len(rows))
	for _, row := range rows {
		out = append(out, paperPositionFromModel(row))
	}
	return out
}

func paperPositionFromModel(row persistmodel.PaperPosition) domainpaper.Position {
	return domainpaper.Position{
		ID:            row.ID,
		AccountID:     row.AccountID,
		Code:          row.Code,
		Quantity:      row.Quantity,
		AvgCost:       row.AvgCost,
		CostAmount:    row.CostAmount,
		LastPrice:     row.LastPrice,
		MarketValue:   row.MarketValue,
		UnrealizedPNL: row.UnrealizedPNL,
		RealizedPNL:   row.RealizedPNL,
		UpdatedAt:     row.UpdatedAt,
	}
}

func paperPositionToModel(row domainpaper.Position) persistmodel.PaperPosition {
	return persistmodel.PaperPosition{
		ID:            row.ID,
		AccountID:     row.AccountID,
		Code:          row.Code,
		Quantity:      row.Quantity,
		AvgCost:       row.AvgCost,
		CostAmount:    row.CostAmount,
		LastPrice:     row.LastPrice,
		MarketValue:   row.MarketValue,
		UnrealizedPNL: row.UnrealizedPNL,
		RealizedPNL:   row.RealizedPNL,
		UpdatedAt:     row.UpdatedAt,
	}
}

func paperOrdersToDomain(rows []persistmodel.PaperOrder) []domainpaper.Order {
	out := make([]domainpaper.Order, 0, len(rows))
	for _, row := range rows {
		out = append(out, paperOrderFromModel(row))
	}
	return out
}

func paperOrderFromModel(row persistmodel.PaperOrder) domainpaper.Order {
	return domainpaper.Order{
		ID:                   row.ID,
		AccountID:            row.AccountID,
		MeetingID:            row.MeetingID,
		Code:                 row.Code,
		Side:                 row.Side,
		Quantity:             row.Quantity,
		Status:               row.Status,
		SuggestedPrice:       row.SuggestedPrice,
		FilledPrice:          row.FilledPrice,
		Reason:               row.Reason,
		SubmittedAt:          row.SubmittedAt,
		ExecuteAfter:         row.ExecuteAfter,
		ExpireAt:             row.ExpireAt,
		SourceMeetingEventID: row.SourceMeetingEventID,
		ExecutionNote:        row.ExecutionNote,
		Commission:           row.Commission,
		StampDuty:            row.StampDuty,
		TransferFee:          row.TransferFee,
		NetAmount:            row.NetAmount,
		CreatedAt:            row.CreatedAt,
		FilledAt:             row.FilledAt,
	}
}

func paperOrderToModel(row domainpaper.Order) persistmodel.PaperOrder {
	return persistmodel.PaperOrder{
		ID:                   row.ID,
		AccountID:            row.AccountID,
		MeetingID:            row.MeetingID,
		Code:                 row.Code,
		Side:                 row.Side,
		Quantity:             row.Quantity,
		Status:               row.Status,
		SuggestedPrice:       row.SuggestedPrice,
		FilledPrice:          row.FilledPrice,
		Reason:               row.Reason,
		SubmittedAt:          row.SubmittedAt,
		ExecuteAfter:         row.ExecuteAfter,
		ExpireAt:             row.ExpireAt,
		SourceMeetingEventID: row.SourceMeetingEventID,
		ExecutionNote:        row.ExecutionNote,
		Commission:           row.Commission,
		StampDuty:            row.StampDuty,
		TransferFee:          row.TransferFee,
		NetAmount:            row.NetAmount,
		CreatedAt:            row.CreatedAt,
		FilledAt:             row.FilledAt,
	}
}

func paperFillsToDomain(rows []persistmodel.PaperFill) []domainpaper.Fill {
	out := make([]domainpaper.Fill, 0, len(rows))
	for _, row := range rows {
		out = append(out, paperFillFromModel(row))
	}
	return out
}

func paperFillFromModel(row persistmodel.PaperFill) domainpaper.Fill {
	return domainpaper.Fill{
		ID:          row.ID,
		OrderID:     row.OrderID,
		AccountID:   row.AccountID,
		Code:        row.Code,
		Side:        row.Side,
		Quantity:    row.Quantity,
		Price:       row.Price,
		GrossAmount: row.GrossAmount,
		Commission:  row.Commission,
		StampDuty:   row.StampDuty,
		TransferFee: row.TransferFee,
		NetAmount:   row.NetAmount,
		RealizedPNL: row.RealizedPNL,
		FilledAt:    row.FilledAt,
	}
}

func paperFillToModel(row domainpaper.Fill) persistmodel.PaperFill {
	return persistmodel.PaperFill{
		ID:          row.ID,
		OrderID:     row.OrderID,
		AccountID:   row.AccountID,
		Code:        row.Code,
		Side:        row.Side,
		Quantity:    row.Quantity,
		Price:       row.Price,
		GrossAmount: row.GrossAmount,
		Commission:  row.Commission,
		StampDuty:   row.StampDuty,
		TransferFee: row.TransferFee,
		NetAmount:   row.NetAmount,
		RealizedPNL: row.RealizedPNL,
		FilledAt:    row.FilledAt,
	}
}

func paperEquitySnapshotToModel(row domainpaper.EquitySnapshot) persistmodel.PaperEquitySnapshot {
	return persistmodel.PaperEquitySnapshot{
		ID:            row.ID,
		AccountID:     row.AccountID,
		SnapshotTime:  row.SnapshotTime,
		Cash:          row.Cash,
		MarketValue:   row.MarketValue,
		TotalEquity:   row.TotalEquity,
		UnrealizedPNL: row.UnrealizedPNL,
		RealizedPNL:   row.RealizedPNL,
		DailyPNL:      row.DailyPNL,
	}
}
