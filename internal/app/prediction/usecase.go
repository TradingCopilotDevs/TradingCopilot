package prediction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainprediction "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/prediction"
	"github.com/shopspring/decimal"
)

const (
	MatchStatusCandidate = "candidate"
	MatchStatusLinked    = "linked"
	MatchStatusReview    = "review_required"
	MatchStatusRejected  = "rejected"
	MatchStatusConfirmed = "confirmed"
)

var ErrPredictionMarketNotFound = errors.New("prediction market not found")
var ErrPredictionWatchlistTeamInvalid = errors.New("prediction watchlist requires a prediction market or mixed research team")

type Usecase struct {
	repo     Repository
	provider Provider
}

func NewUsecase(repo Repository, provider Provider) Usecase {
	return Usecase{repo: repo, provider: provider}
}

func (u Usecase) Configured() bool {
	return u.repo != nil
}

func (u Usecase) ProviderHealth(ctx context.Context) map[string]any {
	health := map[string]any{
		"gammaConnectivity": "not_configured",
		"clobConnectivity":  "not_checked",
		"wsConnectivity":    "not_checked",
		"apiErrorStats":     predictionProviderErrorStats(nil),
	}
	if u.provider == nil {
		return health
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var errs []error
	result, err := u.provider.SyncActive(ctx, 1)
	if err != nil {
		health["gammaConnectivity"] = "error"
		errs = append(errs, err)
		health["apiErrorStats"] = predictionProviderErrorStats(errs)
		return health
	}
	health["gammaConnectivity"] = "ok"
	health["activeEventSampleCount"] = len(result.Events)
	health["activeMarketSampleCount"] = len(result.Markets)
	tokenID := firstTokenID(result.Markets)
	if tokenID == "" {
		health["clobConnectivity"] = "skipped_no_token"
		health["wsConnectivity"] = "skipped_no_token"
		health["apiErrorStats"] = predictionProviderErrorStats(errs)
		return health
	}
	book, err := u.provider.OrderBook(ctx, tokenID)
	if err != nil {
		health["clobConnectivity"] = "error"
		errs = append(errs, err)
	} else {
		health["clobConnectivity"] = "ok"
		health["clobBookSampleKeys"] = mapKeys(book, 8)
	}
	if sampler, ok := u.provider.(MarketWebSocketSampler); ok {
		rows, err := sampler.MarketWebSocketSnapshot(ctx, []string{tokenID}, 2*time.Second)
		if err != nil {
			health["wsConnectivity"] = "error"
			errs = append(errs, err)
		} else {
			health["wsConnectivity"] = "ok"
			health["wsSampleCount"] = len(rows)
		}
	}
	health["apiErrorStats"] = predictionProviderErrorStats(errs)
	return health
}

type Repository interface {
	SearchMarkets(ctx context.Context, q string, limit int) ([]domainprediction.Market, error)
	FindEvent(ctx context.Context, id uint) (*domainprediction.Event, bool, error)
	FindMarket(ctx context.Context, id uint) (*domainprediction.Market, bool, error)
	FindMarketByExternalID(ctx context.Context, provider string, externalID string) (*domainprediction.Market, bool, error)
	ResearchTeamAssetClass(ctx context.Context, teamID uint) (string, bool, error)
	UpsertEvent(ctx context.Context, row *domainprediction.Event) error
	UpsertMarket(ctx context.Context, row *domainprediction.Market) error
	LatestQuote(ctx context.Context, marketID uint) (*domainprediction.Quote, bool, error)
	SaveQuote(ctx context.Context, row *domainprediction.Quote) error
	ListMatches(ctx context.Context, filter MatchFilter) ([]MatchRow, error)
	CreateMatch(ctx context.Context, row *domainprediction.Match) error
	SaveMatch(ctx context.Context, row *domainprediction.Match) error
	FindMatch(ctx context.Context, id uint) (*domainprediction.Match, bool, error)
	UpsertWatchlist(ctx context.Context, item *domainprediction.WatchlistItem) error
	ListWatchlist(ctx context.Context, researchTeamID uint, limit int) ([]WatchlistRow, error)
}

type Provider interface {
	Search(ctx context.Context, q string, limit int) (SearchResult, error)
	SyncActive(ctx context.Context, limit int) (SearchResult, error)
	OrderBook(ctx context.Context, tokenID string) (map[string]any, error)
	PriceHistory(ctx context.Context, tokenID string) ([]map[string]any, error)
}

type MarketWebSocketSampler interface {
	MarketWebSocketSnapshot(ctx context.Context, tokenIDs []string, sampleFor time.Duration) ([]map[string]any, error)
}

type SearchResult struct {
	Events  []domainprediction.Event
	Markets []domainprediction.Market
}

type MarketSearchResult struct {
	Rows       []domainprediction.Market
	NextCursor string
}

type MatchFilter struct {
	MessageID string
	Status    string
	Limit     int
	CursorID  uint64
}

type MatchRow struct {
	Match  domainprediction.Match
	Market domainprediction.Market
}

type WatchlistRow struct {
	Item   domainprediction.WatchlistItem
	Market domainprediction.Market
}

type MatchReviewInput struct {
	Status     string
	Reason     string
	ReviewedBy string
}

func (u Usecase) Search(ctx context.Context, q string, limit int) (MarketSearchResult, error) {
	q = strings.TrimSpace(q)
	if limit <= 0 {
		limit = 20
	}
	if u.provider != nil && q != "" {
		result, err := u.provider.Search(ctx, q, limit)
		if err == nil {
			_ = u.persistSearchResult(ctx, result)
		}
	}
	rows, err := u.repo.SearchMarkets(ctx, q, limit)
	if err != nil {
		return MarketSearchResult{}, err
	}
	return MarketSearchResult{Rows: rows}, nil
}

func (u Usecase) SyncActive(ctx context.Context, limit int) (int, error) {
	if u.provider == nil {
		return 0, errors.New("prediction market provider is not configured")
	}
	if limit <= 0 {
		limit = 100
	}
	result, err := u.provider.SyncActive(ctx, limit)
	if err != nil {
		return 0, err
	}
	if err := u.persistSearchResult(ctx, result); err != nil {
		return 0, err
	}
	return len(result.Markets), nil
}

func (u Usecase) GetEvent(ctx context.Context, id uint) (*domainprediction.Event, bool, error) {
	return u.repo.FindEvent(ctx, id)
}

func (u Usecase) GetMarket(ctx context.Context, id uint, refresh bool) (*domainprediction.Market, bool, error) {
	row, found, err := u.repo.FindMarket(ctx, id)
	if err != nil || !found || row == nil {
		return row, found, err
	}
	if refresh && u.provider != nil {
		_ = u.refreshQuote(ctx, row)
		refreshed, ok, refreshErr := u.repo.FindMarket(ctx, id)
		if refreshErr == nil && ok {
			row = refreshed
		}
	}
	return row, true, nil
}

func (u Usecase) OrderBook(ctx context.Context, marketID uint) ([]map[string]any, error) {
	market, found, err := u.repo.FindMarket(ctx, marketID)
	if err != nil {
		return nil, err
	}
	if !found || market == nil {
		return nil, ErrPredictionMarketNotFound
	}
	tokenIDs := stringListFromJSON(market.CLOBTokenIDs)
	out := []map[string]any{}
	for _, tokenID := range tokenIDs {
		if u.provider == nil {
			continue
		}
		book, err := u.provider.OrderBook(ctx, tokenID)
		if err != nil {
			out = append(out, map[string]any{"token_id": tokenID, "error": err.Error()})
			continue
		}
		out = append(out, book)
	}
	return out, nil
}

func (u Usecase) PriceHistory(ctx context.Context, marketID uint) ([]map[string]any, error) {
	market, found, err := u.repo.FindMarket(ctx, marketID)
	if err != nil {
		return nil, err
	}
	if !found || market == nil {
		return nil, ErrPredictionMarketNotFound
	}
	tokenIDs := stringListFromJSON(market.CLOBTokenIDs)
	if len(tokenIDs) == 0 || u.provider == nil {
		return nil, nil
	}
	return u.provider.PriceHistory(ctx, tokenIDs[0])
}

func (u Usecase) ListMatches(ctx context.Context, filter MatchFilter) ([]MatchRow, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	return u.repo.ListMatches(ctx, filter)
}

func (u Usecase) ListMatchesForMessage(ctx context.Context, messageID uint) ([]domainprediction.Match, error) {
	if messageID == 0 {
		return nil, nil
	}
	rows, err := u.repo.ListMatches(ctx, MatchFilter{MessageID: strconv.FormatUint(uint64(messageID), 10), Limit: 100})
	if err != nil {
		return nil, err
	}
	out := make([]domainprediction.Match, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Match)
	}
	return out, nil
}

