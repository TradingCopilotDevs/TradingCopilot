package httptransport

import (
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainpaper "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/paper"
	"net/http"
	"strconv"
	"strings"

	apppaper "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/paper"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/transport/http/jsonapi"
	"github.com/shopspring/decimal"
)

func (s *Server) paperOverview(w http.ResponseWriter, r *http.Request) {
	overview, err := s.paperUsecase.Overview(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "paper-overview-load-failed", "Paper overview load failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, paperOverviewResource(overview))
}

func (s *Server) listRiskConfigs(w http.ResponseWriter, r *http.Request) {
	rows, err := s.paperUsecase.ListRiskConfigs(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "paper-risk-configs-load-failed", "Paper risk configs load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, riskConfigResource(row))
	}
	writeResourceCollection(w, r, out, 100, 500)
}

func (s *Server) createRiskConfig(w http.ResponseWriter, r *http.Request) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return
	}
	row, err := s.paperUsecase.CreateRiskConfig(r.Context(), riskConfigInput(attrs))
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "paper-risk-config-create-failed", "Paper risk config create failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, riskConfigResource(*row))
}

func (s *Server) updateRiskConfig(w http.ResponseWriter, r *http.Request) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return
	}
	row, found, err := s.paperUsecase.UpdateRiskConfig(r.Context(), uintParam(r, "configId"), riskConfigInput(attrs))
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "paper-risk-config-update-failed", "Paper risk config update failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "paper-risk-config-not-found", "Paper risk config not found", "risk config not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, riskConfigResource(*row))
}

func (s *Server) deleteRiskConfig(w http.ResponseWriter, r *http.Request) {
	id := uintParam(r, "configId")
	deletion, deleted, err := s.paperUsecase.DeleteRiskConfig(r.Context(), id)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "paper-risk-config-delete-failed", "Paper risk config delete failed", err.Error(), "")
		return
	}
	if !deleted {
		writeJSONAPIError(w, http.StatusNotFound, "paper-risk-config-not-found", "Paper risk config not found", "risk config not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("deletions", "paper-risk-config:"+strconv.FormatUint(uint64(id), 10), map[string]any{
		"status":              "deleted",
		"unboundAccountIds":   deletion.UnboundAccountIDs,
		"unboundAccountCount": len(deletion.UnboundAccountIDs),
	}))
}

func (s *Server) listPaperAccounts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.paperUsecase.ListAccounts(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "paper-accounts-load-failed", "Paper accounts load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, paperAccountResource(row))
	}
	writeResourceCollection(w, r, out, 100, 500)
}

func (s *Server) createPaperAccount(w http.ResponseWriter, r *http.Request) {
	payload, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return
	}
	row, err := s.paperUsecase.CreateAccount(r.Context(), paperAccountInput(payload))
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "paper-account-create-failed", "Paper account create failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, paperAccountResource(*row))
}

func (s *Server) updatePaperAccount(w http.ResponseWriter, r *http.Request) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return
	}
	row, found, err := s.paperUsecase.UpdateAccount(r.Context(), uintParam(r, "accountId"), attrs)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "paper-account-update-failed", "Paper account update failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "paper-account-not-found", "Paper account not found", "paper account not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, paperAccountResource(*row))
}

func (s *Server) activatePaperAccount(w http.ResponseWriter, r *http.Request) {
	s.setAccountActive(w, r, true)
}

func (s *Server) deactivatePaperAccount(w http.ResponseWriter, r *http.Request) {
	s.setAccountActive(w, r, false)
}

func (s *Server) setAccountActive(w http.ResponseWriter, r *http.Request, active bool) {
	row, found, err := s.paperUsecase.SetAccountActive(r.Context(), uintParam(r, "accountId"), active)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "paper-account-update-failed", "Paper account update failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "paper-account-not-found", "Paper account not found", "paper account not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, paperAccountResource(*row))
}

