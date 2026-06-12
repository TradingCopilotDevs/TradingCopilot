package prediction

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	appprediction "github.com/TradingCopilotDevs/TradingCopilot/internal/app/prediction"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainprediction "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/prediction"
	"github.com/coder/websocket"
	"github.com/shopspring/decimal"
)

const (
	DefaultGammaBaseURL = "https://gamma-api.polymarket.com"
	DefaultCLOBBaseURL  = "https://clob.polymarket.com"
	DefaultMarketWSURL  = "wss://ws-subscriptions-clob.polymarket.com/ws/market"
)

type Client struct {
	httpClient   *http.Client
	gammaBaseURL string
	clobBaseURL  string
	marketWSURL  string
}

func NewClient(httpClient *http.Client) Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return Client{httpClient: httpClient, gammaBaseURL: DefaultGammaBaseURL, clobBaseURL: DefaultCLOBBaseURL, marketWSURL: DefaultMarketWSURL}
}

func (c Client) Search(ctx context.Context, q string, limit int) (appprediction.SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}
	endpoint := strings.TrimRight(c.gammaBaseURL, "/") + "/public-search"
	values := url.Values{}
	values.Set("q", strings.TrimSpace(q))
	values.Set("limit_per_type", strconv.Itoa(limit))
	values.Set("events_status", "active")
	values.Set("search_profiles", "false")
	values.Set("optimized", "true")
	return c.getSearch(ctx, endpoint+"?"+values.Encode())
}

func (c Client) SyncActive(ctx context.Context, limit int) (appprediction.SearchResult, error) {
	if limit <= 0 {
		limit = 100
	}
	endpoint := strings.TrimRight(c.gammaBaseURL, "/") + "/events"
	values := url.Values{}
	values.Set("active", "true")
	values.Set("closed", "false")
	values.Set("order", "volume_24hr")
	values.Set("ascending", "false")
	values.Set("limit", strconv.Itoa(limit))
	return c.getEvents(ctx, endpoint+"?"+values.Encode())
}

func (c Client) OrderBook(ctx context.Context, tokenID string) (map[string]any, error) {
	endpoint := strings.TrimRight(c.clobBaseURL, "/") + "/book"
	values := url.Values{}
	values.Set("token_id", strings.TrimSpace(tokenID))
	var out map[string]any
	if err := c.getJSON(ctx, endpoint+"?"+values.Encode(), &out); err != nil {
		return nil, err
	}
	out["token_id"] = tokenID
	return out, nil
}

func (c Client) PriceHistory(ctx context.Context, tokenID string) ([]map[string]any, error) {
	endpoint := strings.TrimRight(c.clobBaseURL, "/") + "/prices-history"
	values := url.Values{}
	values.Set("market", strings.TrimSpace(tokenID))
	values.Set("interval", "1d")
	var payload map[string]any
	if err := c.getJSON(ctx, endpoint+"?"+values.Encode(), &payload); err != nil {
		return nil, err
	}
	rows := anyList(payload["history"])
	out := make([]map[string]any, 0, len(rows))
	for _, item := range rows {
		if row, ok := item.(map[string]any); ok {
			out = append(out, row)
		}
	}
	return out, nil
}

func (c Client) MarketWebSocketSnapshot(ctx context.Context, tokenIDs []string, sampleFor time.Duration) ([]map[string]any, error) {
	tokenIDs = uniqueStrings(tokenIDs, 40)
	if len(tokenIDs) == 0 {
		return nil, fmt.Errorf("market websocket requires at least one clob token id")
	}
	if sampleFor <= 0 {
		sampleFor = 3 * time.Second
	}
	if sampleFor > 10*time.Second {
		sampleFor = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, sampleFor)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, c.marketWSURL, &websocket.DialOptions{HTTPClient: c.httpClient})
	if err != nil {
		return nil, err
	}
	defer conn.Close(websocket.StatusNormalClosure, "prediction market snapshot complete")

	subscription := map[string]any{
		"assets_ids":             tokenIDs,
		"type":                   "market",
		"custom_feature_enabled": true,
	}
	payload, _ := json.Marshal(subscription)
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	for {
		typ, message, err := conn.Read(ctx)
		if err != nil {
			if len(rows) > 0 {
				return rows, nil
			}
			return nil, err
		}
		if typ != websocket.MessageText {
			continue
		}
		rows = append(rows, normalizeWebSocketMessage(message)...)
		if len(rows) >= len(tokenIDs) {
			return rows, nil
		}
	}
}

