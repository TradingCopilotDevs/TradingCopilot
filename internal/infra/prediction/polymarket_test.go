package prediction

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shopspring/decimal"
)

func TestSearchUsesEventSlugEndpointForPolymarketURL(t *testing.T) {
	publicSearchCalled := false
	eventSlugCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events/slug/us-x-iran-permanent-peace-deal-by":
			eventSlugCalls++
			writeJSON(t, w, `{
				"id":"357807",
				"slug":"us-x-iran-permanent-peace-deal-by",
				"title":"US x Iran permanent peace deal by...?",
				"description":"Resolution rules",
				"active":true,
				"closed":false,
				"markets":[{
					"id":"1919417",
					"conditionId":"0xbbc6689d0f6d57ea42168836712237c7308b3e0118c8914d31b6126d0f3254c5",
					"question":"US x Iran permanent peace deal by April 22, 2026?",
					"slug":"us-x-iran-permanent-peace-deal-by-april-22-2026",
					"outcomes":"[\"Yes\",\"No\"]",
					"outcomePrices":"[\"0.001\",\"0.999\"]",
					"clobTokenIds":"[\"yes-token\",\"no-token\"]",
					"enableOrderBook":true,
					"active":true,
					"closed":true
				}]
			}`)
		case "/public-search":
			publicSearchCalled = true
			http.Error(w, "public search should not be needed for exact event slug", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.gammaBaseURL = server.URL
	result, err := client.Search(context.Background(), "https://polymarket.com/event/us-x-iran-permanent-peace-deal-by#qTlpdC7", 5)
	if err != nil {
		t.Fatal(err)
	}
	if publicSearchCalled {
		t.Fatal("exact event slug search should not fall back to public search")
	}
	if len(result.Events) != 1 || result.Events[0].Slug != "us-x-iran-permanent-peace-deal-by" {
		t.Fatalf("unexpected events: %#v", result.Events)
	}
	if len(result.Markets) != 1 || result.Markets[0].ExternalMarketID != "1919417" {
		t.Fatalf("unexpected markets: %#v", result.Markets)
	}
	assertJSONStringList(t, result.Markets[0].CLOBTokenIDs, []string{"yes-token", "no-token"})

	result, err = client.Search(context.Background(), "polymarket.com/event/us-x-iran-permanent-peace-deal-by#qTlpdC7", 5)
	if err != nil {
		t.Fatal(err)
	}
	if publicSearchCalled {
		t.Fatal("bare exact event slug search should not fall back to public search")
	}
	if eventSlugCalls != 2 {
		t.Fatalf("event slug endpoint calls = %d, want 2", eventSlugCalls)
	}
	if len(result.Events) != 1 || result.Events[0].Slug != "us-x-iran-permanent-peace-deal-by" {
		t.Fatalf("unexpected bare URL events: %#v", result.Events)
	}

	result, err = client.Search(context.Background(), "https://polymarket.com/events/US-X-Iran-Permanent-Peace-Deal-By#qTlpdC7", 5)
	if err != nil {
		t.Fatal(err)
	}
	if publicSearchCalled {
		t.Fatal("plural uppercase event slug search should not fall back to public search")
	}
	if eventSlugCalls != 3 {
		t.Fatalf("event slug endpoint calls = %d, want 3", eventSlugCalls)
	}
	if len(result.Events) != 1 || result.Events[0].Slug != "us-x-iran-permanent-peace-deal-by" {
		t.Fatalf("unexpected plural uppercase URL events: %#v", result.Events)
	}
}

func TestSearchFallsBackToMarketSlugEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/events/slug/fed-cut-june-2026":
			http.NotFound(w, r)
		case "/markets/slug/fed-cut-june-2026":
			writeJSON(t, w, `{
				"id":"mkt-1",
				"conditionId":"cond-1",
				"question":"Will the Fed cut rates by June 2026?",
				"slug":"fed-cut-june-2026",
				"outcomes":"[\"Yes\",\"No\"]",
				"outcomePrices":"[\"0.42\",\"0.58\"]",
				"clobTokenIds":"[\"yes-token\",\"no-token\"]",
				"enableOrderBook":true,
				"active":true,
				"closed":false,
				"events":[{
					"id":"evt-1",
					"slug":"fed-rates-2026",
					"title":"Fed rates in 2026",
					"active":true,
					"closed":false
				}]
			}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.gammaBaseURL = server.URL
	result, err := client.Search(context.Background(), "https://polymarket.com/market/fed-cut-june-2026?tid=abc", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Markets) != 1 || result.Markets[0].Slug != "fed-cut-june-2026" {
		t.Fatalf("unexpected markets: %#v", result.Markets)
	}
	if len(result.Events) != 1 || result.Events[0].Slug != "fed-rates-2026" {
		t.Fatalf("market slug response should preserve embedded parent event, got %#v", result.Events)
	}
	var raw map[string]any
	if err := json.Unmarshal(result.Markets[0].Raw, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["event_id"] != "evt-1" {
		t.Fatalf("market raw should include embedded event id for persistence, got %#v", raw)
	}
}

func TestSearchRequestsFullPublicSearchPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/public-search" {
			http.NotFound(w, r)
			return
		}
		if got := r.URL.Query().Get("optimized"); got != "false" {
			t.Fatalf("optimized query = %q, want false", got)
		}
		writeJSON(t, w, `{"events":[{
			"id":"evt-1",
			"slug":"fed-rates-2026",
			"title":"Fed rates in 2026",
			"active":true,
			"closed":false,
			"markets":[{
				"id":"mkt-1",
				"conditionId":"cond-1",
				"question":"Will the Fed cut rates by June 2026?",
				"slug":"fed-cut-june-2026",
				"outcomes":"[\"Yes\",\"No\"]",
				"outcomePrices":"[\"0.42\",\"0.58\"]",
				"clobTokenIds":"[\"yes-token\",\"no-token\"]",
				"enableOrderBook":true,
				"active":true,
				"closed":false
			}]
		}]}`)
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.gammaBaseURL = server.URL
	result, err := client.Search(context.Background(), "US x Iran permanent peace deal", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Markets) != 1 || result.Markets[0].ExternalMarketID != "mkt-1" {
		t.Fatalf("public search should parse full market rows, got %#v", result.Markets)
	}
}

func TestParseEventsAndMarketsFromGammaPayload(t *testing.T) {
	var payload []any
	if err := json.Unmarshal([]byte(`[
		{
			"id": "evt-1",
			"slug": "fed-rates-2026",
			"title": "Fed rates in 2026",
			"description": "Resolution source and rules",
			"category": "Economics",
			"tags": [{"label": "macro"}, "rates"],
			"active": true,
			"closed": false,
			"endDate": "2026-06-30T00:00:00Z",
			"volume": "123.45",
			"liquidity": 67.89,
			"openInterest": "10",
			"markets": [
				{
					"id": "mkt-1",
					"conditionId": "cond-1",
					"question": "Will the Fed cut rates by June 2026?",
					"slug": "fed-cut-june-2026",
					"outcomes": "[\"Yes\",\"No\"]",
					"outcomePrices": "[\"0.42\",\"0.58\"]",
					"clobTokenIds": "[\"yes-token\",\"no-token\"]",
					"enableOrderBook": true,
					"bestBid": "0.41",
					"bestAsk": "0.43",
					"lastTradePrice": "0.42",
					"spread": "0.02",
					"active": true,
					"closed": false,
					"restricted": false,
					"endDate": "2026-06-30"
				}
			]
		}
	]`), &payload); err != nil {
		t.Fatal(err)
	}

	events := parseEvents(payload)
	if len(events) != 1 {
		t.Fatalf("expected one event, got %d", len(events))
	}
	event := events[0]
	if event.ExternalEventID != "evt-1" || event.Provider != "polymarket" || !event.Active || event.Closed {
		t.Fatalf("unexpected event mapping: %#v", event)
	}
	if !event.Volume.Equal(decimal.RequireFromString("123.45")) {
		t.Fatalf("unexpected volume: %s", event.Volume)
	}
	if event.EndDate == nil {
		t.Fatal("expected event end date")
	}

	markets := parseMarketsFromRaw(event.Raw)
	if len(markets) != 1 {
		t.Fatalf("expected one market, got %d", len(markets))
	}
	market := markets[0]
	if market.ExternalMarketID != "mkt-1" || market.ConditionID != "cond-1" || market.Question == "" {
		t.Fatalf("unexpected market mapping: %#v", market)
	}
	assertJSONStringList(t, market.Outcomes, []string{"Yes", "No"})
	assertJSONStringList(t, market.OutcomePrices, []string{"0.42", "0.58"})
	assertJSONStringList(t, market.CLOBTokenIDs, []string{"yes-token", "no-token"})
	if !market.EnableOrderBook || !market.Active || market.Closed || market.Restricted {
		t.Fatalf("unexpected market flags: %#v", market)
	}
	if !market.BestBid.Equal(decimal.RequireFromString("0.41")) || !market.BestAsk.Equal(decimal.RequireFromString("0.43")) {
		t.Fatalf("unexpected book prices: bid=%s ask=%s", market.BestBid, market.BestAsk)
	}
}

func TestParseMarketsSkipsRowsWithoutTradableIdentity(t *testing.T) {
	raw := jsonValue(map[string]any{
		"id": "evt-2",
		"markets": []any{
			map[string]any{"question": "Missing id and condition"},
			map[string]any{"id": "mkt-2"},
		},
	})

	markets := parseMarketsFromRaw(raw)
	if len(markets) != 0 {
		t.Fatalf("expected invalid markets to be skipped, got %#v", markets)
	}
}

func TestMarketWebSocketSnapshotSubscribesAndNormalizesEvents(t *testing.T) {
	subscriptionCh := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		_, message, err := conn.Read(r.Context())
		if err != nil {
			t.Errorf("read subscription: %v", err)
			return
		}
		var subscription map[string]any
		if err := json.Unmarshal(message, &subscription); err != nil {
			t.Errorf("decode subscription: %v", err)
			return
		}
		subscriptionCh <- subscription
		payload := `[{"event_type":"best_bid_ask","asset_id":"yes-token","best_bid":"0.41","best_ask":"0.43"}]`
		if err := conn.Write(r.Context(), websocket.MessageText, []byte(payload)); err != nil {
			t.Errorf("write event: %v", err)
		}
	}))
	defer server.Close()

	client := NewClient(server.Client())
	client.marketWSURL = "ws" + strings.TrimPrefix(server.URL, "http")
	rows, err := client.MarketWebSocketSnapshot(context.Background(), []string{"yes-token", "yes-token"}, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected one websocket event, got %d", len(rows))
	}
	if rows[0]["event_type"] != "best_bid_ask" || rows[0]["asset_id"] != "yes-token" {
		t.Fatalf("unexpected event row: %#v", rows[0])
	}
	if rows[0]["received_at"] == nil {
		t.Fatalf("expected received_at on event row: %#v", rows[0])
	}

	subscription := <-subscriptionCh
	if subscription["type"] != "market" || subscription["custom_feature_enabled"] != true {
		t.Fatalf("unexpected subscription: %#v", subscription)
	}
	assets, ok := subscription["assets_ids"].([]any)
	if !ok || len(assets) != 1 || assets[0] != "yes-token" {
		t.Fatalf("expected deduplicated assets_ids, got %#v", subscription["assets_ids"])
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if _, err := io.WriteString(w, payload); err != nil {
		t.Fatalf("write response: %v", err)
	}
}

func assertJSONStringList(t *testing.T, raw []byte, want []string) {
	t.Helper()
	var got []string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal %q: %v", string(raw), err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
