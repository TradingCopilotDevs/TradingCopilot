package telegram

import (
	"context"
	"errors"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domaintelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/telegram"
	"strings"
	"sync/atomic"
	"time"

	appsystem "github.com/TradingCopilotDevs/TradingCopilot/internal/app/system"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	infratelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/telegram"
	"gorm.io/gorm"
)

const TelegramListenerServiceName = "telegram_listener"

var ErrTelegramProxyChanged = errors.New("telegram proxy configuration changed")

type TelegramLiveMessage struct {
	ChannelID   uint
	MessageID   int64
	MessageTime time.Time
	Text        string
	Raw         domainkernel.JSON
}

type TelegramLiveMTProtoClient interface {
	Listen(ctx context.Context, db *gorm.DB, sec security.Service, channels []domaintelegram.Channel, handle func(context.Context, TelegramLiveMessage) error) error
}

var telegramLiveMTProto TelegramLiveMTProtoClient = gotdTelegramLiveMTProtoClient{}

type gotdTelegramLiveMTProtoClient struct{}

func RunTelegramChannelListener(ctx context.Context, db *gorm.DB, settings config.Settings, sec security.Service, dispatch MeetingDispatcher) {
	heartbeat := systemUsecase(db)
	hbCtx, stopHeartbeat := context.WithCancel(ctx)
	defer stopHeartbeat()
	go telegramListenerHeartbeatLoop(hbCtx, db, settings)
	for {
		select {
		case <-ctx.Done():
			_ = heartbeat.MarkStopped(ctx, TelegramListenerServiceName, settings.MeetingDispatchMode, ctx.Err().Error())
			return
		default:
		}
		if err := runTelegramChannelListenerOnce(ctx, db, settings, sec, dispatch); err != nil {
			if errors.Is(err, ErrTelegramProxyChanged) {
				_ = heartbeat.RecordHeartbeat(ctx, TelegramListenerServiceName, settings.MeetingDispatchMode, err.Error())
				continue
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				_ = heartbeat.MarkStopped(ctx, TelegramListenerServiceName, settings.MeetingDispatchMode, err.Error())
				return
			}
			_ = heartbeat.RecordHeartbeat(ctx, TelegramListenerServiceName, settings.MeetingDispatchMode, err.Error())
			sleepWithContext(ctx, 30*time.Second)
			continue
		}
		_ = heartbeat.RecordHeartbeat(ctx, TelegramListenerServiceName, settings.MeetingDispatchMode, "")
	}
}

func RunTelegramChannelListenerOnceForTest(ctx context.Context, db *gorm.DB, settings config.Settings, sec security.Service, dispatch MeetingDispatcher) error {
	return runTelegramChannelListenerOnce(ctx, db, settings, sec, dispatch)
}

func runTelegramChannelListenerOnce(ctx context.Context, db *gorm.DB, settings config.Settings, sec security.Service, dispatch MeetingDispatcher) error {
	if _, _, err := telegramAppCredentials(db, sec); err != nil {
		return err
	}
	if _, err := telegramSecretValue(db, sec, telegramSecretMTSession); err != nil {
		return errors.New("telegram secret mtproto_session is not configured")
	}
	if _, err := ReconcileTelegramMeetingTriggers(db, 200, dispatch); err != nil {
		return err
	}
	var channelRows []persistmodel.TelegramChannel
	if err := db.Where("enabled = ?", true).Order("id").Find(&channelRows).Error; err != nil {
		return err
	}
	channels := telegramChannelsFromModel(channelRows)
	if len(channels) == 0 {
		return errors.New("no enabled telegram channels")
	}
	listenCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	startRevision := runtimeproxy.Revision(db)
	var proxyChanged atomic.Bool
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-listenCtx.Done():
				return
			case <-ticker.C:
				if runtimeproxy.Revision(db) != startRevision {
					proxyChanged.Store(true)
					cancel()
					return
				}
			}
		}
	}()
	err := telegramLiveMTProto.Listen(listenCtx, db, sec, channels, func(ctx context.Context, message TelegramLiveMessage) error {
		_, err := HandleTelegramLiveMessage(db, settings, sec, message, dispatch)
		return err
	})
	if proxyChanged.Load() && errors.Is(err, context.Canceled) {
		return ErrTelegramProxyChanged
	}
	return err
}

