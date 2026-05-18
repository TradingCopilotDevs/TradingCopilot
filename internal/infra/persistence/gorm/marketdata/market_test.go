package marketdata

import (
	"context"
	"encoding/json"
	"errors"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainmarket "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/market"
	domainpaper "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/paper"
	domainsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/settings"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	inframarketdata "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/marketdata"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/connect"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/security"
	"github.com/glebarez/sqlite"
	adatypes "github.com/onepiecelover/adata-go/pkg/types"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var errTestMarketProvider = errors.New("provider unavailable")

type fakeAdataClient struct {
	symbols     []adatypes.StockCode
	marketRows  []adatypes.MarketData
	currentRows []adatypes.CurrentMarket
	minRows     []adatypes.MarketMin
	marketCalls int
	currentCall int
	err         error
}

func (f *fakeAdataClient) AllCode() ([]adatypes.StockCode, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.symbols, nil
}

func (f *fakeAdataClient) GetMarket(params *adatypes.MarketParams) ([]adatypes.MarketData, error) {
	f.marketCalls++
	if f.err != nil {
		return nil, f.err
	}
	return f.marketRows, nil
}

func (f *fakeAdataClient) ListMarketCurrent(codes []string) ([]adatypes.CurrentMarket, error) {
	f.currentCall++
	if f.err != nil {
		return nil, f.err
	}
	return f.currentRows, nil
}

func (f *fakeAdataClient) GetMarketMin(code string) ([]adatypes.MarketMin, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.minRows, nil
}

func (f *fakeAdataClient) GetMarketFive(code string) (*adatypes.MarketFive, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &adatypes.MarketFive{StockCode: code}, nil
}

func withFakeAdata(t *testing.T, client *fakeAdataClient) {
	t.Helper()
	original := newAdataClient
	newAdataClient = func() AdataClient { return client }
	t.Cleanup(func() { newAdataClient = original })
}

func TestEnsureAShareCode(t *testing.T) {
	code, err := EnsureAShareCode("SH600519")
	if err != nil {
		t.Fatal(err)
	}
	if code != "600519" {
		t.Fatalf("expected 600519, got %s", code)
	}
	if _, err := EnsureAShareCode("AVGO"); err == nil {
		t.Fatal("expected invalid code error")
	}
}

func TestBoard(t *testing.T) {
	cases := map[string]string{"600519": "sh_main", "000001": "sz_main", "300750": "chinext", "688981": "star", "510300": "etf_lof"}
	for code, expected := range cases {
		if got := Board(code); got != expected {
			t.Fatalf("%s expected %s got %s", code, expected, got)
		}
	}
}

func TestAdataProviderFetchSymbolsFiltersAShares(t *testing.T) {
	provider := &AdataProvider{Client: &fakeAdataClient{symbols: []adatypes.StockCode{
		{StockCode: "600519", ShortName: "Kweichow Moutai", Exchange: "SSE"},
		{StockCode: "000001", ShortName: "Ping An Bank", Exchange: "SZSE"},
		{StockCode: "AVGO", ShortName: "Broadcom", Exchange: "NASDAQ"},
	}}}
	symbols, err := provider.FetchSymbols()
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 2 || symbols[0].Exchange != "SH" || symbols[1].Exchange != "SZ" {
		t.Fatalf("unexpected adata symbols: %+v", symbols)
	}
}

func TestAdataProviderFetchDailyMapsAndFiltersRows(t *testing.T) {
	provider := &AdataProvider{Client: &fakeAdataClient{marketRows: []adatypes.MarketData{
		{StockCode: "600519", TradeDate: "2026-05-06", Open: 1, High: 2, Low: 0.5, Close: 1.5, Volume: 100, Amount: 200},
		{StockCode: "600519", TradeDate: "2026-05-07", Open: 3, High: 4, Low: 2.5, Close: 3.5, Volume: 300, Amount: 400},
		{StockCode: "600519", TradeDate: "bad-date", Open: 9},
	}}}
	start := time.Date(2026, 5, 7, 0, 0, 0, 0, time.Local)
	rows, err := provider.FetchDaily("SH600519", &start, &start)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Code != "600519" || rows[0].Provider != ProviderAdata || !rows[0].Close.Equal(decimal.RequireFromString("3.5")) {
		t.Fatalf("unexpected daily rows: %+v", rows)
	}
}

func TestAdataProviderFetchSeriesSupportsKLineAndIntraday(t *testing.T) {
	provider := &AdataProvider{Client: &fakeAdataClient{}}
	oldBase := tencentFiveDayBaseURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/appstock/app/fqkline/get":
			if !strings.Contains(r.URL.Query().Get("param"), "sh600519,week") {
				t.Fatalf("unexpected kline query: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"sh600519": map[string]any{
						"qfqweek": []any{[]any{"2026-05-08", "1", "1.5", "2", "0.5", "100", "200"}},
					},
				},
			})
		case "/appstock/app/minute/query":
			if r.URL.Query().Get("code") != "sh600519" {
				t.Fatalf("unexpected intraday query: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"sh600519": map[string]any{
						"data": map[string]any{
							"date": "20260508",
							"data": []string{"0931 10.20 200 2040.00"},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected series request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
	}))
	defer srv.Close()
	tencentFiveDayBaseURL = srv.URL
	defer func() { tencentFiveDayBaseURL = oldBase }()

	kline, err := provider.FetchSeries("600519", "weekly")
	if err != nil {
		t.Fatal(err)
	}
	intraday, err := provider.FetchSeries("600519", "intraday")
	if err != nil {
		t.Fatal(err)
	}
	if kline[0]["time"] != "2026-05-08" || intraday[0]["time"] != "2026-05-08 09:31" {
		t.Fatalf("unexpected series rows kline=%+v intraday=%+v", kline, intraday)
	}
}

