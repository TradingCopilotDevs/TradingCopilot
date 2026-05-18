package queue

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	infralogging "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/logging"
	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

const (
	TypeRunMeeting            = "run_meeting"
	TypeEvaluateWakePlans     = "evaluate_wake_plans"
	TypeRunPaperMaintenance   = "run_paper_maintenance"
	TypeRecoverQueuedMeetings = "recover_queued_meetings"
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
	MessageTasks          MessageTaskHandlers
}

func NewServeMux(handlers Handlers) *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.HandleFunc(TypeRunMeeting, func(ctx context.Context, task *asynq.Task) error {
		start := time.Now()
		var payload RunMeetingPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			infralogging.LogOperation(start, "queue.task", "meeting", "run", "queue task run_meeting failed", err, zap.String("taskType", TypeRunMeeting))
			return err
		}
		err := handlers.RunMeeting(ctx, payload)
		infralogging.LogOperation(start, "queue.task", "meeting", "run", "queue task run_meeting completed", err, zap.String("taskType", TypeRunMeeting), zap.Uint("meetingId", payload.MeetingID))
		return err
	})
	mux.HandleFunc(TypeEvaluateWakePlans, func(ctx context.Context, _ *asynq.Task) error {
		start := time.Now()
		err := handlers.EvaluateWakePlans(ctx)
		infralogging.LogOperation(start, "queue.task", "wake", "evaluate", "queue task evaluate_wake_plans completed", err, zap.String("taskType", TypeEvaluateWakePlans))
		return err
	})
	mux.HandleFunc(TypeRunPaperMaintenance, func(ctx context.Context, _ *asynq.Task) error {
		start := time.Now()
		err := handlers.RunPaperMaintenance(ctx)
		infralogging.LogOperation(start, "queue.task", "paper", "run", "queue task run_paper_maintenance completed", err, zap.String("taskType", TypeRunPaperMaintenance))
		return err
	})
	mux.HandleFunc(TypeRecoverQueuedMeetings, func(ctx context.Context, _ *asynq.Task) error {
		start := time.Now()
		err := handlers.RecoverQueuedMeetings(ctx)
		infralogging.LogOperation(start, "queue.task", "meeting", "recover", "queue task recover_queued_meetings completed", err, zap.String("taskType", TypeRecoverQueuedMeetings))
		return err
	})
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
	start := time.Now()
	client := asynq.NewClient(RedisClientOpt(settings.RedisURL))
	defer client.Close()
	payload, _ := json.Marshal(RunMeetingPayload{MeetingID: meetingID})
	_, err := client.Enqueue(asynq.NewTask(TypeRunMeeting, payload), asynq.TaskID("run_meeting:"+itoa(meetingID)), asynq.Timeout(settings.MeetingRunTimeout), asynq.Retention(0))
	infralogging.LogOperation(start, "queue.enqueue", "meeting", "enqueue", "queue enqueue run_meeting completed", err, zap.String("taskType", TypeRunMeeting), zap.Uint("meetingId", meetingID))
	return err
}

func EnqueueDuePeriodicJobs(client *asynq.Client, now time.Time, jobs []PeriodicJob, next map[string]time.Time) error {
	for _, job := range jobs {
		if now.Before(next[job.Type]) {
			continue
		}
		_, err := client.Enqueue(asynq.NewTask(job.Type, nil), periodicEnqueueOptions(job, now)...)
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
