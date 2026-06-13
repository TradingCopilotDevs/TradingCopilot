package prediction

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	domainprediction "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/prediction"
	"github.com/shopspring/decimal"
)

func TestScoreMatchRequiresSemanticOverlap(t *testing.T) {
	market := domainprediction.Market{
		Question:        "Will the Fed cut rates in June?",
		Slug:            "fed-rate-cut-june",
		Active:          true,
		EnableOrderBook: true,
		Volume:          decimal.NewFromInt(10000),
	}

	score, breakdown := ScoreMatch("Apple announces a new iPhone chip for developers.", market)
	if score.GreaterThanOrEqual(decimal.NewFromFloat(0.45)) {
		t.Fatalf("expected unrelated active market to stay below review threshold, got %s with %#v", score, breakdown)
	}
}

func TestScoreMatchPromotesHighConfidenceOverlap(t *testing.T) {
	market := domainprediction.Market{
		Question:        "Will the Fed cut rates in June after Powell comments on inflation?",
		Slug:            "fed-rate-cut-june-powell-inflation",
		Active:          true,
		EnableOrderBook: true,
		Volume:          decimal.NewFromInt(10000),
	}

	score, breakdown := ScoreMatch("Powell says Fed rate cut in June depends on inflation data.", market)
	if score.LessThan(decimal.NewFromFloat(0.75)) {
		t.Fatalf("expected strong overlap to auto-link threshold, got %s with %#v", score, breakdown)
	}
}

func TestBuildQueryDropsCommonMarketWords(t *testing.T) {
	query := BuildQuery("The Polymarket market asks whether Powell and the Fed will cut rates before June.")
	if query == "" {
		t.Fatal("expected query")
	}
	if query == "the polymarket market" {
		t.Fatalf("expected stop words to be removed, got %q", query)
	}
}

func TestBuildQueryDropsAShareAndTradingArtifacts(t *testing.T) {
	query := BuildQuery("A-share A股 600519.SH 股票 买入 仓位 自选 paper order position Iran permanent peace deal odds moved.")
	terms := stringSet(strings.Fields(query))
	for _, forbidden := range []string{"share", "600519", "股票", "买入", "仓位", "自选", "paper", "order", "position"} {
		if terms[forbidden] {
			t.Fatalf("expected query %q to drop contaminating term %q", query, forbidden)
		}
	}
	for _, required := range []string{"iran", "peace", "deal", "odds"} {
		if !terms[required] {
			t.Fatalf("expected query %q to keep prediction-market term %q", query, required)
		}
	}
}

func TestScoreMatchIgnoresAShareNoiseTerms(t *testing.T) {
	stockOnly := domainprediction.Market{
		Question:        "600519 股票 买入 仓位 paper order position",
		Slug:            "600519-stock-watchlist",
		Active:          true,
		EnableOrderBook: true,
		Volume:          decimal.NewFromInt(10000),
	}
	score, breakdown := ScoreMatch("A-share A股 600519.SH 股票 买入 仓位 自选 paper order position", stockOnly)
	if !score.Equal(decimal.Zero) || breakdown["overlap"] != 0 {
		t.Fatalf("expected A-share artifacts to contribute no score, got %s with %#v", score, breakdown)
	}

	peaceDeal := domainprediction.Market{
		Question:        "Will the US and Iran sign a permanent peace deal?",
		Slug:            "iran-peace-deal",
		EventSlug:       "us-x-iran-permanent-peace-deal-by",
		Active:          true,
		EnableOrderBook: true,
		Volume:          decimal.NewFromInt(10000),
	}
	score, breakdown = ScoreMatch("A-share A股 600519.SH 股票 买入 仓位 自选 paper order position Iran peace deal odds moved.", peaceDeal)
	if score.LessThan(decimal.NewFromFloat(0.75)) {
		t.Fatalf("expected legitimate prediction terms to still score strongly, got %s with %#v", score, breakdown)
	}
	terms := stringSet(breakdown["overlap_terms"].([]string))
	for _, forbidden := range []string{"share", "600519", "股票", "买入", "仓位", "自选", "paper", "order", "position"} {
		if terms[forbidden] {
			t.Fatalf("expected overlap terms %#v to drop contaminating term %q", terms, forbidden)
		}
	}
	for _, required := range []string{"iran", "peace", "deal"} {
		if !terms[required] {
			t.Fatalf("expected overlap terms %#v to keep %q", terms, required)
		}
	}
}

