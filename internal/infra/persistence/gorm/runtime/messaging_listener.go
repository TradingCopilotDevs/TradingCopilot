package runtime

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"

	appmessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/messaging"
	appsystem "github.com/TradingCopilotDevs/TradingCopilot/internal/app/system"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainmsg "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/messaging"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
	infralogging "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/logging"
	inframessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/messaging"
	runtimeproxy "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	gormuow "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/uow"
	"go.uber.org/zap"
)

var ErrMessageSubscriptionProxyChanged = errors.New("message subscription proxy configuration changed")

const (
	messageSubscriptionHeartbeatWriteTimeout = 3 * time.Second
	messageSubscriptionHandleTimeout         = 5 * time.Minute
	messageSubscriptionMetadataWriteTimeout  = 30 * time.Second
	messageSubscriptionCollectInterval       = 30 * time.Second
	messageSubscriptionCollectTimeout        = 10 * time.Minute
	messageSubscriptionLeaseTTL              = 45 * time.Second
	messageSubscriptionLeaseRenewInterval    = 10 * time.Second
	messageSubscriptionCollectLimit          = 200
)

func (r Runner) runMessageSubscriptionListener(ctx context.Context) {
	r.recordMessageSubscriptionHeartbeat("")
	owner := MessageSubscriptionListenerOwner()
	for {
		select {
		case <-ctx.Done():
			r.markMessageSubscriptionStopped(ctx.Err().Error())
			return
		default:
		}

		acquired, err := r.acquireMessageSubscriptionLease(ctx, owner)
		if err != nil {
			r.recordMessageSubscriptionHeartbeat(err.Error())
			sleepWithContext(ctx, messageSubscriptionLeaseRenewInterval)
			continue
		}
		if !acquired {
			r.recordMessageSubscriptionHeartbeat("another message subscription listener instance holds the lease")
			sleepWithContext(ctx, messageSubscriptionLeaseRenewInterval)
			continue
		}
		r.recordMessageSubscriptionHeartbeat("")
		_ = r.runOwnedMessageSubscriptionListener(ctx, owner)
	}
}

func (r Runner) runOwnedMessageSubscriptionListener(ctx context.Context, owner string) error {
	ownedCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go r.messageSubscriptionHeartbeatLoop(ownedCtx)
	go r.messageSubscriptionLeaseRenewalLoop(ownedCtx, owner, cancel)
	go r.messageSubscriptionCollectLoop(ownedCtx)
	go r.runMessageSubscriptionLiveLoop(ownedCtx)
	<-ownedCtx.Done()
	return ownedCtx.Err()
}

func (r Runner) runMessageSubscriptionLiveLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if err := r.runMessageSubscriptionListenerOnce(ctx); err != nil {
			if errors.Is(err, ErrMessageSubscriptionProxyChanged) {
				r.recordMessageSubscriptionHeartbeat(err.Error())
				continue
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			if appmessaging.IsTelegramRuntimeConfigError(err) {
				infralogging.Logger().Info("message subscription live listener disabled",
					infralogging.Event("messaging.listener"),
					infralogging.Group("messaging"),
					infralogging.Method("listen"),
					infralogging.Status(infralogging.StatusSkipped),
					zap.Any(infralogging.FieldDuration, nil),
					zap.String("reason", err.Error()),
				)
			} else {
				r.recordMessageSubscriptionHeartbeat(err.Error())
			}
			sleepWithContext(ctx, 30*time.Second)
			continue
		}
		r.recordMessageSubscriptionHeartbeat("")
	}
}