func (u Usecase) ReviewMatch(ctx context.Context, id uint, input MatchReviewInput) (*domainprediction.Match, bool, error) {
	row, found, err := u.repo.FindMatch(ctx, id)
	if err != nil || !found || row == nil {
		return nil, found, err
	}
	status := strings.TrimSpace(input.Status)
	switch status {
	case MatchStatusConfirmed, MatchStatusRejected, MatchStatusLinked, MatchStatusReview, MatchStatusCandidate:
	default:
		return nil, true, errors.New("match status is not supported")
	}
	now := time.Now().UTC()
	row.Status = status
	row.Reason = strings.TrimSpace(input.Reason)
	if reviewer := strings.TrimSpace(input.ReviewedBy); reviewer != "" {
		row.ReviewedBy = &reviewer
	}
	row.ReviewedAt = &now
	if err := u.repo.SaveMatch(ctx, row); err != nil {
		return nil, true, err
	}
	return row, true, nil
}

func (u Usecase) UpsertWatchlist(ctx context.Context, teamID uint, marketID uint, note *string, active bool) (*domainprediction.WatchlistItem, error) {
	if teamID == 0 || marketID == 0 {
		return nil, errors.New("research team and prediction market are required")
	}
	assetClass, found, err := u.repo.ResearchTeamAssetClass(ctx, teamID)
	if err != nil {
		return nil, err
	}
	if !found || !predictionWatchlistTeamAllowed(assetClass) {
		return nil, ErrPredictionWatchlistTeamInvalid
	}
	if _, found, err := u.repo.FindMarket(ctx, marketID); err != nil {
		return nil, err
	} else if !found {
		return nil, ErrPredictionMarketNotFound
	}
	item := domainprediction.WatchlistItem{ResearchTeamID: teamID, MarketID: marketID, Note: note, Active: active}
	if err := u.repo.UpsertWatchlist(ctx, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

func (u Usecase) ListWatchlist(ctx context.Context, teamID uint, limit int) ([]WatchlistRow, error) {
	if limit <= 0 {
		limit = 100
	}
	return u.repo.ListWatchlist(ctx, teamID, limit)
}

func predictionWatchlistTeamAllowed(assetClass string) bool {
	switch strings.TrimSpace(assetClass) {
	case "prediction_market", "mixed":
		return true
	default:
		return false
	}
}

func (u Usecase) MatchNews(ctx context.Context, messageID *uint, text string) ([]domainprediction.Match, error) {
	query := BuildQuery(text)
	if query == "" {
		return nil, nil
	}
	search, err := u.Search(ctx, query, 8)
	if err != nil {
		return nil, err
	}
	matches := make([]domainprediction.Match, 0, len(search.Rows))
	for _, market := range search.Rows {
		if !eligibleMarketCandidate(market, time.Now().UTC()) {
			continue
		}
		score, breakdown := ScoreMatch(text, market)
		status := MatchStatusCandidate
		if score.GreaterThanOrEqual(decimal.NewFromFloat(0.75)) {
			status = MatchStatusLinked
		} else if score.GreaterThanOrEqual(decimal.NewFromFloat(0.45)) {
			status = MatchStatusReview
		}
		if score.LessThan(decimal.NewFromFloat(0.45)) {
			continue
		}
		row := domainprediction.Match{
			MessageID:         messageID,
			MarketID:          market.ID,
			Query:             query,
			NewsSnippet:       truncate(text, 1000),
			CandidateSnapshot: jsonValue(market),
			Score:             score,
			ScoreBreakdown:    jsonValue(breakdown),
			Status:            status,
			Reason:            "semantic keyword overlap and market activity score",
		}
		if err := u.repo.CreateMatch(ctx, &row); err != nil {
			return nil, err
		}
		matches = append(matches, row)
	}
	return matches, nil
}

func eligibleMarketCandidate(market domainprediction.Market, now time.Time) bool {
	if !market.Active || market.Closed || market.Restricted {
		return false
	}
	if market.EndDate != nil && market.EndDate.Before(now) {
		return false
	}
	if !market.EnableOrderBook && !market.Liquidity.GreaterThan(decimal.Zero) && !market.Volume.GreaterThan(decimal.Zero) {
		return false
	}
	return true
}

func (u Usecase) persistSearchResult(ctx context.Context, result SearchResult) error {
	eventsByExternalID := map[string]uint{}
	for i := range result.Events {
		row := result.Events[i]
		if row.Provider == "" {
			row.Provider = domainprediction.ProviderPolymarket
		}
		if err := u.repo.UpsertEvent(ctx, &row); err != nil {
			return err
		}
		eventsByExternalID[row.ExternalEventID] = row.ID
	}
	for i := range result.Markets {
		row := result.Markets[i]
		if row.Provider == "" {
			row.Provider = domainprediction.ProviderPolymarket
		}
		if row.EventID == nil {
			if eventID, ok := eventIDFromMarketRaw(row.Raw, eventsByExternalID); ok {
				row.EventID = &eventID
			}
		}
		if err := u.repo.UpsertMarket(ctx, &row); err != nil {
			return err
		}
	}
	return nil
}

func (u Usecase) refreshQuote(ctx context.Context, market *domainprediction.Market) error {
	tokenIDs := stringListFromJSON(market.CLOBTokenIDs)
	if len(tokenIDs) == 0 || u.provider == nil {
		return nil
	}
	book, err := u.provider.OrderBook(ctx, tokenIDs[0])
	if err != nil {
		return err
	}
	bestBid := decimalFromAny(firstPresent(book, "best_bid", "bestBid"))
	bestAsk := decimalFromAny(firstPresent(book, "best_ask", "bestAsk"))
	lastPrice := decimalFromAny(firstPresent(book, "last_trade_price", "lastTradePrice"))
	spread := bestAsk.Sub(bestBid)
	now := time.Now().UTC()
	quote := domainprediction.Quote{
		MarketID:  market.ID,
		TokenID:   tokenIDs[0],
		Outcome:   firstStringFromJSON(market.Outcomes),
		QuoteTime: now,
		BestBid:   bestBid,
		BestAsk:   bestAsk,
		Midpoint:  bestBid.Add(bestAsk).Div(decimal.NewFromInt(2)),
		LastPrice: lastPrice,
		Spread:    spread,
		Raw:       jsonValue(book),
		Provider:  domainprediction.ProviderPolymarket,
	}
	if err := u.repo.SaveQuote(ctx, &quote); err != nil {
		return err
	}
	market.BestBid = bestBid
	market.BestAsk = bestAsk
	market.LastTradePrice = lastPrice
	market.Spread = spread
	market.UpdatedAt = now
	return u.repo.UpsertMarket(ctx, market)
}

func BuildQuery(text string) string {
	words := keywords(text, 8)
	return strings.Join(words, " ")
}

func ScoreMatch(text string, market domainprediction.Market) (decimal.Decimal, map[string]any) {
	newsTerms := termSet(text)
	marketText := strings.Join([]string{market.Question, market.Description, market.Slug}, " ")
	marketTerms := termSet(marketText)
	overlap := 0
	for term := range newsTerms {
		if marketTerms[term] {
			overlap++
		}
	}
	denom := math.Max(1, math.Sqrt(float64(len(newsTerms))*float64(maxInt(1, len(marketTerms)))))
	semantic := math.Min(1, float64(overlap)/denom*1.8)
	activity := 0.0
	if market.Active && !market.Closed && !market.Restricted {
		activity += 0.25
	}
	if market.EnableOrderBook {
		activity += 0.15
	}
	if market.Volume.GreaterThan(decimal.Zero) {
		activity += 0.1
	}
	if overlap == 0 {
		activity = 0
	}
	score := math.Min(1, semantic*0.8+activity*0.4)
	return decimal.NewFromFloat(score), map[string]any{"semantic": semantic, "activity": activity, "overlap": overlap}
}

func keywords(text string, limit int) []string {
	counts := map[string]int{}
	for term := range termSet(text) {
		if len(term) >= 3 {
			counts[term]++
		}
	}
	type pair struct {
		Term  string
		Count int
	}
	rows := []pair{}
	for term, count := range counts {
		rows = append(rows, pair{term, count})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Term < rows[j].Term
	})
	out := []string{}
	for _, row := range rows {
		if isStopWord(row.Term) {
			continue
		}
		out = append(out, row.Term)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func termSet(text string) map[string]bool {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	})
	out := map[string]bool{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if len(field) < 3 || isStopWord(field) {
			continue
		}
		out[field] = true
	}
	return out
}

func isStopWord(value string) bool {
	switch value {
	case "the", "and", "for", "with", "will", "this", "that", "from", "are", "was", "were", "have", "has", "not", "but", "you", "about", "after", "before", "into", "market", "polymarket":
		return true
	default:
		return false
	}
}

func eventIDFromMarketRaw(raw domainkernel.JSON, ids map[string]uint) (uint, bool) {
	var data map[string]any
	if json.Unmarshal(raw, &data) != nil {
		return 0, false
	}
	for _, key := range []string{"event_id", "eventId"} {
		value := strings.TrimSpace(toString(data[key]))
		if id, ok := ids[value]; ok {
			return id, true
		}
	}
	return 0, false
}

func stringListFromJSON(raw []byte) []string {
	var values []string
	if len(raw) == 0 || json.Unmarshal(raw, &values) != nil {
		return nil
	}
	out := values[:0]
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			out = append(out, strings.TrimSpace(value))
		}
	}
	return out
}

