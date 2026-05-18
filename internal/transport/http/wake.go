package httptransport

import (
	"encoding/json"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainwake "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/wake"
	"net/http"
	"strconv"

	appwake "github.com/TradingCopilotDevs/TradingCopilot/internal/app/wake"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/transport/http/jsonapi"
)

func (s *Server) listWakePlans(w http.ResponseWriter, r *http.Request) {
	page := jsonapi.ParsePage(r, 100, 500)
	result, err := s.wakeUsecase.List(r.Context(), appwake.ListFilter{
		ResearchTeamID: r.URL.Query().Get("researchTeamId"),
		Status:         r.URL.Query().Get("status"),
		MeetingID:      r.URL.Query().Get("meetingId"),
		Page:           appwake.Page{Limit: page.Limit, Cursor: page.Cursor},
	})
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "wake-plans-load-failed", "Wake plans load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(result.Rows))
	for _, row := range result.Rows {
		out = append(out, wakePlanResource(row))
	}
	jsonapi.Write(w, http.StatusOK, jsonapi.Document{Data: out, Links: jsonapi.PageLinks(r, result.NextCursor), Meta: jsonapi.PageMeta(len(result.Rows), result.NextCursor)})
}

func (s *Server) createWakePlan(w http.ResponseWriter, r *http.Request) {
	row, ok := decodeWakePlanPayload(w, r)
	if !ok {
		return
	}
	saved, err := s.wakeUsecase.Create(r.Context(), row)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "wake-plan-create-failed", "Wake plan create failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, wakePlanResource(*saved))
}

func (s *Server) fireWakePlan(w http.ResponseWriter, r *http.Request) {
	result, found, err := s.wakeUsecase.Fire(r.Context(), uintParam(r, "planId"))
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "wake-plan-fire-failed", "Wake plan fire failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "wake-plan-not-found", "Wake plan not found", "wake plan not found", "")
		return
	}
	if result.Meeting != nil {
		_, _ = s.meetingUsecase.DispatchRun(r.Context(), result.Meeting, "queued", "", nil)
	}
	jsonapi.WriteData(w, http.StatusOK, wakePlanResource(result.Plan))
}

func (s *Server) pauseWakePlan(w http.ResponseWriter, r *http.Request) {
	s.setWakeStatus(w, r, domainkernel.WakePaused)
}

func (s *Server) resumeWakePlan(w http.ResponseWriter, r *http.Request) {
	s.setWakeStatus(w, r, domainkernel.WakeActive)
}

func (s *Server) cancelWakePlan(w http.ResponseWriter, r *http.Request) {
	s.setWakeStatus(w, r, domainkernel.WakeCancelled)
}

func (s *Server) setWakeStatus(w http.ResponseWriter, r *http.Request, status domainkernel.WakePlanStatus) {
	row, found, err := s.wakeUsecase.SetStatus(r.Context(), uintParam(r, "planId"), status)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "wake-plan-update-failed", "Wake plan update failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "wake-plan-not-found", "Wake plan not found", "wake plan not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, wakePlanResource(*row))
}

func (s *Server) deleteWakePlan(w http.ResponseWriter, r *http.Request) {
	id := uintParam(r, "planId")
	if err := s.wakeUsecase.Delete(r.Context(), id); err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "wake-plan-delete-failed", "Wake plan delete failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("deletions", "wake-plan:"+strconv.FormatUint(uint64(id), 10), map[string]any{"status": "deleted"}))
}

func decodeWakePlanPayload(w http.ResponseWriter, r *http.Request) (domainwake.Plan, bool) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return domainwake.Plan{}, false
	}
	row := domainwake.Plan{
		ResearchTeamID: uintAttr(attrs, "researchTeamId"),
		TriggerType:    domainkernel.WakeTriggerType(stringAttr(attrs, "triggerType")),
		Reason:         stringAttr(attrs, "reason"),
		SourceRoleKey:  nullableStringAttr(attrs, "sourceRoleKey"),
		Status:         domainkernel.WakePlanStatus(stringAttr(attrs, "status")),
		NextCheckAt:    timePtrAttr(attrs, "nextCheckAt"),
		FiredAt:        timePtrAttr(attrs, "firedAt"),
		LastRunAt:      timePtrAttr(attrs, "lastRunAt"),
		ResultSummary:  nullableStringAttr(attrs, "resultSummary"),
	}
	row.MeetingID = uintPtrAttr(attrs, "meetingId")
	row.SourceMeetingEventID = uintPtrAttr(attrs, "sourceMeetingEventId")
	if value, exists := attrValue(attrs, "triggerConfig"); exists {
		row.TriggerConfig = mustJSON(value)
	}
	return row, true
}

func mustJSON(value any) domainkernel.JSON {
	raw, _ := json.Marshal(value)
	return domainkernel.JSON(raw)
}

func wakePlanResource(row domainwake.Plan) jsonapi.Resource {
	resource := jsonapi.NewResource("wake-plans", strconv.FormatUint(uint64(row.ID), 10), map[string]any{
		"meetingId":            row.MeetingID,
		"researchTeamId":       row.ResearchTeamID,
		"triggerType":          row.TriggerType,
		"triggerConfig":        rawJSONValue(row.TriggerConfig),
		"reason":               row.Reason,
		"sourceMeetingEventId": row.SourceMeetingEventID,
		"sourceRoleKey":        row.SourceRoleKey,
		"status":               row.Status,
		"nextCheckAt":          row.NextCheckAt,
		"firedAt":              row.FiredAt,
		"lastRunAt":            row.LastRunAt,
		"resultSummary":        row.ResultSummary,
		"createdAt":            row.CreatedAt,
	})
	relationships := map[string]jsonapi.Relationship{}
	if row.MeetingID != nil {
		relationships["meeting"] = jsonapi.Relationship{Data: map[string]string{"type": "meetings", "id": strconv.FormatUint(uint64(*row.MeetingID), 10)}}
	}
	if row.SourceMeetingEventID != nil {
		relationships["sourceMeetingEvent"] = jsonapi.Relationship{Data: map[string]string{"type": "meeting-events", "id": strconv.FormatUint(uint64(*row.SourceMeetingEventID), 10)}}
	}
	if len(relationships) > 0 {
		resource.Relationships = relationships
	}
	return resource
}
