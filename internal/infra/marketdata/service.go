package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainmarket "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/market"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	infralogging "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/logging"
	runtimeproxy "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/proxy/runtime"
	adata "github.com/onepiecelover/adata-go"
	"go.uber.org/zap"
)

type RuntimeStore interface {
	RuntimeConfig(ctx context.Context) (RuntimeConfig, error)
	MarketSecret(ctx context.Context, name string) (string, error)
	ProxyConfig(ctx context.Context) (runtimeproxy.Config, error)
}

type Service struct {
	settings config.Settings
	store    RuntimeStore
}

func NewService(settings config.Settings, store RuntimeStore) Service {
	return Service{settings: settings, store: store}
}

func (s Service) JSON(value any) domainkernel.JSON {
	raw, _ := json.Marshal(value)
	return domainkernel.JSON(raw)
}

func (s Service) NormalizeCode(value string) (string, error)    { return EnsureAShareCode(value) }
func (s Service) EnsureAShareCode(value string) (string, error) { return EnsureAShareCode(value) }
func (s Service) InferExchange(code string) string              { return InferExchange(code) }

func (s Service) FetchSymbols(ctx context.Context) (providerName string, symbols []domainmarket.Symbol, err error) {
	start := time.Now()
	defer func() {
		infralogging.LogOperation(start, "market.symbols", "market", "sync", "market symbols fetch completed", err,
			zap.String("provider", providerName),
			zap.Int("symbolCount", len(symbols)),
		)
	}()
	provider, err := s.historyProvider(ctx)
	if err != nil {
		return "", nil, err
	}
	items, err := provider.FetchSymbols()
	if err != nil {
		return "", nil, err
	}
	return provider.Name(), NormalizeSymbolItems(items), nil
}

func (s Service) RefreshRealtimeQuote(ctx context.Context, code string) (*domainmarket.RealtimeQuote, error) {
	quotes, err := s.RefreshQuotes(ctx, []string{code})
	if err != nil {
		return nil, err
	}
	normalized, err := EnsureAShareCode(code)
	if err != nil {
		return nil, err
	}
	return quotes[normalized], nil
}

func (s Service) Series(ctx context.Context, code string, rangeKey string) (rows []map[string]any, err error) {
	start := time.Now()
	defer func() {
		infralogging.LogOperation(start, "market.series", "market", "fetch", "market series fetch completed", err,
			zap.String("code", code),
			zap.String("range", rangeKey),
			zap.Int("rowCount", len(rows)),
		)
	}()
	code, err = EnsureAShareCode(code)
	if err != nil {
		return nil, err
	}
	if RangeToPeriod(rangeKey) == "five_day" {
		rawRows, err := FetchTencentFiveDaySeriesFrom(TencentBaseURL, s.httpClient(ctx, 10*time.Second), code)
		if err != nil {
			return nil, err
		}
		return NormalizeSeriesRows(rawRows), nil
	}
	provider, err := s.historyProvider(ctx)
	if err != nil {
		return nil, err
	}
	rawRows, err := provider.FetchSeries(code, rangeKey)
	if err != nil {
		return nil, err
	}
	return NormalizeSeriesRows(rawRows), nil
}

