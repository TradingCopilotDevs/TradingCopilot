package queue

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	infralogging "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/logging"
	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

const (
	TypeRunMeeting            = "run_meeting"
	TypeEvaluateWakePlans     = "evaluate_wake_plans"
	TypeRunPaperMaintenance   = "run_paper_maintenance"
	TypeRecoverQueuedMeetings = "recover_queued_meetings"
	TypeRunBackupRestoreDrill = "run_backup_restore_drill"
)

type RunMeetingPayload struct {
	MeetingID uint `json:"meeting_id"`
}

type PeriodicJob struct {
	Type     string
	Interval time.Duration
}

type Handlers struct {
	RunMeeting            func(context.Context, RunMeetingPayload) error
	EvaluateWakePlans     func(context.Context) error
	RunPaperMaintenance   func(context.Context) error
	RecoverQueuedMeetings func(context.Context) error
	RunBackupRestoreDrill func(context.Context) error
	MarketTasks           MarketTaskHandlers
	MessageTasks          MessageTaskHandlers
}

func NewServeMux(handlers Handlers) *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.HandleFunc(TypeRunMeeting, func(ctx context.Context, task *asynq.Task) error {
		start := time.Now()
		ctx, raw, span := beginTaskSpan(ctx, TypeRunMeeting, task.Payload())
		defer span.End()
		var payload RunMeetingPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			finishTaskSpan(span, err)
			infralogging.LogOperation(start, "queue.task", "meeting", "run", "queue task run_meeting failed", err, zap.String("taskType", TypeRunMeeting))
			return err
		}
		err := handlers.RunMeeting(ctx, payload)
		finishTaskSpan(span, err)
		infralogging.LogOperation(start, "queue.task", "meeting", "run", "queue task run_meeting completed", err, zap.String("taskType", TypeRunMeeting), zap.Uint("meetingId", payload.MeetingID))
		return err
	})
	mux.HandleFunc(TypeEvaluateWakePlans, func(ctx context.Context, task *asynq.Task) error {
		start := time.Now()
		ctx, _, span := beginTaskSpan(ctx, TypeEvaluateWakePlans, task.Payload())
		defer span.End()
		err := handlers.EvaluateWakePlans(ctx)
		finishTaskSpan(span, err)
		infralogging.LogOperation(start, "queue.task", "wake", "evaluate", "queue task evaluate_wake_plans completed", err, zap.String("taskType", TypeEvaluateWakePlans))
		return err
	})
	mux.HandleFunc(TypeRunPaperMaintenance, func(ctx context.Context, task *asynq.Task) error {
		start := time.Now()
		ctx, _, span := beginTaskSpan(ctx, TypeRunPaperMaintenance, task.Payload())
		defer span.End()
		err := handlers.RunPaperMaintenance(ctx)
		finishTaskSpan(span, err)
		infralogging.LogOperation(start, "queue.task", "paper", "run", "queue task run_paper_maintenance completed", err, zap.String("taskType", TypeRunPaperMaintenance))
		return err
	})
	mux.HandleFunc(TypeRecoverQueuedMeetings, func(ctx context.Context, task *asynq.Task) error {
		start := time.Now()
		ctx, _, span := beginTaskSpan(ctx, TypeRecoverQueuedMeetings, task.Payload())
		defer span.End()
		err := handlers.RecoverQueuedMeetings(ctx)
		finishTaskSpan(span, err)
		infralogging.LogOperation(start, "queue.task", "meeting", "recover", "queue task recover_queued_meetings completed", err, zap.String("taskType", TypeRecoverQueuedMeetings))
		return err
	})
	mux.HandleFunc(TypeRunBackupRestoreDrill, func(ctx context.Context, task *asynq.Task) error {
		start := time.Now()
		ctx, _, span := beginTaskSpan(ctx, TypeRunBackupRestoreDrill, task.Payload())
		defer span.End()
		if handlers.RunBackupRestoreDrill == nil {
			err := errors.New("backup restore drill handler is not configured")
			finishTaskSpan(span, err)
			infralogging.LogOperation(start, "queue.task", "ops", "restore_drill", "queue task run_backup_restore_drill failed", err, zap.String("taskType", TypeRunBackupRestoreDrill))
			return err
		}
		err := handlers.RunBackupRestoreDrill(ctx)
		finishTaskSpan(span, err)
		infralogging.LogOperation(start, "queue.task", "ops", "restore_drill", "queue task run_backup_restore_drill completed", err, zap.String("taskType", TypeRunBackupRestoreDrill))
		return err
	})
	RegisterMarketTaskHandlers(mux, handlers.MarketTasks)
	RegisterMessageTaskHandlers(mux, handlers.MessageTasks)
	return mux
}

