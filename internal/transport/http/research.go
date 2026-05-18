package httptransport

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	appresearch "github.com/TradingCopilotDevs/TradingCopilot/internal/app/research"
	domainresearch "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/research"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/transport/http/jsonapi"
)

func (s *Server) listResearchTeams(w http.ResponseWriter, r *http.Request) {
	rows, err := s.researchUsecase.ListTeams(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "research-teams-load-failed", "Research teams load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, researchTeamResource(row))
	}
	writeResourceCollection(w, r, out, 100, 500)
}

func (s *Server) createResearchTeam(w http.ResponseWriter, r *http.Request) {
	input, _, ok := decodeResearchTeamInput(w, r)
	if !ok {
		return
	}
	row, err := s.researchUsecase.CreateTeam(r.Context(), input)
	if err != nil {
		writeResearchError(w, err)
		return
	}
	jsonapi.WriteData(w, http.StatusOK, researchTeamResource(*row))
}

func (s *Server) updateResearchTeam(w http.ResponseWriter, r *http.Request) {
	input, fields, ok := decodeResearchTeamInput(w, r)
	if !ok {
		return
	}
	row, found, err := s.researchUsecase.UpdateTeam(r.Context(), uintParam(r, "teamId"), input, fields)
	if err != nil {
		writeResearchError(w, err)
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "research-team-not-found", "Research team not found", "research team not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, researchTeamResource(*row))
}

func (s *Server) deleteResearchTeam(w http.ResponseWriter, r *http.Request) {
	id := uintParam(r, "teamId")
	found, err := s.researchUsecase.DeleteTeam(r.Context(), id)
	if err != nil {
		writeResearchError(w, err)
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "research-team-not-found", "Research team not found", "research team not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("deletions", "research-team:"+strconv.FormatUint(uint64(id), 10), map[string]any{"status": "deleted"}))
}

func (s *Server) listResearchTeamRoles(w http.ResponseWriter, r *http.Request) {
	rows, err := s.researchUsecase.ListRoles(r.Context(), uintParam(r, "teamId"))
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "research-team-roles-load-failed", "Research team roles load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, researchTeamRoleResource(row))
	}
	writeResourceCollection(w, r, out, 100, 500)
}

func (s *Server) putResearchTeamRole(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeResearchTeamRoleInput(w, r)
	if !ok {
		return
	}
	role, err := s.researchUsecase.UpsertRole(r.Context(), uintParam(r, "teamId"), stringParam(r, "roleKey"), input)
	if err != nil {
		writeResearchError(w, err)
		return
	}
	jsonapi.WriteData(w, http.StatusOK, researchTeamRoleResource(*role))
}

func (s *Server) deleteResearchTeamRole(w http.ResponseWriter, r *http.Request) {
	teamID := uintParam(r, "teamId")
	key := stringParam(r, "roleKey")
	if err := s.researchUsecase.DeleteRole(r.Context(), teamID, key); err != nil {
		writeResearchError(w, err)
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("deletions", "research-team-role:"+strconv.FormatUint(uint64(teamID), 10)+":"+key, map[string]any{"status": "deleted"}))
}

func (s *Server) applyDefaultResearchTeamRoles(w http.ResponseWriter, r *http.Request) {
	updated, err := s.researchUsecase.ApplyDefaultRoles(r.Context(), uintParam(r, "teamId"))
	if err != nil {
		writeResearchError(w, err)
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("research-team-role-default-updates", strconv.FormatUint(uint64(uintParam(r, "teamId")), 10), map[string]any{"updated": updated}))
}

func decodeResearchTeamInput(w http.ResponseWriter, r *http.Request) (appresearch.TeamInput, map[string]bool, bool) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return appresearch.TeamInput{}, nil, false
	}
	fields := map[string]bool{}
	for _, name := range []string{"name", "description", "paperAccountId", "active", "copyRolesFromTeamId"} {
		if _, ok := attrValue(attrs, name); ok {
			fields[name] = true
		}
	}
	return appresearch.TeamInput{
		Name:                stringAttr(attrs, "name"),
		Description:         stringAttr(attrs, "description"),
		PaperAccountID:      uintAttr(attrs, "paperAccountId"),
		Active:              boolAttrWithDefault(attrs, true, "active"),
		CopyRolesFromTeamID: uintPtrAttr(attrs, "copyRolesFromTeamId"),
	}, fields, true
}

