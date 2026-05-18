package httptransport

import (
	"encoding/json"
	"errors"
	"fmt"
	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	"net/http"
	"strconv"
	"strings"

	appai "github.com/TradingCopilotDevs/TradingCopilot/internal/app/ai"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/transport/http/jsonapi"
	"github.com/go-chi/chi/v5"
)

func (s *Server) listProviders(w http.ResponseWriter, r *http.Request) {
	rows, err := s.aiUsecase.ListProviders(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "ai-providers-load-failed", "AI providers load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, aiProviderResource(row))
	}
	writeResourceCollection(w, r, out, 100, 500)
}

func (s *Server) createProvider(w http.ResponseWriter, r *http.Request) {
	payload, ok := decodeProviderPayload(w, r)
	if !ok {
		return
	}
	row, err := s.aiUsecase.CreateProvider(r.Context(), payload)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "ai-provider-save-failed", "AI provider save failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, aiProviderResource(*row))
}

func (s *Server) updateProvider(w http.ResponseWriter, r *http.Request) {
	id := uintParam(r, "providerId")
	payload, ok := decodeProviderPayload(w, r)
	if !ok {
		return
	}
	row, err := s.aiUsecase.UpdateProvider(r.Context(), id, payload)
	if err != nil {
		if errors.Is(err, appai.ErrProviderNotFound) {
			writeJSONAPIError(w, http.StatusNotFound, "ai-provider-not-found", "AI provider not found", "provider not found", "")
			return
		}
		writeJSONAPIError(w, http.StatusBadRequest, "ai-provider-save-failed", "AI provider save failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, aiProviderResource(*row))
}

func (s *Server) listProviderModels(w http.ResponseWriter, r *http.Request) {
	rows, err := s.aiUsecase.ListProviderModels(r.Context(), uintParam(r, "providerId"))
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "ai-provider-models-load-failed", "AI provider models load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, aiProviderModelResource(row))
	}
	writeResourceCollection(w, r, out, 100, 500)
}

func (s *Server) syncProviderModels(w http.ResponseWriter, r *http.Request) {
	rows, err := s.aiUsecase.SyncProviderModels(r.Context(), uintParam(r, "providerId"))
	if err != nil {
		if errors.Is(err, appai.ErrProviderNotFound) {
			writeJSONAPIError(w, http.StatusNotFound, "ai-provider-not-found", "AI provider not found", "provider not found", "")
			return
		}
		writeJSONAPIError(w, http.StatusBadRequest, "ai-model-sync-failed", "AI model sync failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, aiProviderModelResource(row))
	}
	writeResourceCollection(w, r, out, 100, 500)
}

func (s *Server) toolDefinitions(w http.ResponseWriter, r *http.Request) {
	writeResourceCollection(w, r, capabilityDefinitionResources("ai-tool-definitions", s.aiUsecase.ToolDefinitions(r.Context())), 100, 500)
}

func (s *Server) skillDefinitions(w http.ResponseWriter, r *http.Request) {
	writeResourceCollection(w, r, capabilityDefinitionResources("ai-skill-definitions", s.aiUsecase.SkillDefinitions(r.Context())), 100, 500)
}

func (s *Server) listRoles(w http.ResponseWriter, r *http.Request) {
	rows, err := s.aiUsecase.ListRoles(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "ai-roles-load-failed", "AI roles load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, aiRoleResource(row))
	}
	writeResourceCollection(w, r, out, 100, 500)
}

func (s *Server) applyDefaultRoleCapabilities(w http.ResponseWriter, r *http.Request) {
	updated, err := s.aiUsecase.ApplyDefaultRoleCapabilities(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "ai-role-capability-update-failed", "AI role capability update failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("ai-role-capability-updates", "default", map[string]any{"updated": updated}))
}

func (s *Server) upsertRole(w http.ResponseWriter, r *http.Request) {
	payload, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return
	}
	row, err := s.aiUsecase.UpsertRole(r.Context(), chi.URLParam(r, "roleKey"), appai.RoleInput{Attributes: payload})
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "ai-role-save-failed", "AI role save failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, aiRoleResource(*row))
}

func decodeProviderPayload(w http.ResponseWriter, r *http.Request) (appai.ProviderInput, bool) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return appai.ProviderInput{}, false
	}
	payload := appai.ProviderInput{
		Name:         stringAttr(attrs, "name"),
		BaseURL:      stringAttr(attrs, "baseUrl"),
		DefaultModel: stringAttr(attrs, "defaultModel"),
		Enabled:      boolAttrWithDefault(attrs, true, "enabled"),
	}
	if value, exists := attrValue(attrs, "apiKey"); exists && value != nil {
		apiKey := fmt.Sprint(value)
		payload.APIKey = &apiKey
	}
	return payload, true
}

