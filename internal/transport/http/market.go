package httptransport

import (
	"encoding/json"
	"fmt"
	domainmarket "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/market"
	"net/http"
	"strconv"
	"strings"

	appmarket "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/market"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/transport/http/jsonapi"
	"github.com/go-chi/chi/v5"
)

func (s *Server) listSymbols(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	page := jsonapi.ParsePage(r, 100, 500)
	result, err := s.marketUsecase.ListSymbols(r.Context(), q, appmarket.Page{Limit: page.Limit, Cursor: page.Cursor})
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "market-symbols-load-failed", "Market symbols load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(result.Rows))
	for _, row := range result.Rows {
		out = append(out, marketSymbolResource(row))
	}
	jsonapi.Write(w, http.StatusOK, jsonapi.Document{Data: out, Links: jsonapi.PageLinks(r, result.NextCursor), Meta: jsonapi.PageMeta(len(result.Rows), result.NextCursor)})
}

func (s *Server) createSymbol(w http.ResponseWriter, r *http.Request) {
	payload, ok := decodeMarketSymbolPayload(w, r)
	if !ok {
		return
	}
	row, err := s.marketUsecase.CreateSymbol(r.Context(), payload)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "market-symbol-save-failed", "Market symbol save failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, marketSymbolResource(*row))
}

func (s *Server) updateSymbol(w http.ResponseWriter, r *http.Request) {
	payload, ok := decodeMarketSymbolPayload(w, r)
	if !ok {
		return
	}
	row, found, err := s.marketUsecase.UpdateSymbol(r.Context(), chi.URLParam(r, "code"), payload)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "market-symbol-save-failed", "Market symbol save failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "market-symbol-not-found", "Market symbol not found", "symbol not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, marketSymbolResource(*row))
}

func (s *Server) deleteSymbol(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	if err := s.marketUsecase.DeleteSymbol(r.Context(), code); err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "market-symbol-delete-failed", "Market symbol delete failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("deletions", "market-symbol:"+code, map[string]any{"status": "deleted"}))
}

func (s *Server) syncMarketSymbols(w http.ResponseWriter, r *http.Request) {
	provider, synced, err := s.marketUsecase.SyncSymbols(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "market-symbol-sync-failed", "Market symbol sync failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("market-symbol-syncs", "latest", map[string]any{"provider": provider, "synced": synced}))
}

func (s *Server) currentQuote(w http.ResponseWriter, r *http.Request) {
	quote, err := s.marketUsecase.CurrentQuote(r.Context(), chi.URLParam(r, "code"), r.URL.Query().Get("refresh") != "false")
	if err != nil {
		writeJSONAPIError(w, http.StatusNotFound, "market-quote-not-found", "Market quote not found", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, marketQuoteResource(*quote))
}

func (s *Server) marketSeries(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	rangeKey := r.URL.Query().Get("range")
	rows, err := s.marketUsecase.Series(r.Context(), code, rangeKey)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "market-series-failed", "Market series failed", err.Error(), "")
		return
	}
	writeResourceCollection(w, r, marketSeriesResources(code, rangeKey, rows), 100, 500)
}

func (s *Server) dailyBars(w http.ResponseWriter, r *http.Request) {
	page := jsonapi.ParsePage(r, 120, 500)
	result, err := s.marketUsecase.DailyBars(r.Context(), chi.URLParam(r, "code"), r.URL.Query().Get("refresh") == "true", appmarket.Page{Limit: page.Limit, Cursor: page.Cursor})
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "market-daily-refresh-failed", "Market daily refresh failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(result.Rows))
	for _, row := range result.Rows {
		out = append(out, marketDailyBarResource(row))
	}
	jsonapi.Write(w, http.StatusOK, jsonapi.Document{Data: out, Links: jsonapi.PageLinks(r, result.NextCursor), Meta: jsonapi.PageMeta(len(result.Rows), result.NextCursor)})
}

