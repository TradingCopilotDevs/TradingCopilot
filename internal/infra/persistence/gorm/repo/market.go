package repo

import (
	"context"
	"errors"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/market"
	domainpaper "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/paper"
	"time"

	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"gorm.io/gorm"
)

type MarketRepository struct {
	db *gorm.DB
}

func NewMarketRepository(db *gorm.DB) MarketRepository {
	return MarketRepository{db: db}
}

func (r MarketRepository) ListSymbols(ctx context.Context, q string, limit int, cursor string) ([]domainmarket.Symbol, error) {
	var rows []persistmodel.MarketSymbol
	query := r.db.WithContext(ctx).Where("code LIKE ? OR name LIKE ?", "%"+q+"%", "%"+q+"%")
	if cursor != "" {
		query = query.Where("code > ?", cursor)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Order("code").Find(&rows).Error; err != nil {
		return nil, err
	}
	return marketSymbolsToDomain(rows), nil
}

func (r MarketRepository) CreateSymbol(ctx context.Context, row *domainmarket.Symbol) error {
	modelRow := marketSymbolToModel(*row)
	if err := r.db.WithContext(ctx).Create(&modelRow).Error; err != nil {
		return err
	}
	*row = marketSymbolFromModel(modelRow)
	return nil
}

func (r MarketRepository) UpsertSymbol(ctx context.Context, row domainmarket.Symbol) error {
	modelRow := marketSymbolToModel(row)
	updates := map[string]any{
		"name":   modelRow.Name,
		"active": modelRow.Active,
	}
	if modelRow.Exchange != "" {
		updates["exchange"] = modelRow.Exchange
	}
	db := r.db.WithContext(ctx)
	err := db.First(&persistmodel.MarketSymbol{}, "code = ?", modelRow.Code).Error
	if err == nil {
		return db.Model(&persistmodel.MarketSymbol{}).Where("code = ?", modelRow.Code).Updates(updates).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return db.Create(&modelRow).Error
	}
	return err
}

func (r MarketRepository) FindSymbol(ctx context.Context, code string) (*domainmarket.Symbol, bool, error) {
	return r.FindSymbolByCode(ctx, code)
}

func (r MarketRepository) UpdateSymbol(ctx context.Context, code string, updates map[string]any) error {
	return r.db.WithContext(ctx).Model(&persistmodel.MarketSymbol{}).Where("code = ?", code).Updates(updates).Error
}

func (r MarketRepository) DeleteSymbol(ctx context.Context, code string) error {
	return r.db.WithContext(ctx).Delete(&persistmodel.MarketSymbol{}, "code = ?", code).Error
}

func (r MarketRepository) DailyBars(ctx context.Context, code string, limit int, before *time.Time) ([]domainmarket.DailyBar, error) {
	var rows []persistmodel.DailyBar
	query := r.db.WithContext(ctx).Where("code = ?", code)
	if before != nil {
		query = query.Where("trade_date < ?", *before)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Order("trade_date desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return dailyBarsToDomain(rows), nil
}

func (r MarketRepository) UpsertDailyBar(ctx context.Context, row domainmarket.DailyBar) error {
	modelRow := dailyBarToModel(row)
	db := r.db.WithContext(ctx)
	var existing persistmodel.DailyBar
	err := db.Where("code = ? AND trade_date = ?", modelRow.Code, modelRow.TradeDate).First(&existing).Error
	if err == nil {
		existing.Open = modelRow.Open
		existing.High = modelRow.High
		existing.Low = modelRow.Low
		existing.Close = modelRow.Close
		existing.Volume = modelRow.Volume
		existing.Amount = modelRow.Amount
		existing.Provider = modelRow.Provider
		return db.Save(&existing).Error
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return db.Create(&modelRow).Error
	}
	return err
}

func (r MarketRepository) CreateRealtimeQuote(ctx context.Context, row *domainmarket.RealtimeQuote) error {
	modelRow := realtimeQuoteToModel(*row)
	if err := r.db.WithContext(ctx).Create(&modelRow).Error; err != nil {
		return err
	}
	*row = realtimeQuoteFromModel(modelRow)
	return nil
}

func (r MarketRepository) LatestRealtimeQuote(ctx context.Context, code string) (*domainmarket.RealtimeQuote, bool, error) {
	var row persistmodel.RealtimeQuote
	err := r.db.WithContext(ctx).Where("code = ?", code).Order("quote_time desc").First(&row).Error
	if err == nil {
		out := realtimeQuoteFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r MarketRepository) ListWatchlistItems(ctx context.Context, researchTeamID uint, limit int, cursor string) ([]domainmarket.WatchlistItem, error) {
	var rows []persistmodel.WatchlistItem
	query := r.db.WithContext(ctx).Where("research_team_id = ?", researchTeamID)
	if cursor != "" {
		query = query.Where("code > ?", cursor)
	}
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Order("code").Find(&rows).Error; err != nil {
		return nil, err
	}
	return watchlistItemsToDomain(rows), nil
}

func (r MarketRepository) UpsertWatchlist(ctx context.Context, researchTeamID uint, code string, note *string, active bool) (*domainmarket.WatchlistItem, error) {
	var row persistmodel.WatchlistItem
	db := r.db.WithContext(ctx)
	if err := db.First(&row, "research_team_id = ? AND code = ?", researchTeamID, code).Error; err == nil {
		row.Note = note
		row.Active = active
		if err := db.Save(&row).Error; err != nil {
			return nil, err
		}
		out := watchlistItemFromModel(row)
		return &out, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	row = persistmodel.WatchlistItem{ResearchTeamID: researchTeamID, Code: code, Note: note, Active: active}
	if err := db.Create(&row).Error; err != nil {
		return nil, err
	}
	out := watchlistItemFromModel(row)
	return &out, nil
}

func (r MarketRepository) FindSymbolByCode(ctx context.Context, code string) (*domainmarket.Symbol, bool, error) {
	var row persistmodel.MarketSymbol
	err := r.db.WithContext(ctx).First(&row, "code = ?", code).Error
	if err == nil {
		out := marketSymbolFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r MarketRepository) WatchlistExists(ctx context.Context, id uint) (bool, error) {
	var row persistmodel.WatchlistItem
	err := r.db.WithContext(ctx).First(&row, id).Error
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}

func (r MarketRepository) DeleteWatchlist(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&persistmodel.WatchlistItem{}, id).Error
}

func (r MarketRepository) ActiveWatchlistToolItems(ctx context.Context, researchTeamID uint) ([]domainmarket.WatchlistItem, error) {
	var rows []persistmodel.WatchlistItem
	if err := r.db.WithContext(ctx).Where("research_team_id = ? AND active = ?", researchTeamID, true).Order("code").Find(&rows).Error; err != nil {
		return nil, err
	}
	return watchlistItemsToDomain(rows), nil
}

func (r MarketRepository) ToolDailyBars(ctx context.Context, code string, limit int) ([]domainmarket.DailyBar, error) {
	var rows []persistmodel.DailyBar
	query := r.db.WithContext(ctx).Where("code = ?", code).Order("trade_date desc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return dailyBarsToDomain(rows), nil
}

func (r MarketRepository) ToolPaperPositions(ctx context.Context) ([]domainpaper.Position, error) {
	var rows []persistmodel.PaperPosition
	if err := r.db.WithContext(ctx).Order("account_id, code").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domainpaper.Position, 0, len(rows))
	for _, row := range rows {
		out = append(out, domainpaper.Position{
			ID:        row.ID,
			AccountID: row.AccountID,
			Code:      row.Code,
			Quantity:  row.Quantity,
			AvgCost:   row.AvgCost,
		})
	}
	return out, nil
}

func marketSymbolsToDomain(rows []persistmodel.MarketSymbol) []domainmarket.Symbol {
	out := make([]domainmarket.Symbol, 0, len(rows))
	for _, row := range rows {
		out = append(out, marketSymbolFromModel(row))
	}
	return out
}

func marketSymbolFromModel(row persistmodel.MarketSymbol) domainmarket.Symbol {
	return domainmarket.Symbol{
		Code:       row.Code,
		Name:       row.Name,
		Exchange:   row.Exchange,
		Industry:   row.Industry,
		ListedDate: row.ListedDate,
		Active:     row.Active,
		UpdatedAt:  row.UpdatedAt,
	}
}

func marketSymbolToModel(row domainmarket.Symbol) persistmodel.MarketSymbol {
	return persistmodel.MarketSymbol{
		Code:       row.Code,
		Name:       row.Name,
		Exchange:   row.Exchange,
		Industry:   row.Industry,
		ListedDate: row.ListedDate,
		Active:     row.Active,
		UpdatedAt:  row.UpdatedAt,
	}
}

func dailyBarsToDomain(rows []persistmodel.DailyBar) []domainmarket.DailyBar {
	out := make([]domainmarket.DailyBar, 0, len(rows))
	for _, row := range rows {
		out = append(out, domainmarket.DailyBar{
			ID:        row.ID,
			Code:      row.Code,
			TradeDate: row.TradeDate,
			Open:      row.Open,
			High:      row.High,
			Low:       row.Low,
			Close:     row.Close,
			Volume:    row.Volume,
			Amount:    row.Amount,
			Provider:  row.Provider,
		})
	}
	return out
}

func dailyBarToModel(row domainmarket.DailyBar) persistmodel.DailyBar {
	return persistmodel.DailyBar{
		ID:        row.ID,
		Code:      row.Code,
		TradeDate: row.TradeDate,
		Open:      row.Open,
		High:      row.High,
		Low:       row.Low,
		Close:     row.Close,
		Volume:    row.Volume,
		Amount:    row.Amount,
		Provider:  row.Provider,
	}
}

func realtimeQuoteFromModel(row persistmodel.RealtimeQuote) domainmarket.RealtimeQuote {
	return domainmarket.RealtimeQuote{
		ID:        row.ID,
		Code:      row.Code,
		QuoteTime: row.QuoteTime,
		Price:     row.Price,
		ChangePct: row.ChangePct,
		Volume:    row.Volume,
		Amount:    row.Amount,
		Raw:       domainkernel.JSON(row.Raw),
		Provider:  row.Provider,
	}
}

func realtimeQuoteToModel(row domainmarket.RealtimeQuote) persistmodel.RealtimeQuote {
	return persistmodel.RealtimeQuote{
		ID:        row.ID,
		Code:      row.Code,
		QuoteTime: row.QuoteTime,
		Price:     row.Price,
		ChangePct: row.ChangePct,
		Volume:    row.Volume,
		Amount:    row.Amount,
		Raw:       []byte(row.Raw),
		Provider:  row.Provider,
	}
}

func watchlistItemsToDomain(rows []persistmodel.WatchlistItem) []domainmarket.WatchlistItem {
	out := make([]domainmarket.WatchlistItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, watchlistItemFromModel(row))
	}
	return out
}

func watchlistItemFromModel(row persistmodel.WatchlistItem) domainmarket.WatchlistItem {
	return domainmarket.WatchlistItem{
		ID:             row.ID,
		ResearchTeamID: row.ResearchTeamID,
		Code:           row.Code,
		Note:           row.Note,
		Active:         row.Active,
		CreatedAt:      row.CreatedAt,
	}
}
