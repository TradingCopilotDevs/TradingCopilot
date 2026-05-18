package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/meeting"
	domainsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/settings"
	domaintelegram "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/telegram"
	"strconv"
	"strings"
	"time"
)

type Usecase struct {
	repo     Repository
	service  Service
	security SecurityService
	tx       Transactor
}

type SecurityService interface {
	EncryptSecret(value string) (string, error)
	DecryptSecret(value string) (string, error)
}

func NewUsecase(repo Repository, service Service, sec SecurityService, tx ...Transactor) Usecase {
	u := Usecase{repo: repo, service: service, security: sec}
	if len(tx) > 0 {
		u.tx = tx[0]
	}
	return u
}

type Repository interface {
	FindAppSetting(ctx context.Context, key string) (*domainsettings.AppSetting, bool, error)
	SaveAppSetting(ctx context.Context, setting *domainsettings.AppSetting) error
	TryAcquireLease(ctx context.Context, key string, value domainkernel.JSON, description string, now time.Time, cutoff time.Time, claimable func(domainsettings.AppSetting) bool) (bool, error)
	FindSecret(ctx context.Context, kind domainkernel.SecretKind, name string) (*domainsettings.Secret, bool, error)
	CreateSecret(ctx context.Context, secret *domainsettings.Secret) error
	SaveSecret(ctx context.Context, secret *domainsettings.Secret) error
	ListChannels(ctx context.Context) ([]domaintelegram.Channel, error)
	CreateChannel(ctx context.Context, channel *domaintelegram.Channel) error
	FindChannel(ctx context.Context, id uint) (*domaintelegram.Channel, bool, error)
	SaveChannel(ctx context.Context, channel *domaintelegram.Channel) error
	ListChannelsForCollect(ctx context.Context, channelID *uint) ([]domaintelegram.Channel, error)
	DeleteChannelGraph(ctx context.Context, id uint, externalRefs []string) error
	ListMessagesByChannel(ctx context.Context, channelID uint) ([]domaintelegram.Message, error)
	ListMessages(ctx context.Context, filter RepositoryMessageFilter) ([]domaintelegram.Message, error)
	CreateMessage(ctx context.Context, message *domaintelegram.Message) error
	FindMessage(ctx context.Context, id uint) (*domaintelegram.Message, bool, error)
	SaveMessage(ctx context.Context, message *domaintelegram.Message) error
	DeleteMessage(ctx context.Context, message *domaintelegram.Message, externalRef string) error
	ListMessagesForRefilter(ctx context.Context, ids []uint, onlyUnfiltered bool, limit int) ([]domaintelegram.Message, error)
	FindChannelForMessage(ctx context.Context, channelID uint) (*domaintelegram.Channel, bool, error)
	RecentMeetings(ctx context.Context, limit int) ([]domainmeeting.Meeting, error)
	CreateMeeting(ctx context.Context, meeting *domainmeeting.Meeting) error
	AppendMeetingEvent(ctx context.Context, event *domainmeeting.Event) error
}

type Service interface {
	MTProtoStatus(ctx context.Context) map[string]bool
	StartMTProtoLogin(ctx context.Context, sec SecurityService, phone string) (string, error)
	CompleteMTProtoLogin(ctx context.Context, sec SecurityService, phone string, code string, phoneCodeHash string, password string) error
	NormalizeChannelRef(value string) string
	ReconcileChannelBackfill(ctx context.Context, channel *domaintelegram.Channel, previousBackfillLimit int, previousChannelRef string, sec SecurityService) (map[string]int, error)
	GetLatestMessage(ctx context.Context, sec SecurityService, channelRef string) (map[string]any, error)
	CollectChannel(ctx context.Context, channel *domaintelegram.Channel, limit int, sec SecurityService) (int, int, error)
	SendBotTest(ctx context.Context, token string, chatID string) (map[string]any, error)
	JSON(value any) domainkernel.JSON
	ExtractRelatedSymbols(text string) []string
	ApplyFilter(ctx context.Context, message *domaintelegram.Message, sec SecurityService) error
	EnsureMeetingForMessage(ctx context.Context, message *domaintelegram.Message, triggerSource string) (*domainmeeting.Meeting, bool, error)
	MessageExternalRef(message domaintelegram.Message) string
}

type Transactor interface {
	WithTx(ctx context.Context, fn func(Repository) error) error
}

type RepositoryMessageFilter struct {
	ChannelID      string
	Query          string
	OnlyUnfiltered bool
	Decision       string
	Limit          int
	CursorID       uint64
}

type ChannelInput struct {
	Title         string
	ChannelRef    string
	Enabled       bool
	BackfillLimit int
}

