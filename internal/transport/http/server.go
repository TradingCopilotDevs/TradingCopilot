package httptransport

import (
	"net/http"
	"strings"
	"time"

	appai "github.com/TradingCopilotDevs/TradingCopilot/internal/app/ai"
	appauth "github.com/TradingCopilotDevs/TradingCopilot/internal/app/auth"
	appdashboard "github.com/TradingCopilotDevs/TradingCopilot/internal/app/dashboard"
	applogging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/logging"
	appmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/app/market"
	appmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/app/meeting"
	appmessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/messaging"
	appops "github.com/TradingCopilotDevs/TradingCopilot/internal/app/ops"
	apppaper "github.com/TradingCopilotDevs/TradingCopilot/internal/app/paper"
	appresearch "github.com/TradingCopilotDevs/TradingCopilot/internal/app/research"
	appsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/app/settings"
	appwake "github.com/TradingCopilotDevs/TradingCopilot/internal/app/wake"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/transport/http/jsonapi"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/transport/http/openapi"
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
	Backup    appops.BackupArchiveStore
}

type Settings struct {
	AppName                    string
	AppEnv                     string
	CORSOrigins                []string
	FrontendDist               string
	DatabaseURL                string
	RedisURL                   string
	MeetingMode                string
	LogDir                     string
	RuntimeEnvFile             string
	BackupArchiveProvider      string
	BackupArchiveS3Bucket      string
	BackupArchiveS3Region      string
	BackupArchiveS3Endpoint    string
	BackupArchiveS3AccessKeyID string
	BackupArchiveS3SecretKey   string
	BackupArchiveS3Prefix      string
	BackupArchiveOSSBucket     string
	BackupArchiveOSSRegion     string
	BackupArchiveOSSEndpoint   string
	BackupArchiveOSSAccessKey  string
	BackupArchiveOSSSecretKey  string
	BackupArchiveOSSPrefix     string
	BackupCopies               int
	BackupDays                 int
	BackupRestoreDrillInterval time.Duration
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
	backupStore      appops.BackupArchiveStore
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
		backupStore:      deps.Backup,
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
		traceRequests,
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
		s.requireAdmin(s.auditProtectedMutation(next)).ServeHTTP(w, r)
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
