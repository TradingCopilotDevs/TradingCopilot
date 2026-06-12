package jobs

import (
	"context"
	"sync"
	"time"

	appmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/app/market"
	appmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/app/meeting"
	appmessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/messaging"
	appops "github.com/TradingCopilotDevs/TradingCopilot/internal/app/ops"
	apppaper "github.com/TradingCopilotDevs/TradingCopilot/internal/app/paper"
	appsystem "github.com/TradingCopilotDevs/TradingCopilot/internal/app/system"
	appwake "github.com/TradingCopilotDevs/TradingCopilot/internal/app/wake"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	infrabackup "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/backup"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	infralogging "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/logging"
	inframarketdata "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/marketdata"
	inframessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/messaging"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/connect"
	gormmarketdata "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/marketdata"
	inframeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/meeting"
	infrapaper "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/paper"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	gormuow "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/uow"
	infraqueue "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/queue"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"github.com/hibiken/asynq"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const (
	TypeRunMeeting            = infraqueue.TypeRunMeeting
	TypeEvaluateWakePlans     = infraqueue.TypeEvaluateWakePlans
	TypeRunPaperMaintenance   = infraqueue.TypeRunPaperMaintenance
	TypeRecoverQueuedMeetings = infraqueue.TypeRecoverQueuedMeetings
	TypeRunBackupRestoreDrill = infraqueue.TypeRunBackupRestoreDrill
	TypeRunMarketTask         = infraqueue.TypeRunMarketTask
)

type RunMeetingPayload = infraqueue.RunMeetingPayload
type MarketTask = appmarket.Task
type PeriodicJob = infraqueue.PeriodicJob

func RedisClientOpt(redisURL string) asynq.RedisClientOpt {
	return infraqueue.RedisClientOpt(redisURL)
}

func EnqueueRunMeeting(settings config.Settings, meetingID uint) error {
	return infraqueue.EnqueueRunMeeting(settings, meetingID)
}

func EnqueueRunMeetingContext(ctx context.Context, settings config.Settings, meetingID uint) error {
	return infraqueue.EnqueueRunMeetingContext(ctx, settings, meetingID)
}

func EnqueueDuePeriodicJobs(client *asynq.Client, now time.Time, jobs []PeriodicJob, next map[string]time.Time) error {
	return infraqueue.EnqueueDuePeriodicJobs(client, now, jobs, next)
}

func PeriodicTaskID(job PeriodicJob, now time.Time) string {
	return infraqueue.PeriodicTaskID(job, now)
}

func isExistingPeriodicTask(err error) bool {
	return infraqueue.IsExistingPeriodicTask(err)
}

func RunWorker(settings config.Settings) error {
	db, err := database.Open(settings)
	if err != nil {
		return err
	}
	if settings.AutoCreateTables {
		if err := database.AutoMigrate(db); err != nil {
			return err
		}
	}
	stopHeartbeat := startHeartbeatLoop(db, "worker", settings.MeetingDispatchMode)
	defer func() {
		stopHeartbeat()
		_ = systemUsecase(db).MarkStopped(context.Background(), "worker", settings.MeetingDispatchMode, "")
	}()
	srv := asynq.NewServer(RedisClientOpt(settings.RedisURL), asynq.Config{Concurrency: 2})
	mux := NewServeMux(db, settings)
	return srv.Run(mux)
}

func NewServeMux(db *gorm.DB, settings config.Settings) *asynq.ServeMux {
	return newServeMux(db, settings, func(ctx context.Context, meetingID uint) error {
		return EnqueueRunMeetingContext(ctx, settings, meetingID)
	})
}

func newServeMux(db *gorm.DB, settings config.Settings, enqueueMeeting appmeeting.EnqueueMeetingContext) *asynq.ServeMux {
	sec := security.New(settings)
	meetingUsecase := newMeetingUsecaseContext(db, settings, enqueueMeeting)
	marketUsecase := appmarket.NewUsecase(gormrepo.NewMarketRepository(db), inframarketdata.NewService(settings, gormmarketdata.NewStore(db, settings, sec)), gormuow.NewMarketUnitOfWork(db, settings))
	wakeUsecase := appwake.NewUsecase(gormrepo.NewWakeRepository(db), inframarketdata.NewService(settings, gormmarketdata.NewStore(db, settings, sec)), gormuow.NewWakeUnitOfWork(db, settings))
	paperUsecase := apppaper.NewUsecase(gormrepo.NewPaperRepository(db), infrapaper.NewService(db), gormuow.NewPaperUnitOfWork(db))
	opsBackupUsecase := appops.NewBackupService(opsBackupSettings(settings), nil).WithArchiveStore(infrabackup.StoreForSettings(settings))
	messageQueue := infraqueue.NewRedisMessageTaskQueue(settings)
	messagingUsecase := appmessaging.NewUsecase(gormrepo.NewMessagingRepository(db), inframessaging.NewService(settings), sec, gormuow.NewMessagingUnitOfWork(db, settings)).WithTaskQueue(messageQueue)
	return infraqueue.NewServeMux(infraqueue.Handlers{
		RunMeeting: func(ctx context.Context, payload RunMeetingPayload) error {
			return meetingUsecase.RunOnce(ctx, payload.MeetingID)
		},
		EvaluateWakePlans: func(ctx context.Context) error {
			_, err := wakeUsecase.ProcessDue(ctx, 100, func(meeting *domainmeeting.Meeting) error {
				return EnqueueRunMeetingContext(ctx, settings, meeting.ID)
			})
			return err
		},
		RunPaperMaintenance: func(ctx context.Context) error {
			paperUsecase.RunMaintenanceOnce(ctx)
			return nil
		},
		RecoverQueuedMeetings: func(context.Context) error {
			_, err := RecoverQueuedMeetings(db, settings, 100, 5*time.Second)
			return err
		},
		RunBackupRestoreDrill: func(ctx context.Context) error {
			_, err := opsBackupUsecase.RunLatestRestoreDrill(ctx)
			return err
		},
		MarketTasks: infraqueue.MarketTaskHandlers{
			Run: func(ctx context.Context, payload appmarket.Task) error {
				_, err := marketUsecase.ProcessTask(ctx, payload)
				return err
			},
		},
		MessageTasks: infraqueue.MessageTaskHandlers{
			Collect: func(ctx context.Context, payload appmessaging.CollectTask) error {
				result, _, err := messagingUsecase.ProcessCollectTask(ctx, payload)
				if err != nil {
					return err
				}
				return dispatchMessageSubscriptionMeetings(ctx, meetingUsecase, result.CreatedMeetings)
			},
			Filter: func(ctx context.Context, payload appmessaging.FilterTask) error {
				result, _, err := messagingUsecase.ProcessFilterTask(ctx, payload)
				if err != nil {
					return err
				}
				if result == nil {
					return nil
				}
				return dispatchMessageSubscriptionMeetings(ctx, meetingUsecase, result.CreatedMeetings)
			},
		},
	})
}

func newMeetingUsecase(db *gorm.DB, settings config.Settings, enqueueMeeting appmeeting.EnqueueMeeting) appmeeting.Usecase {
	usecase := appmeeting.NewUsecase(gormrepo.NewMeetingRepository(db), inframeeting.NewService(db), appmeeting.Settings{
		MeetingDispatchMode: settings.MeetingDispatchMode,
	}, gormuow.NewMeetingUnitOfWork(db))
	if enqueueMeeting != nil {
		usecase = usecase.WithMeetingEnqueuer(enqueueMeeting)
	}
	return usecase
}

func newMeetingUsecaseContext(db *gorm.DB, settings config.Settings, enqueueMeeting appmeeting.EnqueueMeetingContext) appmeeting.Usecase {
	usecase := appmeeting.NewUsecase(gormrepo.NewMeetingRepository(db), inframeeting.NewService(db), appmeeting.Settings{
		MeetingDispatchMode: settings.MeetingDispatchMode,
	}, gormuow.NewMeetingUnitOfWork(db))
	if enqueueMeeting != nil {
		usecase = usecase.WithMeetingEnqueuerContext(enqueueMeeting)
	}
	return usecase
}

func opsBackupSettings(settings config.Settings) appops.BackupSettings {
	return appops.BackupSettings{
		AppName:                    settings.AppName,
		AppEnv:                     settings.AppEnv,
		DatabaseURL:                settings.DatabaseURL,
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
		BackupRetentionCopies:      settings.BackupRetentionCopies,
		BackupRetentionDays:        settings.BackupRetentionDays,
		BackupRestoreDrillInterval: settings.BackupRestoreDrillInterval,
	}
}

func dispatchMessageSubscriptionMeetings(ctx context.Context, meetingUsecase appmeeting.Usecase, meetings []*domainmeeting.Meeting) error {
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

func RunScheduler(settings config.Settings) error {
	db, err := database.Open(settings)
	if err != nil {
		return err
	}
	if settings.AutoCreateTables {
		if err := database.AutoMigrate(db); err != nil {
			return err
		}
	}
	stopHeartbeat := startHeartbeatLoop(db, "scheduler", settings.MeetingDispatchMode)
	defer func() {
		stopHeartbeat()
		_ = systemUsecase(db).MarkStopped(context.Background(), "scheduler", settings.MeetingDispatchMode, "")
	}()
	client := asynq.NewClient(RedisClientOpt(settings.RedisURL))
	defer client.Close()
	jobs := []PeriodicJob{
		{Type: TypeEvaluateWakePlans, Interval: 20 * time.Second},
		{Type: TypeRunPaperMaintenance, Interval: 10 * time.Second},
		{Type: TypeRecoverQueuedMeetings, Interval: 60 * time.Second},
	}
	if settings.BackupRestoreDrillInterval > 0 {
		jobs = append(jobs, PeriodicJob{Type: TypeRunBackupRestoreDrill, Interval: settings.BackupRestoreDrillInterval})
	}
	next := map[string]time.Time{}
	for {
		now := time.Now()
		if err := infraqueue.EnqueueDuePeriodicJobs(client, now, jobs, next); err != nil {
			infralogging.Logger().Error("scheduler enqueue failed",
				infralogging.Event("queue.enqueue"),
				infralogging.Group("scheduler"),
				infralogging.Method("enqueue"),
				infralogging.Status(infralogging.StatusError),
				zap.Any(infralogging.FieldDuration, nil),
				zap.Error(err),
			)
			_ = systemUsecase(db).RecordHeartbeat(context.Background(), "scheduler", settings.MeetingDispatchMode, err.Error())
		}
		time.Sleep(time.Second)
	}
}

func startHeartbeatLoop(db *gorm.DB, serviceName string, mode string) func() {
	done := make(chan struct{})
	var once sync.Once
	heartbeat := systemUsecase(db)
	_ = heartbeat.RecordHeartbeat(context.Background(), serviceName, mode, "")
	go func() {
		ticker := time.NewTicker(time.Duration(appsystem.HeartbeatIntervalSeconds) * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = heartbeat.RecordHeartbeat(context.Background(), serviceName, mode, "")
			case <-done:
				return
			}
		}
	}()
	return func() {
		once.Do(func() { close(done) })
	}
}

func systemUsecase(db *gorm.DB) appsystem.Usecase {
	return appsystem.NewUsecase(gormrepo.NewSystemRepository(db), gormuow.NewSystemUnitOfWork(db))
}

func RecoverQueuedMeetings(db *gorm.DB, settings config.Settings, limit int, olderThan time.Duration) (int, error) {
	meetingUsecase := appmeeting.NewUsecase(gormrepo.NewMeetingRepository(db), inframeeting.NewService(db), appmeeting.Settings{
		MeetingDispatchMode: settings.MeetingDispatchMode,
	}, gormuow.NewMeetingUnitOfWork(db))
	return meetingUsecase.RecoverQueued(context.Background(), appmeeting.RecoverySettings{
		MeetingStaleAfter:       settings.MeetingStaleAfter,
		MeetingAutoRequeueLimit: settings.MeetingAutoRequeueLimit,
	}, limit, olderThan, "redis", func(meeting *domainmeeting.Meeting) error {
		if err := EnqueueRunMeeting(settings, meeting.ID); err != nil {
			return err
		}
		return nil
	})
}
