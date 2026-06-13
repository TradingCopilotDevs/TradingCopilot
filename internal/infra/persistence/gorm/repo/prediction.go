package repo

import (
	"context"
	"errors"
	"strconv"
	"strings"

	appprediction "github.com/TradingCopilotDevs/TradingCopilot/internal/app/prediction"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainprediction "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/prediction"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PredictionRepository struct {
	db *gorm.DB
}

func NewPredictionRepository(db *gorm.DB) PredictionRepository {
	return PredictionRepository{db: db}
}

func (r PredictionRepository) SearchMarkets(ctx context.Context, q string, limit int) ([]domainprediction.Market, error) {
	if limit <= 0 {
		limit = 20
	}
	var rows []persistmodel.PredictionMarket
	query := r.db.WithContext(ctx).
		Model(&persistmodel.PredictionMarket{}).
		Preload("Event").
		Joins("LEFT JOIN prediction_events ON prediction_events.id = prediction_markets.event_id").
		Limit(limit)
	q = strings.TrimSpace(q)
	if q != "" {
		normalized := strings.ToLower(q)
		like := "%" + normalized + "%"
		query = query.Where(strings.Join([]string{
			"LOWER(prediction_markets.question) LIKE ?",
			"LOWER(prediction_markets.slug) LIKE ?",
			"LOWER(prediction_markets.description) LIKE ?",
			"LOWER(prediction_markets.external_market_id) LIKE ?",
			"LOWER(prediction_markets.condition_id) LIKE ?",
			"LOWER(prediction_events.title) LIKE ?",
			"LOWER(prediction_events.slug) LIKE ?",
			"LOWER(prediction_events.description) LIKE ?",
		}, " OR "), like, like, like, like, like, like, like, like)
		query = query.Select(`prediction_markets.*, CASE
WHEN LOWER(prediction_markets.slug) = ? OR LOWER(prediction_markets.external_market_id) = ? OR LOWER(prediction_markets.condition_id) = ? THEN 0
WHEN LOWER(prediction_events.slug) = ? OR LOWER(prediction_events.external_event_id) = ? THEN 1
WHEN LOWER(prediction_markets.slug) LIKE ? THEN 2
WHEN LOWER(prediction_events.slug) LIKE ? THEN 3
WHEN LOWER(prediction_markets.question) LIKE ? THEN 4
WHEN LOWER(prediction_events.title) LIKE ? THEN 5
ELSE 9 END AS search_rank`, normalized, normalized, normalized, normalized, normalized, like, like, like, like).
			Order("search_rank asc")
	}
	query = query.Order("prediction_markets.active desc, prediction_markets.closed asc, prediction_markets.volume desc, prediction_markets.id desc")
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	return predictionMarketsToDomain(rows), nil
}

func (r PredictionRepository) FindEvent(ctx context.Context, id uint) (*domainprediction.Event, bool, error) {
	var row persistmodel.PredictionEvent
	err := r.db.WithContext(ctx).First(&row, id).Error
	if err == nil {
		out := predictionEventFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r PredictionRepository) FindMarket(ctx context.Context, id uint) (*domainprediction.Market, bool, error) {
	var row persistmodel.PredictionMarket
	err := r.db.WithContext(ctx).Preload("Event").First(&row, id).Error
	if err == nil {
		out := predictionMarketFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r PredictionRepository) FindMarketByExternalID(ctx context.Context, provider string, externalID string) (*domainprediction.Market, bool, error) {
	var row persistmodel.PredictionMarket
	err := r.db.WithContext(ctx).Preload("Event").Where("provider = ? AND external_market_id = ?", provider, externalID).First(&row).Error
	if err == nil {
		out := predictionMarketFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r PredictionRepository) ResearchTeamAssetClass(ctx context.Context, teamID uint) (string, bool, error) {
	var row persistmodel.ResearchTeam
	err := r.db.WithContext(ctx).Select("asset_class").First(&row, teamID).Error
	if err == nil {
		return row.AssetClass, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	return "", false, err
}

func (r PredictionRepository) UpsertEvent(ctx context.Context, row *domainprediction.Event) error {
	modelRow := predictionEventToModel(*row)
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "provider"}, {Name: "external_event_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"slug", "title", "description", "category", "tags", "active", "closed", "end_date", "volume", "liquidity", "open_interest", "raw", "updated_at",
		}),
	}).Create(&modelRow).Error
	if err != nil {
		return err
	}
	var saved persistmodel.PredictionEvent
	if err := r.db.WithContext(ctx).Where("provider = ? AND external_event_id = ?", modelRow.Provider, modelRow.ExternalEventID).First(&saved).Error; err != nil {
		return err
	}
	*row = predictionEventFromModel(saved)
	return nil
}

func (r PredictionRepository) UpsertMarket(ctx context.Context, row *domainprediction.Market) error {
	modelRow := predictionMarketToModel(*row)
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "provider"}, {Name: "external_market_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"event_id", "condition_id", "question", "slug", "description", "outcomes", "outcome_prices", "clob_token_ids", "enable_order_book",
			"best_bid", "best_ask", "last_trade_price", "spread", "volume", "liquidity", "active", "closed", "restricted", "end_date", "raw", "updated_at",
		}),
	}).Create(&modelRow).Error
	if err != nil {
		return err
	}
	var saved persistmodel.PredictionMarket
	if err := r.db.WithContext(ctx).Preload("Event").Where("provider = ? AND external_market_id = ?", modelRow.Provider, modelRow.ExternalMarketID).First(&saved).Error; err != nil {
		return err
	}
	*row = predictionMarketFromModel(saved)
	return nil
}

