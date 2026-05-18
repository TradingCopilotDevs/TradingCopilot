package market

import (
	"context"
	"errors"
	"testing"
	"time"

	domainmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/market"
	domainpaper "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/paper"
	"github.com/shopspring/decimal"
)

func TestSyncSymbolsUsesTransactionRepository(t *testing.T) {
	ctx := context.Background()
	baseRepo := &fakeMarketRepo{}
	txRepo := &fakeMarketRepo{}
	tx := fakeMarketTx{repo: txRepo}
	service := fakeMarketData{
		symbolProvider: "fixture",
		symbols: []domainmarket.Symbol{
			{Code: "600519", Name: "Kweichow Moutai"},
			{Code: "000001", Name: "Ping An Bank"},
		},
	}

	provider, synced, err := NewUsecase(baseRepo, service, tx).SyncSymbols(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if provider != "fixture" || synced != 2 {
		t.Fatalf("unexpected sync result provider=%s synced=%d", provider, synced)
	}
	if baseRepo.upsertSymbols != 0 {
		t.Fatalf("base repository received writes outside the unit of work: %d", baseRepo.upsertSymbols)
	}
	if txRepo.upsertSymbols != 2 {
		t.Fatalf("transaction repository upsert count = %d", txRepo.upsertSymbols)
	}
}

func TestCurrentQuotePersistsFetchedQuoteThroughTransaction(t *testing.T) {
	ctx := context.Background()
	baseRepo := &fakeMarketRepo{}
	txRepo := &fakeMarketRepo{}
	service := fakeMarketData{
		quotes: map[string]*domainmarket.RealtimeQuote{
			"600519": {Code: "600519", Price: decimal.NewFromInt(1700), QuoteTime: time.Now()},
		},
	}

	quote, err := NewUsecase(baseRepo, service, fakeMarketTx{repo: txRepo}).CurrentQuote(ctx, "600519", true)
	if err != nil {
		t.Fatal(err)
	}
	if quote == nil || !quote.Price.Equal(decimal.NewFromInt(1700)) {
		t.Fatalf("unexpected quote: %+v", quote)
	}
	if txRepo.createdQuotes != 1 {
		t.Fatalf("fetched quote was not persisted through transaction repo, count=%d", txRepo.createdQuotes)
	}
	if baseRepo.createdQuotes != 0 {
		t.Fatalf("base repository received quote write outside the unit of work")
	}
}

func TestCurrentQuoteFallsBackToCachedQuoteWhenRefreshFails(t *testing.T) {
	ctx := context.Background()
	cached := &domainmarket.RealtimeQuote{ID: 10, Code: "600519", Price: decimal.NewFromInt(1688), QuoteTime: time.Now()}
	repo := &fakeMarketRepo{latestQuote: cached, latestFound: true}
	service := fakeMarketData{refreshErr: errors.New("provider unavailable")}

	quote, err := NewUsecase(repo, service, fakeMarketTx{repo: &fakeMarketRepo{}}).CurrentQuote(ctx, "600519", true)
	if err != nil {
		t.Fatal(err)
	}
	if quote == nil || quote.ID != cached.ID || !quote.Price.Equal(cached.Price) {
		t.Fatalf("expected cached quote, got %+v", quote)
	}
}

type fakeMarketTx struct {
	repo *fakeMarketRepo
}

func (tx fakeMarketTx) WithTx(ctx context.Context, fn func(Repository) error) error {
	return fn(tx.repo)
}

type fakeMarketData struct {
	symbolProvider string
	symbols        []domainmarket.Symbol
	quotes         map[string]*domainmarket.RealtimeQuote
	refreshErr     error
}

func (f fakeMarketData) EnsureAShareCode(value string) (string, error) { return value, nil }
func (f fakeMarketData) InferExchange(string) string                   { return "SH" }
func (f fakeMarketData) FetchSymbols(context.Context) (string, []domainmarket.Symbol, error) {
	return f.symbolProvider, f.symbols, nil
}
func (f fakeMarketData) Series(context.Context, string, string) ([]map[string]any, error) {
	return nil, nil
}
func (f fakeMarketData) FetchDailyBars(context.Context, string) ([]domainmarket.DailyBar, error) {
	return nil, nil
}
func (f fakeMarketData) RefreshQuotes(context.Context, []string) (map[string]*domainmarket.RealtimeQuote, error) {
	if f.refreshErr != nil {
		return nil, f.refreshErr
	}
	return f.quotes, nil
}

type fakeMarketRepo struct {
	upsertSymbols int
	createdQuotes int
	latestQuote   *domainmarket.RealtimeQuote
	latestFound   bool
}

func (r *fakeMarketRepo) ListSymbols(context.Context, string, int, string) ([]domainmarket.Symbol, error) {
	return nil, nil
}
func (r *fakeMarketRepo) CreateSymbol(context.Context, *domainmarket.Symbol) error { return nil }
func (r *fakeMarketRepo) UpsertSymbol(context.Context, domainmarket.Symbol) error {
	r.upsertSymbols++
	return nil
}
func (r *fakeMarketRepo) FindSymbol(context.Context, string) (*domainmarket.Symbol, bool, error) {
	return nil, false, nil
}
func (r *fakeMarketRepo) UpdateSymbol(context.Context, string, map[string]any) error { return nil }
func (r *fakeMarketRepo) DeleteSymbol(context.Context, string) error                 { return nil }
func (r *fakeMarketRepo) DailyBars(context.Context, string, int, *time.Time) ([]domainmarket.DailyBar, error) {
	return nil, nil
}
func (r *fakeMarketRepo) UpsertDailyBar(context.Context, domainmarket.DailyBar) error {
	return nil
}
func (r *fakeMarketRepo) CreateRealtimeQuote(_ context.Context, row *domainmarket.RealtimeQuote) error {
	r.createdQuotes++
	row.ID = uint(r.createdQuotes)
	return nil
}
func (r *fakeMarketRepo) LatestRealtimeQuote(context.Context, string) (*domainmarket.RealtimeQuote, bool, error) {
	return r.latestQuote, r.latestFound, nil
}
func (r *fakeMarketRepo) ListWatchlistItems(context.Context, uint, int, string) ([]domainmarket.WatchlistItem, error) {
	return nil, nil
}
func (r *fakeMarketRepo) UpsertWatchlist(context.Context, uint, string, *string, bool) (*domainmarket.WatchlistItem, error) {
	return nil, nil
}
func (r *fakeMarketRepo) FindSymbolByCode(context.Context, string) (*domainmarket.Symbol, bool, error) {
	return nil, false, nil
}
func (r *fakeMarketRepo) WatchlistExists(context.Context, uint) (bool, error) { return false, nil }
func (r *fakeMarketRepo) DeleteWatchlist(context.Context, uint) error         { return nil }
func (r *fakeMarketRepo) ActiveWatchlistToolItems(context.Context, uint) ([]domainmarket.WatchlistItem, error) {
	return nil, nil
}
func (r *fakeMarketRepo) ToolDailyBars(context.Context, string, int) ([]domainmarket.DailyBar, error) {
	return nil, nil
}
func (r *fakeMarketRepo) ToolPaperPositions(context.Context) ([]domainpaper.Position, error) {
	return nil, nil
}