func RedisClientOpt(redisURL string) asynq.RedisClientOpt {
	opt, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		return asynq.RedisClientOpt{Addr: "127.0.0.1:6379"}
	}
	if clientOpt, ok := opt.(asynq.RedisClientOpt); ok {
		return clientOpt
	}
	return asynq.RedisClientOpt{Addr: "127.0.0.1:6379"}
}

func EnqueueRunMeeting(settings config.Settings, meetingID uint) error {
	return EnqueueRunMeetingContext(context.Background(), settings, meetingID)
}

func EnqueueRunMeetingContext(ctx context.Context, settings config.Settings, meetingID uint) error {
	start := time.Now()
	client := asynq.NewClient(RedisClientOpt(settings.RedisURL))
	defer client.Close()
	payload := encodeTaskPayload(ctx, RunMeetingPayload{MeetingID: meetingID})
	_, err := client.EnqueueContext(ctx, asynq.NewTask(TypeRunMeeting, payload), asynq.TaskID("run_meeting:"+itoa(meetingID)), asynq.Timeout(settings.MeetingRunTimeout), asynq.Retention(0))
	infralogging.LogOperation(start, "queue.enqueue", "meeting", "enqueue", "queue enqueue run_meeting completed", err, zap.String("taskType", TypeRunMeeting), zap.Uint("meetingId", meetingID))
	return err
}

func EnqueueDuePeriodicJobs(client *asynq.Client, now time.Time, jobs []PeriodicJob, next map[string]time.Time) error {
	return EnqueueDuePeriodicJobsContext(context.Background(), client, now, jobs, next)
}

func EnqueueDuePeriodicJobsContext(ctx context.Context, client *asynq.Client, now time.Time, jobs []PeriodicJob, next map[string]time.Time) error {
	for _, job := range jobs {
		if now.Before(next[job.Type]) {
			continue
		}
		_, err := client.EnqueueContext(ctx, asynq.NewTask(job.Type, encodeTaskPayloadBytes(ctx, nil)), periodicEnqueueOptions(job, now)...)
		if err != nil {
			if IsExistingPeriodicTask(err) {
				infralogging.Logger().Info("queue periodic task skipped",
					infralogging.Event("queue.enqueue"),
					infralogging.Group("scheduler"),
					infralogging.Method("enqueue"),
					infralogging.Status(infralogging.StatusSkipped),
					zap.Any(infralogging.FieldDuration, nil),
					zap.String("taskType", job.Type),
				)
				next[job.Type] = now.Add(job.Interval)
				continue
			}
			infralogging.Logger().Error("queue periodic task enqueue failed",
				infralogging.Event("queue.enqueue"),
				infralogging.Group("scheduler"),
				infralogging.Method("enqueue"),
				infralogging.Status(infralogging.StatusError),
				zap.Any(infralogging.FieldDuration, nil),
				zap.String("taskType", job.Type),
				zap.Error(err),
			)
			return err
		}
		infralogging.Logger().Info("queue periodic task enqueued",
			infralogging.Event("queue.enqueue"),
			infralogging.Group("scheduler"),
			infralogging.Method("enqueue"),
			infralogging.Status(infralogging.StatusOK),
			zap.Any(infralogging.FieldDuration, nil),
			zap.String("taskType", job.Type),
		)
		next[job.Type] = now.Add(job.Interval)
	}
	return nil
}

func PeriodicTaskID(job PeriodicJob, _ time.Time) string {
	return "periodic:" + job.Type
}

func periodicEnqueueOptions(job PeriodicJob, now time.Time) []asynq.Option {
	return []asynq.Option{
		asynq.TaskID(PeriodicTaskID(job, now)),
		asynq.Retention(0),
		asynq.Timeout(5 * time.Minute),
	}
}

func IsExistingPeriodicTask(err error) bool {
	return errors.Is(err, asynq.ErrTaskIDConflict) || errors.Is(err, asynq.ErrDuplicateTask)
}

func itoa(v uint) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
