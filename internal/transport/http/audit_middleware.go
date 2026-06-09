package httptransport

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	domainauth "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/auth"
	"go.uber.org/zap"
)

func (s *Server) auditProtectedMutation(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isMutationMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		meta := requestMeta(r)
		event := domainauth.AuditEvent{
			Actor:        meta.Actor,
			Action:       "http." + strings.ToLower(r.Method),
			ResourceType: auditResourceType(r.URL.Path),
			ResourceID:   auditResourceID(r.URL.Path),
			Outcome:      auditOutcome(status),
			Detail:       fmt.Sprintf("%s %s -> %d", r.Method, r.URL.Path, status),
			IP:           meta.IP,
			UserAgent:    meta.UserAgent,
			CreatedAt:    time.Now(),
		}
		if err := s.auth.RecordAudit(r.Context(), event); err != nil {
			zap.L().Warn("audit event write failed",
				zap.String("source", "http"),
				zap.String("event", "audit.write_failed"),
				zap.String("group", "auth"),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.String("status", "warning"),
				zap.Any("durationMs", nil),
				zap.Error(err),
			)
		}
	})
}

func isMutationMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func auditOutcome(status int) string {
	if status >= 200 && status < 400 {
		return "ok"
	}
	return "error"
}

func auditResourceType(path string) string {
	parts := auditPathParts(path)
	if len(parts) == 0 {
		return "api"
	}
	resource := []string{parts[0]}
	for i := 1; i < len(parts); i++ {
		if isPathIdentifier(parts[i]) {
			continue
		}
		resource = append(resource, parts[i])
		if len(resource) >= 2 {
			break
		}
	}
	return strings.Join(resource, ".")
}

func auditResourceID(path string) string {
	for _, part := range auditPathParts(path) {
		if isPathIdentifier(part) {
			return part
		}
	}
	return ""
}

func auditPathParts(path string) []string {
	path = strings.TrimPrefix(strings.TrimSpace(path), "/api/")
	if path == "" || strings.HasPrefix(path, "/") {
		return nil
	}
	raw := strings.Split(path, "/")
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func isPathIdentifier(value string) bool {
	if value == "" {
		return false
	}
	if _, err := strconv.ParseUint(value, 10, 64); err == nil {
		return true
	}
	if strings.Contains(value, ".") || strings.Contains(value, "@") {
		return true
	}
	if len(value) >= 6 && strings.IndexFunc(value, func(r rune) bool {
		return !(r >= '0' && r <= '9') && !(r >= 'a' && r <= 'f') && !(r >= 'A' && r <= 'F') && r != '-'
	}) == -1 {
		return true
	}
	return false
}