func (s Service) FetchDailyBars(ctx context.Context, code string) (bars []domainmarket.DailyBar, err error) {
	start := time.Now()
	defer func() {
		infralogging.LogOperation(start, "market.daily", "market", "fetch", "market daily bars fetch completed", err,
			zap.String("code", code),
			zap.Int("barCount", len(bars)),
		)
	}()
	provider, err := s.historyProvider(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := provider.FetchDaily(code, nil, nil)
	if err != nil {
		return nil, err
	}
	return NormalizeDailyBarItems(rows), nil
}

func (s Service) RefreshQuotes(ctx context.Context, codes []string) (out map[string]*domainmarket.RealtimeQuote, err error) {
	start := time.Now()
	clean := UniqueAShareCodes(codes)
	status := infralogging.StatusOK
	skipReason := ""
	defer func() {
		fields := []zap.Field{
			zap.Int("inputCodeCount", len(codes)),
			zap.Int("codeCount", len(clean)),
			zap.Int("quoteCount", len(out)),
		}
		if skipReason != "" {
			fields = append(fields, zap.String("skipReason", skipReason))
		}
		if err != nil {
			fields = append(fields, zap.Error(err))
			infralogging.Logger().Error("market realtime quotes refresh completed", infralogging.OperationFields("market.quotes", "market", "refresh", infralogging.StatusError, start, fields...)...)
			return
		}
		infralogging.Logger().Info("market realtime quotes refresh completed", infralogging.OperationFields("market.quotes", "market", "refresh", status, start, fields...)...)
	}()
	if len(clean) == 0 {
		status = infralogging.StatusSkipped
		skipReason = "no_valid_a_share_codes"
		return map[string]*domainmarket.RealtimeQuote{}, nil
	}
	cfg, err := s.runtimeConfig(ctx)
	if err != nil {
		return nil, err
	}
	s.applyAdataProxy(ctx)
	provider, quotes, fetchErr := FetchRealtimeQuotesWithFetcher(clean, cfg.RealtimeProvider, FetchAdataRealtimeQuotes)
	out = make(map[string]*domainmarket.RealtimeQuote, len(clean))
	for _, quote := range quotes {
		if quote == nil {
			continue
		}
		code, err := EnsureAShareCode(quote.Code)
		if err != nil {
			continue
		}
		row := domainmarket.RealtimeQuote{
			Code:      code,
			QuoteTime: quote.QuoteTime,
			Price:     quote.Price,
			ChangePct: quote.ChangePct,
			Volume:    quote.Volume,
			Amount:    quote.Amount,
			Raw:       domainkernel.JSON(jsonData(quote.Raw)),
			Provider:  firstNonEmptyString(quote.Provider, provider),
		}
		if row.QuoteTime.IsZero() {
			row.QuoteTime = time.Now()
		}
		out[code] = &row
	}
	missing := CompatibleRealtimeCandidates(clean, out, fetchErr)
	if len(missing) > 0 {
		compatQuotes, compatErr := FetchCompatibleRealtimeQuotes(cfg.RealtimeCompatProvider, missing, s.httpClient(ctx, 10*time.Second))
		if compatErr == nil {
			for _, quote := range compatQuotes {
				if quote == nil {
					continue
				}
				code, err := EnsureAShareCode(quote.Code)
				if err != nil || out[code] != nil {
					continue
				}
				row := domainmarket.RealtimeQuote{
					Code:      code,
					QuoteTime: quote.QuoteTime,
					Price:     quote.Price,
					ChangePct: quote.ChangePct,
					Volume:    quote.Volume,
					Amount:    quote.Amount,
					Raw:       domainkernel.JSON(jsonData(quote.Raw)),
					Provider:  firstNonEmptyString(quote.Provider, cfg.RealtimeCompatProvider),
				}
				if row.QuoteTime.IsZero() {
					row.QuoteTime = time.Now()
				}
				out[code] = &row
			}
		}
		if len(out) == 0 && fetchErr == nil {
			fetchErr = compatErr
		}
	}
	if len(out) == 0 && fetchErr != nil {
		return nil, fetchErr
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no realtime quote available")
	}
	return out, nil
}

func CompatibleRealtimeCandidates(codes []string, existing map[string]*domainmarket.RealtimeQuote, fetchErr error) []string {
	out := make([]string, 0, len(codes))
	allowAllMissing := fetchErr == nil || RealtimeInvalidCodeOrEmpty(fetchErr)
	for _, code := range codes {
		if existing[code] != nil {
			continue
		}
		if allowAllMissing || Board(code) == "etf_lof" {
			out = append(out, code)
		}
	}
	return out
}

func RealtimeInvalidCodeOrEmpty(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "empty realtime quote") ||
		strings.Contains(text, "no valid") ||
		strings.Contains(text, "没有有效")
}