type CollectInput struct {
	ChannelID *uint
	Limit     int
}

type CollectResult struct {
	Channels  int
	Collected int
	Filtered  int
}

type MessageFilter struct {
	ChannelID      string
	Query          string
	OnlyUnfiltered bool
	Decision       string
	Limit          int
	Cursor         string
}

type MessageList struct {
	Rows       []MessageRow
	Limit      int
	NextCursor string
}

type MessageRow struct {
	Message domaintelegram.Message
	Channel domaintelegram.Channel
}

type MessageInput struct {
	ChannelID      uint
	MessageID      int64
	MessageTime    time.Time
	Text           string
	FilterDecision any
	FilterReason   any
	RelatedSymbols any
	HasMessageTime bool
	HasDecision    bool
	HasReason      bool
	HasSymbols     bool
}

type MessageMutation struct {
	Row             MessageRow
	CreatedMeetings []*domainmeeting.Meeting
}

type RefilterInput struct {
	IDs            []uint
	Limit          int
	OnlyUnfiltered bool
}

type RefilterResult struct {
	Rows            []MessageRow
	CreatedMeetings []*domainmeeting.Meeting
}

const BotOffsetSettingKey = "telegram_bot_update_offset"
const BotListenerLeaseSettingKey = "telegram_bot_listener_lease"

func (u Usecase) AppConfigStatus(ctx context.Context) map[string]bool {
	return u.service.MTProtoStatus(ctx)
}

func (u Usecase) SaveAppConfig(ctx context.Context, appID string, appHash string) error {
	return u.withTx(ctx, func(repo Repository) error {
		if strings.TrimSpace(appID) != "" {
			if err := u.upsertEncryptedSecret(ctx, repo, domainkernel.SecretKindTelegram, "app_id", appID); err != nil {
				return err
			}
		}
		if strings.TrimSpace(appHash) != "" {
			if err := u.upsertEncryptedSecret(ctx, repo, domainkernel.SecretKindTelegram, "app_hash", appHash); err != nil {
				return err
			}
		}
		return nil
	})
}

func (u Usecase) StartLogin(ctx context.Context, phone string) (string, error) {
	return u.service.StartMTProtoLogin(ctx, u.security, phone)
}

func (u Usecase) VerifyLogin(ctx context.Context, phone string, code string, phoneCodeHash string, password string) error {
	return u.service.CompleteMTProtoLogin(ctx, u.security, phone, code, phoneCodeHash, password)
}

func (u Usecase) ListChannels(ctx context.Context) ([]domaintelegram.Channel, error) {
	return u.repo.ListChannels(ctx)
}

