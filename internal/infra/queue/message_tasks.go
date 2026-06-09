package queue

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	appmessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/messaging"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	infralogging "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/logging"
	"github.com/hibiken/asynq"
	"go.uber.org/zap"
)

const (
	TypeCollectMessageSubscription = "message_subscription_collect"
	TypeFilterIngestedMessage      = "message_filter_message"
)

type MessageTaskHandlers struct {
	Collect func(context.Context, appmessaging.CollectTask) error
	Filter  func(context.Context, appmessaging.FilterTask) error
}

type RedisMessageTaskQueue struct {
	settings config.Settings
}

func NewRedisMessageTaskQueue(settings config.Settings) RedisMessageTaskQueue {
	return RedisMessageTaskQueue{settings: settings}
}

func (q RedisMessageTaskQueue) EnqueueCollect(ctx context.Context, task appmessaging.CollectTask) error {
	start := time.Now()
	client := asynq.NewClient(RedisClientOpt(q.settings.RedisURL))
	defer client.Close()
	payload := encodeTaskPayload(ctx, task)
	id := "message_subscription_collect:all"
	if task.SubscriptionID != 0 {
		id = "message_subscription_collect:" + itoa(task.SubscriptionID)
	}
	_, err := client.EnqueueContext(ctx, asynq.NewTask(TypeCollectMessageSubscription, payload), asynq.TaskID(id), asynq.Timeout(15*time.Minute), asynq.Retention(0))
	if IsExistingPeriodicTask(err) {
		infralogging.Logger().Info("message collect task skipped",
			infralogging.Event("queue.enqueue"),
			infralogging.Group("messaging"),
			infralogging.Method("enqueue"),
			infralogging.Status(infralogging.StatusSkipped),
			infralogging.DurationSince(start),
			zap.String("taskType", TypeCollectMessageSubscription),
			zap.Uint("subscriptionId", task.SubscriptionID),
		)
		return nil
	}
	infralogging.LogOperation(start, "queue.enqueue", "messaging", "enqueue", "message collect task enqueue completed", err, zap.String("taskType", TypeCollectMessageSubscription), zap.Uint("subscriptionId", task.SubscriptionID))
	return err
}

func (q RedisMessageTaskQueue) EnqueueFilter(ctx context.Context, task appmessaging.FilterTask) error {
	start := time.Now()
	client := asynq.NewClient(RedisClientOpt(q.settings.RedisURL))
	defer client.Close()
	payload := encodeTaskPayload(ctx, task)
	id := "message_filter_message:" + itoa(task.MessageID)
	if task.Force {
		id += ":force"
	}
	_, err := client.EnqueueContext(ctx, asynq.NewTask(TypeFilterIngestedMessage, payload), asynq.TaskID(id), asynq.Timeout(q.filterTimeout()), asynq.Retention(0))
	if IsExistingPeriodicTask(err) {
		infralogging.Logger().Info("message filter task skipped",
			infralogging.Event("queue.enqueue"),
			infralogging.Group("messaging"),
			infralogging.Method("enqueue"),
			infralogging.Status(infralogging.StatusSkipped),
			infralogging.DurationSince(start),
			zap.String("taskType", TypeFilterIngestedMessage),
			zap.Uint("messageId", task.MessageID),
			zap.Bool("force", task.Force),
		)
		return nil
	}
	infralogging.LogOperation(start, "queue.enqueue", "messaging", "enqueue", "message filter task enqueue completed", err, zap.String("taskType", TypeFilterIngestedMessage), zap.Uint("messageId", task.MessageID), zap.Bool("force", task.Force))
	return err
}

func (q RedisMessageTaskQueue) filterTimeout() time.Duration {
	timeout := q.settings.AIChatTimeout
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	attempts := q.settings.AIJSONMaxAttempts
	if attempts <= 0 {
		attempts = 1
	}
	return time.Duration(attempts)*timeout + 30*time.Second
}

func RegisterMessageTaskHandlers(mux *asynq.ServeMux, handlers MessageTaskHandlers) {
	mux.HandleFunc(TypeCollectMessageSubscription, func(ctx context.Context, task *asynq.Task) error {
		start := time.Now()
		ctx, raw, span := beginTaskSpan(ctx, TypeCollectMessageSubscription, task.Payload())
		defer span.End()
		if handlers.Collect == nil {
			err := errors.New("message collect handler is not configured")
			finishTaskSpan(span, err)
			infralogging.LogOperation(start, "queue.task", "messaging", "collect", "message collect task failed", err, zap.String("taskType", TypeCollectMessageSubscription))
			return err
		}
		var payload appmessaging.CollectTask
		if err := json.Unmarshal(raw, &payload); err != nil {
			finishTaskSpan(span, err)
			infralogging.LogOperation(start, "queue.task", "messaging", "collect", "message collect task failed", err, zap.String("taskType", TypeCollectMessageSubscription))
			return err
		}
		err := handlers.Collect(ctx, payload)
		finishTaskSpan(span, err)
		infralogging.LogOperation(start, "queue.task", "messaging", "collect", "message collect task completed", err, zap.String("taskType", TypeCollectMessageSubscription), zap.Uint("subscriptionId", payload.SubscriptionID))
		return err
	})
	mux.HandleFunc(TypeFilterIngestedMessage, func(ctx context.Context, task *asynq.Task) error {
		start := time.Now()
		ctx, raw, span := beginTaskSpan(ctx, TypeFilterIngestedMessage, task.Payload())
		defer span.End()
		if handlers.Filter == nil {
			err := errors.New("message filter handler is not configured")
			finishTaskSpan(span, err)
			infralogging.LogOperation(start, "queue.task", "messaging", "filter", "message filter task failed", err, zap.String("taskType", TypeFilterIngestedMessage))
			return err
		}
		var payload appmessaging.FilterTask
		if err := json.Unmarshal(raw, &payload); err != nil {
			finishTaskSpan(span, err)
			infralogging.LogOperation(start, "queue.task", "messaging", "filter", "message filter task failed", err, zap.String("taskType", TypeFilterIngestedMessage))
			return err
		}
		err := handlers.Filter(ctx, payload)
		finishTaskSpan(span, err)
		infralogging.LogOperation(start, "queue.task", "messaging", "filter", "message filter task completed", err, zap.String("taskType", TypeFilterIngestedMessage), zap.Uint("messageId", payload.MessageID), zap.Bool("force", payload.Force))
		return err
	})
}

