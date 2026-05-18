package httptransport

import (
	"encoding/json"
	"fmt"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	"io"
	"net/http"
	"strconv"
	"strings"

	appmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/app/meeting"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/transport/http/jsonapi"
	"github.com/go-chi/chi/v5"
)

func (s *Server) listMeetings(w http.ResponseWriter, r *http.Request) {
	page := jsonapi.ParsePage(r, 100, 500)
	result, err := s.meetingUsecase.List(r.Context(), appmeeting.ListFilter{
		ResearchTeamID: r.URL.Query().Get("researchTeamId"),
		Status:         r.URL.Query().Get("status"),
		TriggerSource:  r.URL.Query().Get("triggerSource"),
		Tag:            r.URL.Query().Get("tag"),
		Page:           appmeeting.Page{Limit: page.Limit, Cursor: page.Cursor},
	})
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "meeting-list-failed", "Meeting list failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(result.Rows))
	for _, row := range result.Rows {
		out = append(out, meetingResource(row))
	}
	jsonapi.Write(w, http.StatusOK, jsonapi.Document{Data: out, Links: jsonapi.PageLinks(r, result.NextCursor), Meta: jsonapi.PageMeta(len(result.Rows), result.NextCursor)})
}

func (s *Server) startMeeting(w http.ResponseWriter, r *http.Request) {
	payload, ok := decodeMeetingStartPayload(w, r)
	if !ok {
		return
	}
	meeting, err := s.meetingUsecase.Start(r.Context(), appmeeting.StartInput(payload))
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "meeting-create-failed", "Meeting create failed", err.Error(), "")
		return
	}
	if _, err := s.meetingUsecase.DispatchRun(r.Context(), meeting, "queued", "", nil); err != nil {
		writeJSONAPIError(w, http.StatusBadGateway, "meeting-dispatch-failed", "Meeting dispatch failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, meetingResource(*meeting))
}

func (s *Server) getMeeting(w http.ResponseWriter, r *http.Request) {
	row, found, err := s.meetingUsecase.Get(r.Context(), uintParam(r, "meetingId"))
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "meeting-load-failed", "Meeting load failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "meeting-not-found", "Meeting not found", "meeting not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, meetingResource(*row))
}

func (s *Server) updateMeeting(w http.ResponseWriter, r *http.Request) {
	payload, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return
	}
	input := appmeeting.UpdateInput{}
	if topic, ok := stringAttrValue(payload, "topic"); ok {
		input.Topic = &topic
	}
	if trigger, ok := stringAttrValue(payload, "triggerSource"); ok {
		input.TriggerSource = &trigger
	}
	if tags, ok := attrValue(payload, "tags"); ok {
		input.Tags = tags
		input.HasTags = true
	}
	row, found, err := s.meetingUsecase.Update(r.Context(), uintParam(r, "meetingId"), input)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "meeting-update-failed", "Meeting update failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "meeting-not-found", "Meeting not found", "meeting not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, meetingResource(*row))
}

func (s *Server) recapMeeting(w http.ResponseWriter, r *http.Request) {
	payload := decodeOptionalMeetingRecapPayload(r)
	row, found, err := s.meetingUsecase.Recap(r.Context(), uintParam(r, "meetingId"), payload.RequestedBy)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "meeting-recap-failed", "Meeting recap failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "meeting-not-found", "Meeting not found", "meeting not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, meetingResource(*row))
}

func (s *Server) deleteMeeting(w http.ResponseWriter, r *http.Request) {
	id := uintParam(r, "meetingId")
	found, err := s.meetingUsecase.Delete(r.Context(), id)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "meeting-delete-failed", "Meeting delete failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "meeting-not-found", "Meeting not found", "meeting not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("deletions", "meeting:"+strconv.FormatUint(uint64(id), 10), map[string]any{"status": "deleted"}))
}

func (s *Server) cancelMeeting(w http.ResponseWriter, r *http.Request) {
	row, found, err := s.meetingUsecase.Cancel(r.Context(), uintParam(r, "meetingId"))
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "meeting-cancel-failed", "Meeting cancel failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "meeting-not-found", "Meeting not found", "meeting not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, meetingResource(*row))
}