func (r Runner) runMessageSubscriptionListenerOnce(ctx context.Context) (err error) {
	start := time.Now()
	defer func() {
		if appmessaging.IsTelegramRuntimeConfigError(err) {
			infralogging.Logger().Info("message subscription listener iteration skipped",
				infralogging.Event("messaging.listener"),
				infralogging.Group("messaging"),
				infralogging.Method("listen"),
				infralogging.Status(infralogging.StatusSkipped),
				infralogging.DurationSince(start),
				zap.String("reason", err.Error()),
			)
			return
		}
		infralogging.LogOperation(start, "messaging.listener", "messaging", "listen", "message subscription listener iteration completed", err)
	}()
	usecase := messagingUsecase(r)
	credentials, err := usecase.TelegramRuntimeCredentials(ctx, true)
	if err != nil {
		return err
	}
	repo := gormrepo.NewMessagingRepository(r.db)
	subscriptions, err := repo.ListSubscriptionsForCollect(ctx, nil)
	if err != nil {
		return err
	}
	liveSubscriptions := make([]domainmsg.MessageSubscription, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		if subscription.Provider == domainmsg.ProviderTelegramChannel {
			liveSubscriptions = append(liveSubscriptions, subscription)
		}
	}
	if len(liveSubscriptions) == 0 {
		sleepWithContext(ctx, 30*time.Second)
		return nil
	}
	listenCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	startRevision := runtimeproxy.Revision(r.db)
	var proxyChanged atomic.Bool
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-listenCtx.Done():
				return
			case <-ticker.C:
				if runtimeproxy.Revision(r.db) != startRevision {
					proxyChanged.Store(true)
					cancel()
					return
				}
			}
		}
	}()
	service := inframessaging.NewService(r.settings)
	err = service.Listen(listenCtx, credentials, liveSubscriptions, r.handleLiveMessage, func(_ context.Context, subscriptionID uint, title string) error {
		writeCtx, cancel := context.WithTimeout(context.Background(), messageSubscriptionMetadataWriteTimeout)
		defer cancel()
		return usecase.UpdateSubscriptionTitle(writeCtx, subscriptionID, title)
	})
	if proxyChanged.Load() && errors.Is(err, context.Canceled) {
		return ErrMessageSubscriptionProxyChanged
	}
	return err
}

func (r Runner) handleLiveMessage(_ context.Context, live inframessaging.LiveMessage) (err error) {
	start := time.Now()
	defer func() {
		infralogging.LogOperation(start, "messaging.live_message", "messaging", "ingest", "message subscription live message handled", err,
			zap.Uint("subscriptionId", live.SubscriptionID),
			zap.String("sourceMessageId", live.SourceMessageID),
		)
	}()
	if strings.TrimSpace(live.Text) == "" {
		infralogging.Logger().Info("message subscription live message skipped",
			infralogging.Event("messaging.live_message"),
			infralogging.Group("messaging"),
			infralogging.Method("ingest"),
			infralogging.Status(infralogging.StatusSkipped),
			infralogging.DurationSince(start),
			zap.Uint("subscriptionId", live.SubscriptionID),
			zap.String("sourceMessageId", live.SourceMessageID),
		)
		return nil
	}
	workCtx, cancel := context.WithTimeout(context.Background(), r.messageSubscriptionHandleTimeout())
	defer cancel()
	repo := gormrepo.NewMessagingRepository(r.db)
	subscription, found, err := repo.FindSubscription(workCtx, live.SubscriptionID)
	if err != nil || !found || !subscription.Enabled {
		return err
	}
	_, _, err = messagingUsecase(r).IngestFetchedMessage(workCtx, *subscription, appmessaging.FetchedMessage{
		SourceMessageID: live.SourceMessageID,
		MessageTime:     live.MessageTime,
		Text:            live.Text,
		Raw:             live.Raw,
	}, "mtproto_live", nil)
	return err
}

func (r Runner) messageSubscriptionCollectLoop(ctx context.Context) {
	r.collectMessageSubscriptionsOnce(ctx)
	ticker := time.NewTicker(messageSubscriptionCollectInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.collectMessageSubscriptionsOnce(ctx)
		}
	}
}

func (r Runner) collectMessageSubscriptionsOnce(ctx context.Context) {
	repo := gormrepo.NewMessagingRepository(r.db)
	subscriptions, err := repo.ListSubscriptionsForCollect(ctx, nil)
	if err != nil {
		r.recordMessageSubscriptionHeartbeat(err.Error())
		return
	}
	for _, subscription := range subscriptions {
		if !subscriptionDueForCollect(subscription, time.Now()) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return
		}
		collectCtx, cancel := context.WithTimeout(ctx, messageSubscriptionCollectTimeout)
		_, found, err := messagingUsecase(r).QueueCollectSubscriptions(collectCtx, appmessaging.CollectInput{
			SubscriptionID: &subscription.ID,
			Limit:          messageSubscriptionCollectLimit,
		})
		cancel()
		if err != nil {
			r.recordMessageSubscriptionHeartbeat(fmt.Sprintf("%s: %v", subscription.Title, err))
			continue
		}
		if !found {
			continue
		}
		r.recordMessageSubscriptionHeartbeat("")
	}
}

func subscriptionDueForCollect(subscription domainmsg.MessageSubscription, now time.Time) bool {
	if subscription.NextCollectAt == nil || subscription.NextCollectAt.IsZero() {
		return true
	}
	return !subscription.NextCollectAt.After(now)
}