func firstStringFromJSON(raw []byte) string {
	values := stringListFromJSON(raw)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func firstTokenID(markets []domainprediction.Market) string {
	for _, market := range markets {
		if tokenID := firstStringFromJSON(market.CLOBTokenIDs); tokenID != "" {
			return tokenID
		}
	}
	return ""
}

func firstPresent(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}
	return nil
}

func decimalFromAny(value any) decimal.Decimal {
	switch typed := value.(type) {
	case decimal.Decimal:
		return typed
	case json.Number:
		out, _ := decimal.NewFromString(typed.String())
		return out
	case float64:
		return decimal.NewFromFloat(typed)
	case int:
		return decimal.NewFromInt(int64(typed))
	case string:
		out, _ := decimal.NewFromString(strings.TrimSpace(typed))
		return out
	default:
		out, _ := decimal.NewFromString(strings.TrimSpace(toString(value)))
		return out
	}
}

func toString(value any) string {
	if value == nil {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return typed.String()
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func jsonValue(value any) domainkernel.JSON {
	b, _ := json.Marshal(value)
	return domainkernel.JSON(b)
}

func truncate(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func mapKeys(values map[string]any, limit int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}
	return keys
}

func predictionProviderErrorStats(errs []error) map[string]any {
	stats := map[string]any{
		"errorCount":                len(errs),
		"rateLimit429Count":         0,
		"server5xxCount":            0,
		"cloudflareThrottlingCount": 0,
		"samples":                   []string{},
	}
	samples := []string{}
	rateLimit := 0
	server5xx := 0
	cloudflare := 0
	for _, err := range errs {
		if err == nil {
			continue
		}
		text := strings.TrimSpace(err.Error())
		lower := strings.ToLower(text)
		if strings.Contains(lower, "429") || strings.Contains(lower, "too many request") || strings.Contains(lower, "rate limit") {
			rateLimit++
		}
		if strings.Contains(lower, "http 5") || strings.Contains(lower, " 5xx") || strings.Contains(lower, "server error") || strings.Contains(lower, "bad gateway") || strings.Contains(lower, "gateway") {
			server5xx++
		}
		if strings.Contains(lower, "cloudflare") || strings.Contains(lower, "cf-") || strings.Contains(lower, "throttl") {
			cloudflare++
		}
		if len(samples) < 5 {
			samples = append(samples, truncate(text, 240))
		}
	}
	stats["rateLimit429Count"] = rateLimit
	stats["server5xxCount"] = server5xx
	stats["cloudflareThrottlingCount"] = cloudflare
	stats["samples"] = samples
	return stats
}