func (s *Server) restartMeeting(w http.ResponseWriter, r *http.Request) {
	meeting, found, err := s.meetingUsecase.Restart(r.Context(), uintParam(r, "meetingId"))
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "meeting-restart-failed", "Meeting restart failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "meeting-not-found", "Meeting not found", "meeting not found", "")
		return
	}
	if _, err := s.meetingUsecase.DispatchRun(r.Context(), meeting, "queued", "", nil); err != nil {
		writeJSONAPIError(w, http.StatusBadGateway, "meeting-dispatch-failed", "Meeting dispatch failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, meetingResource(*meeting))
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	page := jsonapi.ParsePage(r, 100, 500)
	result, err := s.meetingUsecase.ListEvents(r.Context(), uintParam(r, "meetingId"), appmeeting.Page{Limit: page.Limit, Cursor: page.Cursor})
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "meeting-events-load-failed", "Meeting events load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(result.Rows))
	for _, row := range result.Rows {
		out = append(out, meetingEventResource(row))
	}
	jsonapi.Write(w, http.StatusOK, jsonapi.Document{Data: out, Links: jsonapi.PageLinks(r, result.NextCursor), Meta: jsonapi.PageMeta(len(result.Rows), result.NextCursor)})
}

func (s *Server) listReferences(w http.ResponseWriter, r *http.Request) {
	rows, err := s.meetingUsecase.ListReferences(r.Context(), uintParam(r, "meetingId"))
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "meeting-references-load-failed", "Meeting references load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, meetingReferenceResource(row))
	}
	writeResourceCollection(w, r, out, 100, 500)
}

func (s *Server) createReference(w http.ResponseWriter, r *http.Request) {
	payload, ok := decodeMeetingReferencePayload(w, r)
	if !ok {
		return
	}
	ref, err := s.meetingUsecase.CreateReference(r.Context(), uintParam(r, "meetingId"), appmeeting.ReferenceInput(payload))
	if err != nil {
		if err == appmeeting.ErrNotFound {
			writeJSONAPIError(w, http.StatusNotFound, "meeting-not-found", "Meeting not found", "meeting not found", "")
			return
		}
		if strings.Contains(err.Error(), "target meeting not found") {
			writeJSONAPIError(w, http.StatusNotFound, "target-meeting-not-found", "Target meeting not found", "target meeting not found", "")
			return
		}
		writeJSONAPIError(w, http.StatusBadRequest, "meeting-reference-invalid", "Meeting reference invalid", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, meetingReferenceResource(*ref))
}

func (s *Server) deleteReference(w http.ResponseWriter, r *http.Request) {
	id := uintParam(r, "referenceId")
	found, err := s.meetingUsecase.DeleteReference(r.Context(), uintParam(r, "meetingId"), id)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "meeting-reference-delete-failed", "Meeting reference delete failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "meeting-reference-not-found", "Meeting reference not found", "reference not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("deletions", "meeting-reference:"+strconv.FormatUint(uint64(id), 10), map[string]any{"status": "deleted"}))
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	result, _ := s.meetingUsecase.ListEvents(r.Context(), uintParam(r, "meetingId"), appmeeting.Page{Limit: 500})
	for _, event := range result.Rows {
		b, _ := json.Marshal(event)
		fmt.Fprintf(w, "event: meeting_event\ndata: %s\n\n", b)
	}
}

type meetingStartPayload struct {
	ResearchTeamID uint
	Topic          string
	TriggerSource  string
	Context        map[string]any
}

func decodeMeetingStartPayload(w http.ResponseWriter, r *http.Request) (meetingStartPayload, bool) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return meetingStartPayload{}, false
	}
	payload := meetingStartPayload{ResearchTeamID: uintAttr(attrs, "researchTeamId"), Topic: stringAttr(attrs, "topic"), TriggerSource: stringAttr(attrs, "triggerSource")}
	if payload.TriggerSource == "" {
		payload.TriggerSource = "manual"
	}
	if value, exists := attrValue(attrs, "context"); exists {
		if context, ok := value.(map[string]any); ok {
			payload.Context = context
		}
	}
	return payload, true
}

