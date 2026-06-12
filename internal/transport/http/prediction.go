package httptransport

import (
	"net/http"
	"strconv"

	appprediction "github.com/TradingCopilotDevs/TradingCopilot/internal/app/prediction"
	domainprediction "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/prediction"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/transport/http/jsonapi"
)

func (s *Server) searchPredictionMarkets(w http.ResponseWriter, r *http.Request) {
	page := jsonapi.ParsePage(r, 20, 100)
	result, err := s.predictionUsecase.Search(r.Context(), r.URL.Query().Get("q"), page.Limit)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadGateway, "prediction-market-search-failed", "Prediction market search failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(result.Rows))
	for _, row := range result.Rows {
		out = append(out, predictionMarketResource(row))
	}
	jsonapi.Write(w, http.StatusOK, jsonapi.Document{Data: out, Meta: jsonapi.PageMeta(len(result.Rows), "")})
}

func (s *Server) syncPredictionMarkets(w http.ResponseWriter, r *http.Request) {
	limit := intQueryWithDefault(r, "limit", 100)
	count, err := s.predictionUsecase.SyncActive(r.Context(), limit)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadGateway, "prediction-market-sync-failed", "Prediction market sync failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("prediction-market-syncs", "latest", map[string]any{"provider": domainprediction.ProviderPolymarket, "synced": count}))
}

func (s *Server) getPredictionEvent(w http.ResponseWriter, r *http.Request) {
	row, found, err := s.predictionUsecase.GetEvent(r.Context(), uintParam(r, "eventId"))
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "prediction-event-load-failed", "Prediction event load failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "prediction-event-not-found", "Prediction event not found", "prediction event not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, predictionEventResource(*row))
}

func (s *Server) getPredictionMarket(w http.ResponseWriter, r *http.Request) {
	row, found, err := s.predictionUsecase.GetMarket(r.Context(), uintParam(r, "marketId"), r.URL.Query().Get("refresh") == "true")
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "prediction-market-load-failed", "Prediction market load failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "prediction-market-not-found", "Prediction market not found", "prediction market not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, predictionMarketResource(*row))
}

func (s *Server) listPredictionMatches(w http.ResponseWriter, r *http.Request) {
	page := jsonapi.ParsePage(r, 50, 200)
	cursorID, _ := strconv.ParseUint(page.Cursor, 10, 64)
	rows, err := s.predictionUsecase.ListMatches(r.Context(), appprediction.MatchFilter{
		MessageID: r.URL.Query().Get("messageId"),
		Status:    r.URL.Query().Get("status"),
		Limit:     page.Limit,
		CursorID:  cursorID,
	})
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "prediction-match-list-failed", "Prediction matches load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, predictionMatchResource(row))
	}
	jsonapi.Write(w, http.StatusOK, jsonapi.Document{Data: out, Meta: jsonapi.PageMeta(len(rows), "")})
}

func (s *Server) reviewPredictionMatch(w http.ResponseWriter, r *http.Request) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return
	}
	row, found, err := s.predictionUsecase.ReviewMatch(r.Context(), uintParam(r, "matchId"), appprediction.MatchReviewInput{
		Status:     stringAttr(attrs, "status"),
		Reason:     stringAttr(attrs, "reason"),
		ReviewedBy: currentPrincipal(r).Username,
	})
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "prediction-match-review-failed", "Prediction match review failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "prediction-match-not-found", "Prediction match not found", "prediction match not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, predictionMatchOnlyResource(*row))
}

func (s *Server) listPredictionWatchlist(w http.ResponseWriter, r *http.Request) {
	rows, err := s.predictionUsecase.ListWatchlist(r.Context(), uintQuery(r, "researchTeamId"), intQueryWithDefault(r, "limit", 100))
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "prediction-watchlist-load-failed", "Prediction watchlist load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, predictionWatchlistResource(row))
	}
	writeResourceCollection(w, r, out, 100, 500)
}

func (s *Server) upsertPredictionWatchlist(w http.ResponseWriter, r *http.Request) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return
	}
	item, err := s.predictionUsecase.UpsertWatchlist(r.Context(), uintAttr(attrs, "researchTeamId"), uintAttr(attrs, "marketId"), nullableStringAttr(attrs, "note"), boolAttrWithDefault(attrs, true, "active"))
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "prediction-watchlist-save-failed", "Prediction watchlist save failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, predictionWatchlistOnlyResource(*item))
}