func TestParseTencentKLinePayload(t *testing.T) {
	rows, err := ParseTencentKLinePayload(map[string]any{
		"data": map[string]any{
			"sh601899": map[string]any{
				"qfqday": []any{
					[]any{"2026-05-06", "33.50", "33.60", "33.90", "33.10", "105234", "352883206.34"},
					[]any{"bad-date", "1", "2", "3", "0", "10"},
				},
			},
		},
	}, "sh601899", "day")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["time"] != "2026-05-06" || rows[0]["close"] != "33.60" || rows[0]["amount"] != "352883206.34" {
		t.Fatalf("unexpected kline rows: %+v", rows)
	}
	if _, err := ParseTencentKLinePayload(map[string]any{"data": map[string]any{"sh601899": map[string]any{"day": []any{}}}}, "sh601899", "day"); err == nil {
		t.Fatal("expected empty kline payload error")
	}
}

func TestAdataProviderFetchSeriesUsesTencentIntradayDirectly(t *testing.T) {
	client := &fakeAdataClient{err: errors.New("[50102] permission denied")}
	provider := &AdataProvider{Client: client}
	oldBase := tencentFiveDayBaseURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/appstock/app/minute/query" || r.URL.Query().Get("code") != "sh600519" {
			t.Fatalf("unexpected intraday request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"sh600519": map[string]any{
					"data": map[string]any{
						"date": "20260508",
						"data": []string{"0930 10.10 100 1010.00", "0931 10.20 200 2040.00"},
					},
				},
			},
		})
	}))
	defer srv.Close()
	tencentFiveDayBaseURL = srv.URL
	defer func() { tencentFiveDayBaseURL = oldBase }()

	rows, err := provider.FetchSeries("600519", "intraday")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["time"] != "2026-05-08 09:30" || rows[1]["close"] != "10.20" {
		t.Fatalf("unexpected intraday rows: %+v", rows)
	}
}

func TestParseTencentIntradayPayload(t *testing.T) {
	rows, err := ParseTencentIntradayPayload(map[string]any{
		"data": map[string]any{
			"sh601899": map[string]any{
				"data": map[string]any{
					"date": "20260506",
					"data": []any{"0930 33.50 25083 84028050.00", "0931 33.60 105234 352883206.34", "bad-row"},
				},
			},
		},
	}, "sh601899")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["time"] != "2026-05-06 09:30" || rows[1]["volume"] != "105234" {
		t.Fatalf("unexpected intraday rows: %+v", rows)
	}
	if _, err := ParseTencentIntradayPayload(map[string]any{"data": map[string]any{"sh601899": map[string]any{"data": map[string]any{"date": "20260506", "data": []any{}}}}}, "sh601899"); err == nil {
		t.Fatal("expected empty intraday payload error")
	}
}