func (r PredictionRepository) LatestQuote(ctx context.Context, marketID uint) (*domainprediction.Quote, bool, error) {
	var row persistmodel.PredictionMarketQuote
	err := r.db.WithContext(ctx).Where("market_id = ?", marketID).Order("quote_time desc, id desc").First(&row).Error
	if err == nil {
		out := predictionQuoteFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r PredictionRepository) SaveQuote(ctx context.Context, row *domainprediction.Quote) error {
	modelRow := predictionQuoteToModel(*row)
	if err := r.db.WithContext(ctx).Create(&modelRow).Error; err != nil {
		return err
	}
	*row = predictionQuoteFromModel(modelRow)
	return nil
}

func (r PredictionRepository) ListMatches(ctx context.Context, filter appprediction.MatchFilter) ([]appprediction.MatchRow, error) {
	var rows []persistmodel.PredictionMarketMatch
	q := r.db.WithContext(ctx).Preload("Market.Event").Order("id desc")
	if filter.MessageID != "" {
		q = q.Where("message_id = ?", filter.MessageID)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if filter.CursorID > 0 {
		q = q.Where("id < ?", filter.CursorID)
	}
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]appprediction.MatchRow, 0, len(rows))
	for _, row := range rows {
		item := appprediction.MatchRow{Match: predictionMatchFromModel(row)}
		if row.Market != nil {
			item.Market = predictionMarketFromModel(*row.Market)
		}
		out = append(out, item)
	}
	return out, nil
}

func (r PredictionRepository) CreateMatch(ctx context.Context, row *domainprediction.Match) error {
	modelRow := predictionMatchToModel(*row)
	if err := r.db.WithContext(ctx).Create(&modelRow).Error; err != nil {
		return err
	}
	*row = predictionMatchFromModel(modelRow)
	return nil
}

func (r PredictionRepository) SaveMatch(ctx context.Context, row *domainprediction.Match) error {
	modelRow := predictionMatchToModel(*row)
	if err := r.db.WithContext(ctx).Save(&modelRow).Error; err != nil {
		return err
	}
	*row = predictionMatchFromModel(modelRow)
	return nil
}

func (r PredictionRepository) FindMatch(ctx context.Context, id uint) (*domainprediction.Match, bool, error) {
	var row persistmodel.PredictionMarketMatch
	err := r.db.WithContext(ctx).First(&row, id).Error
	if err == nil {
		out := predictionMatchFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r PredictionRepository) UpsertWatchlist(ctx context.Context, item *domainprediction.WatchlistItem) error {
	db := r.db.WithContext(ctx)
	var saved persistmodel.PredictionWatchlistItem
	err := db.Where("research_team_id = ? AND market_id = ?", item.ResearchTeamID, item.MarketID).First(&saved).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		row := predictionWatchlistToModel(*item)
		if err := db.Create(&row).Error; err != nil {
			return err
		}
		*item = predictionWatchlistFromModel(row)
		return nil
	}
	if err != nil {
		return err
	}
	updates := map[string]any{
		"note":   item.Note,
		"active": item.Active,
	}
	if item.SourceMeetingID != nil {
		updates["source_meeting_id"] = *item.SourceMeetingID
	}
	if item.SourceMeetingEventID != nil {
		updates["source_meeting_event_id"] = *item.SourceMeetingEventID
	}
	if item.SourceRoleKey != nil {
		updates["source_role_key"] = *item.SourceRoleKey
	}
	if err := db.Model(&saved).Updates(updates).Error; err != nil {
		return err
	}
	if err := db.First(&saved, saved.ID).Error; err != nil {
		return err
	}
	*item = predictionWatchlistFromModel(saved)
	return nil
}