type LocalMessageTaskQueue struct {
	handlers MessageTaskHandlers
	tasks    chan localMessageTask
	seen     map[string]struct{}
	mu       sync.Mutex
}

type localMessageTask struct {
	kind         string
	collect      appmessaging.CollectTask
	filter       appmessaging.FilterTask
	key          string
	tracePayload []byte
}

func NewLocalMessageTaskQueue(buffer int) *LocalMessageTaskQueue {
	if buffer <= 0 {
		buffer = 256
	}
	return &LocalMessageTaskQueue{tasks: make(chan localMessageTask, buffer), seen: map[string]struct{}{}}
}

func (q *LocalMessageTaskQueue) SetHandlers(handlers MessageTaskHandlers) {
	q.handlers = handlers
}

func (q *LocalMessageTaskQueue) EnqueueCollect(ctx context.Context, task appmessaging.CollectTask) error {
	key := "collect:all"
	if task.SubscriptionID != 0 {
		key = "collect:" + itoa(task.SubscriptionID)
	}
	return q.enqueue(ctx, localMessageTask{kind: TypeCollectMessageSubscription, collect: task, key: key, tracePayload: encodeTaskPayloadBytes(ctx, nil)})
}

func (q *LocalMessageTaskQueue) EnqueueFilter(ctx context.Context, task appmessaging.FilterTask) error {
	key := "filter:" + itoa(task.MessageID)
	if task.Force {
		key += ":force"
	}
	return q.enqueue(ctx, localMessageTask{kind: TypeFilterIngestedMessage, filter: task, key: key, tracePayload: encodeTaskPayloadBytes(ctx, nil)})
}

func (q *LocalMessageTaskQueue) Run(ctx context.Context, workers int) {
	if workers <= 0 {
		workers = 2
	}
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case task := <-q.tasks:
					q.runTask(ctx, task)
					q.forget(task.key)
				}
			}
		}()
	}
	<-ctx.Done()
	wg.Wait()
}

func (q *LocalMessageTaskQueue) enqueue(ctx context.Context, task localMessageTask) error {
	start := time.Now()
	q.mu.Lock()
	if _, ok := q.seen[task.key]; ok {
		q.mu.Unlock()
		infralogging.Logger().Info("local message task skipped",
			infralogging.Event("queue.enqueue"),
			infralogging.Group("messaging"),
			infralogging.Method("enqueue"),
			infralogging.Status(infralogging.StatusSkipped),
			infralogging.DurationSince(start),
			zap.String("taskType", task.kind),
			zap.String("taskKey", task.key),
		)
		return nil
	}
	q.seen[task.key] = struct{}{}
	q.mu.Unlock()
	select {
	case <-ctx.Done():
		q.forget(task.key)
		infralogging.LogOperation(start, "queue.enqueue", "messaging", "enqueue", "local message task enqueue failed", ctx.Err(), zap.String("taskType", task.kind), zap.String("taskKey", task.key))
		return ctx.Err()
	case q.tasks <- task:
		infralogging.LogOperation(start, "queue.enqueue", "messaging", "enqueue", "local message task enqueued", nil, zap.String("taskType", task.kind), zap.String("taskKey", task.key))
		return nil
	}
}

func (q *LocalMessageTaskQueue) runTask(ctx context.Context, task localMessageTask) {
	start := time.Now()
	ctx, _, span := beginTaskSpan(ctx, task.kind, task.tracePayload)
	defer span.End()
	var err error
	switch task.kind {
	case TypeCollectMessageSubscription:
		if q.handlers.Collect != nil {
			err = q.handlers.Collect(ctx, task.collect)
		}
	case TypeFilterIngestedMessage:
		if q.handlers.Filter != nil {
			err = q.handlers.Filter(ctx, task.filter)
		}
	}
	finishTaskSpan(span, err)
	infralogging.LogOperation(start, "queue.task", "messaging", taskMethod(task.kind), "local message task completed", err, zap.String("taskType", task.kind), zap.String("taskKey", task.key))
}

func (q *LocalMessageTaskQueue) forget(key string) {
	q.mu.Lock()
	delete(q.seen, key)
	q.mu.Unlock()
}

func taskMethod(kind string) string {
	switch kind {
	case TypeCollectMessageSubscription:
		return "collect"
	case TypeFilterIngestedMessage:
		return "filter"
	default:
		return "run"
	}
}
