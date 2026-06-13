package ai

import (
	"strings"
	"testing"
)

func TestPredictionToolDefinitionsExplainOutcomeTokenMapping(t *testing.T) {
	required := map[string][]string{
		"prediction.search_markets":   {"event slug/title", "local event"},
		"prediction.market_snapshot":  {"event slug/title", "outcome-token mapping"},
		"prediction.orderbook":        {"event slug/title", "selected outcome label", "token mapping"},
		"prediction.price_history":    {"event slug/title", "selected outcome label", "token mapping"},
		"prediction.watchlist":        {"current prediction research team", "local market ID", "event slug/title"},
		"prediction.upsert_watchlist": {"local market ID", "moderator recap"},
	}
	defs := map[string]CapabilityDefinition{}
	for _, def := range ToolDefinitions {
		defs[def.Key] = def
	}
	for key, fragments := range required {
		def, ok := defs[key]
		if !ok {
			t.Fatalf("missing prediction tool definition %s", key)
		}
		for _, fragment := range fragments {
			if !strings.Contains(def.Description, fragment) {
				t.Fatalf("%s description %q does not explain %q", key, def.Description, fragment)
			}
		}
	}
}