func (r PredictionRepository) ListWatchlist(ctx context.Context, researchTeamID uint, limit int) ([]appprediction.WatchlistRow, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []persistmodel.PredictionWatchlistItem
	q := r.db.WithContext(ctx).Preload("Market.Event").Order("created_at desc, id desc").Limit(limit)
	if researchTeamID != 0 {
		q = q.Where("research_team_id = ?", researchTeamID)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]appprediction.WatchlistRow, 0, len(rows))
	for _, row := range rows {
		item := appprediction.WatchlistRow{Item: predictionWatchlistFromModel(row)}
		if row.Market != nil {
			item.Market = predictionMarketFromModel(*row.Market)
		}
		out = append(out, item)
	}
	return out, nil
}

func predictionEventsToDomain(rows []persistmodel.PredictionEvent) []domainprediction.Event {
	out := make([]domainprediction.Event, 0, len(rows))
	for _, row := range rows {
		out = append(out, predictionEventFromModel(row))
	}
	return out
}

func predictionEventFromModel(row persistmodel.PredictionEvent) domainprediction.Event {
	return domainprediction.Event{
		ID: row.ID, Provider: row.Provider, ExternalEventID: row.ExternalEventID, Slug: row.Slug, Title: row.Title, Description: row.Description,
		Category: row.Category, Tags: domainkernel.JSON(row.Tags), Active: row.Active, Closed: row.Closed, EndDate: row.EndDate,
		Volume: row.Volume, Liquidity: row.Liquidity, OpenInterest: row.OpenInterest, Raw: domainkernel.JSON(row.Raw), UpdatedAt: row.UpdatedAt, CreatedAt: row.CreatedAt,
	}
}

func predictionEventToModel(row domainprediction.Event) persistmodel.PredictionEvent {
	return persistmodel.PredictionEvent{
		ID: row.ID, Provider: firstPredictionString(row.Provider, domainprediction.ProviderPolymarket), ExternalEventID: row.ExternalEventID, Slug: row.Slug,
		Title: row.Title, Description: row.Description, Category: row.Category, Tags: datatypes.JSON(row.Tags), Active: row.Active, Closed: row.Closed,
		EndDate: row.EndDate, Volume: row.Volume, Liquidity: row.Liquidity, OpenInterest: row.OpenInterest, Raw: datatypes.JSON(row.Raw),
		UpdatedAt: row.UpdatedAt, CreatedAt: row.CreatedAt,
	}
}

func predictionMarketsToDomain(rows []persistmodel.PredictionMarket) []domainprediction.Market {
	out := make([]domainprediction.Market, 0, len(rows))
	for _, row := range rows {
		out = append(out, predictionMarketFromModel(row))
	}
	return out
}

func predictionMarketFromModel(row persistmodel.PredictionMarket) domainprediction.Market {
	out := domainprediction.Market{
		ID: row.ID, EventID: row.EventID, Provider: row.Provider, ExternalMarketID: row.ExternalMarketID, ConditionID: row.ConditionID, Question: row.Question,
		Slug: row.Slug, Description: row.Description, Outcomes: domainkernel.JSON(row.Outcomes), OutcomePrices: domainkernel.JSON(row.OutcomePrices),
		CLOBTokenIDs: domainkernel.JSON(row.CLOBTokenIDs), EnableOrderBook: row.EnableOrderBook, BestBid: row.BestBid, BestAsk: row.BestAsk,
		LastTradePrice: row.LastTradePrice, Spread: row.Spread, Volume: row.Volume, Liquidity: row.Liquidity, Active: row.Active, Closed: row.Closed,
		Restricted: row.Restricted, EndDate: row.EndDate, Raw: domainkernel.JSON(row.Raw), UpdatedAt: row.UpdatedAt, CreatedAt: row.CreatedAt,
	}
	if row.Event != nil {
		out.EventExternalEventID = row.Event.ExternalEventID
		out.EventSlug = row.Event.Slug
		out.EventTitle = row.Event.Title
	}
	return out
}