type meetingRecapPayload struct {
	RequestedBy string
}

func decodeOptionalMeetingRecapPayload(r *http.Request) meetingRecapPayload {
	if r.Body == nil || r.ContentLength == 0 {
		return meetingRecapPayload{}
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil || len(strings.TrimSpace(string(raw))) == 0 {
		return meetingRecapPayload{}
	}
	var envelope struct {
		Data struct {
			Attributes map[string]any `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Data.Attributes != nil {
		return meetingRecapPayload{RequestedBy: stringAttr(envelope.Data.Attributes, "requestedBy")}
	}
	return meetingRecapPayload{}
}

type meetingReferencePayload struct {
	TargetMeetingID uint
	Note            *string
}

func decodeMeetingReferencePayload(w http.ResponseWriter, r *http.Request) (meetingReferencePayload, bool) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return meetingReferencePayload{}, false
	}
	var targetID uint
	if id := uintPtrAttr(attrs, "targetMeetingId"); id != nil {
		targetID = *id
	}
	return meetingReferencePayload{TargetMeetingID: targetID, Note: nullableStringAttr(attrs, "note")}, true
}

func meetingResource(row domainmeeting.Meeting) jsonapi.Resource {
	return jsonapi.NewResource("meetings", strconv.FormatUint(uint64(row.ID), 10), map[string]any{
		"topic":            row.Topic,
		"researchTeamId":   row.ResearchTeamID,
		"status":           row.Status,
		"triggerSource":    row.TriggerSource,
		"summary":          row.Summary,
		"conclusion":       row.Conclusion,
		"tags":             appmeeting.TagsFromJSON(row.Tags),
		"recapStatus":      row.RecapStatus,
		"recapUpdatedAt":   row.RecapUpdatedAt,
		"tokenBudget":      row.TokenBudget,
		"runId":            row.RunID,
		"runAttempt":       row.RunAttempt,
		"heartbeatAt":      row.HeartbeatAt,
		"autoRequeueCount": row.AutoRequeueCount,
		"startedAt":        row.StartedAt,
		"completedAt":      row.CompletedAt,
		"createdAt":        row.CreatedAt,
	})
}

func meetingEventResource(row domainmeeting.Event) jsonapi.Resource {
	resource := jsonapi.NewResource("meeting-events", strconv.FormatUint(uint64(row.ID), 10), map[string]any{
		"meetingId": row.MeetingID,
		"sequence":  row.Sequence,
		"type":      row.Type,
		"roleKey":   row.RoleKey,
		"content":   row.Content,
		"payload":   rawJSONValue(row.Payload),
		"createdAt": row.CreatedAt,
	})
	resource.Relationships = map[string]jsonapi.Relationship{
		"meeting": {Data: map[string]string{"type": "meetings", "id": strconv.FormatUint(uint64(row.MeetingID), 10)}},
	}
	return resource
}

func meetingReferenceResource(row domainmeeting.Reference) jsonapi.Resource {
	resource := jsonapi.NewResource("meeting-references", strconv.FormatUint(uint64(row.ID), 10), map[string]any{
		"sourceMeetingId":       row.SourceMeetingID,
		"targetMeetingId":       row.TargetMeetingID,
		"referenceType":         row.ReferenceType,
		"note":                  row.Note,
		"targetTopicSnapshot":   row.TargetTopicSnapshot,
		"targetSummarySnapshot": row.TargetSummarySnapshot,
		"targetDeleted":         row.TargetDeleted,
		"externalRef":           row.ExternalRef,
		"createdAt":             row.CreatedAt,
	})
	relationships := map[string]jsonapi.Relationship{
		"sourceMeeting": {Data: map[string]string{"type": "meetings", "id": strconv.FormatUint(uint64(row.SourceMeetingID), 10)}},
	}
	if row.TargetMeetingID != nil {
		relationships["targetMeeting"] = jsonapi.Relationship{Data: map[string]string{"type": "meetings", "id": strconv.FormatUint(uint64(*row.TargetMeetingID), 10)}}
	}
	resource.Relationships = relationships
	return resource
}

var _ = chi.URLParam