func (s Service) historyProvider(ctx context.Context) (HistoryProvider, error) {
	cfg, err := s.runtimeConfig(ctx)
	if err != nil {
		return nil, err
	}
	s.applyAdataProxy(ctx)
	return ResolveHistoryProvider(cfg, HistoryProviderFactory{
		AdataProvider:  NewAdataProvider(nil, s.httpClient(ctx, 15*time.Second)),
		TushareToken:   func() (string, error) { return s.marketSecret(ctx, "tushare_token") },
		TushareBaseURL: TushareAPIBaseURL,
		HTTPClient:     s.httpClient(ctx, 15*time.Second),
	})
}

func (s Service) runtimeConfig(ctx context.Context) (RuntimeConfig, error) {
	cfg := RuntimeConfig{
		DefaultProvider:        s.settings.DefaultMarketProvider,
		RealtimeProvider:       s.settings.MarketRealtimeProvider,
		RealtimeCompatProvider: s.settings.MarketRealtimeCompatProvider,
		RealtimeCacheTTL:       s.settings.MarketRealtimeCacheTTL,
	}
	if s.store != nil {
		var err error
		cfg, err = s.store.RuntimeConfig(ctx)
		if err != nil {
			return RuntimeConfig{}, err
		}
	}
	if cfg.DefaultProvider == "" {
		cfg.DefaultProvider = ProviderAdata
	}
	if cfg.RealtimeProvider == "" {
		cfg.RealtimeProvider = ProviderAdata
	}
	cfg.RealtimeCompatProvider = NormalizeRealtimeCompatProvider(cfg.RealtimeCompatProvider)
	if cfg.RealtimeCacheTTL <= 0 {
		cfg.RealtimeCacheTTL = 5 * time.Second
	}
	return cfg, nil
}

func (s Service) marketSecret(ctx context.Context, name string) (string, error) {
	if s.store == nil {
		return "", fmt.Errorf("market data secret '%s' is not configured", name)
	}
	return s.store.MarketSecret(ctx, name)
}

func (s Service) httpClient(ctx context.Context, timeout time.Duration) *http.Client {
	cfg := runtimeproxy.Config{NoProxy: append([]string{}, runtimeproxy.DefaultNoProxy...)}
	if s.store != nil {
		if loaded, err := s.store.ProxyConfig(ctx); err == nil {
			cfg = loaded
		}
	}
	return runtimeproxy.HTTPClientForConfig(cfg, runtimeproxy.ModuleMarket, timeout)
}

func (s Service) applyAdataProxy(ctx context.Context) {
	if s.store == nil {
		adata.SetProxy(false, "")
		return
	}
	cfg, err := s.store.ProxyConfig(ctx)
	if err != nil || !cfg.EnabledMarket || strings.TrimSpace(cfg.ProxyURL) == "" {
		adata.SetProxy(false, "")
		return
	}
	adata.SetProxy(true, cfg.ProxyURL)
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

func NormalizeDailyBarItems(rows []DailyBarItem) []domainmarket.DailyBar {
	out := make([]domainmarket.DailyBar, 0, len(rows))
	for _, item := range rows {
		code, err := EnsureAShareCode(item.Code)
		if err != nil {
			continue
		}
		out = append(out, domainmarket.DailyBar{Code: code, TradeDate: truncateDate(item.TradeDate), Open: item.Open, High: item.High, Low: item.Low, Close: item.Close, Volume: item.Volume, Amount: item.Amount, Provider: item.Provider})
	}
	return out
}

func jsonData(value any) domainkernel.JSON {
	raw, _ := json.Marshal(value)
	return domainkernel.JSON(raw)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
