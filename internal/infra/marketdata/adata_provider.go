package marketdata

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	adata "github.com/onepiecelover/adata-go"
	adatypes "github.com/onepiecelover/adata-go/pkg/types"
	"github.com/shopspring/decimal"
)

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

var NewAdataClient = func() AdataClient {
	return defaultAdataClient{}
}

type AdataProvider struct {
	Client         AdataClient
	HTTPClient     *http.Client
	TencentBaseURL string
}

func NewAdataProvider(client AdataClient, httpClient *http.Client) *AdataProvider {
	if client == nil {
		client = NewAdataClient()
	}
	return &AdataProvider{Client: client, HTTPClient: httpClient}
}

func (p *AdataProvider) client() AdataClient {
	if p.Client != nil {
		return p.Client
	}
	return NewAdataClient()
}

func (p *AdataProvider) httpClient() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

func (p *AdataProvider) tencentBaseURL() string {
	if strings.TrimSpace(p.TencentBaseURL) != "" {
		return p.TencentBaseURL
	}
	return TencentBaseURL
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
	switch RangeToPeriod(rangeKey) {
	case "", "daily", "weekly", "monthly":
		return FetchTencentKLineSeriesFrom(p.tencentBaseURL(), p.httpClient(), code, rangeKey)
	case "intraday", "1d":
		return FetchTencentIntradaySeriesFrom(p.tencentBaseURL(), p.httpClient(), code)
	case "five_day":
		return FetchTencentFiveDaySeriesFrom(p.tencentBaseURL(), p.httpClient(), code)
	default:
		return nil, fmt.Errorf("unsupported range: %s", rangeKey)
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

var FetchAdataRealtimeQuotes = func(codes []string) ([]*Quote, error) {
	return FetchAdataRealtimeQuotesWithClient(NewAdataClient(), codes)
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
	switch RangeToPeriod(rangeKey) {
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

func truncateDate(value time.Time) time.Time {
	y, m, d := value.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, value.Location())
}
