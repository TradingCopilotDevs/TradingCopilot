package marketdata

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type SymbolItem struct {
	Code     string
	Name     string
	Exchange string
}

type DailyBarItem struct {
	Code      string
	TradeDate time.Time
	Open      decimal.Decimal
	High      decimal.Decimal
	Low       decimal.Decimal
	Close     decimal.Decimal
	Volume    decimal.Decimal
	Amount    decimal.Decimal
	Provider  string
}

type HistoryProvider interface {
	FetchSymbols() ([]SymbolItem, error)
	FetchDaily(code string, start *time.Time, end *time.Time) ([]DailyBarItem, error)
	FetchSeries(code string, rangeKey string) ([]map[string]any, error)
	Name() string
}

type HTTPHistoryProvider struct {
	ProviderName string
	Token        string
	BaseURL      string
	HTTPClient   *http.Client
}

type TushareAPIError struct {
	Code int
	Msg  string
}

func (e *TushareAPIError) Error() string {
	return fmt.Sprintf("tushare error code=%d msg=%s", e.Code, e.Msg)
}

func (p *HTTPHistoryProvider) Name() string {
	name := strings.ToLower(strings.TrimSpace(p.ProviderName))
	if name == "" {
		return ProviderTushare
	}
	return name
}

func (p *HTTPHistoryProvider) client() *http.Client {
	if p.HTTPClient != nil {
		return p.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (p *HTTPHistoryProvider) FetchSymbols() ([]SymbolItem, error) {
	if p.Name() != ProviderTushare {
		return nil, UnsupportedProviderError(p.Name())
	}
	rows, err := p.tushareCall("stock_basic", map[string]any{"exchange": "", "list_status": "L"}, "ts_code,symbol,name")
	if err != nil {
		return nil, err
	}
	out := make([]SymbolItem, 0, len(rows))
	for _, row := range rows {
		code := strings.TrimSpace(fmt.Sprint(row["symbol"]))
		if _, err := EnsureAShareCode(code); err != nil {
			continue
		}
		out = append(out, SymbolItem{Code: code, Name: strings.TrimSpace(fmt.Sprint(row["name"])), Exchange: TushareExchange(fmt.Sprint(row["ts_code"]))})
	}
	return out, nil
}

func (p *HTTPHistoryProvider) FetchDaily(code string, start *time.Time, end *time.Time) ([]DailyBarItem, error) {
	code, err := EnsureAShareCode(code)
	if err != nil {
		return nil, err
	}
	if p.Name() != ProviderTushare {
		return nil, UnsupportedProviderError(p.Name())
	}
	params := map[string]any{"ts_code": TushareSymbol(code), "start_date": DateParam(start, "19900101"), "end_date": DateParam(end, time.Now().Format("20060102"))}
	rows, err := p.tushareCall("daily", params, "ts_code,trade_date,open,high,low,close,vol,amount")
	if err != nil {
		return nil, err
	}
	out := make([]DailyBarItem, 0, len(rows))
	for _, row := range rows {
		tradeDate, err := time.ParseInLocation("20060102", fmt.Sprint(row["trade_date"]), time.Local)
		if err != nil {
			continue
		}
		out = append(out, DailyBarItem{Code: code, TradeDate: tradeDate, Open: DecimalFromAny(row["open"]), High: DecimalFromAny(row["high"]), Low: DecimalFromAny(row["low"]), Close: DecimalFromAny(row["close"]), Volume: DecimalFromAny(row["vol"]), Amount: DecimalFromAny(row["amount"]), Provider: ProviderTushare})
	}
	return out, nil
}

func (p *HTTPHistoryProvider) FetchSeries(code string, rangeKey string) ([]map[string]any, error) {
	if p.Name() != ProviderTushare {
		return nil, UnsupportedProviderError(p.Name())
	}
	return nil, errors.New("tushare is configured for historical daily bars only")
}

func (p *HTTPHistoryProvider) tushareCall(apiName string, params map[string]any, fields string) ([]map[string]any, error) {
	if strings.TrimSpace(p.Token) == "" {
		return nil, errors.New("market data secret 'tushare_token' is not configured")
	}
	baseURL := strings.TrimSpace(p.BaseURL)
	if baseURL == "" {
		baseURL = TushareAPIBaseURL
	}
	body, _ := json.Marshal(map[string]any{"api_name": apiName, "token": p.Token, "params": params, "fields": fields})
	resp, err := p.client().Post(strings.TrimRight(baseURL, "/"), "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tushare status=%d", resp.StatusCode)
	}
	return ParseTushareRows(raw)
}

func IsTusharePermissionError(err error) bool {
	var apiErr *TushareAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	msg := strings.ToLower(apiErr.Msg)
	return strings.Contains(msg, "permission") || strings.Contains(msg, "privilege") || strings.Contains(msg, "unauthorized")
}

func IsTushareAccessBoundaryError(err error) bool {
	if IsTusharePermissionError(err) {
		return true
	}
	var apiErr *TushareAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	msg := strings.ToLower(apiErr.Msg)
	return strings.Contains(msg, "rate") || strings.Contains(msg, "quota") || strings.Contains(msg, "limit")
}

func ParseTushareRows(raw []byte) ([]map[string]any, error) {
	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Fields []string `json:"fields"`
			Items  [][]any  `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, err
	}
	if payload.Code != 0 {
		return nil, &TushareAPIError{Code: payload.Code, Msg: payload.Msg}
	}
	out := make([]map[string]any, 0, len(payload.Data.Items))
	for _, item := range payload.Data.Items {
		row := map[string]any{}
		for i, field := range payload.Data.Fields {
			if i < len(item) {
				row[field] = item[i]
			}
		}
		out = append(out, row)
	}
	return out, nil
}

func TushareSymbol(code string) string {
	exchange := InferExchange(code)
	if exchange == "" {
		exchange = "SZ"
	}
	return code + "." + exchange
}

func TushareExchange(tsCode string) string {
	parts := strings.Split(tsCode, ".")
	if len(parts) < 2 {
		return ""
	}
	switch strings.ToUpper(parts[len(parts)-1]) {
	case "SH", "SZ", "BJ":
		return strings.ToUpper(parts[len(parts)-1])
	default:
		return ""
	}
}

func DateParam(value *time.Time, fallback string) string {
	if value == nil {
		return fallback
	}
	return value.Format("20060102")
}

func DecimalFromAny(v any) decimal.Decimal {
	switch t := v.(type) {
	case float64:
		return decimal.NewFromFloat(t)
	case string:
		d, _ := decimal.NewFromString(t)
		return d
	default:
		d, _ := decimal.NewFromString(fmt.Sprint(v))
		return d
	}
}

var TushareAPIBaseURL = "https://api.tushare.pro"