func (s *Server) listWatchlist(w http.ResponseWriter, r *http.Request) {
	page := jsonapi.ParsePage(r, 100, 500)
	result, err := s.marketUsecase.ListWatchlist(r.Context(), uintQuery(r, "researchTeamId"), appmarket.Page{Limit: page.Limit, Cursor: page.Cursor})
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "market-watchlist-load-failed", "Market watchlist load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(result.Rows))
	for _, row := range result.Rows {
		out = append(out, marketWatchlistResource(row.Item, row.Extra))
	}
	jsonapi.Write(w, http.StatusOK, jsonapi.Document{Data: out, Links: jsonapi.PageLinks(r, result.NextCursor), Meta: jsonapi.PageMeta(len(result.Rows), result.NextCursor)})
}

func (s *Server) upsertWatchlist(w http.ResponseWriter, r *http.Request) {
	payload, ok := decodeWatchlistPayload(w, r)
	if !ok {
		return
	}
	item, err := s.marketUsecase.UpsertWatchlist(r.Context(), payload.ResearchTeamID, payload.Code, payload.Note, payload.Active)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "market-watchlist-save-failed", "Market watchlist save failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, marketWatchlistResource(*item, nil))
}

func (s *Server) updateWatchlist(w http.ResponseWriter, r *http.Request) {
	if !s.marketUsecase.WatchlistExists(r.Context(), uintParam(r, "itemId")) {
		writeJSONAPIError(w, http.StatusNotFound, "market-watchlist-not-found", "Market watchlist item not found", "watchlist item not found", "")
		return
	}
	s.upsertWatchlist(w, r)
}

func (s *Server) deleteWatchlist(w http.ResponseWriter, r *http.Request) {
	id := uintParam(r, "itemId")
	if err := s.marketUsecase.DeleteWatchlist(r.Context(), id); err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "market-watchlist-delete-failed", "Market watchlist delete failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("deletions", "market-watchlist:"+strconv.FormatUint(uint64(id), 10), map[string]any{"status": "deleted"}))
}

func (s *Server) toolQuery(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Tool      string         `json:"tool"`
		Arguments map[string]any `json:"arguments"`
	}
	if !decodeJSONAPIRequest(w, r, &payload) {
		return
	}
	rows, err := s.marketUsecase.QueryTool(r.Context(), payload.Tool, payload.Arguments)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "market-tool-query-failed", "Market tool query failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, marketToolResultResources(rows))
}

func decodeMarketSymbolPayload(w http.ResponseWriter, r *http.Request) (domainmarket.Symbol, bool) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return domainmarket.Symbol{}, false
	}
	return domainmarket.Symbol{
		Code:     stringAttr(attrs, "code"),
		Name:     stringAttr(attrs, "name"),
		Exchange: stringAttr(attrs, "exchange"),
		Industry: nullableStringAttr(attrs, "industry"),
		Active:   boolAttrWithDefault(attrs, true, "active"),
	}, true
}

type watchlistPayload struct {
	ResearchTeamID uint
	Code           string
	Note           *string
	Active         bool
}

func decodeWatchlistPayload(w http.ResponseWriter, r *http.Request) (watchlistPayload, bool) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return watchlistPayload{}, false
	}
	var note *string
	if value, exists := attrValue(attrs, "note"); exists && value != nil {
		text := fmt.Sprint(value)
		note = &text
	}
	return watchlistPayload{
		ResearchTeamID: uintAttr(attrs, "researchTeamId"),
		Code:           stringAttr(attrs, "code"),
		Note:           note,
		Active:         boolAttrWithDefault(attrs, true, "active"),
	}, true
}

func marketSymbolResource(row domainmarket.Symbol) jsonapi.Resource {
	return jsonapi.NewResource("market-symbols", row.Code, map[string]any{
		"code":       row.Code,
		"name":       row.Name,
		"exchange":   row.Exchange,
		"industry":   row.Industry,
		"listedDate": row.ListedDate,
		"active":     row.Active,
		"updatedAt":  row.UpdatedAt,
	})
}