func telegramListenerHeartbeatLoop(ctx context.Context, db *gorm.DB, settings config.Settings) {
	heartbeat := systemUsecase(db)
	_ = heartbeat.RecordHeartbeat(ctx, TelegramListenerServiceName, settings.MeetingDispatchMode, "")
	ticker := time.NewTicker(time.Duration(appsystem.HeartbeatIntervalSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = heartbeat.RecordHeartbeat(ctx, TelegramListenerServiceName, settings.MeetingDispatchMode, "")
		}
	}
}

func HandleTelegramLiveMessage(db *gorm.DB, settings config.Settings, sec security.Service, live TelegramLiveMessage, dispatch MeetingDispatcher) (bool, error) {
	if strings.TrimSpace(live.Text) == "" {
		return false, nil
	}
	var channelRow persistmodel.TelegramChannel
	if err := db.First(&channelRow, live.ChannelID).Error; err != nil {
		return false, err
	}
	channel := telegramChannelFromModel(channelRow)
	if !channel.Enabled {
		return false, nil
	}
	item := TelegramPublicMessage{MessageID: live.MessageID, MessageTime: live.MessageTime, Text: live.Text, Raw: live.Raw}
	message, created, err := IngestTelegramChannelMessage(db, &channel, item, "mtproto_live", nil, sec, settings)
	if err != nil || !created || message == nil {
		return created, err
	}
	if message.FilterDecision == nil || *message.FilterDecision != domainkernel.NewsMeeting {
		return true, nil
	}
	meeting, meetingCreated, err := EnsureMeetingForTelegramMessage(db, message, "telegram")
	if err != nil {
		return true, err
	}
	if meetingCreated && meeting != nil && dispatch != nil {
		if err := dispatch(meeting); err != nil {
			return true, err
		}
	}
	return true, nil
}

func ReconcileTelegramMeetingTriggers(db *gorm.DB, limit int, dispatch MeetingDispatcher) (int, error) {
	if limit <= 0 {
		limit = 200
	}
	var messageRows []persistmodel.TelegramMessage
	if err := db.Where("filter_decision = ?", domainkernel.NewsMeeting).Order("message_time asc, id asc").Limit(limit).Find(&messageRows).Error; err != nil {
		return 0, err
	}
	messages := telegramMessagesFromModel(messageRows)
	created := 0
	for i := range messages {
		meeting, wasCreated, err := EnsureMeetingForTelegramMessage(db, &messages[i], "telegram_reconcile")
		if err != nil {
			return created, err
		}
		if !wasCreated || meeting == nil {
			continue
		}
		created++
		if dispatch != nil {
			if err := dispatch(meeting); err != nil {
				return created, err
			}
		}
	}
	return created, nil
}

func (gotdTelegramLiveMTProtoClient) Listen(ctx context.Context, db *gorm.DB, sec security.Service, channels []domaintelegram.Channel, handle func(context.Context, TelegramLiveMessage) error) error {
	client, err := telegramMTProtoRuntimeClient(db, sec)
	if err != nil {
		return err
	}
	liveChannels := make([]infratelegram.MTProtoLiveChannel, 0, len(channels))
	for _, channel := range channels {
		liveChannels = append(liveChannels, infratelegram.MTProtoLiveChannel{ID: channel.ID, ChannelRef: channel.ChannelRef, Title: channel.Title})
	}
	return client.Listen(ctx, liveChannels, func(ctx context.Context, message infratelegram.MTProtoLiveMessage) error {
		return handle(ctx, TelegramLiveMessage{
			ChannelID:   message.ChannelID,
			MessageID:   message.MessageID,
			MessageTime: message.MessageTime,
			Text:        message.Text,
			Raw:         message.Raw,
		})
	}, func(ctx context.Context, channelID uint, title string) error {
		var channel domaintelegram.Channel
		for i := range channels {
			if channels[i].ID == channelID {
				channel = channels[i]
				break
			}
		}
		if channel.ID == 0 || strings.TrimSpace(title) == "" || title == channel.Title {
			return nil
		}
		channel.Title = title
		return gormrepo.NewTelegramRepository(db).SaveChannel(ctx, &channel)
	})
}