func TestNormalizeSearchQueryExtractsPolymarketSlug(t *testing.T) {
	got := NormalizeSearchQuery(" https://polymarket.com/event/us-x-iran-permanent-peace-deal-by#qTlpdC7 ")
	if got != "us-x-iran-permanent-peace-deal-by" {
		t.Fatalf("normalized query = %q", got)
	}
	got = NormalizeSearchQuery("https://polymarket.com/market/fed-cut-june-2026?tid=123")
	if got != "fed-cut-june-2026" {
		t.Fatalf("market URL normalized query = %q", got)
	}
	got = NormalizeSearchQuery("polymarket.com/event/us-x-iran-permanent-peace-deal-by#qTlpdC7")
	if got != "us-x-iran-permanent-peace-deal-by" {
		t.Fatalf("bare event URL normalized query = %q", got)
	}
	got = NormalizeSearchQuery("www.polymarket.com/market/fed-cut-june-2026?tid=123")
	if got != "fed-cut-june-2026" {
		t.Fatalf("bare market URL normalized query = %q", got)
	}
	got = NormalizeSearchQuery("https://polymarket.com/events/US-X-Iran-Permanent-Peace-Deal-By#qTlpdC7")
	if got != "us-x-iran-permanent-peace-deal-by" {
		t.Fatalf("plural uppercase event URL normalized query = %q", got)
	}
	got = NormalizeSearchQuery("www.polymarket.com/markets/Fed-Cut-June-2026?tid=123")
	if got != "fed-cut-june-2026" {
		t.Fatalf("plural uppercase market URL normalized query = %q", got)
	}
	got = NormalizeSearchQuery("us-x-iran-permanent-peace-deal-by#qTlpdC7")
	if got != "us-x-iran-permanent-peace-deal-by" {
		t.Fatalf("fragment slug normalized query = %q", got)
	}
	got = NormalizeSearchQuery("US-X-Iran-Permanent-Peace-Deal-By#qTlpdC7")
	if got != "us-x-iran-permanent-peace-deal-by" {
		t.Fatalf("uppercase fragment slug normalized query = %q", got)
	}
	got = NormalizeSearchQuery("https://example.com/event/us-x-iran-permanent-peace-deal-by")
	if got != "https://example.com/event/us-x-iran-permanent-peace-deal-by" {
		t.Fatalf("non-polymarket URL should be unchanged, got %q", got)
	}
	got = NormalizeSearchQuery("example.com/event/us-x-iran-permanent-peace-deal-by")
	if got != "example.com/event/us-x-iran-permanent-peace-deal-by" {
		t.Fatalf("bare non-polymarket URL should be unchanged, got %q", got)
	}
}

func TestSearchNormalizesPolymarketURLForProviderAndRepository(t *testing.T) {
	repo := &fakePredictionRepo{}
	provider := &fakePredictionProvider{}
	usecase := NewUsecase(repo, provider)

	_, err := usecase.Search(context.Background(), "https://polymarket.com/event/us-x-iran-permanent-peace-deal-by#qTlpdC7", 10)
	if err != nil {
		t.Fatal(err)
	}
	want := "us-x-iran-permanent-peace-deal-by"
	if provider.lastSearchQuery != want {
		t.Fatalf("provider query = %q, want %q", provider.lastSearchQuery, want)
	}
	if repo.lastSearchQuery != want {
		t.Fatalf("repo query = %q, want %q", repo.lastSearchQuery, want)
	}
}

func TestSearchReturnsCachedRowsWithProviderWarning(t *testing.T) {
	repo := &fakePredictionRepo{markets: []domainprediction.Market{{ID: 1, Question: "US x Iran permanent peace deal?", Slug: "us-x-iran-permanent-peace-deal-by"}}}
	provider := &fakePredictionProvider{err: errors.New("provider unavailable")}
	usecase := NewUsecase(repo, provider)

	result, err := usecase.Search(context.Background(), "https://polymarket.com/event/us-x-iran-permanent-peace-deal-by#qTlpdC7", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Rows) != 1 || result.Rows[0].ID != 1 {
		t.Fatalf("expected cached row despite provider failure, got %+v", result.Rows)
	}
	if result.ProviderError != "provider unavailable" {
		t.Fatalf("provider warning = %q", result.ProviderError)
	}
	if result.NormalizedQuery != "us-x-iran-permanent-peace-deal-by" {
		t.Fatalf("normalized query = %q", result.NormalizedQuery)
	}
}