func marketQuoteResource(row domainmarket.RealtimeQuote) jsonapi.Resource {
	return jsonapi.NewResource("market-quotes", strconv.FormatUint(uint64(row.ID), 10), map[string]any{
		"code":      row.Code,
		"quoteTime": row.QuoteTime,
		"price":     row.Price,
		"changePct": row.ChangePct,
		"volume":    row.Volume,
		"amount":    row.Amount,
		"raw":       rawJSONValue(row.Raw),
		"provider":  row.Provider,
	})
}

func marketSeriesResources(code string, rangeKey string, rows []map[string]any) []jsonapi.Resource {
	out := make([]jsonapi.Resource, 0, len(rows))
	for i, row := range rows {
		attrs := camelizeJSONKeys(row)
		id := fmt.Sprintf("%s:%s:%d", code, firstNonEmptyString(rangeKey, "default"), i)
		if item, ok := attrs.(map[string]any); ok {
			if t, exists := item["time"]; exists {
				id = fmt.Sprintf("%s:%s:%v", code, firstNonEmptyString(rangeKey, "default"), t)
			}
			out = append(out, jsonapi.NewResource("market-series-points", id, item))
			continue
		}
		out = append(out, jsonapi.NewResource("market-series-points", id, attrs))
	}
	return out
}

func marketDailyBarResource(row domainmarket.DailyBar) jsonapi.Resource {
	return jsonapi.NewResource("market-daily-bars", strconv.FormatUint(uint64(row.ID), 10), map[string]any{
		"code":      row.Code,
		"tradeDate": row.TradeDate,
		"open":      row.Open,
		"high":      row.High,
		"low":       row.Low,
		"close":     row.Close,
		"volume":    row.Volume,
		"amount":    row.Amount,
		"provider":  row.Provider,
	})
}

func marketWatchlistResource(item domainmarket.WatchlistItem, extra map[string]any) jsonapi.Resource {
	attrs := map[string]any{
		"code":           item.Code,
		"researchTeamId": item.ResearchTeamID,
		"note":           item.Note,
		"active":         item.Active,
		"createdAt":      item.CreatedAt,
	}
	for key, value := range extra {
		attrs[key] = value
	}
	return jsonapi.NewResource("market-watchlist-items", strconv.FormatUint(uint64(item.ID), 10), attrs)
}

func marketToolResultResources(rows []map[string]any) []jsonapi.Resource {
	out := make([]jsonapi.Resource, 0, len(rows))
	for i, row := range rows {
		out = append(out, jsonapi.NewResource("market-tool-results", strconv.Itoa(i+1), camelizeJSONKeys(row)))
	}
	return out
}

func nullableStringAttr(attrs map[string]any, keys ...string) *string {
	value, ok := attrValue(attrs, keys...)
	if !ok || value == nil {
		return nil
	}
	text := fmt.Sprint(value)
	if text == "" {
		return nil
	}
	return &text
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func camelizeJSONKeys(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			out[snakeToLowerCamel(key)] = camelizeJSONKeys(item)
		}
		return out
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, camelizeJSONKeys(item))
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, camelizeJSONKeys(item))
		}
		return out
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return value
		}
		var object map[string]any
		if err := json.Unmarshal(raw, &object); err == nil && object != nil {
			return camelizeJSONKeys(object)
		}
		var list []any
		if err := json.Unmarshal(raw, &list); err == nil && list != nil {
			return camelizeJSONKeys(list)
		}
		return value
	}
}

func snakeToLowerCamel(value string) string {
	parts := strings.Split(value, "_")
	if len(parts) == 1 {
		return value
	}
	var b strings.Builder
	b.WriteString(parts[0])
	for _, part := range parts[1:] {
		if part == "" {
			continue
		}
		b.WriteString(strings.ToUpper(part[:1]))
		if len(part) > 1 {
			b.WriteString(part[1:])
		}
	}
	return b.String()
}
