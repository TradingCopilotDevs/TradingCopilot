package marketdata

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

var TencentBaseURL = "https://web.ifzq.gtimg.cn"

func FetchTencentFiveDaySeriesFrom(baseURL string, client *http.Client, code string) ([]map[string]any, error) {
	code, err := EnsureAShareCode(code)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = TencentBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	symbol := TencentCompatibilitySymbol(code)
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(baseURL, "/")+"/appstock/app/day/query?code="+symbol, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://stockapp.finance.qq.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tencent five_day status=%d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return ParseTencentFiveDayPayload(payload, symbol)
}

func FetchTencentIntradaySeriesFrom(baseURL string, client *http.Client, code string) ([]map[string]any, error) {
	code, err := EnsureAShareCode(code)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = TencentBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	symbol := TencentCompatibilitySymbol(code)
	endpoint := strings.TrimRight(baseURL, "/") + "/appstock/app/minute/query?code=" + url.QueryEscape(symbol)
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://stockapp.finance.qq.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tencent intraday status=%d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return ParseTencentIntradayPayload(payload, symbol)
}

func FetchTencentKLineSeriesFrom(baseURL string, client *http.Client, code string, rangeKey string) ([]map[string]any, error) {
	code, err := EnsureAShareCode(code)
	if err != nil {
		return nil, err
	}
	period := TencentKLinePeriod(rangeKey)
	if period == "" {
		return nil, errors.New("unsupported range")
	}
	if strings.TrimSpace(baseURL) == "" {
		baseURL = TencentBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	symbol := TencentCompatibilitySymbol(code)
	query := url.Values{}
	query.Set("param", fmt.Sprintf("%s,%s,,,%d,qfq", symbol, period, TencentKLineLimit(period)))
	req, err := http.NewRequest(http.MethodGet, strings.TrimRight(baseURL, "/")+"/appstock/app/fqkline/get?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://stockapp.finance.qq.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("tencent kline status=%d", resp.StatusCode)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return ParseTencentKLinePayload(payload, symbol, period)
}

func ParseTencentIntradayPayload(payload map[string]any, symbol string) ([]map[string]any, error) {
	data, _ := nestedMap(payload, "data", symbol, "data")
	tradeDate := strings.TrimSpace(fmt.Sprint(data["date"]))
	rawRows, _ := data["data"].([]any)
	if tradeDate == "" || len(rawRows) == 0 {
		return nil, errors.New("unexpected tencent intraday payload")
	}
	out := []map[string]any{}
	for _, row := range rawRows {
		if parsed := parseTencentSeriesRow(tradeDate, fmt.Sprint(row)); parsed != nil {
			out = append(out, parsed)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("tencent intraday payload is empty")
	}
	return out, nil
}

func ParseTencentKLinePayload(payload map[string]any, symbol string, period string) ([]map[string]any, error) {
	data, _ := nestedMap(payload, "data", symbol)
	rawRows, _ := data["qfq"+period].([]any)
	if len(rawRows) == 0 {
		rawRows, _ = data[period].([]any)
	}
	if len(rawRows) == 0 {
		return nil, errors.New("unexpected tencent kline payload")
	}
	out := []map[string]any{}
	for _, rawRow := range rawRows {
		if parsed := parseTencentKLineRow(rawRow); parsed != nil {
			out = append(out, parsed)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("tencent kline payload is empty")
	}
	return out, nil
}

func ParseTencentFiveDayPayload(payload map[string]any, symbol string) ([]map[string]any, error) {
	data, _ := nestedMap(payload, "data", symbol)
	rawDays, _ := data["data"].([]any)
	if len(rawDays) == 0 {
		return nil, errors.New("unexpected tencent five_day payload")
	}
	out := []map[string]any{}
	for _, rawDay := range rawDays {
		day, _ := rawDay.(map[string]any)
		if day == nil {
			continue
		}
		tradeDate := strings.TrimSpace(fmt.Sprint(day["date"]))
		rawRows, _ := day["data"].([]any)
		if tradeDate == "" || len(rawRows) == 0 {
			continue
		}
		for _, row := range rawRows {
			if parsed := parseTencentSeriesRow(tradeDate, fmt.Sprint(row)); parsed != nil {
				out = append(out, parsed)
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("tencent five_day payload is empty")
	}
	return out, nil
}

func NormalizeSeriesRows(rows []map[string]any) []map[string]any {
	normalized := map[string]map[string]any{}
	for _, row := range rows {
		if row["time"] == nil {
			continue
		}
		timeValue := strings.TrimSpace(fmt.Sprint(row["time"]))
		if timeValue == "" || timeValue == "<nil>" {
			continue
		}
		row["time"] = timeValue
		normalized[timeValue] = row
	}
	keys := make([]string, 0, len(normalized))
	for key := range normalized {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]map[string]any, 0, len(keys))
	for _, key := range keys {
		out = append(out, normalized[key])
	}
	return out
}

func TencentCompatibilitySymbol(code string) string {
	switch InferExchange(code) {
	case "SH":
		return "sh" + code
	case "BJ":
		return "bj" + code
	default:
		return "sz" + code
	}
}

func RangeToPeriod(rangeKey string) string {
	switch strings.ToLower(strings.TrimSpace(rangeKey)) {
	case "", "daily":
		return "daily"
	case "weekly":
		return "weekly"
	case "monthly":
		return "monthly"
	case "intraday", "1d":
		return "intraday"
	case "five_day":
		return "five_day"
	default:
		return strings.ToLower(strings.TrimSpace(rangeKey))
	}
}

func TencentKLinePeriod(rangeKey string) string {
	switch RangeToPeriod(rangeKey) {
	case "", "daily":
		return "day"
	case "weekly":
		return "week"
	case "monthly":
		return "month"
	default:
		return ""
	}
}

func TencentKLineLimit(period string) int {
	switch period {
	case "month":
		return 240
	case "week":
		return 360
	default:
		return 500
	}
}

func parseTencentKLineRow(raw any) map[string]any {
	parts := []string{}
	switch row := raw.(type) {
	case []any:
		for _, value := range row {
			parts = append(parts, fmt.Sprint(value))
		}
	case []string:
		parts = append(parts, row...)
	case string:
		parts = strings.Fields(row)
	}
	if len(parts) < 6 {
		return nil
	}
	tradeDate := strings.TrimSpace(parts[0])
	if _, err := time.Parse("2006-01-02", tradeDate); err != nil {
		return nil
	}
	return map[string]any{
		"time":   tradeDate,
		"open":   parts[1],
		"close":  parts[2],
		"high":   parts[3],
		"low":    parts[4],
		"volume": parts[5],
		"amount": valueAt(parts, 6),
	}
}

func parseTencentSeriesRow(tradeDate string, row string) map[string]any {
	parts := strings.Fields(strings.TrimSpace(row))
	if len(parts) < 4 {
		return nil
	}
	minute := parts[0]
	if len(minute) < 4 {
		minute = strings.Repeat("0", 4-len(minute)) + minute
	}
	if len(minute) != 4 {
		return nil
	}
	for _, ch := range minute {
		if ch < '0' || ch > '9' {
			return nil
		}
	}
	parsedDate, err := time.Parse("20060102", tradeDate)
	if err != nil {
		return nil
	}
	price := parts[1]
	return map[string]any{
		"time":   parsedDate.Format("2006-01-02") + " " + minute[:2] + ":" + minute[2:],
		"open":   price,
		"high":   price,
		"low":    price,
		"close":  price,
		"volume": parts[2],
		"amount": parts[3],
	}
}

func nestedMap(root map[string]any, keys ...string) (map[string]any, bool) {
	current := root
	for _, key := range keys {
		next, _ := current[key].(map[string]any)
		if next == nil {
			return nil, false
		}
		current = next
	}
	return current, true
}

func valueAt(values []string, index int) string {
	if index >= 0 && index < len(values) {
		return values[index]
	}
	return ""
}