func predictionMarketToModel(row domainprediction.Market) persistmodel.PredictionMarket {
	return persistmodel.PredictionMarket{
		ID: row.ID, EventID: row.EventID, Provider: firstPredictionString(row.Provider, domainprediction.ProviderPolymarket), ExternalMarketID: row.ExternalMarketID,
		ConditionID: row.ConditionID, Question: row.Question, Slug: row.Slug, Description: row.Description, Outcomes: datatypes.JSON(row.Outcomes),
		OutcomePrices: datatypes.JSON(row.OutcomePrices), CLOBTokenIDs: datatypes.JSON(row.CLOBTokenIDs), EnableOrderBook: row.EnableOrderBook,
		BestBid: row.BestBid, BestAsk: row.BestAsk, LastTradePrice: row.LastTradePrice, Spread: row.Spread, Volume: row.Volume, Liquidity: row.Liquidity,
		Active: row.Active, Closed: row.Closed, Restricted: row.Restricted, EndDate: row.EndDate, Raw: datatypes.JSON(row.Raw), UpdatedAt: row.UpdatedAt, CreatedAt: row.CreatedAt,
	}
}

func predictionQuoteFromModel(row persistmodel.PredictionMarketQuote) domainprediction.Quote {
	return domainprediction.Quote{
		ID: row.ID, MarketID: row.MarketID, TokenID: row.TokenID, Outcome: row.Outcome, QuoteTime: row.QuoteTime,
		BestBid: row.BestBid, BestAsk: row.BestAsk, Midpoint: row.Midpoint, LastPrice: row.LastPrice, Spread: row.Spread,
		Raw: domainkernel.JSON(row.Raw), Provider: row.Provider,
	}
}

func predictionQuoteToModel(row domainprediction.Quote) persistmodel.PredictionMarketQuote {
	return persistmodel.PredictionMarketQuote{
		ID: row.ID, MarketID: row.MarketID, TokenID: row.TokenID, Outcome: row.Outcome, QuoteTime: row.QuoteTime,
		BestBid: row.BestBid, BestAsk: row.BestAsk, Midpoint: row.Midpoint, LastPrice: row.LastPrice, Spread: row.Spread,
		Raw: datatypes.JSON(row.Raw), Provider: firstPredictionString(row.Provider, domainprediction.ProviderPolymarket),
	}
}

func predictionMatchFromModel(row persistmodel.PredictionMarketMatch) domainprediction.Match {
	return domainprediction.Match{
		ID: row.ID, MessageID: row.MessageID, MarketID: row.MarketID, Query: row.Query, NewsSnippet: row.NewsSnippet,
		CandidateSnapshot: domainkernel.JSON(row.CandidateSnapshot), Score: row.Score, ScoreBreakdown: domainkernel.JSON(row.ScoreBreakdown),
		Status: row.Status, Reason: row.Reason, ReviewedBy: row.ReviewedBy, ReviewedAt: row.ReviewedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func predictionMatchToModel(row domainprediction.Match) persistmodel.PredictionMarketMatch {
	return persistmodel.PredictionMarketMatch{
		ID: row.ID, MessageID: row.MessageID, MarketID: row.MarketID, Query: row.Query, NewsSnippet: row.NewsSnippet,
		CandidateSnapshot: datatypes.JSON(row.CandidateSnapshot), Score: row.Score, ScoreBreakdown: datatypes.JSON(row.ScoreBreakdown),
		Status: firstPredictionString(row.Status, appprediction.MatchStatusCandidate), Reason: row.Reason, ReviewedBy: row.ReviewedBy, ReviewedAt: row.ReviewedAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func predictionWatchlistFromModel(row persistmodel.PredictionWatchlistItem) domainprediction.WatchlistItem {
	return domainprediction.WatchlistItem{
		ID: row.ID, ResearchTeamID: row.ResearchTeamID, MarketID: row.MarketID, Note: row.Note, Active: row.Active,
		SourceMeetingID: row.SourceMeetingID, SourceMeetingEventID: row.SourceMeetingEventID, SourceRoleKey: row.SourceRoleKey, CreatedAt: row.CreatedAt,
	}
}

func predictionWatchlistToModel(row domainprediction.WatchlistItem) persistmodel.PredictionWatchlistItem {
	return persistmodel.PredictionWatchlistItem{
		ID: row.ID, ResearchTeamID: row.ResearchTeamID, MarketID: row.MarketID, Note: row.Note, Active: row.Active,
		SourceMeetingID: row.SourceMeetingID, SourceMeetingEventID: row.SourceMeetingEventID, SourceRoleKey: row.SourceRoleKey, CreatedAt: row.CreatedAt,
	}
}

func firstPredictionString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func uintFromPredictionString(value string) uint64 {
	parsed, _ := strconv.ParseUint(strings.TrimSpace(value), 10, 64)
	return parsed
}