func (s *Server) deletePaperAccount(w http.ResponseWriter, r *http.Request) {
	id := uintParam(r, "accountId")
	if err := s.paperUsecase.DeleteAccount(r.Context(), id); err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "paper-account-delete-failed", "Paper account delete failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("deletions", "paper-account:"+strconv.FormatUint(uint64(id), 10), map[string]any{"status": "deleted"}))
}

func (s *Server) listPositions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.paperUsecase.ListPositions(r.Context(), uintParam(r, "accountId"))
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "paper-positions-load-failed", "Paper positions load failed", err.Error(), "")
		return
	}
	writeResourceCollection(w, r, paperPositionResources(rows), 100, 500)
}

func (s *Server) listAccountOrders(w http.ResponseWriter, r *http.Request) {
	page := jsonapi.ParsePage(r, 100, 500)
	result, err := s.paperUsecase.ListAccountOrders(r.Context(), uintParam(r, "accountId"), r.URL.Query().Get("status"), apppaper.Page{Limit: page.Limit, Cursor: page.Cursor})
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "paper-orders-load-failed", "Paper orders load failed", err.Error(), "")
		return
	}
	jsonapi.Write(w, http.StatusOK, jsonapi.Document{Data: paperOrderResources(result.Rows), Links: jsonapi.PageLinks(r, result.NextCursor), Meta: jsonapi.PageMeta(len(result.Rows), result.NextCursor)})
}

func (s *Server) listFills(w http.ResponseWriter, r *http.Request) {
	page := jsonapi.ParsePage(r, 100, 500)
	result, err := s.paperUsecase.ListFills(r.Context(), uintParam(r, "accountId"), apppaper.Page{Limit: page.Limit, Cursor: page.Cursor})
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "paper-fills-load-failed", "Paper fills load failed", err.Error(), "")
		return
	}
	jsonapi.Write(w, http.StatusOK, jsonapi.Document{Data: paperFillResources(result.Rows), Links: jsonapi.PageLinks(r, result.NextCursor), Meta: jsonapi.PageMeta(len(result.Rows), result.NextCursor)})
}

func (s *Server) paperPerformance(w http.ResponseWriter, r *http.Request) {
	performance, found, err := s.paperUsecase.Performance(r.Context(), uintParam(r, "accountId"))
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "paper-performance-load-failed", "Paper performance load failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "paper-account-not-found", "Paper account not found", "paper account not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, paperPerformanceResource(uintParam(r, "accountId"), performance))
}

func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	page := jsonapi.ParsePage(r, 100, 500)
	result, err := s.paperUsecase.ListOrders(r.Context(), apppaper.Page{Limit: page.Limit, Cursor: page.Cursor})
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "paper-orders-load-failed", "Paper orders load failed", err.Error(), "")
		return
	}
	jsonapi.Write(w, http.StatusOK, jsonapi.Document{Data: paperOrderResources(result.Rows), Links: jsonapi.PageLinks(r, result.NextCursor), Meta: jsonapi.PageMeta(len(result.Rows), result.NextCursor)})
}

func (s *Server) createOrder(w http.ResponseWriter, r *http.Request) {
	payload, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return
	}
	if payload["meetingId"] == nil || payload["sourceMeetingEventId"] == nil {
		writeJSONAPIError(w, http.StatusBadRequest, "paper-manual-order-disabled", "Manual paper orders are disabled", "manual paper orders are disabled; orders must come from meetings", "")
		return
	}
	row, err := s.paperUsecase.CreateOrder(r.Context(), paperOrderInput(payload))
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "paper-order-create-failed", "Paper order create failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, paperOrderResource(*row))
}