func TestParseTencentFiveDayPayloadSkipsMalformedRows(t *testing.T) {
	rows, err := ParseTencentFiveDayPayload(map[string]any{
		"data": map[string]any{
			"sh600519": map[string]any{
				"data": []any{
					map[string]any{"date": "20260507", "data": []any{"930 10.10 100 1010.00", "bad-row"}},
					map[string]any{"date": "bad-date", "data": []any{"0931 10.20 200 2040.00"}},
					map[string]any{"date": "20260508", "data": []any{"0931 10.30 300 3090.00"}},
				},
			},
		},
	}, "sh600519")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["time"] != "2026-05-07 09:30" || rows[1]["amount"] != "3090.00" {
		t.Fatalf("unexpected five_day rows: %+v", rows)
	}
	if _, err := ParseTencentFiveDayPayload(map[string]any{"data": map[string]any{"sh600519": map[string]any{"data": []any{}}}}, "sh600519"); err == nil {
		t.Fatal("expected empty five_day payload error")
	}
}

func TestFetchTencentFiveDaySeriesFromConfiguredBase(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/appstock/app/day/query" || r.URL.Query().Get("code") != "sh600519" {
			t.Fatalf("unexpected five_day request: %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		if r.Header.Get("Referer") == "" || r.Header.Get("User-Agent") == "" {
			t.Fatalf("expected browser-like headers, got referer=%q ua=%q", r.Header.Get("Referer"), r.Header.Get("User-Agent"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"sh600519": map[string]any{
					"data": []map[string]any{
						{"date": "20260507", "data": []string{"1500 10.10 100 1010.00"}},
						{"date": "20260508", "data": []string{"0930 10.20 200 2040.00"}},
					},
				},
			},
		})
	}))
	defer srv.Close()

	rows, err := fetchTencentFiveDaySeriesFrom(srv.URL, srv.Client(), "600519")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["time"] != "2026-05-07 15:00" || rows[1]["close"] != "10.20" {
		t.Fatalf("unexpected fetched five_day rows: %+v", rows)
	}
}

func TestSeriesForCodeFromDBUsesFiveDayCompatibilityFetcherBeforeHistoryProvider(t *testing.T) {
	db := newMarketTestDB(t)
	oldBase := tencentFiveDayBaseURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"sh600519": map[string]any{
					"data": []map[string]any{
						{"date": "20260508", "data": []string{"0931 10.30 300 3090.00", "0930 10.20 200 2040.00"}},
					},
				},
			},
		})
	}))
	defer srv.Close()
	tencentFiveDayBaseURL = srv.URL
	defer func() { tencentFiveDayBaseURL = oldBase }()

	rows, err := SeriesForCodeFromDB(db, config.Settings{DefaultMarketProvider: ProviderTushare}, "600519", "five_day")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0]["time"] != "2026-05-08 09:30" || rows[1]["time"] != "2026-05-08 09:31" {
		t.Fatalf("expected normalized five_day rows before Tushare resolution, got %+v", rows)
	}
}

func TestDeprecatedRealtimeProviderReturnsExplicitError(t *testing.T) {
	_, _, err := FetchRealtimeQuote("600519", "eastmoney")
	if err == nil || !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("expected deprecated provider error, got %v", err)
	}
}

func TestQuoteForCodeStoresAdataProvider(t *testing.T) {
	db := newMarketTestDB(t)
	client := &fakeAdataClient{currentRows: []adatypes.CurrentMarket{{StockCode: "600519", Price: 10, ChangePct: 1, Volume: 2, Amount: 3}}}
	withFakeAdata(t, client)

	quote, err := QuoteForCodeWithProvider(db, "600519", true, "adata")
	if err != nil {
		t.Fatal(err)
	}
	if quote.Provider != "adata" || !quote.Price.Equal(decimal.NewFromInt(10)) {
		t.Fatalf("unexpected stored quote %+v", quote)
	}
}

