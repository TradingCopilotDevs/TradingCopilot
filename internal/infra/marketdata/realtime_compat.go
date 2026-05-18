package marketdata

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const (
	ProviderTencentCompat  = "tencent"
	ProviderSinaCompat     = "sina"
	ProviderCompatDisabled = "disabled"
)

var (
	TencentRealtimeBaseURL = "https://qt.gtimg.cn"
	SinaRealtimeBaseURL    = "https://hq.sinajs.cn"
)

func NormalizeRealtimeCompatProvider(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ProviderSinaCompat:
		return ProviderSinaCompat
	case ProviderCompatDisabled:
		return ProviderCompatDisabled
	default:
		return ProviderTencentCompat
	}
}

func FetchCompatibleRealtimeQuotes(provider string, codes []string, client *http.Client) ([]*Quote, error) {
	provider = NormalizeRealtimeCompatProvider(provider)
	switch provider {
	case ProviderCompatDisabled:
		return nil, errors.New("realtime compatibility provider is disabled")
	case ProviderSinaCompat:
		return FetchSinaRealtimeQuotesFrom(SinaRealtimeBaseURL, client, codes)
	default:
		return FetchTencentRealtimeQuotesFrom(TencentRealtimeBaseURL, client, codes)
	}
}

func FetchTencentRealtimeQuotesFrom(baseURL string, client *http.Client, codes []string) ([]*Quote, error) {
	symbols := realtimeCompatibilitySymbols(codes)
	if len(symbols) == 0 {
		return nil, errors.New("no valid realtime compatibility symbols")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = TencentRealtimeBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/q=" + url.QueryEscape(strings.Join(symbols, ","))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://finance.qq.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tencent realtime status=%d", resp.StatusCode)
	}
	return ParseTencentRealtimeQuotes(string(raw))
}

func FetchSinaRealtimeQuotesFrom(baseURL string, client *http.Client, codes []string) ([]*Quote, error) {
	symbols := realtimeCompatibilitySymbols(codes)
	if len(symbols) == 0 {
		return nil, errors.New("no valid realtime compatibility symbols")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = SinaRealtimeBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	endpoint := strings.TrimRight(baseURL, "/") + "/list=" + url.QueryEscape(strings.Join(symbols, ","))
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn/")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("sina realtime status=%d", resp.StatusCode)
	}
	return ParseSinaRealtimeQuotes(string(raw))
}

func ParseTencentRealtimeQuotes(response string) ([]*Quote, error) {
	lines := strings.Split(response, ";")
	out := make([]*Quote, 0, len(lines))
	now := time.Now()
	for _, line := range lines {
		parts := strings.Split(line, "~")
		if len(parts) < 8 {
			continue
		}
		code, err := EnsureAShareCode(parts[2])
		if err != nil {
			continue
		}
		price := decimalFromText(parts[3])
		if price.IsZero() {
			continue
		}
		out = append(out, &Quote{
			Code:      code,
			Provider:  ProviderTencentCompat,
			Price:     price,
			ChangePct: decimalFromText(parts[5]),
			Volume:    decimal.NewFromInt(int64FromText(parts[6]) * 100),
			Amount:    decimalFromText(parts[7]).Mul(decimal.NewFromInt(10000)),
			QuoteTime: now,
			Raw:       map[string]any{"provider": ProviderTencentCompat, "symbol": compatibilitySymbol(code)},
		})
	}
	if len(out) == 0 {
		return nil, errors.New("tencent realtime quote is empty")
	}
	return out, nil
}

func ParseSinaRealtimeQuotes(response string) ([]*Quote, error) {
	lines := strings.Split(response, "\n")
	out := make([]*Quote, 0, len(lines))
	now := time.Now()
	for _, line := range lines {
		header, data, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		symbol := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(header), "var hq_str_"))
		code := ""
		if len(symbol) >= 6 {
			code = symbol[len(symbol)-6:]
		}
		code, err := EnsureAShareCode(code)
		if err != nil {
			continue
		}
		data = strings.Trim(data, `"; `)
		parts := strings.Split(data, ",")
		if len(parts) < 6 {
			continue
		}
		price := decimalFromText(parts[1])
		if price.IsZero() {
			continue
		}
		out = append(out, &Quote{
			Code:      code,
			Provider:  ProviderSinaCompat,
			Price:     price,
			ChangePct: decimalFromText(parts[3]),
			Volume:    decimal.NewFromInt(int64FromText(parts[4]) * 100),
			Amount:    decimalFromText(parts[5]).Mul(decimal.NewFromInt(10000)),
			QuoteTime: now,
			Raw:       map[string]any{"provider": ProviderSinaCompat, "symbol": compatibilitySymbol(code)},
		})
	}
	if len(out) == 0 {
		return nil, errors.New("sina realtime quote is empty")
	}
	return out, nil
}

func realtimeCompatibilitySymbols(codes []string) []string {
	clean := UniqueAShareCodes(codes)
	out := make([]string, 0, len(clean))
	for _, code := range clean {
		out = append(out, compatibilitySymbol(code))
	}
	return out
}

func compatibilitySymbol(code string) string {
	switch InferExchange(code) {
	case "SH":
		return "s_sh" + code
	case "BJ":
		return "s_bj" + code
	default:
		return "s_sz" + code
	}
}

func decimalFromText(value string) decimal.Decimal {
	parsed, err := decimal.NewFromString(strings.TrimSpace(value))
	if err != nil {
		return decimal.Zero
	}
	return parsed
}

func int64FromText(value string) int64 {
	parsed, _ := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	return parsed
}
