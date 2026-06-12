package meeting

import (
	"context"
	"fmt"
	"strings"
	"time"

	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	infraprediction "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/prediction"
	"gorm.io/gorm"
)

func appendPredictionRealtimeSnapshotEvent(ctx context.Context, db *gorm.DB, meetingID uint) {
	marketIDs := MeetingPredictionMarketIDs(db, meetingID)
	if len(marketIDs) == 0 {
		return
	}
	markets := predictionMarketsForRealtime(db, marketIDs)
	tokenIDs, tokenMarkets := predictionRealtimeTokenIDs(markets)
	payload := map[string]any{
		"status":     "prediction_realtime_snapshot",
		"market_ids": marketIDs,
		"token_ids":  tokenIDs,
	}
	if len(tokenIDs) == 0 {
		payload["source"] = "skipped"
		payload["error"] = "linked prediction markets do not have clob token ids"
		_, _ = AppendEvent(db, meetingID, domainkernel.EventSystem, nil, "Prediction market realtime snapshot skipped.", payload)
		return
	}

	settings := config.Load()
	provider := infraprediction.NewClient(runtimeproxy.NewHTTPClient(db, settings, runtimeproxy.ModuleMarket, 8*time.Second))
	sampleCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	rows, err := provider.MarketWebSocketSnapshot(sampleCtx, tokenIDs, 4*time.Second)
	if err == nil && len(rows) > 0 {
		payload["source"] = "websocket"
		payload["rows"] = enrichPredictionRealtimeRows(rows, tokenMarkets)
		_, _ = AppendEvent(db, meetingID, domainkernel.EventToolResult, nil, "Prediction market realtime snapshot received from WebSocket.", payload)
		return
	}

	payload["source"] = "rest_fallback"
	if err != nil {
		payload["websocket_error"] = err.Error()
	} else {
		payload["websocket_error"] = "websocket returned no market events before timeout"
	}
	payload["rows"] = predictionRealtimeRESTFallback(ctx, provider, tokenIDs, tokenMarkets)
	_, _ = AppendEvent(db, meetingID, domainkernel.EventToolResult, nil, "Prediction market realtime snapshot received from CLOB REST fallback.", payload)
}

func predictionMarketsForRealtime(db *gorm.DB, marketIDs []uint) []persistmodel.PredictionMarket {
	var rows []persistmodel.PredictionMarket
	db.Where("id IN ?", marketIDs).Order("id").Find(&rows)
	return rows
}

func predictionRealtimeTokenIDs(markets []persistmodel.PredictionMarket) ([]string, map[string]map[string]any) {
	tokenMarkets := map[string]map[string]any{}
	tokenIDs := []string{}
	for _, market := range markets {
		outcomes := stringListFromJSON(market.Outcomes)
		for index, tokenID := range stringListFromJSON(market.CLOBTokenIDs) {
			tokenID = strings.TrimSpace(tokenID)
			if tokenID == "" {
				continue
			}
			outcome := ""
			if index < len(outcomes) {
				outcome = outcomes[index]
			}
			if _, ok := tokenMarkets[tokenID]; !ok {
				tokenIDs = append(tokenIDs, tokenID)
			}
			tokenMarkets[tokenID] = map[string]any{
				"market_id":          market.ID,
				"external_market_id": market.ExternalMarketID,
				"condition_id":       market.ConditionID,
				"question":           market.Question,
				"slug":               market.Slug,
				"outcome":            outcome,
			}
		}
	}
	return tokenIDs, tokenMarkets
}

func enrichPredictionRealtimeRows(rows []map[string]any, tokenMarkets map[string]map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := map[string]any{}
		for key, value := range row {
			item[key] = value
		}
		tokenID := firstPredictionTokenID(item)
		if market, ok := tokenMarkets[tokenID]; ok {
			for key, value := range market {
				item[key] = value
			}
		}
		out = append(out, item)
	}
	return out
}

func predictionRealtimeRESTFallback(ctx context.Context, provider infraprediction.Client, tokenIDs []string, tokenMarkets map[string]map[string]any) []map[string]any {
	rows := []map[string]any{}
	for _, tokenID := range tokenIDs {
		if len(rows) >= 20 {
			break
		}
		book, err := provider.OrderBook(ctx, tokenID)
		item := map[string]any{"token_id": tokenID, "source": "clob_rest"}
		if market, ok := tokenMarkets[tokenID]; ok {
			for key, value := range market {
				item[key] = value
			}
		}
		if err != nil {
			item["error"] = err.Error()
		} else {
			item["orderbook"] = book
		}
		rows = append(rows, item)
	}
	return rows
}

func firstPredictionTokenID(row map[string]any) string {
	for _, key := range []string{"asset_id", "assetId", "token_id", "tokenId"} {
		if value := strings.TrimSpace(fmt.Sprint(row[key])); value != "" && value != "<nil>" {
			return value
		}
	}
	return ""
}
