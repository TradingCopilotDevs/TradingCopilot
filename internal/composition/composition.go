package composition

import (
	"context"
	"errors"
	"io"
	"net/http"

	appai "github.com/TradingCopilotDevs/TradingCopilot/internal/app/ai"
	appauth "github.com/TradingCopilotDevs/TradingCopilot/internal/app/auth"
	appdashboard "github.com/TradingCopilotDevs/TradingCopilot/internal/app/dashboard"
	applogging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/logging"
	appmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/app/market"
	appmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/app/meeting"
	appmessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/messaging"
	apppaper "github.com/TradingCopilotDevs/TradingCopilot/internal/app/paper"
	appresearch "github.com/TradingCopilotDevs/TradingCopilot/internal/app/research"
	appruntime "github.com/TradingCopilotDevs/TradingCopilot/internal/app/runtime"
	appsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/app/settings"
	appwake "github.com/TradingCopilotDevs/TradingCopilot/internal/app/wake"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	infraai "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/ai"
	infrabackup "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/backup"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	infralogging "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/logging"
	inframarketdata "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/marketdata"
	inframessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/messaging"
	infradashboard "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/dashboard"
	gormmarketdata "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/marketdata"
	inframeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/meeting"
	infrapaper "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/paper"
	infraproxy "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/proxy"
	runtimeproxy "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	infraruntime "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/runtime"
	gormuow "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/uow"
	infraqueue "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/queue"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	httptransport "github.com/TradingCopilotDevs/TradingCopilot/internal/transport/http"
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
	infralogging.Logger().Info("TradingCopilot listening",
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
		Market:    appmarket.NewUsecase(gormrepo.NewMarketRepository(db), inframarketdata.NewService(settings, gormmarketdata.NewStore(db, settings, sec)), gormuow.NewMarketUnitOfWork(db, settings)).WithTaskQueue(infraqueue.NewRedisMarketTaskQueue(settings)),
		Meeting: appmeeting.NewUsecase(gormrepo.NewMeetingRepository(db), inframeeting.NewService(db), appmeeting.Settings{
			MeetingDispatchMode: settings.MeetingDispatchMode,
		}, gormuow.NewMeetingUnitOfWork(db)).WithMeetingEnqueuerContext(func(ctx context.Context, meetingID uint) error {
			return infraqueue.EnqueueRunMeetingContext(ctx, settings, meetingID)
		}),
		Messaging: appmessaging.NewUsecase(gormrepo.NewMessagingRepository(db), inframessaging.NewService(settings), sec, gormuow.NewMessagingUnitOfWork(db, settings)).WithTaskQueue(messageQueue),
		Paper:     apppaper.NewUsecase(gormrepo.NewPaperRepository(db), infrapaper.NewService(db), gormuow.NewPaperUnitOfWork(db)),
		Research:  appresearch.NewUsecase(gormrepo.NewResearchRepository(db), gormuow.NewResearchUnitOfWork(db)),
		AppConfig: appsettings.NewUsecase(gormrepo.NewSettingsRepository(db), sec, appSettings(settings), gormuow.NewSettingsUnitOfWork(db), config.WriteEnvOverrides).WithProxyTester(infraproxy.NewTester(db, settings, sec)),
		Wake:      appwake.NewUsecase(gormrepo.NewWakeRepository(db), inframarketdata.NewService(settings, gormmarketdata.NewStore(db, settings, sec)), gormuow.NewWakeUnitOfWork(db, settings)),
		Backup:    infrabackup.StoreForSettings(settings),
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
	}, gormuow.NewMeetingUnitOfWork(db)).WithMeetingEnqueuerContext(func(ctx context.Context, meetingID uint) error {
		return infraqueue.EnqueueRunMeetingContext(ctx, settings, meetingID)
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
		AppName:                    settings.AppName,
		AppEnv:                     settings.AppEnv,
		CORSOrigins:                settings.CORSOrigins,
		FrontendDist:               settings.FrontendDist,
		DatabaseURL:                settings.DatabaseURL,
		RedisURL:                   settings.RedisURL,
		MeetingMode:                settings.MeetingDispatchMode,
		LogDir:                     settings.LogDir,
		RuntimeEnvFile:             settings.RuntimeEnvFile,
		BackupArchiveProvider:      settings.BackupArchiveProvider,
		BackupArchiveS3Bucket:      settings.BackupArchiveS3Bucket,
		BackupArchiveS3Region:      settings.BackupArchiveS3Region,
		BackupArchiveS3Endpoint:    settings.BackupArchiveS3Endpoint,
		BackupArchiveS3AccessKeyID: settings.BackupArchiveS3AccessKeyID,
		BackupArchiveS3SecretKey:   settings.BackupArchiveS3SecretAccessKey,
		BackupArchiveS3Prefix:      settings.BackupArchiveS3Prefix,
		BackupArchiveOSSBucket:     settings.BackupArchiveOSSBucket,
		BackupArchiveOSSRegion:     settings.BackupArchiveOSSRegion,
		BackupArchiveOSSEndpoint:   settings.BackupArchiveOSSEndpoint,
		BackupArchiveOSSAccessKey:  settings.BackupArchiveOSSAccessKeyID,
		BackupArchiveOSSSecretKey:  settings.BackupArchiveOSSAccessKeySecret,
		BackupArchiveOSSPrefix:     settings.BackupArchiveOSSPrefix,
		BackupCopies:               settings.BackupRetentionCopies,
		BackupDays:                 settings.BackupRetentionDays,
		BackupRestoreDrillInterval: settings.BackupRestoreDrillInterval,
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
		AIDailyCostBudget:            settings.AIDailyCostBudget,
		ToolResultLimit:              settings.ToolResultLimit,
		SQLStatementTimeoutMillis:    settings.SQLStatementTimeoutMillis,
		LogDir:                       settings.LogDir,
		LogLevel:                     settings.LogLevel,
		LogRotationMode:              settings.LogRotationMode,
		LogRotationSizeMB:            settings.LogRotationSizeMB,
		LogRotationTotalSizeMB:       settings.LogRotationTotalSizeMB,
		LogRotationMaxAgeDays:        settings.LogRotationMaxAgeDays,
		BackupArchiveProvider:        settings.BackupArchiveProvider,
		BackupArchiveS3Bucket:        settings.BackupArchiveS3Bucket,
		BackupArchiveS3Region:        settings.BackupArchiveS3Region,
		BackupArchiveS3Endpoint:      settings.BackupArchiveS3Endpoint,
		BackupArchiveS3AccessKeyID:   settings.BackupArchiveS3AccessKeyID,
		BackupArchiveS3SecretKey:     settings.BackupArchiveS3SecretAccessKey,
		BackupArchiveS3Prefix:        settings.BackupArchiveS3Prefix,
		BackupArchiveOSSBucket:       settings.BackupArchiveOSSBucket,
		BackupArchiveOSSRegion:       settings.BackupArchiveOSSRegion,
		BackupArchiveOSSEndpoint:     settings.BackupArchiveOSSEndpoint,
		BackupArchiveOSSAccessKey:    settings.BackupArchiveOSSAccessKeyID,
		BackupArchiveOSSSecretKey:    settings.BackupArchiveOSSAccessKeySecret,
		BackupArchiveOSSPrefix:       settings.BackupArchiveOSSPrefix,
		BackupRestoreDrillInterval:   settings.BackupRestoreDrillInterval,
		OTELServiceName:              settings.OTELServiceName,
		OTELTracesExporter:           settings.OTELTracesExporter,
		OTELExporterOTLPEndpoint:     settings.OTELExporterOTLPEndpoint,
		OTELExporterOTLPProtocol:     settings.OTELExporterOTLPProtocol,
		OTELTracesSampler:            settings.OTELTracesSampler,
		OTELTracesSamplerArg:         settings.OTELTracesSamplerArg,
		RuntimeEnvFile:               settings.RuntimeEnvFile,
	}
}
