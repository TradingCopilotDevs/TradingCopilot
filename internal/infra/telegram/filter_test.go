package telegram

import (
	"strings"
	"testing"
)

func TestNewsFilterJSONSchemaKeepsPredictionMarketFields(t *testing.T) {
	for _, required := range []string{"related_prediction_markets", "match_confidence", "match_reason"} {
		if !strings.Contains(newsFilterJSONSchema, required) {
			t.Fatalf("news filter repair schema should preserve %s: %s", required, newsFilterJSONSchema)
		}
	}
}
