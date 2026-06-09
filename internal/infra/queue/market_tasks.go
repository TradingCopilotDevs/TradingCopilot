package queue

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	appmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/app/market"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	infralogging "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/logging"
	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

const TypeRunMarketTask = "market_run_task"

type MarketTaskHandlers struct {
	Run func(context.Context, appmarket.Task) error
}

type RedisMarketTaskQueue struct {
	settings config.Settings
}

func NewRedisMarketTaskQueue(settings config.Settings) RedisMarketTaskQueue {
	return RedisMarketTaskQueue{settings: settings}
}

func (q RedisMarketTaskQueue) EnqueueMarketTask(ctx context.Context, task appmarket.Task) (string, error) {
	start := time.Now()
	client := asynq.NewClient(RedisClientOpt(q.settings.RedisURL))
	defer client.Close()
	taskID := MarketTaskID(task)
	payload := encodeTaskPayload(ctx, task)
	_, err := client.EnqueueContext(ctx, asynq.NewTask(TypeRunMarketTask, payload), asynq.TaskID(taskID), asynq.Timeout(marketTaskTimeout(task)), asynq.Retention(0))
	if IsExistingPeriodicTask(err) {
		infralogging.Logger().Info("market task skipped",
			infralogging.Event("queue.enqueue"),
			infralogging.Group("market"),
			infralogging.Method("enqueue"),
			infralogging.Status(infralogging.StatusSkipped),
			infralogging.DurationSince(start),
			zap.String("taskType", TypeRunMarketTask),
			zap.String("taskAction", task.Action),
			zap.String("code", task.Code),
		)
		return taskID, nil
	}
	infralogging.LogOperation(start, "queue.enqueue", "market", "enqueue", "market task enqueue completed", err, zap.String("taskType", TypeRunMarketTask), zap.String("taskAction", task.Action), zap.String("code", task.Code))
	return taskID, err
}

func RegisterMarketTaskHandlers(mux *asynq.ServeMux, handlers MarketTaskHandlers) {
	mux.HandleFunc(TypeRunMarketTask, func(ctx context.Context, task *asynq.Task) error {
		start := time.Now()
		ctx, raw, span := beginTaskSpan(ctx, TypeRunMarketTask, task.Payload())
		defer span.End()
		if handlers.Run == nil {
			err := errors.New("market task handler is not configured")
			finishTaskSpan(span, err)
			infralogging.LogOperation(start, "queue.task", "market", "run", "market task failed", err, zap.String("taskType", TypeRunMarketTask))
			return err
		}
		var payload appmarket.Task
		if err := json.Unmarshal(raw, &payload); err != nil {
			finishTaskSpan(span, err)
			infralogging.LogOperation(start, "queue.task", "market", "run", "market task failed", err, zap.String("taskType", TypeRunMarketTask))
			return err
		}
		err := handlers.Run(ctx, payload)
		finishTaskSpan(span, err)
		infralogging.LogOperation(start, "queue.task", "market", "run", "market task completed", err, zap.String("taskType", TypeRunMarketTask), zap.String("taskAction", payload.Action), zap.String("code", payload.Code))
		return err
	})
}

func MarketTaskID(task appmarket.Task) string {
	action := strings.ToLower(strings.TrimSpace(task.Action))
	switch action {
	case appmarket.TaskSyncSymbols:
		return "market:sync_symbols"
	case appmarket.TaskRefreshQuote, appmarket.TaskRefreshDailyBars:
		return "market:" + action + ":" + strings.TrimSpace(task.Code)
	default:
		return "market:" + firstNonEmpty(action, "unknown")
	}
}

func marketTaskTimeout(task appmarket.Task) time.Duration {
	switch strings.ToLower(strings.TrimSpace(task.Action)) {
	case appmarket.TaskRefreshQuote:
		return 2 * time.Minute
	case appmarket.TaskSyncSymbols, appmarket.TaskRefreshDailyBars:
		return 15 * time.Minute
	default:
		return 5 * time.Minute
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