func decodeResearchTeamRoleInput(w http.ResponseWriter, r *http.Request) (appresearch.RoleInput, bool) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return appresearch.RoleInput{}, false
	}
	toolNames, _ := stringSliceAttr(attrs, "toolNames")
	skillNames, _ := stringSliceAttr(attrs, "skillNames")
	model := nullableStringAttr(attrs, "model")
	return appresearch.RoleInput{
		Key:            stringAttr(attrs, "key"),
		Name:           stringAttr(attrs, "name"),
		Responsibility: stringAttr(attrs, "responsibility"),
		PromptTemplate: stringAttr(attrs, "promptTemplate"),
		ProviderID:     uintPtrAttr(attrs, "providerId"),
		Model:          model,
		ToolNames:      toolNames,
		SkillNames:     skillNames,
		Enabled:        boolAttrWithDefault(attrs, true, "enabled"),
		SortOrder:      intAttr(attrs, "sortOrder"),
	}, true
}

func researchTeamResource(row domainresearch.Team) jsonapi.Resource {
	resource := jsonapi.NewResource("research-teams", strconv.FormatUint(uint64(row.ID), 10), map[string]any{
		"name":           row.Name,
		"description":    row.Description,
		"paperAccountId": row.PaperAccountID,
		"active":         row.Active,
		"createdAt":      row.CreatedAt,
		"updatedAt":      row.UpdatedAt,
	})
	resource.Relationships = map[string]jsonapi.Relationship{
		"paperAccount": {Data: map[string]string{"type": "paper-accounts", "id": strconv.FormatUint(uint64(row.PaperAccountID), 10)}},
	}
	return resource
}

func researchTeamRoleResource(row domainresearch.TeamRole) jsonapi.Resource {
	resource := jsonapi.NewResource("research-team-roles", strconv.FormatUint(uint64(row.ResearchTeamID), 10)+":"+row.Key, map[string]any{
		"researchTeamId": row.ResearchTeamID,
		"key":            row.Key,
		"name":           row.Name,
		"responsibility": row.Responsibility,
		"promptTemplate": row.PromptTemplate,
		"providerId":     row.ProviderID,
		"model":          row.Model,
		"toolNames":      stringListFromJSON(row.ToolNames),
		"skillNames":     stringListFromJSON(row.SkillNames),
		"enabled":        row.Enabled,
		"sortOrder":      row.SortOrder,
	})
	resource.Relationships = map[string]jsonapi.Relationship{
		"researchTeam": {Data: map[string]string{"type": "research-teams", "id": strconv.FormatUint(uint64(row.ResearchTeamID), 10)}},
	}
	return resource
}

func writeResearchError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, appresearch.ErrTeamNotFound):
		writeJSONAPIError(w, http.StatusNotFound, "research-team-not-found", "Research team not found", err.Error(), "")
	case errors.Is(err, appresearch.ErrSubscriptionBound):
		writeJSONAPIError(w, http.StatusConflict, "research-team-subscription-bound", "Research team is still bound", err.Error(), "messageSubscriptions")
	case errors.Is(err, appresearch.ErrPaperAccountBound):
		writeJSONAPIError(w, http.StatusConflict, "paper-account-bound", "Paper account is already bound", err.Error(), "paperAccountId")
	default:
		writeJSONAPIError(w, http.StatusBadRequest, "research-team-invalid", "Research team invalid", err.Error(), "")
	}
}

func stringListFromJSON(raw []byte) []string {
	var values []string
	if len(raw) == 0 || json.Unmarshal(raw, &values) != nil {
		return []string{}
	}
	return values
}