func (c Client) getSearch(ctx context.Context, endpoint string) (appprediction.SearchResult, error) {
	var payload map[string]any
	if err := c.getJSON(ctx, endpoint, &payload); err != nil {
		return appprediction.SearchResult{}, err
	}
	events := parseEvents(anyList(payload["events"]))
	markets := []domainprediction.Market{}
	for _, event := range events {
		markets = append(markets, parseMarketsFromRaw(event.Raw)...)
	}
	return appprediction.SearchResult{Events: events, Markets: markets}, nil
}

func (c Client) getEvents(ctx context.Context, endpoint string) (appprediction.SearchResult, error) {
	var payload any
	if err := c.getJSON(ctx, endpoint, &payload); err != nil {
		return appprediction.SearchResult{}, err
	}
	events := parseEvents(anyList(payload))
	markets := []domainprediction.Market{}
	for _, event := range events {
		markets = append(markets, parseMarketsFromRaw(event.Raw)...)
	}
	return appprediction.SearchResult{Events: events, Markets: markets}, nil
}

func (c Client) getJSON(ctx context.Context, endpoint string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("polymarket returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	decoder := json.NewDecoder(resp.Body)
	decoder.UseNumber()
	return decoder.Decode(out)
}

func parseEvents(rows []any) []domainprediction.Event {
	out := make([]domainprediction.Event, 0, len(rows))
	for _, item := range rows {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		event := domainprediction.Event{
			Provider:        domainprediction.ProviderPolymarket,
			ExternalEventID: firstString(row, "id", "eventId"),
			Slug:            firstString(row, "slug"),
			Title:           firstString(row, "title", "question"),
			Description:     firstString(row, "description"),
			Category:        stringPtr(firstString(row, "category")),
			Tags:            jsonValue(tagsFromRaw(row["tags"])),
			Active:          boolValue(row["active"]),
			Closed:          boolValue(row["closed"]),
			EndDate:         timePtr(firstString(row, "endDate", "endDateIso")),
			Volume:          decimalValue(firstPresent(row, "volume", "volumeNum")),
			Liquidity:       decimalValue(firstPresent(row, "liquidity", "liquidityNum")),
			OpenInterest:    decimalValue(firstPresent(row, "openInterest")),
			Raw:             jsonValue(row),
		}
		if event.ExternalEventID == "" {
			event.ExternalEventID = event.Slug
		}
		if event.ExternalEventID == "" || event.Title == "" {
			continue
		}
		out = append(out, event)
	}
	return out
}

func parseMarketsFromRaw(raw domainkernel.JSON) []domainprediction.Market {
	var event map[string]any
	if json.Unmarshal(raw, &event) != nil {
		return nil
	}
	eventID := firstString(event, "id", "eventId")
	rows := anyList(event["markets"])
	out := make([]domainprediction.Market, 0, len(rows))
	for _, item := range rows {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		market := domainprediction.Market{
			Provider:         domainprediction.ProviderPolymarket,
			ExternalMarketID: firstString(row, "id", "marketId"),
			ConditionID:      firstString(row, "conditionId", "condition_id"),
			Question:         firstString(row, "question", "title"),
			Slug:             firstString(row, "slug"),
			Description:      firstString(row, "description"),
			Outcomes:         jsonValue(parseJSONStringList(row["outcomes"])),
			OutcomePrices:    jsonValue(parseJSONStringList(row["outcomePrices"])),
			CLOBTokenIDs:     jsonValue(parseJSONStringList(row["clobTokenIds"])),
			EnableOrderBook:  boolValue(row["enableOrderBook"]),
			BestBid:          decimalValue(firstPresent(row, "bestBid", "best_bid")),
			BestAsk:          decimalValue(firstPresent(row, "bestAsk", "best_ask")),
			LastTradePrice:   decimalValue(firstPresent(row, "lastTradePrice", "last_trade_price")),
			Spread:           decimalValue(firstPresent(row, "spread")),
			Volume:           decimalValue(firstPresent(row, "volume", "volumeNum")),
			Liquidity:        decimalValue(firstPresent(row, "liquidity", "liquidityNum")),
			Active:           boolValue(row["active"]),
			Closed:           boolValue(row["closed"]),
			Restricted:       boolValue(row["restricted"]),
			EndDate:          timePtr(firstString(row, "endDate", "endDateIso")),
			Raw:              jsonValue(withRawEventID(row, eventID)),
		}
		if market.ExternalMarketID == "" {
			market.ExternalMarketID = market.ConditionID
		}
		if market.ExternalMarketID == "" || market.Question == "" {
			continue
		}
		out = append(out, market)
	}
	return out
}

func withRawEventID(row map[string]any, eventID string) map[string]any {
	out := map[string]any{}
	for key, value := range row {
		out[key] = value
	}
	if eventID != "" {
		out["event_id"] = eventID
	}
	return out
}

func anyList(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	default:
		return nil
	}
}