func (s *Server) cancelOrder(w http.ResponseWriter, r *http.Request) {
	row, found, err := s.paperUsecase.CancelOrder(r.Context(), uintParam(r, "orderId"))
	if err != nil {
		writeJSONAPIError(w, http.StatusConflict, "paper-order-cancel-failed", "Paper order cancel failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "paper-order-not-found", "Paper order not found", "order not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, paperOrderResource(*row))
}

func (s *Server) fillOrder(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Price decimal.Decimal `json:"price"`
	}
	if !decodeJSONAPIRequest(w, r, &payload) {
		return
	}
	row, found, err := s.paperUsecase.FillOrder(r.Context(), uintParam(r, "orderId"), payload.Price)
	if err != nil {
		writeJSONAPIError(w, http.StatusConflict, "paper-order-fill-failed", "Paper order fill failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "paper-order-not-found", "Paper order not found", "order not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, paperOrderResource(*row))
}

func (s *Server) deleteOrder(w http.ResponseWriter, r *http.Request) {
	id := uintParam(r, "orderId")
	found, err := s.paperUsecase.DeleteOrder(r.Context(), id)
	if err != nil {
		writeJSONAPIError(w, http.StatusConflict, "paper-order-delete-failed", "Paper order delete failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "paper-order-not-found", "Paper order not found", "order not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("deletions", "paper-order:"+strconv.FormatUint(uint64(id), 10), map[string]any{"status": "deleted"}))
}

func paperOverviewResource(overview map[string]any) jsonapi.Resource {
	return jsonapi.NewResource("paper-overviews", "current", camelizeJSONKeys(overview))
}

func paperAccountInput(attrs map[string]any) domainpaper.AccountInput {
	return domainpaper.AccountInput{
		Name:         stringAttr(attrs, "name"),
		Cash:         decimalAttr(attrs, "cash"),
		InitialCash:  decimalAttr(attrs, "initialCash"),
		RiskConfigID: uintPtrAttr(attrs, "riskConfigId"),
		Active:       boolPtrAttr(attrs, "active"),
	}
}

func riskConfigInput(attrs map[string]any) domainpaper.RiskConfigInput {
	accountIDs, accountIDsSet := uintSliceAttr(attrs, "accountIds")
	return domainpaper.RiskConfigInput{
		Name:            stringAttrPtr(attrs, "name"),
		InitialCash:     decimalAttrPtr(attrs, "initialCash"),
		MaxPositionPct:  decimalAttrPtr(attrs, "maxPositionPct"),
		MaxOrderPct:     decimalAttrPtr(attrs, "maxOrderPct"),
		AllowShort:      boolPtrAttr(attrs, "allowShort"),
		AllowMargin:     boolPtrAttr(attrs, "allowMargin"),
		AllowSHMain:     boolPtrAttr(attrs, "allowShMain"),
		AllowSZMain:     boolPtrAttr(attrs, "allowSzMain"),
		AllowBJ:         boolPtrAttr(attrs, "allowBj"),
		AllowSTAR:       boolPtrAttr(attrs, "allowStar"),
		AllowChiNext:    boolPtrAttr(attrs, "allowChinext"),
		AllowETFLOF:     boolPtrAttr(attrs, "allowEtfLof"),
		CommissionRate:  decimalAttrPtr(attrs, "commissionRate"),
		MinCommission:   decimalAttrPtr(attrs, "minCommission"),
		StampDutyRate:   decimalAttrPtr(attrs, "stampDutyRate"),
		TransferFeeRate: decimalAttrPtr(attrs, "transferFeeRate"),
		Enabled:         boolPtrAttr(attrs, "enabled"),
		AccountIDs:      accountIDs,
		AccountIDsSet:   accountIDsSet,
	}
}

func paperOrderInput(attrs map[string]any) domainpaper.OrderInput {
	return domainpaper.OrderInput{
		AccountID:            uintAttr(attrs, "accountId"),
		MeetingID:            uintPtrAttr(attrs, "meetingId"),
		SourceMeetingEventID: uintPtrAttr(attrs, "sourceMeetingEventId"),
		Code:                 stringAttr(attrs, "code"),
		Side:                 domainkernel.OrderSide(stringAttr(attrs, "side")),
		Quantity:             intAttr(attrs, "quantity"),
		SuggestedPrice:       decimalAttr(attrs, "suggestedPrice"),
		Reason:               stringPtrAttr(attrs, "reason"),
		ExecuteAfter:         timePtrAttr(attrs, "executeAfter"),
		ExpireAt:             timePtrAttr(attrs, "expireAt"),
		PositionPct:          decimalAttr(attrs, "positionPct"),
		AllocationPct:        decimalAttr(attrs, "allocationPct"),
		MeetingSummary:       stringAttr(attrs, "meetingSummary"),
		MeetingConclusion:    stringAttr(attrs, "meetingConclusion"),
	}
}

func decimalAttrPtr(attrs map[string]any, keys ...string) *decimal.Decimal {
	if _, ok := attrValue(attrs, keys...); !ok {
		return nil
	}
	value := decimalAttr(attrs, keys...)
	return &value
}

func decimalAttr(attrs map[string]any, keys ...string) decimal.Decimal {
	value, ok := attrValue(attrs, keys...)
	if !ok || value == nil {
		return decimal.Zero
	}
	switch typed := value.(type) {
	case decimal.Decimal:
		return typed
	case int:
		return decimal.NewFromInt(int64(typed))
	case int64:
		return decimal.NewFromInt(typed)
	case uint:
		return decimal.NewFromInt(int64(typed))
	case uint64:
		return decimal.NewFromInt(int64(typed))
	case float64:
		return decimal.NewFromFloat(typed)
	case float32:
		return decimal.NewFromFloat(float64(typed))
	default:
		parsed, _ := decimal.NewFromString(strings.TrimSpace(fmtSprint(value)))
		return parsed
	}
}

func uintSliceAttr(attrs map[string]any, keys ...string) ([]uint, bool) {
	value, ok := attrValue(attrs, keys...)
	if !ok || value == nil {
		return nil, ok
	}
	items, ok := value.([]any)
	if !ok {
		return nil, true
	}
	out := make([]uint, 0, len(items))
	for _, item := range items {
		if id := uintPtrFromAny(item); id != nil {
			out = append(out, *id)
		}
	}
	return out, true
}

func uintAttr(attrs map[string]any, keys ...string) uint {
	if value := uintPtrAttr(attrs, keys...); value != nil {
		return *value
	}
	return 0
}

func intAttr(attrs map[string]any, keys ...string) int {
	value, ok := numberAttrValue(attrs, keys...)
	if !ok {
		return 0
	}
	return int(value)
}

func stringAttrPtr(attrs map[string]any, keys ...string) *string {
	value, ok := stringAttrValue(attrs, keys...)
	if !ok {
		return nil
	}
	return &value
}

func stringPtrAttr(attrs map[string]any, keys ...string) *string {
	value, ok := stringAttrValue(attrs, keys...)
	if !ok || strings.TrimSpace(value) == "" {
		return nil
	}
	trimmed := strings.TrimSpace(value)
	return &trimmed
}

func boolPtrAttr(attrs map[string]any, keys ...string) *bool {
	if _, ok := attrValue(attrs, keys...); !ok {
		return nil
	}
	value := boolAttr(attrs, keys...)
	return &value
}

func riskConfigResource(row apppaper.RiskConfigRow) jsonapi.Resource {
	attrs := riskConfigAttributes(row.Config)
	attrs["accountIds"] = row.AccountIDs
	attrs["accountCount"] = len(row.AccountIDs)
	return jsonapi.NewResource("paper-risk-configs", strconv.FormatUint(uint64(row.Config.ID), 10), attrs)
}

func riskConfigAttributes(row domainpaper.RiskConfig) map[string]any {
	return map[string]any{
		"name":            row.Name,
		"initialCash":     row.InitialCash,
		"maxPositionPct":  row.MaxPositionPct,
		"maxOrderPct":     row.MaxOrderPct,
		"allowShort":      row.AllowShort,
		"allowMargin":     row.AllowMargin,
		"allowShMain":     row.AllowSHMain,
		"allowSzMain":     row.AllowSZMain,
		"allowBj":         row.AllowBJ,
		"allowStar":       row.AllowSTAR,
		"allowChinext":    row.AllowChiNext,
		"allowEtfLof":     row.AllowETFLOF,
		"commissionRate":  row.CommissionRate,
		"minCommission":   row.MinCommission,
		"stampDutyRate":   row.StampDutyRate,
		"transferFeeRate": row.TransferFeeRate,
		"enabled":         row.Enabled,
		"createdAt":       row.CreatedAt,
		"updatedAt":       row.UpdatedAt,
	}
}

func paperAccountResource(row apppaper.AccountRow) jsonapi.Resource {
	attrs := jsonResourceAttributes(row.Public)
	if row.Team != nil {
		attrs["researchTeamId"] = row.Team.ID
		attrs["researchTeamName"] = row.Team.Name
	} else {
		attrs["researchTeamId"] = nil
		attrs["researchTeamName"] = nil
	}
	resource := jsonapi.NewResource("paper-accounts", strconv.FormatUint(uint64(row.Account.ID), 10), attrs)
	if row.Account.RiskConfigID != nil {
		resource.Relationships = map[string]jsonapi.Relationship{
			"riskConfig": {Data: map[string]string{"type": "paper-risk-configs", "id": strconv.FormatUint(uint64(*row.Account.RiskConfigID), 10)}},
		}
	}
	return resource
}

func paperPerformanceResource(accountID uint, performance map[string]any) jsonapi.Resource {
	return jsonapi.NewResource("paper-performances", strconv.FormatUint(uint64(accountID), 10), jsonResourceAttributes(performance))
}

func paperPositionResources(rows []map[string]any) []jsonapi.Resource {
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, item := range rows {
		id := strconv.FormatInt(int64(numberAttrValueOrZero(item, "id")), 10)
		if id == "0" {
			id = firstNonEmptyString(fmtSprint(item["id"]), "0")
		}
		resource := jsonapi.NewResource("paper-positions", id, jsonResourceAttributes(item))
		if accountID, ok := item["accountId"]; ok {
			resource.Relationships = map[string]jsonapi.Relationship{
				"account": {Data: map[string]string{"type": "paper-accounts", "id": fmtSprint(accountID)}},
			}
		}
		out = append(out, resource)
	}
	return out
}

func paperOrderResources(rows []apppaper.OrderRow) []jsonapi.Resource {
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, paperOrderResource(row))
	}
	return out
}

