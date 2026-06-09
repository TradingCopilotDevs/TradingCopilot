package httptransport

import (
	"context"
	"net/http"
	"strings"

	appauth "github.com/TradingCopilotDevs/TradingCopilot/internal/app/auth"
)

type principalContextKey struct{}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		principal, err := s.auth.AuthenticatePrincipal(r.Context(), token)
		if err != nil {
			writeJSONAPIError(w, http.StatusUnauthorized, "invalid-access-token", "Invalid access token", "invalid access token", "")
			return
		}
		if !canAccessProtectedAPI(principal.Role, r.Method, r.URL.Path) {
			writeJSONAPIError(w, http.StatusForbidden, "permission-denied", "Permission denied", "role is not allowed to access this route", "")
			return
		}
		ctx := context.WithValue(r.Context(), principalContextKey{}, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	if header == "" {
		return ""
	}
	parts := strings.Fields(header)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return parts[1]
	}
	return ""
}

func currentPrincipal(r *http.Request) appauth.Principal {
	principal, _ := r.Context().Value(principalContextKey{}).(appauth.Principal)
	return principal
}

func requestMeta(r *http.Request) appauth.RequestMeta {
	principal := currentPrincipal(r)
	return appauth.RequestMeta{Actor: principal.Username, IP: r.RemoteAddr, UserAgent: r.UserAgent()}
}

func canUseAdminConsole(role string) bool {
	switch normalizeHTTPRole(role) {
	case "owner", "admin", "":
		return true
	default:
		return false
	}
}

func canAccessProtectedAPI(role string, method string, path string) bool {
	role = normalizeHTTPRole(role)
	path = normalizeAPIPath(path)
	if canUseAdminConsole(role) {
		return true
	}
	if role != "operator" && role != "viewer" {
		return false
	}
	if isAdminOnlyProtectedPath(path) {
		return false
	}
	if isReadMethod(method) {
		return true
	}
	if path == "/api/auth/logout" {
		return true
	}
	if role == "operator" {
		return canOperatorMutate(path)
	}
	return false
}

func canOperatorMutate(path string) bool {
	path = normalizeAPIPath(path)
	if hasRoutePrefix(path, "/api/message-subscriptions/telegram/login") {
		return false
	}
	for _, prefix := range []string{
		"/api/market",
		"/api/message-subscriptions",
		"/api/message-subscription-filters",
		"/api/ingested-messages",
		"/api/meetings",
		"/api/wake-plans",
		"/api/paper",
	} {
		if hasRoutePrefix(path, prefix) {
			return true
		}
	}
	return path == "/api/ops/jobs/retry-failed" || hasRoutePrefix(path, "/api/ops/jobs")
}

func isAdminOnlyProtectedPath(path string) bool {
	path = normalizeAPIPath(path)
	return hasRoutePrefix(path, "/api/admin") || path == "/api/audit-events"
}

func isReadMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func hasRoutePrefix(path string, prefix string) bool {
	path = normalizeAPIPath(path)
	prefix = strings.TrimRight(normalizeAPIPath(prefix), "/")
	return path == prefix || strings.HasPrefix(path, prefix+"/")
}

func normalizeAPIPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/"
	}
	path = strings.TrimRight(path, "/")
	if path == "" {
		return "/"
	}
	return path
}

func normalizeHTTPRole(role string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	if role == "" {
		return "admin"
	}
	return role
}
