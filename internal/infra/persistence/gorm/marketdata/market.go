package marketdata

import (
	"encoding/json"
	"errors"
	"fmt"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/market"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	inframarketdata "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/marketdata"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	adata "github.com/onepiecelover/adata-go"
	adatypes "github.com/onepiecelover/adata-go/pkg/types"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const (
	ProviderAdata   = inframarketdata.ProviderAdata
	ProviderTushare = inframarketdata.ProviderTushare
)

var deprecatedProviders = map[string]struct{}{
	"akshare":   {},
	"eastmoney": {},
	"tencent":   {},
	"sina":      {},
}

func EnsureAShareCode(value string) (string, error) {
	return inframarketdata.EnsureAShareCode(value)
}

func InferExchange(code string) string {
	return inframarketdata.InferExchange(code)
}

func Board(code string) string {
	return inframarketdata.Board(code)
}

type Quote = inframarketdata.Quote

type SymbolItem = inframarketdata.SymbolItem

type RuntimeConfig = inframarketdata.RuntimeConfig

type DailyBarItem = inframarketdata.DailyBarItem

type HistoryProvider = inframarketdata.HistoryProvider

type AdataClient interface {
	AllCode() ([]adatypes.StockCode, error)
	GetMarket(params *adatypes.MarketParams) ([]adatypes.MarketData, error)
	ListMarketCurrent(codes []string) ([]adatypes.CurrentMarket, error)
	GetMarketMin(code string) ([]adatypes.MarketMin, error)
	GetMarketFive(code string) (*adatypes.MarketFive, error)
}

type defaultAdataClient struct{}

func (defaultAdataClient) AllCode() ([]adatypes.StockCode, error) {
	return adata.Stock.Info.AllCode()
}

func (defaultAdataClient) GetMarket(params *adatypes.MarketParams) ([]adatypes.MarketData, error) {
	return adata.Stock.Market.GetMarket(params)
}

func (defaultAdataClient) ListMarketCurrent(codes []string) ([]adatypes.CurrentMarket, error) {
	return adata.Stock.Market.ListMarketCurrent(codes)
}

func (defaultAdataClient) GetMarketMin(code string) ([]adatypes.MarketMin, error) {
	return adata.Stock.Market.GetMarketMin(code)
}

func (defaultAdataClient) GetMarketFive(code string) (*adatypes.MarketFive, error) {
	return adata.Stock.Market.GetMarketFive(code)
}

var newAdataClient = func() AdataClient {
	return defaultAdataClient{}
}

type AdataProvider struct {
	Client AdataClient
}

func NewAdataProvider() *AdataProvider {
	return &AdataProvider{Client: newAdataClient()}
}

func (p *AdataProvider) client() AdataClient {
	if p.Client != nil {
		return p.Client
	}
	return newAdataClient()
}

func (p *AdataProvider) Name() string {
	return ProviderAdata
}

func (p *AdataProvider) FetchSymbols() ([]SymbolItem, error) {
	rows, err := p.client().AllCode()
	if err != nil {
		return nil, err
	}
	out := make([]SymbolItem, 0, len(rows))
	for _, row := range rows {
		code, err := EnsureAShareCode(row.StockCode)
		if err != nil {
			continue
		}
		out = append(out, SymbolItem{Code: code, Name: strings.TrimSpace(row.ShortName), Exchange: normalizeExchange(row.Exchange, code)})
	}
	return out, nil
}

func (p *AdataProvider) FetchDaily(code string, start *time.Time, end *time.Time) ([]DailyBarItem, error) {
	return p.fetchKLine(code, start, end, "", ProviderAdata)
}

func (p *AdataProvider) FetchSeries(code string, rangeKey string) ([]map[string]any, error) {
	code, err := EnsureAShareCode(code)
	if err != nil {
		return nil, err
	}
	switch rangeToPeriod(rangeKey) {
	case "", "daily", "weekly", "monthly":
		rows, err := fetchTencentKLineSeries(code, rangeKey)
		if err != nil {
			return nil, err
		}
		return rows, nil
	case "intraday", "1d":
		return fetchTencentIntradaySeries(code)
	case "five_day":
		return fetchTencentFiveDaySeries(code)
	default:
		return nil, errors.New("unsupported range")
	}
}

func (p *AdataProvider) fetchKLine(code string, start *time.Time, end *time.Time, rangeKey string, provider string) ([]DailyBarItem, error) {
	code, err := EnsureAShareCode(code)
	if err != nil {
		return nil, err
	}
	params := &adatypes.MarketParams{
		StockCode:  code,
		StartDate:  valueOrZero(start),
		EndDate:    valueOrZero(end),
		KType:      adataKType(rangeKey),
		AdjustType: 1,
	}
	rows, err := p.client().GetMarket(params)
	if err != nil {
		return nil, err
	}
	out := make([]DailyBarItem, 0, len(rows))
	for _, row := range rows {
		tradeDate, err := parseAdataTradeDate(row.TradeDate)
		if err != nil {
			continue
		}
		if start != nil && tradeDate.Before(truncateDate(*start)) {
			continue
		}
		if end != nil && tradeDate.After(truncateDate(*end)) {
			continue
		}
		out = append(out, DailyBarItem{
			Code:      code,
			TradeDate: tradeDate,
			Open:      decimal.NewFromFloat(row.Open),
			High:      decimal.NewFromFloat(row.High),
			Low:       decimal.NewFromFloat(row.Low),
			Close:     decimal.NewFromFloat(row.Close),
			Volume:    decimal.NewFromInt(row.Volume),
			Amount:    decimal.NewFromFloat(row.Amount),
			Provider:  provider,
		})
	}
	return out, nil
}

type HTTPHistoryProvider = inframarketdata.HTTPHistoryProvider
type TushareAPIError = inframarketdata.TushareAPIError

func IsTusharePermissionError(err error) bool {
	return inframarketdata.IsTusharePermissionError(err)
}

func IsTushareAccessBoundaryError(err error) bool {
	return inframarketdata.IsTushareAccessBoundaryError(err)
}

func ParseTushareRows(raw []byte) ([]map[string]any, error) {
	return inframarketdata.ParseTushareRows(raw)
}

var tushareAPIBaseURL = "https://api.tushare.pro"
var tencentFiveDayBaseURL = "https://web.ifzq.gtimg.cn"
var marketProxyMu sync.RWMutex
var marketProxyConfig runtimeproxy.Config

func ApplyRuntimeProxy(db *gorm.DB, settings config.Settings) {
	cfg := runtimeproxy.Load(db, settings)
	marketProxyMu.Lock()
	marketProxyConfig = cfg
	marketProxyMu.Unlock()
	if cfg.EnabledMarket && strings.TrimSpace(cfg.ProxyURL) != "" {
		adata.SetProxy(true, cfg.ProxyURL)
		return
	}
	adata.SetProxy(false, "")
}

func marketHTTPClient(timeout time.Duration) *http.Client {
	marketProxyMu.RLock()
	cfg := marketProxyConfig
	marketProxyMu.RUnlock()
	return runtimeproxy.HTTPClientForConfig(cfg, runtimeproxy.ModuleMarket, timeout)
}

func SyncSymbols(db *gorm.DB) (int, error) {
	rows, err := FetchSymbols(db, config.Load())
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func SyncSymbolItems(db *gorm.DB, symbols []SymbolItem) (int, error) {
	return len(NormalizeSymbolItems(symbols)), nil
}

func FetchSymbols(db *gorm.DB, settings config.Settings) ([]domainmarket.Symbol, error) {
	ApplyRuntimeProxy(db, settings)
	provider, err := ResolveHistoryProvider(db, settings)
	if err != nil {
		return nil, err
	}
	symbols, err := provider.FetchSymbols()
	if err != nil {
		return nil, err
	}
	return NormalizeSymbolItems(symbols), nil
}

func NormalizeSymbolItems(symbols []SymbolItem) []domainmarket.Symbol {
	rows := make([]domainmarket.Symbol, 0, len(symbols))
	for _, item := range symbols {
		code, err := EnsureAShareCode(item.Code)
		if err != nil {
			continue
		}
		exchange := strings.TrimSpace(item.Exchange)
		if exchange == "" {
			exchange = InferExchange(code)
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = code
		}
		rows = append(rows, domainmarket.Symbol{Code: code, Name: name, Exchange: exchange, Active: true})
	}
	return rows
}

func ResolveHistoryProvider(db *gorm.DB, settings config.Settings) (HistoryProvider, error) {
	ApplyRuntimeProxy(db, settings)
	cfg := RuntimeConfigFromDB(db, settings)
	return inframarketdata.ResolveHistoryProvider(cfg, inframarketdata.HistoryProviderFactory{
		AdataProvider: NewAdataProvider(),
		TushareToken: func() (string, error) {
			return marketSecret(db, settings, "tushare_token")
		},
		TushareBaseURL: tushareAPIBaseURL,
		HTTPClient:     runtimeproxy.NewHTTPClient(db, settings, runtimeproxy.ModuleMarket, 15*time.Second),
	})
}

func QuoteForCode(db *gorm.DB, code string, refresh bool) (*domainmarket.RealtimeQuote, error) {
	return QuoteForCodeWithProvider(db, code, refresh, ProviderAdata)
}

func QuoteForCodeWithProvider(db *gorm.DB, code string, refresh bool, provider string) (*domainmarket.RealtimeQuote, error) {
	return QuoteForCodeWithProviderAndTTL(db, code, refresh, provider, 0)
}

func QuoteForCodeWithProviderAndTTL(db *gorm.DB, code string, refresh bool, provider string, ttl time.Duration) (*domainmarket.RealtimeQuote, error) {
	code, err := EnsureAShareCode(code)
	if err != nil {
		return nil, err
	}
	if refresh {
		quotes, err := RefreshQuotesWithProvider(db, []string{code}, provider, ttl)
		if err == nil {
			if quote := quotes[code]; quote != nil {
				return quote, nil
			}
		}
	}
	var row persistmodel.RealtimeQuote
	if err := db.Where("code = ?", code).Order("quote_time desc").First(&row).Error; err != nil {
		return nil, err
	}
	out := realtimeQuoteFromModel(row)
	return &out, nil
}

func RuntimeConfigFromDB(db *gorm.DB, settings config.Settings) RuntimeConfig {
	cfg := RuntimeConfig{DefaultProvider: settings.DefaultMarketProvider, RealtimeProvider: settings.MarketRealtimeProvider, RealtimeCompatProvider: settings.MarketRealtimeCompatProvider, RealtimeCacheTTL: settings.MarketRealtimeCacheTTL}
	if cfg.DefaultProvider == "" {
		cfg.DefaultProvider = ProviderAdata
	}
	if cfg.RealtimeProvider == "" {
		cfg.RealtimeProvider = ProviderAdata
	}
	cfg.RealtimeCompatProvider = inframarketdata.NormalizeRealtimeCompatProvider(cfg.RealtimeCompatProvider)
	if cfg.RealtimeCacheTTL <= 0 {
		cfg.RealtimeCacheTTL = 5 * time.Second
	}
	if value, ok := appSettingScalar(db, "DEFAULT_MARKET_PROVIDER"); ok {
		cfg.DefaultProvider = fmt.Sprint(value)
	}
	if value, ok := appSettingScalar(db, "MARKET_REALTIME_PROVIDER"); ok {
		cfg.RealtimeProvider = fmt.Sprint(value)
	}
	if value, ok := appSettingScalar(db, "MARKET_REALTIME_COMPAT_PROVIDER"); ok {
		cfg.RealtimeCompatProvider = inframarketdata.NormalizeRealtimeCompatProvider(fmt.Sprint(value))
	}
	if value, ok := appSettingScalar(db, "MARKET_REALTIME_CACHE_TTL_SECONDS"); ok {
		if seconds, err := strconv.Atoi(fmt.Sprint(value)); err == nil {
			cfg.RealtimeCacheTTL = time.Duration(maxInt(seconds, 0)) * time.Second
		}
	}
	return cfg
}

func RefreshQuotesWithProvider(db *gorm.DB, codes []string, provider string, ttl time.Duration) (map[string]*domainmarket.RealtimeQuote, error) {
	settings := config.Load()
	ApplyRuntimeProxy(db, settings)
	cfg := RuntimeConfigFromDB(db, settings)
	clean := uniqueAShareCodes(codes)
	out := make(map[string]*domainmarket.RealtimeQuote, len(clean))
	if len(clean) == 0 {
		return out, nil
	}
	staleThreshold := time.Time{}
	if ttl > 0 {
		staleThreshold = time.Now().Add(-ttl)
	}
	toFetch := make([]string, 0, len(clean))
	cachedRows := map[string]domainmarket.RealtimeQuote{}
	for _, code := range clean {
		var cached persistmodel.RealtimeQuote
		err := db.Where("code = ?", code).Order("quote_time desc").First(&cached).Error
		if err == nil {
			cachedRows[code] = realtimeQuoteFromModel(cached)
			if ttl > 0 && !cached.QuoteTime.Before(staleThreshold) {
				row := realtimeQuoteFromModel(cached)
				out[code] = &row
				continue
			}
		}
		toFetch = append(toFetch, code)
	}
	if len(toFetch) == 0 {
		return out, nil
	}
	providerName, quotes, fetchErr := FetchRealtimeQuotes(toFetch, provider)
	applyFetchedRealtimeQuotes(out, quotes, providerName)
	if missing := inframarketdata.CompatibleRealtimeCandidates(toFetch, out, fetchErr); len(missing) > 0 {
		compatQuotes, compatErr := inframarketdata.FetchCompatibleRealtimeQuotes(cfg.RealtimeCompatProvider, missing, marketHTTPClient(10*time.Second))
		if compatErr == nil {
			applyFetchedRealtimeQuotes(out, compatQuotes, cfg.RealtimeCompatProvider)
		}
		if len(out) == 0 && fetchErr == nil {
			fetchErr = compatErr
		}
	}
	if fetchErr != nil {
		for _, code := range toFetch {
			if cached, ok := cachedRows[code]; ok {
				row := cached
				out[code] = &row
			}
		}
		if len(out) == 0 {
			return out, errors.New("no realtime quote available")
		}
		return out, nil
	}
	for _, code := range toFetch {
		if out[code] == nil {
			if cached, ok := cachedRows[code]; ok {
				row := cached
				out[code] = &row
			}
		}
	}
	if len(out) == 0 {
		return out, errors.New("no realtime quote available")
	}
	return out, nil
}

func applyFetchedRealtimeQuotes(out map[string]*domainmarket.RealtimeQuote, quotes []*Quote, providerName string) {
	for _, quote := range quotes {
		if quote == nil {
			continue
		}
		code, err := EnsureAShareCode(quote.Code)
		if err != nil || out[code] != nil {
			continue
		}
		raw := jsonData(quote.Raw)
		row := domainmarket.RealtimeQuote{
			Code:      code,
			QuoteTime: quote.QuoteTime,
			Price:     quote.Price,
			ChangePct: quote.ChangePct,
			Volume:    quote.Volume,
			Amount:    quote.Amount,
			Raw:       domainkernel.JSON(raw),
			Provider:  firstNonEmptyString(quote.Provider, providerName),
		}
		if row.QuoteTime.IsZero() {
			row.QuoteTime = time.Now()
		}
		out[code] = &row
	}
}

func FetchRealtimeQuote(code string, provider string) (string, *Quote, error) {
	return inframarketdata.FetchRealtimeQuoteWithFetcher(code, provider, FetchAdataRealtimeQuotes)
}

func FetchRealtimeQuotes(codes []string, provider string) (string, []*Quote, error) {
	return inframarketdata.FetchRealtimeQuotesWithFetcher(codes, provider, FetchAdataRealtimeQuotes)
}

func FetchAdataRealtimeQuotes(codes []string) ([]*Quote, error) {
	return inframarketdata.FetchAdataRealtimeQuotesWithClient(newAdataClient(), codes)
}

func SeriesForCode(code string, rangeKey string) ([]map[string]any, error) {
	if rangeToPeriod(rangeKey) == "five_day" {
		code, err := EnsureAShareCode(code)
		if err != nil {
			return nil, err
		}
		rows, err := fetchTencentFiveDaySeries(code)
		if err != nil {
			return nil, err
		}
		return normalizeSeriesRows(rows), nil
	}
	return NewAdataProvider().FetchSeries(code, rangeKey)
}

func SeriesForCodeFromDB(db *gorm.DB, settings config.Settings, code string, rangeKey string) ([]map[string]any, error) {
	code, err := EnsureAShareCode(code)
	if err != nil {
		return nil, err
	}
	if rangeToPeriod(rangeKey) == "five_day" {
		rows, err := fetchTencentFiveDaySeries(code)
		if err != nil {
			return nil, err
		}
		return normalizeSeriesRows(rows), nil
	}
	provider, err := ResolveHistoryProvider(db, settings)
	if err != nil {
		return nil, err
	}
	rows, err := provider.FetchSeries(code, rangeKey)
	if err != nil {
		return nil, err
	}
	return normalizeSeriesRows(rows), nil
}

func RefreshDailyBars(db *gorm.DB, settings config.Settings, code string, start *time.Time, end *time.Time) (int, error) {
	rows, err := FetchDailyBars(db, settings, code, start, end)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}

func FetchDailyBars(db *gorm.DB, settings config.Settings, code string, start *time.Time, end *time.Time) ([]domainmarket.DailyBar, error) {
	provider, err := ResolveHistoryProvider(db, settings)
	if err != nil {
		return nil, err
	}
	rows, err := provider.FetchDaily(code, start, end)
	if err != nil {
		return nil, err
	}
	return NormalizeDailyBarItems(rows), nil
}

func UpsertDailyBars(db *gorm.DB, rows []DailyBarItem) (int, error) {
	return len(NormalizeDailyBarItems(rows)), nil
}

func NormalizeDailyBarItems(rows []DailyBarItem) []domainmarket.DailyBar {
	out := make([]domainmarket.DailyBar, 0, len(rows))
	for _, item := range rows {
		code, err := EnsureAShareCode(item.Code)
		if err != nil {
			continue
		}
		tradeDate := truncateDate(item.TradeDate)
		out = append(out, domainmarket.DailyBar{Code: code, TradeDate: tradeDate, Open: item.Open, High: item.High, Low: item.Low, Close: item.Close, Volume: item.Volume, Amount: item.Amount, Provider: item.Provider})
	}
	return out
}

func QueryTool(db *gorm.DB, tool string, args map[string]any) ([]map[string]any, error) {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "watchlist", "market.watchlist":
		var items []persistmodel.WatchlistItem
		if err := db.Where("active = ?", true).Order("code").Find(&items).Error; err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			out = append(out, map[string]any{"code": item.Code, "note": item.Note})
		}
		return out, nil
	case "realtime_quote", "market.realtime_quote":
		code := fmt.Sprint(args["code"])
		quote, err := QuoteForCode(db, code, true)
		if err != nil {
			return nil, err
		}
		return []map[string]any{{"code": quote.Code, "price": quote.Price, "quote_time": quote.QuoteTime}}, nil
	case "daily_bars":
		code, err := EnsureAShareCode(fmt.Sprint(args["code"]))
		if err != nil {
			return nil, err
		}
		limit := intFromToolArgs(args["limit"], 120, 500)
		var rows []persistmodel.DailyBar
		if err := db.Where("code = ?", code).Order("trade_date desc").Limit(limit).Find(&rows).Error; err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			out = append(out, map[string]any{"code": row.Code, "trade_date": row.TradeDate, "open": row.Open, "close": row.Close, "volume": row.Volume})
		}
		return out, nil
	case "paper_positions":
		var rows []persistmodel.PaperPosition
		if err := db.Order("account_id, code").Find(&rows).Error; err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			out = append(out, map[string]any{"account_id": row.AccountID, "code": row.Code, "quantity": row.Quantity, "avg_cost": row.AvgCost})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown tool: %s", tool)
	}
}