func predictionEventResource(row domainprediction.Event) jsonapi.Resource {
	return jsonapi.NewResource("prediction-events", strconv.FormatUint(uint64(row.ID), 10), map[string]any{
		"provider": row.Provider, "externalEventId": row.ExternalEventID, "slug": row.Slug, "title": row.Title, "description": row.Description,
		"category": row.Category, "tags": rawJSONValue(row.Tags), "active": row.Active, "closed": row.Closed, "endDate": row.EndDate,
		"volume": row.Volume, "liquidity": row.Liquidity, "openInterest": row.OpenInterest, "raw": rawJSONValue(row.Raw), "updatedAt": row.UpdatedAt, "createdAt": row.CreatedAt,
	})
}

func predictionMarketResource(row domainprediction.Market) jsonapi.Resource {
	return jsonapi.NewResource("prediction-markets", strconv.FormatUint(uint64(row.ID), 10), predictionMarketAttributes(row))
}

func predictionMatchResource(row appprediction.MatchRow) jsonapi.Resource {
	attrs := predictionMatchAttributes(row.Match)
	attrs["market"] = predictionMarketAttributes(row.Market)
	return jsonapi.NewResource("prediction-market-matches", strconv.FormatUint(uint64(row.Match.ID), 10), attrs)
}

func predictionMatchOnlyResource(row domainprediction.Match) jsonapi.Resource {
	return jsonapi.NewResource("prediction-market-matches", strconv.FormatUint(uint64(row.ID), 10), predictionMatchAttributes(row))
}

func predictionMatchAttributes(row domainprediction.Match) map[string]any {
	return map[string]any{
		"messageId": row.MessageID, "marketId": row.MarketID, "query": row.Query, "newsSnippet": row.NewsSnippet,
		"candidateSnapshot": rawJSONValue(row.CandidateSnapshot), "score": row.Score, "scoreBreakdown": rawJSONValue(row.ScoreBreakdown),
		"status": row.Status, "reason": row.Reason, "reviewedBy": row.ReviewedBy, "reviewedAt": row.ReviewedAt, "createdAt": row.CreatedAt, "updatedAt": row.UpdatedAt,
	}
}

func predictionWatchlistResource(row appprediction.WatchlistRow) jsonapi.Resource {
	attrs := predictionWatchlistAttributes(row.Item)
	attrs["market"] = predictionMarketAttributes(row.Market)
	return jsonapi.NewResource("prediction-watchlist-items", strconv.FormatUint(uint64(row.Item.ID), 10), attrs)
}

func predictionWatchlistOnlyResource(row domainprediction.WatchlistItem) jsonapi.Resource {
	return jsonapi.NewResource("prediction-watchlist-items", strconv.FormatUint(uint64(row.ID), 10), predictionWatchlistAttributes(row))
}

func predictionWatchlistAttributes(row domainprediction.WatchlistItem) map[string]any {
	return map[string]any{
		"researchTeamId": row.ResearchTeamID, "marketId": row.MarketID, "note": row.Note, "active": row.Active, "createdAt": row.CreatedAt,
	}
}

func predictionMarketAttributes(row domainprediction.Market) map[string]any {
	return map[string]any{
		"eventId": row.EventID, "provider": row.Provider, "externalMarketId": row.ExternalMarketID, "conditionId": row.ConditionID, "question": row.Question,
		"slug": row.Slug, "description": row.Description, "outcomes": rawJSONValue(row.Outcomes), "outcomePrices": rawJSONValue(row.OutcomePrices),
		"clobTokenIds": rawJSONValue(row.CLOBTokenIDs), "enableOrderBook": row.EnableOrderBook, "bestBid": row.BestBid, "bestAsk": row.BestAsk,
		"lastTradePrice": row.LastTradePrice, "spread": row.Spread, "volume": row.Volume, "liquidity": row.Liquidity, "active": row.Active,
		"closed": row.Closed, "restricted": row.Restricted, "endDate": row.EndDate, "raw": rawJSONValue(row.Raw), "updatedAt": row.UpdatedAt, "createdAt": row.CreatedAt,
	}
}

func intQueryWithDefault(r *http.Request, key string, fallback int) int {
	value, err := strconv.Atoi(r.URL.Query().Get(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
