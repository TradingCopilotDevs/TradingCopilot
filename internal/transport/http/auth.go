package httptransport

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	appauth "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/auth"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/transport/http/jsonapi"
)

type authPayload struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) bootstrapRequired(w http.ResponseWriter, r *http.Request) {
	required, err := s.auth.BootstrapRequired(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "auth-bootstrap-state-failed", "Bootstrap state failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("auth-bootstrap-states", "default", map[string]any{
		"bootstrapRequired": required,
	}))
}

func (s *Server) bootstrap(w http.ResponseWriter, r *http.Request) {
	var payload authPayload
	if !decodeJSONAPIRequest(w, r, &payload) {
		return
	}
	token, err := s.auth.Bootstrap(r.Context(), payload.Username, payload.Password)
	if err != nil {
		switch {
		case errors.Is(err, appauth.ErrAdminExists):
			writeJSONAPIError(w, http.StatusConflict, "admin-exists", "Admin already exists", "admin already exists", "")
		case errors.Is(err, appauth.ErrInvalidAdmin):
			writeJSONAPIError(w, http.StatusBadRequest, "invalid-admin", "Invalid admin", err.Error(), "")
		default:
			writeJSONAPIError(w, http.StatusBadRequest, "invalid-admin", "Invalid admin", err.Error(), "")
		}
		return
	}
	writeAuthToken(w, token)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var payload authPayload
	if !decodeJSONAPIRequest(w, r, &payload) {
		return
	}
	token, err := s.auth.Login(r.Context(), payload.Username, payload.Password)
	if err != nil {
		writeJSONAPIError(w, http.StatusUnauthorized, "invalid-credentials", "Invalid credentials", "invalid username or password", "")
		return
	}
	writeAuthToken(w, token)
}

func writeAuthToken(w http.ResponseWriter, token string) {
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("auth-tokens", "current", map[string]any{
		"accessToken": token,
		"tokenType":   "bearer",
	}))
}

func writeJSONAPIError(w http.ResponseWriter, status int, code string, message string, detail string, field string) {
	jsonapi.WriteError(w, status, jsonapi.ErrorObject{
		Code:    code,
		Message: message,
		Detail:  detail,
		Field:   field,
	})
}

func decodeJSONAPIRequest(w http.ResponseWriter, r *http.Request, target any) bool {
	if !requireJSONAPIContentType(w, r) {
		return false
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "invalid-json", "Invalid JSON", "invalid json", "body")
		return false
	}
	var envelope struct {
		Data struct {
			Type       string          `json:"type"`
			Attributes json.RawMessage `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "invalid-json", "Invalid JSON", "invalid json", "body")
		return false
	}
	if envelope.Data.Type == "" || len(envelope.Data.Attributes) == 0 {
		writeJSONAPIError(w, http.StatusBadRequest, "invalid-jsonapi-request", "Invalid JSON:API request", "request body must be a JSON:API resource object with data.type and data.attributes", "body")
		return false
	}
	if !attributesAreCamelCase(w, envelope.Data.Attributes) {
		return false
	}
	if err := json.Unmarshal(envelope.Data.Attributes, target); err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "invalid-json", "Invalid JSON", "invalid json", "body")
		return false
	}
	return true
}

func decodeJSONAPIAttributesMap(w http.ResponseWriter, r *http.Request) (map[string]any, bool) {
	if !requireJSONAPIContentType(w, r) {
		return nil, false
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "invalid-json", "Invalid JSON", "invalid json", "body")
		return nil, false
	}
	var envelope struct {
		Data struct {
			Type       string         `json:"type"`
			Attributes map[string]any `json:"attributes"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "invalid-json", "Invalid JSON", "invalid json", "body")
		return nil, false
	}
	if envelope.Data.Type == "" || envelope.Data.Attributes == nil {
		writeJSONAPIError(w, http.StatusBadRequest, "invalid-jsonapi-request", "Invalid JSON:API request", "request body must be a JSON:API resource object with data.type and data.attributes", "body")
		return nil, false
	}
	if !attributesMapKeysAreCamelCase(w, envelope.Data.Attributes) {
		return nil, false
	}
	return envelope.Data.Attributes, true
}

func requireJSONAPIContentType(w http.ResponseWriter, r *http.Request) bool {
	contentType := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, jsonapi.ContentType) {
		return true
	}
	writeJSONAPIError(w, http.StatusUnsupportedMediaType, "unsupported-media-type", "Unsupported media type", "request body must use application/vnd.api+json", "Content-Type")
	return false
}

func attributesAreCamelCase(w http.ResponseWriter, raw json.RawMessage) bool {
	var attrs map[string]any
	if err := json.Unmarshal(raw, &attrs); err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "invalid-json", "Invalid JSON", "invalid json", "body")
		return false
	}
	return attributesMapKeysAreCamelCase(w, attrs)
}

func attributesMapKeysAreCamelCase(w http.ResponseWriter, attrs map[string]any) bool {
	for key := range attrs {
		if strings.Contains(key, "_") {
			writeJSONAPIError(w, http.StatusBadRequest, "invalid-jsonapi-request", "Invalid JSON:API request", "attribute names must be lower camelCase", key)
			return false
		}
	}
	return true
}