func appSettingScalar(db *gorm.DB, key string) (any, bool) {
	var setting persistmodel.AppSetting
	result := db.Where("key = ?", key).Limit(1).Find(&setting)
	if result.Error != nil || result.RowsAffected == 0 {
		return nil, false
	}
	var value any
	if err := json.Unmarshal(setting.Value, &value); err != nil {
		return nil, false
	}
	if obj, ok := value.(map[string]any); ok {
		if inner, exists := obj["value"]; exists {
			return inner, true
		}
	}
	return value, true
}

func marketSecret(db *gorm.DB, settings config.Settings, name string) (string, error) {
	var secret persistmodel.Secret
	if err := db.First(&secret, "kind = ? AND name = ?", domainkernel.SecretKindMarketData, name).Error; err != nil {
		return "", fmt.Errorf("market data secret '%s' is not configured", name)
	}
	return security.New(settings).DecryptSecret(secret.EncryptedValue)
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

func watchlistItemFromModel(row persistmodel.WatchlistItem) domainmarket.WatchlistItem {
	return domainmarket.WatchlistItem{
		ID:        row.ID,
		Code:      row.Code,
		Note:      row.Note,
		Active:    row.Active,
		CreatedAt: row.CreatedAt,
	}
}

func jsonData(value any) domainkernel.JSON {
	raw, _ := json.Marshal(value)
	return domainkernel.JSON(raw)
}

func normalizeProviderName(value string, fallback string) string {
	return inframarketdata.NormalizeProviderName(value, fallback)
}

func unsupportedProviderError(name string) error {
	return inframarketdata.UnsupportedProviderError(name)
}

func fetchTencentFiveDaySeries(code string) ([]map[string]any, error) {
	return fetchTencentFiveDaySeriesFrom(tencentFiveDayBaseURL, marketHTTPClient(10*time.Second), code)
}

func fetchTencentIntradaySeries(code string) ([]map[string]any, error) {
	return fetchTencentIntradaySeriesFrom(tencentFiveDayBaseURL, marketHTTPClient(10*time.Second), code)
}

func fetchTencentKLineSeries(code string, rangeKey string) ([]map[string]any, error) {
	return fetchTencentKLineSeriesFrom(tencentFiveDayBaseURL, marketHTTPClient(10*time.Second), code, rangeKey)
}

func fetchTencentKLineSeriesFrom(baseURL string, client *http.Client, code string, rangeKey string) ([]map[string]any, error) {
	return inframarketdata.FetchTencentKLineSeriesFrom(baseURL, client, code, rangeKey)
}

func fetchTencentIntradaySeriesFrom(baseURL string, client *http.Client, code string) ([]map[string]any, error) {
	return inframarketdata.FetchTencentIntradaySeriesFrom(baseURL, client, code)
}

func fetchTencentFiveDaySeriesFrom(baseURL string, client *http.Client, code string) ([]map[string]any, error) {
	return inframarketdata.FetchTencentFiveDaySeriesFrom(baseURL, client, code)
}

func ParseTencentIntradayPayload(payload map[string]any, symbol string) ([]map[string]any, error) {
	return inframarketdata.ParseTencentIntradayPayload(payload, symbol)
}

func ParseTencentKLinePayload(payload map[string]any, symbol string, period string) ([]map[string]any, error) {
	return inframarketdata.ParseTencentKLinePayload(payload, symbol, period)
}

func ParseTencentFiveDayPayload(payload map[string]any, symbol string) ([]map[string]any, error) {
	return inframarketdata.ParseTencentFiveDayPayload(payload, symbol)
}

func tencentCompatibilitySymbol(code string) string {
	return inframarketdata.TencentCompatibilitySymbol(code)
}

func normalizeExchange(value string, code string) string {
	exchange := strings.ToUpper(strings.TrimSpace(value))
	switch exchange {
	case "SH", "SSE", "SHSE":
		return "SH"
	case "SZ", "SZSE":
		return "SZ"
	case "BJ", "BSE":
		return "BJ"
	default:
		return InferExchange(code)
	}
}

func adataKType(rangeKey string) int {
	switch rangeToPeriod(rangeKey) {
	case "weekly":
		return 2
	case "monthly":
		return 3
	default:
		return 1
	}
}

func parseAdataTradeDate(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	for _, layout := range []string{"2006-01-02", "20060102"} {
		if t, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid adata trade date: %s", value)
}

func valueOrZero(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func decimalFromAny(v any) decimal.Decimal {
	return inframarketdata.DecimalFromAny(v)
}

func JSONMap(raw []byte) map[string]any {
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func Int64(v any) int64 {
	i, _ := strconv.ParseInt(fmt.Sprint(v), 10, 64)
	return i
}

func intFromToolArgs(value any, fallback int, maxValue int) int {
	parsed, err := strconv.Atoi(fmt.Sprint(value))
	if value == nil || err != nil {
		parsed = fallback
	}
	if parsed <= 0 {
		return fallback
	}
	if parsed > maxValue {
		return maxValue
	}
	return parsed
}

func uniqueAShareCodes(codes []string) []string {
	return inframarketdata.UniqueAShareCodes(codes)
}

func maxInt(value int, minimum int) int {
	if value < minimum {
		return minimum
	}
	return value
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func tushareSymbol(code string) string {
	return inframarketdata.TushareSymbol(code)
}

func tushareExchange(tsCode string) string {
	return inframarketdata.TushareExchange(tsCode)
}

func dateParam(value *time.Time, fallback string) string {
	return inframarketdata.DateParam(value, fallback)
}

func rangeToPeriod(rangeKey string) string {
	return inframarketdata.RangeToPeriod(rangeKey)
}

func tencentKLinePeriod(rangeKey string) string {
	return inframarketdata.TencentKLinePeriod(rangeKey)
}

func tencentKLineLimit(period string) int {
	return inframarketdata.TencentKLineLimit(period)
}

func dailyBarItemSeriesRow(row DailyBarItem) map[string]any {
	return map[string]any{"time": row.TradeDate.Format("2006-01-02"), "open": row.Open, "high": row.High, "low": row.Low, "close": row.Close, "volume": row.Volume, "amount": row.Amount}
}

func marketMinSeriesRow(row adatypes.MarketMin) map[string]any {
	return map[string]any{
		"time":       row.TradeTime.Format("2006-01-02 15:04"),
		"open":       decimal.NewFromFloat(row.Price),
		"high":       decimal.NewFromFloat(row.Price),
		"low":        decimal.NewFromFloat(row.Price),
		"close":      decimal.NewFromFloat(row.Price),
		"volume":     decimal.NewFromInt(row.Volume),
		"amount":     decimal.NewFromFloat(row.Amount),
		"avg_price":  decimal.NewFromFloat(row.AvgPrice),
		"change":     decimal.NewFromFloat(row.Change),
		"change_pct": decimal.NewFromFloat(row.ChangePct),
	}
}

func normalizeSeriesRows(rows []map[string]any) []map[string]any {
	return inframarketdata.NormalizeSeriesRows(rows)
}

func truncateDate(value time.Time) time.Time {
	y, m, d := value.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, value.Location())
}

func valueAt(values []string, index int) string {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return ""
}
