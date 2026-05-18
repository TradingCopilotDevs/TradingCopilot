package marketdata

import (
	"errors"
	"fmt"
	"strings"
	"time"

	adatypes "github.com/onepiecelover/adata-go/pkg/types"
	"github.com/shopspring/decimal"
)

const (
	ProviderAdata   = "adata"
	ProviderTushare = "tushare"
)

var deprecatedProviders = map[string]struct{}{
	"akshare":   {},
	"eastmoney": {},
	"tencent":   {},
	"sina":      {},
}

type Quote struct {
	Code      string
	Provider  string
	Price     decimal.Decimal
	ChangePct decimal.Decimal
	Volume    decimal.Decimal
	Amount    decimal.Decimal
	QuoteTime time.Time
	Raw       map[string]any
}

type AdataRealtimeClient interface {
	ListMarketCurrent(codes []string) ([]adatypes.CurrentMarket, error)
}

func FetchRealtimeQuoteWithFetcher(code string, provider string, fetchAdata func([]string) ([]*Quote, error)) (string, *Quote, error) {
	providerName, quotes, err := FetchRealtimeQuotesWithFetcher([]string{code}, provider, fetchAdata)
	if err != nil {
		return "", nil, err
	}
	if len(quotes) == 0 {
		return "", nil, errors.New("no realtime quote available")
	}
	return providerName, quotes[0], nil
}

func FetchRealtimeQuotesWithFetcher(codes []string, provider string, fetchAdata func([]string) ([]*Quote, error)) (string, []*Quote, error) {
	clean := UniqueAShareCodes(codes)
	if len(clean) == 0 {
		return "", nil, errors.New("no valid A-share codes")
	}
	name := NormalizeProviderName(provider, ProviderAdata)
	if name != ProviderAdata {
		return "", nil, UnsupportedProviderError(name)
	}
	quotes, err := fetchAdata(clean)
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", name, err)
	}
	if len(quotes) == 0 {
		return "", nil, fmt.Errorf("%s: empty realtime quote", name)
	}
	return name, quotes, nil
}

func FetchAdataRealtimeQuotesWithClient(client AdataRealtimeClient, codes []string) ([]*Quote, error) {
	rows, err := client.ListMarketCurrent(UniqueAShareCodes(codes))
	if err != nil {
		return nil, err
	}
	out := make([]*Quote, 0, len(rows))
	now := time.Now()
	for _, row := range rows {
		code, err := EnsureAShareCode(row.StockCode)
		if err != nil {
			continue
		}
		quote := &Quote{
			Code:      code,
			Provider:  ProviderAdata,
			Price:     decimal.NewFromFloat(row.Price),
			ChangePct: decimal.NewFromFloat(row.ChangePct),
			Volume:    decimal.NewFromInt(row.Volume),
			Amount:    decimal.NewFromFloat(row.Amount),
			QuoteTime: now,
			Raw: map[string]any{
				"provider":    ProviderAdata,
				"stock_code":  row.StockCode,
				"short_name":  row.ShortName,
				"change":      row.Change,
				"open":        row.Open,
				"high":        row.High,
				"low":         row.Low,
				"pre_close":   row.PreClose,
				"turnover":    row.Turnover,
				"market_cap":  row.MarketCap,
				"circ_market": row.CircMarket,
				"pb":          row.PB,
				"pe":          row.PE,
			},
		}
		if quote.Price.IsZero() {
			continue
		}
		out = append(out, quote)
	}
	return out, nil
}

func UniqueAShareCodes(codes []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(codes))
	for _, value := range codes {
		code, err := EnsureAShareCode(value)
		if err != nil {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out
}

func UnsupportedProviderError(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		name = ProviderAdata
	}
	if _, deprecated := deprecatedProviders[strings.ToLower(name)]; deprecated {
		return fmt.Errorf("market provider %q is no longer supported; configure %q or %q", name, ProviderAdata, ProviderTushare)
	}
	return fmt.Errorf("market data provider %q is not supported by the Go runtime; configure provider %q", name, ProviderAdata)
}