func TestSearchPersistsMarketSlugEmbeddedEventIdentity(t *testing.T) {
	repo := &fakePredictionRepo{}
	provider := &fakePredictionProvider{
		result: SearchResult{
			Events: []domainprediction.Event{{
				ExternalEventID: "evt-1",
				Slug:            "fed-rates-2026",
				Title:           "Fed rates in 2026",
				Active:          true,
			}},
			Markets: []domainprediction.Market{{
				ExternalMarketID: "mkt-1",
				Question:         "Will the Fed cut rates by June 2026?",
				Slug:             "fed-cut-june-2026",
				Raw:              jsonValue(map[string]any{"event_id": "evt-1"}),
				Active:           true,
			}},
		},
	}
	usecase := NewUsecase(repo, provider)

	_, err := usecase.Search(context.Background(), "https://polymarket.com/market/fed-cut-june-2026", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.upsertedEvents) != 1 || repo.upsertedEvents[0].ID == 0 {
		t.Fatalf("expected embedded event to be persisted with local id, got %+v", repo.upsertedEvents)
	}
	if len(repo.upsertedMarkets) != 1 || repo.upsertedMarkets[0].EventID == nil || *repo.upsertedMarkets[0].EventID != repo.upsertedEvents[0].ID {
		t.Fatalf("expected market to link to persisted event, events=%+v markets=%+v", repo.upsertedEvents, repo.upsertedMarkets)
	}
}

func TestMatchNewsFiltersInactiveClosedRestrictedAndExpiredMarkets(t *testing.T) {
	now := time.Now().UTC()
	active := domainprediction.Market{
		ID:              1,
		Question:        "Will the Fed cut rates in June?",
		Slug:            "fed-rate-cut-june",
		Active:          true,
		EnableOrderBook: true,
		Liquidity:       decimal.NewFromInt(100),
	}
	closed := active
	closed.ID = 2
	closed.Closed = true
	restricted := active
	restricted.ID = 3
	restricted.Restricted = true
	expired := active
	expired.ID = 4
	expired.EndDate = ptrTime(now.Add(-time.Hour))
	inactive := active
	inactive.ID = 5
	inactive.Active = false
	noLiquidity := active
	noLiquidity.ID = 6
	noLiquidity.EnableOrderBook = false
	noLiquidity.Liquidity = decimal.Zero
	noLiquidity.Volume = decimal.Zero

	repo := &fakePredictionRepo{markets: []domainprediction.Market{active, closed, restricted, expired, inactive, noLiquidity}}
	usecase := NewUsecase(repo, nil)
	matches, err := usecase.MatchNews(context.Background(), nil, "Powell says Fed rate cut in June depends on inflation data.")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].MarketID != active.ID {
		t.Fatalf("expected only eligible market to match, got %+v", matches)
	}
}