func paperOrderResource(row apppaper.OrderRow) jsonapi.Resource {
	resource := jsonapi.NewResource("paper-orders", strconv.FormatUint(uint64(row.Order.ID), 10), jsonResourceAttributes(row.Public))
	resource.Relationships = map[string]jsonapi.Relationship{
		"account": {Data: map[string]string{"type": "paper-accounts", "id": strconv.FormatUint(uint64(row.Order.AccountID), 10)}},
	}
	if row.Order.MeetingID != nil {
		resource.Relationships["meeting"] = jsonapi.Relationship{Data: map[string]string{"type": "meetings", "id": strconv.FormatUint(uint64(*row.Order.MeetingID), 10)}}
	}
	if row.Order.SourceMeetingEventID != nil {
		resource.Relationships["sourceMeetingEvent"] = jsonapi.Relationship{Data: map[string]string{"type": "meeting-events", "id": strconv.FormatUint(uint64(*row.Order.SourceMeetingEventID), 10)}}
	}
	return resource
}

func paperFillResources(rows []apppaper.FillRow) []jsonapi.Resource {
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		id := fmtSprint(row.Public["id"])
		resource := jsonapi.NewResource("paper-fills", id, jsonResourceAttributes(row.Public))
		resource.Relationships = map[string]jsonapi.Relationship{}
		if accountID, ok := row.Public["accountId"]; ok {
			resource.Relationships["account"] = jsonapi.Relationship{Data: map[string]string{"type": "paper-accounts", "id": fmtSprint(accountID)}}
		}
		if orderID, ok := row.Public["orderId"]; ok {
			resource.Relationships["order"] = jsonapi.Relationship{Data: map[string]string{"type": "paper-orders", "id": fmtSprint(orderID)}}
		}
		out = append(out, resource)
	}
	return out
}
