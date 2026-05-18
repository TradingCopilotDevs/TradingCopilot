package httptransport

import (
	"net/http"
	"strings"

	appai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/ai"
	appauth "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/auth"
	appdashboard "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/dashboard"
	applogging "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/logging"
	appmarket "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/market"
	appmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/meeting"
	appmessaging "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/messaging"
	apppaper "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/paper"
	appresearch "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/research"
	appsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/settings"
	appwake "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/wake"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/transport/http/jsonapi"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/transport/http/openapi"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

type Dependencies struct {
	Settings  Settings
	Auth      appauth.Usecase
	AI        appai.Usecase
	Dashboard appdashboard.Usecase
	Logs      applogging.Usecase
	Market    appmarket.Usecase
	Meeting   appmeeting.Usecase
	Messaging appmessaging.Usecase
	Paper     apppaper.Usecase
	Research  appresearch.Usecase
	AppConfig appsettings.Usecase
	Wake      appwake.Usecase
}

type Settings struct {
	AppName      string
	AppEnv       string
	CORSOrigins  []string
	FrontendDist string
}

type Server struct {
	settings         Settings
	auth             appauth.Usecase
	aiUsecase        appai.Usecase
	dashboardUsecase appdashboard.Usecase
	loggingUsecase   applogging.Usecase
	marketUsecase    appmarket.Usecase
	meetingUsecase   appmeeting.Usecase
	messagingUsecase appmessaging.Usecase
	paperUsecase     apppaper.Usecase
	researchUsecase  appresearch.Usecase
	settingsUsecase  appsettings.Usecase
	wakeUsecase      appwake.Usecase
}

func New(deps Dependencies) *Server {
	return &Server{
		settings:         deps.Settings,
		auth:             deps.Auth,
		aiUsecase:        deps.AI,
		dashboardUsecase: deps.Dashboard,
		loggingUsecase:   deps.Logs,
		marketUsecase:    deps.Market,
		meetingUsecase:   deps.Meeting,
		messagingUsecase: deps.Messaging,
		paperUsecase:     deps.Paper,
		researchUsecase:  deps.Research,
		settingsUsecase:  deps.AppConfig,
		wakeUsecase:      deps.Wake,
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(s.middleware()...)
	openapi.HandlerWithOptions(s, openapi.ChiServerOptions{
		BaseURL:    "/api",
		BaseRouter: r,
		Middlewares: []openapi.MiddlewareFunc{
			s.requireProtectedAPI,
		},
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeJSONAPIError(w, http.StatusBadRequest, "invalid-request", "Invalid request", err.Error(), "")
		},
	})
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") {
			writeJSONAPIError(w, http.StatusNotFound, "not-found", "Not found", "api route not found", "")
			return
		}
		s.frontend(w, req)
	})
	return r
}

func (s *Server) middleware() []func(http.Handler) http.Handler {
	return []func(http.Handler) http.Handler{
		middleware.RequestID,
		middleware.RealIP,
		recoverLogger,
		requestLogger,
		cors.Handler(cors.Options{
			AllowedOrigins:   s.settings.CORSOrigins,
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
			AllowCredentials: true,
		}),
	}
}

func (s *Server) requireProtectedAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isPublicAPI(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		s.requireAdmin(next).ServeHTTP(w, r)
	})
}

func isPublicAPI(path string) bool {
	switch path {
	case "/api/health", "/api/auth/bootstrap-required", "/api/auth/bootstrap", "/api/auth/login":
		return true
	default:
		return false
	}
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("health-checks", "api", map[string]any{
		"status": "ok",
		"app":    s.settings.AppName,
		"env":    s.settings.AppEnv,
	}))
}