func TestQuoteForCodeHonorsRealtimeCacheTTL(t *testing.T) {
	db := newMarketTestDB(t)
	client := &fakeAdataClient{}
	client.currentRows = []adatypes.CurrentMarket{{StockCode: "600519", Price: 101, ChangePct: 1, Volume: 2, Amount: 3}}
	withFakeAdata(t, client)

	if err := db.Create(&domainmarket.RealtimeQuote{Code: "600519", QuoteTime: time.Now(), Price: decimal.NewFromInt(101), ChangePct: decimal.NewFromInt(1), Provider: "cached"}).Error; err != nil {
		t.Fatal(err)
	}
	client.currentRows = []adatypes.CurrentMarket{{StockCode: "600519", Price: 102, ChangePct: 1, Volume: 2, Amount: 3}}
	second, err := QuoteForCodeWithProviderAndTTL(db, "600519", true, "adata", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if client.currentCall != 0 || !second.Price.Equal(decimal.NewFromInt(101)) {
		t.Fatalf("expected existing cached quote reuse, calls=%d second=%s", client.currentCall, second.Price)
	}
	third, err := QuoteForCodeWithProviderAndTTL(db, "600519", true, "adata", 0)
	if err != nil {
		t.Fatal(err)
	}
	if client.currentCall != 1 || !third.Price.Equal(decimal.NewFromInt(102)) {
		t.Fatalf("expected cache bypass when ttl=0, calls=%d third=%s", client.currentCall, third.Price)
	}
}

func TestRefreshQuotesFallsBackToCachedQuoteOnProviderFailure(t *testing.T) {
	db := newMarketTestDB(t)
	cachedAt := time.Now().Add(-time.Hour)
	if err := db.Create(&domainmarket.RealtimeQuote{Code: "600519", QuoteTime: cachedAt, Price: decimal.NewFromInt(1688), ChangePct: decimal.NewFromInt(1), Provider: "cached"}).Error; err != nil {
		t.Fatal(err)
	}
	withFakeAdata(t, &fakeAdataClient{err: errTestMarketProvider})

	quotes, err := RefreshQuotesWithProvider(db, []string{"600519"}, "adata", 0)
	if err != nil {
		t.Fatal(err)
	}
	if quotes["600519"] == nil || quotes["600519"].Provider != "cached" || !quotes["600519"].Price.Equal(decimal.NewFromInt(1688)) {
		t.Fatalf("expected stale cached quote fallback, got %+v", quotes["600519"])
	}
}

func TestRefreshQuotesWithProviderUsesCompatProviderForMissingETFQuotes(t *testing.T) {
	db := newMarketTestDB(t)
	withFakeAdata(t, &fakeAdataClient{currentRows: []adatypes.CurrentMarket{{StockCode: "600519", Price: 1688, ChangePct: 1, Volume: 2, Amount: 3}}})
	if err := saveTestAppSetting(db, domainsettings.AppSetting{Key: "MARKET_REALTIME_COMPAT_PROVIDER", Value: testJSON(map[string]any{"value": "tencent"})}); err != nil {
		t.Fatal(err)
	}

	oldTencentBase := inframarketdata.TencentRealtimeBaseURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/q=s_sh510300" {
			t.Fatalf("unexpected Tencent compatibility path %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`v_s_sh510300="51~CSI 300 ETF~510300~3.900~0.010~0.26~100~390";`))
	}))
	defer srv.Close()
	inframarketdata.TencentRealtimeBaseURL = srv.URL
	t.Cleanup(func() { inframarketdata.TencentRealtimeBaseURL = oldTencentBase })

	quotes, err := RefreshQuotesWithProvider(db, []string{"600519", "510300"}, "adata", 0)
	if err != nil {
		t.Fatal(err)
	}
	if quotes["600519"] == nil || quotes["600519"].Provider != ProviderAdata {
		t.Fatalf("expected adata stock quote, got %+v", quotes["600519"])
	}
	if quotes["510300"] == nil || quotes["510300"].Provider != inframarketdata.ProviderTencentCompat || !quotes["510300"].Price.Equal(decimal.RequireFromString("3.900")) {
		t.Fatalf("expected Tencent ETF quote, got %+v", quotes["510300"])
	}
}

