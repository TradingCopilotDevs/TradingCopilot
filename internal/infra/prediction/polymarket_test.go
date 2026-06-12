package prediction

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shopspring/decimal"
)

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