func firstString(row map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			text := strings.TrimSpace(fmt.Sprint(value))
			if text != "" && text != "<nil>" {
				return text
			}
		}
	}
	return ""
}

func firstPresent(row map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := row[key]; ok {
			return value
		}
	}
	return nil
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
		return parsed
	default:
		return fmt.Sprint(value) == "true"
	}
}

func decimalValue(value any) decimal.Decimal {
	switch typed := value.(type) {
	case decimal.Decimal:
		return typed
	case json.Number:
		out, _ := decimal.NewFromString(typed.String())
		return out
	case float64:
		return decimal.NewFromFloat(typed)
	case string:
		out, _ := decimal.NewFromString(strings.TrimSpace(typed))
		return out
	default:
		out, _ := decimal.NewFromString(strings.TrimSpace(fmt.Sprint(value)))
		return out
	}
}

func timePtr(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return &parsed
		}
	}
	return nil
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func tagsFromRaw(value any) []string {
	rows := anyList(value)
	out := []string{}
	for _, item := range rows {
		switch typed := item.(type) {
		case map[string]any:
			if label := firstString(typed, "label", "slug", "id"); label != "" {
				out = append(out, label)
			}
		default:
			if label := strings.TrimSpace(fmt.Sprint(typed)); label != "" && label != "<nil>" {
				out = append(out, label)
			}
		}
	}
	return out
}

func parseJSONStringList(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out = append(out, text)
			}
		}
		return out
	case string:
		var values []string
		if json.Unmarshal([]byte(typed), &values) == nil {
			return values
		}
		return nil
	default:
		return nil
	}
}

func jsonValue(value any) domainkernel.JSON {
	b, _ := json.Marshal(value)
	return domainkernel.JSON(b)
}

func normalizeWebSocketMessage(message []byte) []map[string]any {
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(message)))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return []map[string]any{{"event_type": "raw", "payload": string(message), "received_at": time.Now().UTC()}}
	}
	now := time.Now().UTC()
	switch typed := value.(type) {
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if row, ok := item.(map[string]any); ok {
				row["received_at"] = now
				out = append(out, row)
			}
		}
		return out
	case map[string]any:
		typed["received_at"] = now
		return []map[string]any{typed}
	default:
		return []map[string]any{{"event_type": "raw", "payload": typed, "received_at": now}}
	}
}

func uniqueStrings(values []string, limit int) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}