func TestRefreshQuotesReturnsErrorWhenProviderFailsAndNoCache(t *testing.T) {
	db := newMarketTestDB(t)
	withFakeAdata(t, &fakeAdataClient{err: errTestMarketProvider})

	quotes, err := RefreshQuotesWithProvider(db, []string{"600519"}, "adata", 0)
	if err == nil || len(quotes) != 0 || !strings.Contains(err.Error(), "no realtime quote available") {
		t.Fatalf("expected no-cache upstream failure, quotes=%+v err=%v", quotes, err)
	}
}

func TestRuntimeConfigFromDBUsesAppSettingOverrides(t *testing.T) {
	db := newMarketTestDB(t)
	if err := saveTestAppSetting(db, domainsettings.AppSetting{Key: "MARKET_REALTIME_PROVIDER", Value: testJSON(map[string]any{"value": "adata"})}); err != nil {
		t.Fatal(err)
	}
	if err := saveTestAppSetting(db, domainsettings.AppSetting{Key: "MARKET_REALTIME_COMPAT_PROVIDER", Value: testJSON(map[string]any{"value": "sina"})}); err != nil {
		t.Fatal(err)
	}
	cfg := RuntimeConfigFromDB(db, config.Settings{MarketRealtimeProvider: "adata"})
	if cfg.RealtimeProvider != "adata" || cfg.RealtimeCompatProvider != "sina" {
		t.Fatalf("unexpected runtime config %+v", cfg)
	}
}

func TestResolveHistoryProviderRejectsDeprecatedProviders(t *testing.T) {
	db := newMarketTestDB(t)
	if err := saveTestAppSetting(db, domainsettings.AppSetting{Key: "DEFAULT_MARKET_PROVIDER", Value: testJSON(map[string]any{"value": "akshare"})}); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveHistoryProvider(db, config.Settings{DefaultMarketProvider: ProviderAdata})
	if err == nil || !strings.Contains(err.Error(), "no longer supported") {
		t.Fatalf("expected deprecated provider error, got %v", err)
	}
}

func saveTestAppSetting(db *gorm.DB, setting domainsettings.AppSetting) error {
	return gormrepo.NewSettingsRepository(db).SaveAppSetting(context.Background(), &setting)
}

func TestSyncSymbolItemsNormalizesWithoutPersisting(t *testing.T) {
	db := newMarketTestDB(t)
	if err := db.Create(&domainmarket.Symbol{Code: "600519", Name: "old", Exchange: "SH", Active: false}).Error; err != nil {
		t.Fatal(err)
	}
	synced, err := SyncSymbolItems(db, []SymbolItem{{Code: "600519", Name: "Kweichow Moutai", Exchange: "SH"}, {Code: "000001", Name: "Ping An Bank", Exchange: "SZ"}, {Code: "AVGO", Name: "Broadcom"}})
	if err != nil {
		t.Fatal(err)
	}
	if synced != 2 {
		t.Fatalf("synced mismatch: %d", synced)
	}
	var row domainmarket.Symbol
	if err := db.First(&row, "code = ?", "600519").Error; err != nil {
		t.Fatal(err)
	}
	if row.Name != "old" || row.Active {
		t.Fatalf("SyncSymbolItems should not persist from infra, got %+v", row)
	}
	rows := NormalizeSymbolItems([]SymbolItem{{Code: "600519", Name: "Kweichow Moutai", Exchange: "SH"}, {Code: "AVGO", Name: "Broadcom"}})
	if len(rows) != 1 || rows[0].Code != "600519" || rows[0].Name != "Kweichow Moutai" || !rows[0].Active {
		t.Fatalf("normalized rows mismatch: %+v", rows)
	}
}

func TestQueryToolSupportsOriginalToolNames(t *testing.T) {
	db := newMarketTestDB(t)
	note := "core"
	if err := db.Create(&domainmarket.WatchlistItem{Code: "600519", Note: &note, Active: true}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domainmarket.DailyBar{Code: "600519", TradeDate: time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC), Open: decimal.NewFromInt(1), Close: decimal.NewFromInt(2), Volume: decimal.NewFromInt(3)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domainpaper.Position{AccountID: 1, Code: "600519", Quantity: 100, AvgCost: decimal.NewFromInt(10)}).Error; err != nil {
		t.Fatal(err)
	}
	watchlist, err := QueryTool(db, "watchlist", nil)
	if err != nil || len(watchlist) != 1 || watchlist[0]["code"] != "600519" {
		t.Fatalf("watchlist tool mismatch: rows=%+v err=%v", watchlist, err)
	}
	bars, err := QueryTool(db, "daily_bars", map[string]any{"code": "600519", "limit": 1})
	closeValue, _ := bars[0]["close"].(decimal.Decimal)
	if err != nil || len(bars) != 1 || !closeValue.Equal(decimal.NewFromInt(2)) {
		t.Fatalf("daily_bars tool mismatch: rows=%+v err=%v", bars, err)
	}
	positions, err := QueryTool(db, "paper_positions", nil)
	if err != nil || len(positions) != 1 || positions[0]["quantity"] != 100 {
		t.Fatalf("paper_positions tool mismatch: rows=%+v err=%v", positions, err)
	}
	if _, err := QueryTool(db, "unknown", nil); err == nil {
		t.Fatal("expected unknown tool error")
	}
}