func (u Usecase) CreateChannel(ctx context.Context, input ChannelInput) (*domaintelegram.Channel, error) {
	row := domaintelegram.Channel{
		Title:         input.Title,
		ChannelRef:    u.service.NormalizeChannelRef(input.ChannelRef),
		Enabled:       input.Enabled,
		BackfillLimit: input.BackfillLimit,
		CollectFrom:   time.Now(),
	}
	if row.BackfillLimit == 0 {
		row.BackfillLimit = 20
	}
	if row.Title == "" {
		row.Title = strings.TrimPrefix(row.ChannelRef, "@")
	}
	if err := u.withTx(ctx, func(repo Repository) error {
		if err := repo.CreateChannel(ctx, &row); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if row.BackfillLimit > 0 {
		if _, err := u.service.ReconcileChannelBackfill(ctx, &row, 0, "", u.security); err != nil {
			return nil, err
		}
		if refreshed, ok, err := u.repo.FindChannel(ctx, row.ID); err == nil && ok {
			row = *refreshed
		} else if err != nil {
			return nil, err
		}
	}
	return &row, nil
}

func (u Usecase) UpdateChannel(ctx context.Context, id uint, input ChannelInput, fields map[string]bool) (*domaintelegram.Channel, bool, error) {
	row, found, err := u.repo.FindChannel(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	previousBackfillLimit := row.BackfillLimit
	previousChannelRef := row.ChannelRef
	if fields["title"] && input.Title != "" {
		row.Title = input.Title
	}
	if fields["channelRef"] && input.ChannelRef != "" {
		row.ChannelRef = u.service.NormalizeChannelRef(input.ChannelRef)
	}
	if fields["enabled"] {
		row.Enabled = input.Enabled
	}
	if fields["backfillLimit"] {
		row.BackfillLimit = input.BackfillLimit
	}
	if err := u.withTx(ctx, func(repo Repository) error {
		if err := repo.SaveChannel(ctx, row); err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, true, err
	}
	if previousBackfillLimit != row.BackfillLimit || previousChannelRef != row.ChannelRef {
		if _, err := u.service.ReconcileChannelBackfill(ctx, row, previousBackfillLimit, previousChannelRef, u.security); err != nil {
			return nil, true, err
		}
		if refreshed, ok, err := u.repo.FindChannel(ctx, row.ID); err == nil && ok {
			row = refreshed
		} else if err != nil {
			return nil, true, err
		}
	}
	return row, true, nil
}

func (u Usecase) DeleteChannel(ctx context.Context, id uint) error {
	return u.withTx(ctx, func(repo Repository) error {
		messages, err := repo.ListMessagesByChannel(ctx, id)
		if err != nil {
			return err
		}
		externalRefs := []string{}
		if len(messages) > 0 {
			externalRefs = make([]string, 0, len(messages))
			for _, message := range messages {
				externalRefs = append(externalRefs, u.service.MessageExternalRef(message))
			}
		}
		return repo.DeleteChannelGraph(ctx, id, externalRefs)
	})
}

func (u Usecase) TestChannelRef(ctx context.Context, channelRef string) (map[string]any, error) {
	return u.service.GetLatestMessage(ctx, u.security, channelRef)
}

func (u Usecase) TestChannel(ctx context.Context, id uint) (map[string]any, bool, error) {
	channel, found, err := u.repo.FindChannel(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	result, err := u.service.GetLatestMessage(ctx, u.security, channel.ChannelRef)
	return result, true, err
}

func (u Usecase) CollectChannels(ctx context.Context, input CollectInput) (CollectResult, bool, error) {
	if input.Limit <= 0 {
		input.Limit = 20
	}
	if input.Limit > 200 {
		input.Limit = 200
	}
	channels, err := u.repo.ListChannelsForCollect(ctx, input.ChannelID)
	if err != nil {
		return CollectResult{}, false, err
	}
	if len(channels) == 0 {
		return CollectResult{}, false, nil
	}
	result := CollectResult{Channels: len(channels)}
	for i := range channels {
		collected, filtered, err := u.service.CollectChannel(ctx, &channels[i], input.Limit, u.security)
		if err != nil {
			return CollectResult{}, true, fmt.Errorf("%s: %w", channels[i].Title, err)
		}
		result.Collected += collected
		result.Filtered += filtered
	}
	return result, true, nil
}

func (u Usecase) BotConfig(ctx context.Context) (map[string]any, error) {
	chatID := ""
	if secret, ok := u.secret(ctx, domainkernel.SecretKindTelegram, "bot_chat_id"); ok {
		value, err := u.security.DecryptSecret(secret.EncryptedValue)
		if err != nil {
			return nil, err
		}
		chatID = value
	}
	return map[string]any{
		"has_bot_token": u.hasSecret(ctx, domainkernel.SecretKindTelegram, "bot_token"),
		"has_chat_id":   u.hasSecret(ctx, domainkernel.SecretKindTelegram, "bot_chat_id"),
		"chat_id":       chatID,
	}, nil
}

func (u Usecase) SaveBotConfig(ctx context.Context, token string, chatID string) error {
	return u.withTx(ctx, func(repo Repository) error {
		if strings.TrimSpace(token) != "" {
			if err := u.upsertEncryptedSecret(ctx, repo, domainkernel.SecretKindTelegram, "bot_token", token); err != nil {
				return err
			}
		}
		if strings.TrimSpace(chatID) != "" {
			if err := u.upsertEncryptedSecret(ctx, repo, domainkernel.SecretKindTelegram, "bot_chat_id", chatID); err != nil {
				return err
			}
		}
		return nil
	})
}

func (u Usecase) TestBot(ctx context.Context) (map[string]any, error) {
	tokenSecret, ok := u.secret(ctx, domainkernel.SecretKindTelegram, "bot_token")
	if !ok {
		return nil, errors.New("Telegram Bot Token is not configured")
	}
	chatSecret, ok := u.secret(ctx, domainkernel.SecretKindTelegram, "bot_chat_id")
	if !ok {
		return nil, errors.New("Telegram Bot Chat ID is not configured")
	}
	token, err := u.security.DecryptSecret(tokenSecret.EncryptedValue)
	if err != nil {
		return nil, err
	}
	chatID, err := u.security.DecryptSecret(chatSecret.EncryptedValue)
	if err != nil {
		return nil, err
	}
	return u.service.SendBotTest(ctx, token, chatID)
}

func (u Usecase) LoadBotOffset(ctx context.Context) (int64, error) {
	setting, found, err := u.repo.FindAppSetting(ctx, BotOffsetSettingKey)
	if err != nil || !found || setting == nil {
		return 0, err
	}
	var payload map[string]any
	if json.Unmarshal(setting.Value, &payload) != nil {
		return 0, nil
	}
	switch v := payload["offset"].(type) {
	case float64:
		return int64(v), nil
	case string:
		parsed, _ := strconv.ParseInt(v, 10, 64)
		return parsed, nil
	default:
		return 0, nil
	}
}

func (u Usecase) StoreBotOffset(ctx context.Context, offset int64) error {
	description := "telegram bot update offset"
	row := domainsettings.AppSetting{Key: BotOffsetSettingKey, Value: u.service.JSON(map[string]any{"offset": offset}), Description: &description, UpdatedAt: time.Now()}
	return u.withTx(ctx, func(repo Repository) error {
		return repo.SaveAppSetting(ctx, &row)
	})
}

func (u Usecase) AcquireBotListenerLease(ctx context.Context, owner string, ttl time.Duration) (bool, error) {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = "unknown"
	}
	if ttl <= 0 {
		ttl = 45 * time.Second
	}
	now := time.Now()
	cutoff := now.Add(-ttl)
	description := "telegram bot listener lease: " + owner
	value := u.service.JSON(map[string]any{
		"owner":      owner,
		"expires_at": now.Add(ttl).Format(time.RFC3339Nano),
	})
	return u.repo.TryAcquireLease(ctx, BotListenerLeaseSettingKey, value, description, now, cutoff, func(setting domainsettings.AppSetting) bool {
		return botLeaseClaimable(setting, owner, now, ttl)
	})
}

func (u Usecase) ListMessages(ctx context.Context, filter MessageFilter) (MessageList, error) {
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	var cursorID uint64
	if filter.Cursor != "" {
		cursorID, _ = strconv.ParseUint(filter.Cursor, 10, 64)
	}
	messages, err := u.repo.ListMessages(ctx, RepositoryMessageFilter{
		ChannelID:      filter.ChannelID,
		Query:          filter.Query,
		OnlyUnfiltered: filter.OnlyUnfiltered,
		Decision:       filter.Decision,
		Limit:          filter.Limit + 1,
		CursorID:       cursorID,
	})
	if err != nil {
		return MessageList{}, err
	}
	nextCursor := ""
	if len(messages) > filter.Limit {
		nextCursor = fmt.Sprint(messages[filter.Limit-1].ID)
		messages = messages[:filter.Limit]
	}
	return MessageList{Rows: u.hydrateMessages(ctx, messages), Limit: filter.Limit, NextCursor: nextCursor}, nil
}

func (u Usecase) CreateMessage(ctx context.Context, input MessageInput) (*MessageMutation, error) {
	messageTime := time.Now()
	if input.HasMessageTime {
		messageTime = input.MessageTime
	}
	row := domaintelegram.Message{
		ChannelID:   input.ChannelID,
		MessageID:   input.MessageID,
		MessageTime: messageTime,
		Text:        input.Text,
		Raw:         u.service.JSON(map[string]any{"manual": true}),
	}
	u.applyMessageFilterInput(&row, input)
	if row.MessageID == 0 {
		row.MessageID = -time.Now().UnixMilli()
	}
	if row.RelatedSymbols == nil {
		row.RelatedSymbols = u.service.JSON(u.service.ExtractRelatedSymbols(row.Text))
	}
	var created []*domainmeeting.Meeting
	if err := u.withTx(ctx, func(repo Repository) error {
		if err := repo.CreateMessage(ctx, &row); err != nil {
			return err
		}
		created = u.ensureMeetingForMessage(ctx, u.service, &row, "telegram_manual")
		return nil
	}); err != nil {
		return nil, err
	}
	return &MessageMutation{Row: u.hydrateMessage(ctx, row), CreatedMeetings: created}, nil
}

func (u Usecase) UpdateMessage(ctx context.Context, id uint, input MessageInput) (*MessageMutation, bool, error) {
	row, found, err := u.repo.FindMessage(ctx, id)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	u.applyMessageFilterInput(row, input)
	if row.RelatedSymbols == nil {
		row.RelatedSymbols = u.service.JSON(u.service.ExtractRelatedSymbols(row.Text))
	}
	now := time.Now()
	row.FilteredAt = &now
	key := "manual"
	row.FilterModelRoleKey = &key
	var created []*domainmeeting.Meeting
	if err := u.withTx(ctx, func(repo Repository) error {
		if err := repo.SaveMessage(ctx, row); err != nil {
			return err
		}
		created = u.ensureMeetingForMessage(ctx, u.service, row, "telegram_manual")
		return nil
	}); err != nil {
		return nil, true, err
	}
	return &MessageMutation{Row: u.hydrateMessage(ctx, *row), CreatedMeetings: created}, true, nil
}