func TestMatchNewsKeepsSingleOverlapAsReviewAndRecordsEventEvidence(t *testing.T) {
	eventID := uint(9)
	market := domainprediction.Market{
		ID:                   1,
		EventID:              &eventID,
		EventExternalEventID: "event-iran",
		EventSlug:            "us-x-iran-permanent-peace-deal-by",
		EventTitle:           "US x Iran permanent peace deal by 2026?",
		Question:             "Will Iran sign a treaty?",
		Slug:                 "iran-treaty",
		Active:               true,
		EnableOrderBook:      true,
		Volume:               decimal.NewFromInt(10000),
	}
	repo := &fakePredictionRepo{markets: []domainprediction.Market{market}}
	usecase := NewUsecase(repo, nil)

	matches, err := usecase.MatchNews(context.Background(), nil, "Iran update.")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one review match, got %+v", matches)
	}
	if matches[0].Status != MatchStatusReview {
		t.Fatalf("single-term overlap should require manual review, got %+v", matches[0])
	}
	if !strings.Contains(matches[0].Reason, "iran") || !strings.Contains(matches[0].Reason, "us-x-iran-permanent-peace-deal-by") {
		t.Fatalf("reason should include overlap terms and event identity, got %q", matches[0].Reason)
	}
	var breakdown map[string]any
	if err := json.Unmarshal(matches[0].ScoreBreakdown, &breakdown); err != nil {
		t.Fatal(err)
	}
	if breakdown["overlap"].(float64) != 1 || breakdown["event_slug"] != "us-x-iran-permanent-peace-deal-by" {
		t.Fatalf("score breakdown should expose overlap count and event identity, got %+v", breakdown)
	}
	terms, ok := breakdown["overlap_terms"].([]any)
	if !ok || len(terms) != 1 || terms[0] != "iran" {
		t.Fatalf("overlap terms missing from score breakdown: %+v", breakdown)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(matches[0].CandidateSnapshot, &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot["event_slug"] != "us-x-iran-permanent-peace-deal-by" || snapshot["event_title"] != "US x Iran permanent peace deal by 2026?" || snapshot["external_event_id"] != "event-iran" {
		t.Fatalf("candidate snapshot should expose event identity, got %+v", snapshot)
	}
}

func TestMatchNewsAutoLinksOnlyWithMultipleOverlapTerms(t *testing.T) {
	market := domainprediction.Market{
		ID:              1,
		Question:        "Will Fed cut rates after Powell inflation comments?",
		Slug:            "fed-cut-rates-powell-inflation",
		Active:          true,
		EnableOrderBook: true,
		Volume:          decimal.NewFromInt(10000),
	}
	repo := &fakePredictionRepo{markets: []domainprediction.Market{market}}
	usecase := NewUsecase(repo, nil)

	matches, err := usecase.MatchNews(context.Background(), nil, "Powell says Fed rate cut depends on inflation.")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Status != MatchStatusLinked {
		t.Fatalf("strong multi-term overlap should auto-link, got %+v", matches)
	}
}

func TestUpsertWatchlistRequiresPredictionOrMixedTeamAndExistingMarket(t *testing.T) {
	repo := &fakePredictionRepo{
		markets:          []domainprediction.Market{{ID: 10, Question: "Will the Fed cut rates?", Active: true}},
		teamAssetClasses: map[uint]string{1: "prediction_market", 2: "mixed", 3: "a_share"},
	}
	usecase := NewUsecase(repo, nil)

	if _, err := usecase.UpsertWatchlist(context.Background(), 1, 10, nil, true); err != nil {
		t.Fatalf("prediction team watchlist should be accepted: %v", err)
	}
	if _, err := usecase.UpsertWatchlist(context.Background(), 2, 10, nil, true); err != nil {
		t.Fatalf("mixed team watchlist should be accepted: %v", err)
	}
	if _, err := usecase.UpsertWatchlist(context.Background(), 3, 10, nil, true); !errors.Is(err, ErrPredictionWatchlistTeamInvalid) {
		t.Fatalf("expected invalid team error for A-share team, got %v", err)
	}
	if _, err := usecase.UpsertWatchlist(context.Background(), 99, 10, nil, true); !errors.Is(err, ErrPredictionWatchlistTeamInvalid) {
		t.Fatalf("expected invalid team error for missing team, got %v", err)
	}
	if _, err := usecase.UpsertWatchlist(context.Background(), 1, 999, nil, true); !errors.Is(err, ErrPredictionMarketNotFound) {
		t.Fatalf("expected market not found, got %v", err)
	}
}

func TestListWatchlistRequiresPredictionOrMixedTeam(t *testing.T) {
	repo := &fakePredictionRepo{
		teamAssetClasses: map[uint]string{1: "prediction_market", 2: "mixed", 3: "a_share"},
	}
	usecase := NewUsecase(repo, nil)

	if _, err := usecase.ListWatchlist(context.Background(), 1, 10); err != nil {
		t.Fatalf("prediction team watchlist list should be accepted: %v", err)
	}
	if _, err := usecase.ListWatchlist(context.Background(), 2, 10); err != nil {
		t.Fatalf("mixed team watchlist list should be accepted: %v", err)
	}
	if _, err := usecase.ListWatchlist(context.Background(), 3, 10); !errors.Is(err, ErrPredictionWatchlistTeamInvalid) {
		t.Fatalf("expected invalid team error for A-share team, got %v", err)
	}
	if _, err := usecase.ListWatchlist(context.Background(), 99, 10); !errors.Is(err, ErrPredictionWatchlistTeamInvalid) {
		t.Fatalf("expected invalid team error for missing team, got %v", err)
	}
	if _, err := usecase.ListWatchlist(context.Background(), 0, 10); !errors.Is(err, ErrPredictionWatchlistTeamRequired) {
		t.Fatalf("expected missing team error, got %v", err)
	}
}

type fakePredictionRepo struct {
	markets          []domainprediction.Market
	matches          []domainprediction.Match
	teamAssetClasses map[uint]string
	lastSearchQuery  string
	upsertedEvents   []domainprediction.Event
	upsertedMarkets  []domainprediction.Market
}

func (r *fakePredictionRepo) SearchMarkets(_ context.Context, q string, _ int) ([]domainprediction.Market, error) {
	r.lastSearchQuery = q
	return r.markets, nil
}

func (r *fakePredictionRepo) FindEvent(context.Context, uint) (*domainprediction.Event, bool, error) {
	return nil, false, nil
}

func (r *fakePredictionRepo) FindMarket(_ context.Context, id uint) (*domainprediction.Market, bool, error) {
	for _, market := range r.markets {
		if market.ID == id {
			copy := market
			return &copy, true, nil
		}
	}
	return nil, false, nil
}

func (r *fakePredictionRepo) FindMarketByExternalID(context.Context, string, string) (*domainprediction.Market, bool, error) {
	return nil, false, nil
}

func (r *fakePredictionRepo) ResearchTeamAssetClass(_ context.Context, teamID uint) (string, bool, error) {
	if r.teamAssetClasses == nil {
		return "prediction_market", true, nil
	}
	value, ok := r.teamAssetClasses[teamID]
	return value, ok, nil
}

func (r *fakePredictionRepo) UpsertEvent(_ context.Context, row *domainprediction.Event) error {
	if row.ID == 0 {
		row.ID = uint(len(r.upsertedEvents) + 1)
	}
	r.upsertedEvents = append(r.upsertedEvents, *row)
	return nil
}

func (r *fakePredictionRepo) UpsertMarket(_ context.Context, row *domainprediction.Market) error {
	r.upsertedMarkets = append(r.upsertedMarkets, *row)
	return nil
}

func (r *fakePredictionRepo) LatestQuote(context.Context, uint) (*domainprediction.Quote, bool, error) {
	return nil, false, nil
}

func (r *fakePredictionRepo) SaveQuote(context.Context, *domainprediction.Quote) error {
	return nil
}

func (r *fakePredictionRepo) ListMatches(context.Context, MatchFilter) ([]MatchRow, error) {
	return nil, nil
}

func (r *fakePredictionRepo) CreateMatch(_ context.Context, row *domainprediction.Match) error {
	row.ID = uint(len(r.matches) + 1)
	r.matches = append(r.matches, *row)
	return nil
}

func (r *fakePredictionRepo) SaveMatch(context.Context, *domainprediction.Match) error {
	return nil
}

func (r *fakePredictionRepo) FindMatch(context.Context, uint) (*domainprediction.Match, bool, error) {
	return nil, false, nil
}

func (r *fakePredictionRepo) UpsertWatchlist(context.Context, *domainprediction.WatchlistItem) error {
	return nil
}

func (r *fakePredictionRepo) ListWatchlist(context.Context, uint, int) ([]WatchlistRow, error) {
	return nil, nil
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func stringSet(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		out[value] = true
	}
	return out
}

type fakePredictionProvider struct {
	lastSearchQuery string
	err             error
	result          SearchResult
}

func (p *fakePredictionProvider) Search(_ context.Context, q string, _ int) (SearchResult, error) {
	p.lastSearchQuery = q
	if p.err != nil {
		return SearchResult{}, p.err
	}
	return p.result, nil
}

func (p *fakePredictionProvider) SyncActive(context.Context, int) (SearchResult, error) {
	return SearchResult{}, nil
}

func (p *fakePredictionProvider) OrderBook(context.Context, string) (map[string]any, error) {
	return nil, nil
}

func (p *fakePredictionProvider) PriceHistory(context.Context, string) ([]map[string]any, error) {
	return nil, nil
}
