package composition

import (
	"context"
	"errors"
	"io"
	"net/http"

	appai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/ai"
	appauth "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/auth"
	appdashboard "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/dashboard"
	applogging "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/logging"
	appmarket "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/market"
	appmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/meeting"
	appmessaging "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/messaging"
	apppaper "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/paper"
	appresearch "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/research"
	appruntime "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/runtime"
	appsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/settings"
	appwake "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/wake"
	domainmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/meeting"
	infraai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/ai"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	infralogging "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/logging"
	inframarketdata "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/marketdata"
	inframessaging "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/messaging"
	infradashboard "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/dashboard"
	gormmarketdata "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/marketdata"
	inframeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/meeting"
	infrapaper "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/paper"
	infraproxy "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/proxy"
	runtimeproxy "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
	infraruntime "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/runtime"
	gormuow "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/uow"
	infraqueue "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/queue"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/security"
	httptransport "github.com/TreadingCopilotDevs/TreadingCopilot/internal/transport/http"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var LocalMeetingStarter = infraruntime.StartLocalMeetingRun

func Serve(settings config.Settings) error {
	db, err := infraruntime.OpenMigratedDB(settings)
	if err != nil {
		return err
	}
	sec := security.New(settings)
	ctx := context.Background()
	messageQueue := messageTaskQueue(settings)
	runtime := infraruntime.New(settings, db, sec).WithLocalMeetingStarter(LocalMeetingStarter).WithMessageTaskQueue(messageQueue)
	if localQueue, ok := messageQueue.(*infraqueue.LocalMessageTaskQueue); ok {
		localQueue.SetHandlers(messageTaskHandlers(settings, db, sec, messageQueue))
		go localQueue.Run(ctx, 2)
	}
	server := &http.Server{
		Addr:    settings.HTTPAddr,
		Handler: httptransport.New(httpDependencies(settings, db, sec, messageQueue)).Router(),
	}
	infralogging.Logger().Info("TreadingCopilot listening",
		infralogging.Event("app.lifecycle"),
		infralogging.Group("app"),
		infralogging.Method("serve"),
		infralogging.Status(infralogging.StatusOK),
		zap.Any(infralogging.FieldDuration, nil),
		zap.String("addr", settings.HTTPAddr),
	)
	return appruntime.Serve(ctx, appruntime.ServeOptions{
		Runtime:                             runtime,
		Server:                              server,
		MessageSubscriptionListenersInServe: settings.MessageSubscriptionListenersInServe,
		RunLocalBackgroundLoops:             infraruntime.ShouldRunLocalBackgroundLoops(settings),
		ExpectedShutdownError: func(err error) bool {
			return errors.Is(err, http.ErrServerClosed)
		},
	})
}

func RunWorker(settings config.Settings) error {
	return infraruntime.RunWorker(settings)
}

func RunScheduler(settings config.Settings) error {
	return infraruntime.RunScheduler(settings)
}

func Migrate(settings config.Settings) error {
	return infraruntime.Migrate(settings)
}

func RuntimeUsecase(settings config.Settings, in io.Reader, out io.Writer) (appruntime.Usecase, error) {
	runtime, err := newRuntime(settings)
	if err != nil {
		return appruntime.Usecase{}, err
	}
	return appruntime.NewUsecase(runtime, in, out), nil
}

func RunMessageSubscriptionListener(settings config.Settings) error {
	runtime, err := newRuntime(settings)
	if err != nil {
		return err
	}
	return runtime.RunMessageSubscriptionListenerCommand(context.Background())
}

func newRuntime(settings config.Settings) (infraruntime.Runner, error) {
	runtime, err := infraruntime.NewMigrated(settings)
	if err != nil {
		return infraruntime.Runner{}, err
	}
	return runtime.WithLocalMeetingStarter(LocalMeetingStarter).WithMessageTaskQueue(messageTaskQueue(settings)), nil
}

func httpDependencies(settings config.Settings, db *gorm.DB, sec security.Service, messageQueue appmessaging.TaskQueue) httptransport.Dependencies {
	return httptransport.Dependencies{
		Settings:  httpSettings(settings),
		Auth:      appauth.NewUsecase(gormrepo.NewAuthRepository(db), sec, gormuow.NewAuthUnitOfWork(db)),
		AI:        appai.NewUsecase(gormrepo.NewAIRepository(db), sec, infraai.NewModelSyncer(settings, sec, runtimeproxy.NewHTTPClient(db, settings, runtimeproxy.ModuleAI, 0)), gormuow.NewAIUnitOfWork(db)),
		Dashboard: appdashboard.NewUsecase(infradashboard.NewLoader(db, settings), appdashboard.Settings{AppName: settings.AppName, AppEnv: settings.AppEnv}),
		Logs:      applogging.NewUsecase(infralogging.NewReader(settings)),
		Market:    appmarket.NewUsecase(gormrepo.NewMarketRepository(db), inframarketdata.NewService(settings, gormmarketdata.NewStore(db, settings, sec)), gormuow.NewMarketUnitOfWork(db, settings)),
		Meeting: appmeeting.NewUsecase(gormrepo.NewMeetingRepository(db), inframeeting.NewService(db), appmeeting.Settings{
			MeetingDispatchMode: settings.MeetingDispatchMode,
		}, gormuow.NewMeetingUnitOfWork(db)).WithMeetingEnqueuer(func(meetingID uint) error {
			return infraqueue.EnqueueRunMeeting(settings, meetingID)
		}),
		Messaging: appmessaging.NewUsecase(gormrepo.NewMessagingRepository(db), inframessaging.NewService(settings), sec, gormuow.NewMessagingUnitOfWork(db, settings)).WithTaskQueue(messageQueue),
		Paper:     apppaper.NewUsecase(gormrepo.NewPaperRepository(db), infrapaper.NewService(db), gormuow.NewPaperUnitOfWork(db)),
		Research:  appresearch.NewUsecase(gormrepo.NewResearchRepository(db), gormuow.NewResearchUnitOfWork(db)),
		AppConfig: appsettings.NewUsecase(gormrepo.NewSettingsRepository(db), sec, appSettings(settings), gormuow.NewSettingsUnitOfWork(db), config.WriteEnvOverrides).WithProxyTester(infraproxy.NewTester(db, settings, sec)),
		Wake:      appwake.NewUsecase(gormrepo.NewWakeRepository(db), inframarketdata.NewService(settings, gormmarketdata.NewStore(db, settings, sec)), gormuow.NewWakeUnitOfWork(db, settings)),
	}
}

func messageTaskQueue(settings config.Settings) appmessaging.TaskQueue {
	mode := settings.MessageTaskQueueMode
	if mode == "" || mode == "auto" {
		if settings.MeetingDispatchMode == "local" || settings.MeetingDispatchMode == "" {
			mode = "local"
		} else {
			mode = "redis"
		}
	}
	if mode == "local" {
		return infraqueue.NewLocalMessageTaskQueue(256)
	}
	return infraqueue.NewRedisMessageTaskQueue(settings)
}

func messageTaskHandlers(settings config.Settings, db *gorm.DB, sec security.Service, queue appmessaging.TaskQueue) infraqueue.MessageTaskHandlers {
	messagingUsecase := appmessaging.NewUsecase(gormrepo.NewMessagingRepository(db), inframessaging.NewService(settings), sec, gormuow.NewMessagingUnitOfWork(db, settings)).WithTaskQueue(queue)
	meetingUsecase := appmeeting.NewUsecase(gormrepo.NewMeetingRepository(db), inframeeting.NewService(db), appmeeting.Settings{
		MeetingDispatchMode: settings.MeetingDispatchMode,
	}, gormuow.NewMeetingUnitOfWork(db)).WithMeetingEnqueuer(func(meetingID uint) error {
		return infraqueue.EnqueueRunMeeting(settings, meetingID)
	})
	return infraqueue.MessageTaskHandlers{
		Collect: func(ctx context.Context, task appmessaging.CollectTask) error {
			result, _, err := messagingUsecase.ProcessCollectTask(ctx, task)
			if err != nil {
				return err
			}
			return dispatchMessageMeetings(ctx, meetingUsecase, result.CreatedMeetings)
		},
		Filter: func(ctx context.Context, task appmessaging.FilterTask) error {
			result, _, err := messagingUsecase.ProcessFilterTask(ctx, task)
			if err != nil || result == nil {
				return err
			}
			return dispatchMessageMeetings(ctx, meetingUsecase, result.CreatedMeetings)
		},
	}
}

func dispatchMessageMeetings(ctx context.Context, meetingUsecase appmeeting.Usecase, meetings []*domainmeeting.Meeting) error {
	for _, meeting := range meetings {
		if meeting == nil {
			continue
		}
		if _, err := meetingUsecase.DispatchRun(ctx, meeting, "queued", "", nil); err != nil {
			return err
		}
	}
	return nil
}

func httpSettings(settings config.Settings) httptransport.Settings {
	return httptransport.Settings{
		AppName:      settings.AppName,
		AppEnv:       settings.AppEnv,
		CORSOrigins:  settings.CORSOrigins,
		FrontendDist: settings.FrontendDist,
	}
}

func appSettings(settings config.Settings) appsettings.RuntimeSettings {
	return appsettings.RuntimeSettings{
		DatabaseURL:                  settings.DatabaseURL,
		RedisURL:                     settings.RedisURL,
		PublicBaseURL:                settings.PublicBaseURL,
		DefaultMarketProvider:        settings.DefaultMarketProvider,
		MarketRealtimeProvider:       settings.MarketRealtimeProvider,
		MarketRealtimeCompatProvider: settings.MarketRealtimeCompatProvider,
		MarketRealtimeCacheTTL:       settings.MarketRealtimeCacheTTL,
		MeetingMaxRounds:             settings.MeetingMaxRounds,
		MeetingDailyTokenBudget:      settings.MeetingDailyTokenBudget,
		ToolResultLimit:              settings.ToolResultLimit,
		SQLStatementTimeoutMillis:    settings.SQLStatementTimeoutMillis,
		LogDir:                       settings.LogDir,
		LogLevel:                     settings.LogLevel,
		LogRotationMode:              settings.LogRotationMode,
		LogRotationSizeMB:            settings.LogRotationSizeMB,
		LogRotationTotalSizeMB:       settings.LogRotationTotalSizeMB,
		LogRotationMaxAgeDays:        settings.LogRotationMaxAgeDays,
		RuntimeEnvFile:               settings.RuntimeEnvFile,
	}
}