func (r Runner) dispatchMessageSubscriptionMeetings(ctx context.Context, meetings []*domainmeeting.Meeting) error {
	for _, meeting := range meetings {
		if meeting == nil {
			continue
		}
		if _, err := r.meetingDispatchUsecase().DispatchRun(ctx, meeting, "queued", "", nil); err != nil {
			return err
		}
	}
	return nil
}

func (r Runner) acquireMessageSubscriptionLease(ctx context.Context, owner string) (bool, error) {
	now := time.Now()
	expiresAt := now.Add(messageSubscriptionLeaseTTL)
	value := domainkernel.NewJSON(map[string]any{"owner": owner, "expires_at": expiresAt})
	description := "message subscription listener lease: " + owner
	repo := gormrepo.NewMessagingRepository(r.db)
	return repo.TryAcquireLease(ctx, appmessaging.SubscriptionListenerLeaseKey, value, description, now, now.Add(-messageSubscriptionLeaseTTL), func(setting domainsettings.AppSetting) bool {
		return appmessaging.ListenerLeaseClaimable(setting, owner, now, messageSubscriptionLeaseTTL)
	})
}

func (r Runner) messageSubscriptionLeaseRenewalLoop(ctx context.Context, owner string, cancel context.CancelFunc) {
	ticker := time.NewTicker(messageSubscriptionLeaseRenewInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			acquired, err := r.acquireMessageSubscriptionLease(ctx, owner)
			if err != nil {
				r.recordMessageSubscriptionHeartbeat(err.Error())
				continue
			}
			if !acquired {
				r.recordMessageSubscriptionHeartbeat("message subscription listener lease was lost")
				cancel()
				return
			}
		}
	}
}

func (r Runner) messageSubscriptionHeartbeatLoop(ctx context.Context) {
	r.recordMessageSubscriptionHeartbeat("")
	ticker := time.NewTicker(time.Duration(appsystem.HeartbeatIntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.recordMessageSubscriptionHeartbeat("")
		}
	}
}

func (r Runner) recordMessageSubscriptionHeartbeat(errText string) {
	ctx, cancel := context.WithTimeout(context.Background(), messageSubscriptionHeartbeatWriteTimeout)
	defer cancel()
	if err := r.systemUsecase().RecordHeartbeat(ctx, appmessaging.SubscriptionListenerServiceName, r.settings.MeetingDispatchMode, errText); err != nil {
		infralogging.Logger().Error("message subscription listener heartbeat write failed",
			infralogging.Event("system.heartbeat"),
			infralogging.Group("system"),
			infralogging.Method("record"),
			infralogging.Status(infralogging.StatusError),
			zap.Any(infralogging.FieldDuration, nil),
			zap.Error(err),
		)
	}
}

func (r Runner) markMessageSubscriptionStopped(errText string) {
	ctx, cancel := context.WithTimeout(context.Background(), messageSubscriptionHeartbeatWriteTimeout)
	defer cancel()
	if err := r.systemUsecase().MarkStopped(ctx, appmessaging.SubscriptionListenerServiceName, r.settings.MeetingDispatchMode, errText); err != nil {
		infralogging.Logger().Error("message subscription listener stopped heartbeat write failed",
			infralogging.Event("system.heartbeat"),
			infralogging.Group("system"),
			infralogging.Method("stop"),
			infralogging.Status(infralogging.StatusError),
			zap.Any(infralogging.FieldDuration, nil),
			zap.Error(err),
		)
	}
}

func (r Runner) messageSubscriptionHandleTimeout() time.Duration {
	if r.settings.AIChatTimeout > 0 && r.settings.AIJSONMaxAttempts > 0 {
		timeout := time.Duration(r.settings.AIJSONMaxAttempts)*r.settings.AIChatTimeout + 30*time.Second
		if timeout > messageSubscriptionHandleTimeout {
			return timeout
		}
	}
	return messageSubscriptionHandleTimeout
}

func MessageSubscriptionListenerOwner() string {
	host, _ := os.Hostname()
	if strings.TrimSpace(host) == "" {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s:%d", host, os.Getpid())
}

func (r Runner) systemUsecase() appsystem.Usecase {
	return appsystem.NewUsecase(gormrepo.NewSystemRepository(r.db), gormuow.NewSystemUnitOfWork(r.db))
}

func sleepWithContext(ctx context.Context, duration time.Duration) {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
