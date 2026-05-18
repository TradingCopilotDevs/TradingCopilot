package meeting

import (
	"context"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainwake "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/wake"
	"time"

	appwake "github.com/TradingCopilotDevs/TradingCopilot/internal/app/wake"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	inframarketdata "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/marketdata"
	gormmarketdata "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/marketdata"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"gorm.io/gorm"
)

func FireWakePlan(db *gorm.DB, plan *domainwake.Plan) (*domainmeeting.Meeting, error) {
	result, found, err := wakeUsecase(db).Fire(context.Background(), plan.ID)
	if err != nil || !found {
		return nil, err
	}
	*plan = result.Plan
	return result.Meeting, nil
}

func ProcessDueWakePlansWithDispatcher(db *gorm.DB, limit int, dispatch func(*domainmeeting.Meeting) error) (map[string]int, error) {
	stats, err := wakeUsecase(db).ProcessDue(context.Background(), limit, dispatch)
	if err != nil {
		return nil, err
	}
	return map[string]int{
		"checked":    stats.Checked,
		"fired":      stats.Fired,
		"dispatched": stats.Dispatched,
		"failed":     stats.Failed,
	}, nil
}

func RunWakePlanLoop(ctx context.Context, db *gorm.DB, interval time.Duration, limit int, dispatch func(*domainmeeting.Meeting) error) {
	if interval <= 0 {
		interval = 20 * time.Second
	}
	for {
		_, _ = ProcessDueWakePlansWithDispatcher(db, limit, dispatch)
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func wakeUsecase(db *gorm.DB) appwake.Usecase {
	settings := config.Settings{}
	return appwake.NewUsecase(gormrepo.NewWakeRepository(db), inframarketdata.NewService(settings, gormmarketdata.NewStore(db, settings, security.New(settings))), wakeTestUnitOfWork{db: db, settings: settings})
}

type wakeTestUnitOfWork struct {
	db       *gorm.DB
	settings config.Settings
}

func (u wakeTestUnitOfWork) WithTx(ctx context.Context, fn func(appwake.Repository) error) error {
	return u.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(gormrepo.NewWakeRepository(tx))
	})
}
