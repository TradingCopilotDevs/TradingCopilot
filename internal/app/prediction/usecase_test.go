package prediction

import (
	"context"
	"errors"
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

type fakePredictionRepo struct {
	markets          []domainprediction.Market
	matches          []domainprediction.Match
	teamAssetClasses map[uint]string
}

func (r *fakePredictionRepo) SearchMarkets(context.Context, string, int) ([]domainprediction.Market, error) {
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

func (r *fakePredictionRepo) UpsertEvent(context.Context, *domainprediction.Event) error {
	return nil
}

func (r *fakePredictionRepo) UpsertMarket(context.Context, *domainprediction.Market) error {
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
