package httptransport

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	appauth "github.com/TradingCopilotDevs/TradingCopilot/internal/app/auth"
	domainauth "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/auth"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/transport/http/jsonapi"
)

func (s *Server) listAdminUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.auth.ListUsers(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "admin-users-list-failed", "Admin users list failed", err.Error(), "")
		return
	}
	resources := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		resources = append(resources, adminUserResource(row))
	}
	writeResourceCollection(w, r, resources, 100, 200)
}

func (s *Server) createAdminUser(w http.ResponseWriter, r *http.Request) {
	var input appauth.CreateUserInput
	if !decodeJSONAPIRequest(w, r, &input) {
		return
	}
	row, err := s.auth.CreateUser(r.Context(), input, requestMeta(r))
	if err != nil {
		writeAdminError(w, err, "admin-user-create-failed", "Admin user create failed")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, adminUserResource(*row))
}

func (s *Server) updateAdminUser(w http.ResponseWriter, r *http.Request) {
	var input appauth.UpdateUserInput
	if !decodeJSONAPIRequest(w, r, &input) {
		return
	}
	row, err := s.auth.UpdateUser(r.Context(), uintParam(r, "userId"), input, requestMeta(r))
	if err != nil {
		writeAdminError(w, err, "admin-user-update-failed", "Admin user update failed")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, adminUserResource(*row))
}

func (s *Server) resetAdminUserPassword(w http.ResponseWriter, r *http.Request) {
	var input appauth.ResetPasswordInput
	if !decodeJSONAPIRequest(w, r, &input) {
		return
	}
	row, err := s.auth.ResetPassword(r.Context(), uintParam(r, "userId"), input, requestMeta(r))
	if err != nil {
		writeAdminError(w, err, "admin-user-password-reset-failed", "Admin user password reset failed")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, adminUserResource(*row))
}

func (s *Server) listAdminSessions(w http.ResponseWriter, r *http.Request) {
	includeRevoked, _ := strconv.ParseBool(strings.TrimSpace(r.URL.Query().Get("includeRevoked")))
	page := jsonapi.ParsePage(r, 100, 200)
	rows, err := s.auth.ListSessions(r.Context(), strings.TrimSpace(r.URL.Query().Get("username")), includeRevoked, page.Limit)
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "admin-sessions-list-failed", "Admin sessions list failed", err.Error(), "")
		return
	}
	resources := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		resources = append(resources, adminSessionResource(row))
	}
	writeResourceCollection(w, r, resources, 100, 200)
}

func (s *Server) revokeAdminSession(w http.ResponseWriter, r *http.Request) {
	var input appauth.RevokeSessionInput
	if !decodeJSONAPIRequest(w, r, &input) {
		return
	}
	row, err := s.auth.RevokeSession(r.Context(), uintParam(r, "sessionId"), input, requestMeta(r))
	if err != nil {
		writeAdminError(w, err, "admin-session-revoke-failed", "Admin session revoke failed")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, adminSessionResource(*row))
}

func (s *Server) listAuditEvents(w http.ResponseWriter, r *http.Request) {
	page := jsonapi.ParsePage(r, 100, 500)
	rows, err := s.auth.ListAuditEvents(r.Context(), page.Limit)
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "audit-events-list-failed", "Audit events list failed", err.Error(), "")
		return
	}
	resources := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		resources = append(resources, auditEventResource(row))
	}
	writeResourceCollection(w, r, resources, 100, 500)
}

func adminUserResource(row domainauth.AdminUser) jsonapi.Resource {
	return jsonapi.NewResource("admin-users", strconv.FormatUint(uint64(row.ID), 10), map[string]any{
		"username":    row.Username,
		"displayName": firstNonEmptyString(row.DisplayName, row.Username),
		"role":        row.Role,
		"active":      row.Active,
		"lastLoginAt": row.LastLoginAt,
		"createdAt":   row.CreatedAt,
		"updatedAt":   row.UpdatedAt,
	})
}

func adminSessionResource(row domainauth.AuthSession) jsonapi.Resource {
	return jsonapi.NewResource("auth-sessions", strconv.FormatUint(uint64(row.ID), 10), map[string]any{
		"username":     row.Username,
		"userId":       row.UserID,
		"role":         row.Role,
		"ip":           row.IP,
		"userAgent":    row.UserAgent,
		"status":       sessionStatus(row),
		"expiresAt":    row.ExpiresAt,
		"revokedAt":    row.RevokedAt,
		"revokedBy":    row.RevokedBy,
		"revokeReason": row.RevokeReason,
		"createdAt":    row.CreatedAt,
		"updatedAt":    row.UpdatedAt,
	})
}

func auditEventResource(row domainauth.AuditEvent) jsonapi.Resource {
	return jsonapi.NewResource("audit-events", strconv.FormatUint(uint64(row.ID), 10), map[string]any{
		"actor":        row.Actor,
		"action":       row.Action,
		"resourceType": row.ResourceType,
		"resourceId":   row.ResourceID,
		"outcome":      row.Outcome,
		"detail":       row.Detail,
		"ip":           row.IP,
		"userAgent":    row.UserAgent,
		"createdAt":    row.CreatedAt,
	})
}

func sessionStatus(row domainauth.AuthSession) string {
	if row.RevokedAt != nil {
		return "revoked"
	}
	if !row.ExpiresAt.IsZero() && time.Now().After(row.ExpiresAt) {
		return "expired"
	}
	return "active"
}

func writeAdminError(w http.ResponseWriter, err error, fallbackCode string, fallbackTitle string) {
	switch {
	case errors.Is(err, appauth.ErrInvalidUser), errors.Is(err, appauth.ErrInvalidAdmin):
		writeJSONAPIError(w, http.StatusBadRequest, "invalid-admin-user", "Invalid admin user", err.Error(), "")
	case errors.Is(err, appauth.ErrUserExists):
		writeJSONAPIError(w, http.StatusConflict, "admin-user-exists", "Admin user exists", err.Error(), "")
	case errors.Is(err, appauth.ErrUserNotFound), errors.Is(err, appauth.ErrSessionNotFound):
		writeJSONAPIError(w, http.StatusNotFound, "admin-resource-not-found", "Admin resource not found", err.Error(), "")
	case errors.Is(err, appauth.ErrConfirmationRequired):
		writeJSONAPIError(w, http.StatusBadRequest, "confirmation-required", "Confirmation required", err.Error(), "confirm")
	default:
		writeJSONAPIError(w, http.StatusInternalServerError, fallbackCode, fallbackTitle, err.Error(), "")
	}
}