func aiProviderResource(provider domainai.Provider) jsonapi.Resource {
	return jsonapi.NewResource("ai-providers", strconv.FormatUint(uint64(provider.ID), 10), map[string]any{
		"name":         provider.Name,
		"baseUrl":      provider.BaseURL,
		"defaultModel": provider.DefaultModel,
		"hasApiKey":    provider.APIKeySecretID != nil,
		"enabled":      provider.Enabled,
		"createdAt":    provider.CreatedAt,
	})
}

func aiProviderModelResource(model domainai.ProviderModel) jsonapi.Resource {
	resource := jsonapi.NewResource("ai-provider-models", strconv.FormatUint(uint64(model.ID), 10), map[string]any{
		"providerId":  model.ProviderID,
		"modelId":     model.ModelID,
		"displayName": model.DisplayName,
		"ownedBy":     model.OwnedBy,
		"enabled":     model.Enabled,
		"raw":         rawJSONValue(model.Raw),
		"createdAt":   model.CreatedAt,
		"updatedAt":   model.UpdatedAt,
	})
	resource.Relationships = map[string]jsonapi.Relationship{
		"provider": {Data: map[string]string{"type": "ai-providers", "id": strconv.FormatUint(uint64(model.ProviderID), 10)}},
	}
	return resource
}

func capabilityDefinitionResources(resourceType string, definitions []appai.CapabilityDefinition) []jsonapi.Resource {
	out := make([]jsonapi.Resource, 0, len(definitions))
	for _, definition := range definitions {
		out = append(out, jsonapi.NewResource(resourceType, definition.Key, map[string]any{
			"key":         definition.Key,
			"title":       definition.Title,
			"description": definition.Description,
			"category":    definition.Category,
		}))
	}
	return out
}

func aiRoleResource(role domainai.AgentRole) jsonapi.Resource {
	resource := jsonapi.NewResource("ai-roles", role.Key, map[string]any{
		"key":            role.Key,
		"name":           role.Name,
		"responsibility": role.Responsibility,
		"promptTemplate": role.PromptTemplate,
		"providerId":     role.ProviderID,
		"model":          role.Model,
		"toolNames":      stringSliceFromJSON(role.ToolNames),
		"skillNames":     stringSliceFromJSON(role.SkillNames),
		"enabled":        role.Enabled,
		"sortOrder":      role.SortOrder,
	})
	if role.ProviderID != nil {
		resource.Relationships = map[string]jsonapi.Relationship{
			"provider": {Data: map[string]string{"type": "ai-providers", "id": strconv.FormatUint(uint64(*role.ProviderID), 10)}},
		}
	}
	return resource
}

func rawJSONValue(raw []byte) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}

func stringSliceFromJSON(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err == nil {
		return values
	}
	var generic []any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return []string{}
	}
	out := make([]string, 0, len(generic))
	for _, value := range generic {
		out = append(out, fmt.Sprint(value))
	}
	return out
}

func uintParam(r *http.Request, key string) uint {
	value, _ := strconv.ParseUint(chi.URLParam(r, key), 10, 64)
	return uint(value)
}

func stringParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}

func stringAttr(attrs map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := attrs[key]; ok {
			return fmt.Sprint(value)
		}
	}
	return ""
}

func attrValue(attrs map[string]any, keys ...string) (any, bool) {
	for _, key := range keys {
		value, ok := attrs[key]
		if ok {
			return value, true
		}
	}
	return nil, false
}

func boolAttr(attrs map[string]any, keys ...string) bool {
	for _, key := range keys {
		if value, ok := attrs[key]; ok {
			switch typed := value.(type) {
			case bool:
				return typed
			case string:
				parsed, _ := strconv.ParseBool(strings.TrimSpace(typed))
				return parsed
			default:
				return fmt.Sprint(value) == "true"
			}
		}
	}
	return false
}

func boolAttrWithDefault(attrs map[string]any, defaultValue bool, keys ...string) bool {
	if _, ok := attrValue(attrs, keys...); !ok {
		return defaultValue
	}
	return boolAttr(attrs, keys...)
}
