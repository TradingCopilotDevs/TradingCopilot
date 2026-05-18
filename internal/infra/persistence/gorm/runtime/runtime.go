package runtime

import (
	"context"
	"time"

	appmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/app/meeting"
	appmessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/messaging"
	apppaper "github.com/TradingCopilotDevs/TradingCopilot/internal/app/paper"
	appresearch "github.com/TradingCopilotDevs/TradingCopilot/internal/app/research"
	appwake "github.com/TradingCopilotDevs/TradingCopilot/internal/app/wake"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainmsg "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/messaging"
	domainpaper "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/paper"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	infralogging "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/logging"
	inframarketdata "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/marketdata"
	inframessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/messaging"
	gormmarketdata "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/marketdata"
	inframeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/meeting"
	infrapaper "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/paper"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	gormuow "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/uow"
	infraqueue "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/queue"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type LocalMeetingStarter func(db *gorm.DB, meetingID uint) bool

func StartLocalMeetingRun(db *gorm.DB, meetingID uint) bool {
	return inframeeting.StartLocalRun(db, meetingID)
}

type Runner struct {
	settings     config.Settings
	db           *gorm.DB
	security     security.Service
	starter      LocalMeetingStarter
	messageQueue appmessaging.TaskQueue
}

func New(settings config.Settings, db *gorm.DB, sec security.Service) Runner {
	return Runner{
		settings: settings,
		db:       db,
		security: sec,
		starter:  StartLocalMeetingRun,
	}
}

func (r Runner) WithLocalMeetingStarter(starter LocalMeetingStarter) Runner {
	if starter != nil {
		r.starter = starter
	}
	return r
}

func (r Runner) WithMessageTaskQueue(queue appmessaging.TaskQueue) Runner {
	r.messageQueue = queue
	return r
}

func (r Runner) SeedDefaults() (err error) {
	start := time.Now()
	defer func() {
		infralogging.LogOperation(start, "app.seed_defaults", "app", "seed", "runtime default seeding completed", err)
	}()
	if err := inframeeting.SeedDefaultRoles(r.db, false); err != nil {
		return err
	}
	if _, err := messagingUsecase(r).EnsureDefaultSubscriptionFilter(context.Background()); err != nil {
		return err
	}
	_, accounts, err := infrapaper.EnsureDefaultSetup(r.db)
	if err != nil {
		return err
	}
	accountID := defaultResearchTeamAccountID(accounts)
	if accountID == 0 {
		return nil
	}
	_, _, err = researchUsecase(r).EnsureDefaultTeam(context.Background(), accountID)
	return err
}

func (r Runner) SaveTelegramMTProtoAppConfig(appID string, appHash string) error {
	return messagingUsecase(r).SaveTelegramAppConfig(context.Background(), appID, appHash)
}

func (r Runner) StartTelegramMTProtoLogin(phone string) (string, error) {
	return messagingUsecase(r).StartTelegramLogin(context.Background(), phone)
}

func (r Runner) CompleteTelegramMTProtoLogin(phone string, code string, phoneCodeHash string, password string) error {
	return messagingUsecase(r).VerifyTelegramLogin(context.Background(), phone, code, phoneCodeHash, password)
}

func (r Runner) GetLatestTelegramMTProtoMessage(channelRef string) (map[string]any, error) {
	return messagingUsecase(r).TestSubscriptionRef(context.Background(), domainmsg.ProviderTelegramChannel, channelRef)
}

func (r Runner) RunMessageSubscriptionListener(ctx context.Context) {
	infralogging.Logger().Info("message subscription listener started",
		infralogging.Event("messaging.listener"),
		infralogging.Group("messaging"),
		infralogging.Method("listen"),
		infralogging.Status(infralogging.StatusOK),
		zap.Any(infralogging.FieldDuration, nil),
	)
	r.runMessageSubscriptionListener(ctx)
}

func ShouldRunLocalBackgroundLoops(settings config.Settings) bool {
	return settings.MeetingDispatchMode == "" || settings.MeetingDispatchMode == "local"
}

func messagingUsecase(r Runner) appmessaging.Usecase {
	return appmessaging.NewUsecase(gormrepo.NewMessagingRepository(r.db), inframessaging.NewService(r.settings), r.security, gormuow.NewMessagingUnitOfWork(r.db, r.settings)).WithTaskQueue(r.messageQueue)
}

func researchUsecase(r Runner) appresearch.Usecase {
	return appresearch.NewUsecase(gormrepo.NewResearchRepository(r.db), gormuow.NewResearchUnitOfWork(r.db))
}

func defaultResearchTeamAccountID(accounts []domainpaper.Account) uint {
	if len(accounts) == 0 {
		return 0
	}
	for _, account := range accounts {
		if account.Name == infrapaper.DefaultPaperAccountName {
			return account.ID
		}
	}
	return accounts[0].ID
}

func (r Runner) StartLocalBackgroundLoops(ctx context.Context) {
	go r.runLocalWakePlanLoop(ctx)
	go r.runLocalPaperMaintenanceLoop(ctx)
	go r.runLocalMeetingRecoveryLoop(ctx)
}

func (r Runner) runLocalWakePlanLoop(ctx context.Context) {
	wakeUsecase := appwake.NewUsecase(gormrepo.NewWakeRepository(r.db), inframarketdata.NewService(r.settings, gormmarketdata.NewStore(r.db, r.settings, r.security)), gormuow.NewWakeUnitOfWork(r.db, r.settings))
	interval := 20 * time.Second
	for {
		start := time.Now()
		_, err := wakeUsecase.ProcessDue(ctx, 100, func(meeting *domainmeeting.Meeting) error {
			r.starter(r.db, meeting.ID)
			return nil
		})
		infralogging.LogOperation(start, "wake.process_due", "wake", "evaluate", "local wake plan loop completed", err)
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (r Runner) runLocalPaperMaintenanceLoop(ctx context.Context) {
	paperUsecase := apppaper.NewUsecase(gormrepo.NewPaperRepository(r.db), infrapaper.NewService(r.db), gormuow.NewPaperUnitOfWork(r.db))
	interval := 10 * time.Second
	for {
		start := time.Now()
		paperUsecase.RunMaintenanceOnce(ctx)
		infralogging.LogOperation(start, "paper.maintenance", "paper", "run", "local paper maintenance loop completed", ctx.Err())
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (r Runner) runLocalMeetingRecoveryLoop(ctx context.Context) {
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		start := time.Now()
		recovered, err := r.RecoverLocalQueuedMeetings(100, 5*time.Second)
		infralogging.LogOperation(start, "meeting.recovery", "meeting", "recover", "local queued meeting recovery completed", err, zap.Int("recovered", recovered))
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r Runner) RecoverLocalQueuedMeetings(limit int, olderThan time.Duration) (int, error) {
	meetingUsecase := appmeeting.NewUsecase(gormrepo.NewMeetingRepository(r.db), inframeeting.NewService(r.db), appmeeting.Settings{
		MeetingDispatchMode: r.settings.MeetingDispatchMode,
	}, gormuow.NewMeetingUnitOfWork(r.db))
	return meetingUsecase.RecoverQueued(context.Background(), appmeeting.RecoverySettings{
		MeetingStaleAfter:       r.settings.MeetingStaleAfter,
		MeetingAutoRequeueLimit: r.settings.MeetingAutoRequeueLimit,
	}, limit, olderThan, "local", func(meeting *domainmeeting.Meeting) error {
		r.starter(r.db, meeting.ID)
		return nil
	})
}

func (r Runner) meetingDispatchUsecase() appmeeting.Usecase {
	service := runnerMeetingService{Service: inframeeting.NewService(r.db), runner: r}
	return appmeeting.NewUsecase(gormrepo.NewMeetingRepository(r.db), service, appmeeting.Settings{
		MeetingDispatchMode: r.settings.MeetingDispatchMode,
	}, gormuow.NewMeetingUnitOfWork(r.db)).WithMeetingEnqueuer(func(meetingID uint) error {
		return infraqueue.EnqueueRunMeeting(r.settings, meetingID)
	})
}

type runnerMeetingService struct {
	inframeeting.Service
	runner Runner
}

func (s runnerMeetingService) StartLocalRun(ctx context.Context, meetingID uint) bool {
	return s.runner.starter(s.runner.db.WithContext(ctx), meetingID)
}

func (r Runner) DispatchMeeting(meeting *domainmeeting.Meeting) error {
	if meeting == nil {
		return nil
	}
	return r.DispatchMeetingID(r.settings, meeting.ID)
}

func (r Runner) DispatchMeetingID(_ config.Settings, meetingID uint) error {
	start := time.Now()
	var err error
	defer func() {
		infralogging.LogOperation(start, "meeting.dispatch", "meeting", "dispatch", "meeting dispatch completed", err,
			zap.Uint("meetingId", meetingID),
			zap.String("mode", r.settings.MeetingDispatchMode),
		)
	}()
	switch r.settings.MeetingDispatchMode {
	case "redis":
		err = infraqueue.EnqueueRunMeeting(r.settings, meetingID)
		return err
	case "auto":
		enqueueResult := make(chan error, 1)
		go func() { enqueueResult <- infraqueue.EnqueueRunMeeting(r.settings, meetingID) }()
		select {
		case enqueueErr := <-enqueueResult:
			if enqueueErr == nil {
				return nil
			}
		case <-time.After(800 * time.Millisecond):
		}
		r.starter(r.db, meetingID)
		return nil
	default:
		r.starter(r.db, meetingID)
		return nil
	}
}