func TestParseTushareRowsAndRefreshDailyBars(t *testing.T) {
	db := newMarketTestDB(t)
	settings := config.Settings{AppSecretKey: "market-test-secret", DefaultMarketProvider: "tushare"}
	encrypted, err := security.New(settings).EncryptSecret("token-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domainsettings.Secret{Kind: domainkernel.SecretKindMarketData, Name: "tushare_token", EncryptedValue: encrypted}).Error; err != nil {
		t.Fatal(err)
	}
	oldBase := tushareAPIBaseURL
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload["api_name"] != "daily" || payload["token"] != "token-1" {
			t.Fatalf("unexpected tushare payload: %+v", payload)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"fields": []string{"ts_code", "trade_date", "open", "high", "low", "close", "vol", "amount"}, "items": [][]any{{"600519.SH", "20260508", 1.0, 2.0, 0.5, 1.5, 100.0, 200.0}}}})
	}))
	defer srv.Close()
	tushareAPIBaseURL = srv.URL
	defer func() { tushareAPIBaseURL = oldBase }()

	rows, err := FetchDailyBars(db, settings, "600519", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one refreshed row, got %d", len(rows))
	}
	row := rows[0]
	if row.Provider != "tushare" || !row.Close.Equal(decimal.RequireFromString("1.5")) {
		t.Fatalf("unexpected daily row: %+v", row)
	}
	var count int64
	db.Model(&domainmarket.DailyBar{}).Where("code = ?", "600519").Count(&count)
	if count != 0 {
		t.Fatalf("FetchDailyBars should not persist from infra, got %d rows", count)
	}
}

func TestParseTushareRowsClassifiesPermissionError(t *testing.T) {
	_, err := ParseTushareRows([]byte(`{"code":-2001,"msg":"permission denied","data":{"fields":[],"items":[]}}`))
	if err == nil {
		t.Fatal("expected tushare permission error")
	}
	var apiErr *TushareAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected TushareAPIError, got %T", err)
	}
	if apiErr.Code != -2001 || !IsTusharePermissionError(err) {
		t.Fatalf("unexpected tushare error classification: %+v", apiErr)
	}
}

func TestParseTushareRowsClassifiesAccessBoundaryErrors(t *testing.T) {
	_, err := ParseTushareRows([]byte(`{"code":40203,"msg":"quota limit reached for stock_basic","data":{"fields":[],"items":[]}}`))
	if err == nil {
		t.Fatal("expected tushare rate limit error")
	}
	if IsTusharePermissionError(err) {
		t.Fatalf("rate limit should not be classified as permission denied: %v", err)
	}
	if !IsTushareAccessBoundaryError(err) {
		t.Fatalf("expected access boundary classification: %v", err)
	}
}

func TestNormalizeSeriesRowsSortsAndDeduplicates(t *testing.T) {
	rows := normalizeSeriesRows([]map[string]any{
		{"time": "2026-05-08", "close": "2"},
		{"time": "2026-05-07", "close": "1"},
		{"time": "2026-05-08", "close": "3"},
		{"close": "ignored"},
	})
	if len(rows) != 2 || rows[0]["time"] != "2026-05-07" || rows[1]["close"] != "3" {
		t.Fatalf("unexpected normalized series: %+v", rows)
	}
}

func testJSON(value any) domainkernel.JSON {
	raw, _ := json.Marshal(value)
	return domainkernel.JSON(raw)
}

func newMarketTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}
