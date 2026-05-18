package telegram

import (
	"context"
	"errors"
	"fmt"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domaintelegram "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/telegram"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	persistmodel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/model"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/security"
	infratelegram "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/telegram"
	"gorm.io/gorm"
)

const (
	telegramSecretAppID        = "app_id"
	telegramSecretAppHash      = "app_hash"
	telegramSecretMTSession    = "mtproto_session"
	telegramSecretLoginSession = "mtproto_login_session"
	telegramSecretLoginPhone   = "mtproto_login_phone"
	telegramSecretLoginHash    = "mtproto_login_phone_code_hash"
)

type TelegramMTProtoClient interface {
	StartLogin(ctx context.Context, db *gorm.DB, sec security.Service, phone string) (string, error)
	CompleteLogin(ctx context.Context, db *gorm.DB, sec security.Service, phone string, code string, phoneCodeHash string, password string) error
	LatestMessage(ctx context.Context, db *gorm.DB, sec security.Service, channelRef string) (map[string]any, error)
	RecentMessages(ctx context.Context, db *gorm.DB, sec security.Service, channelRef string, limit int, minMessageID *int64) (TelegramPublicResult, error)
}

var telegramMTProto TelegramMTProtoClient = gotdMTProtoClient{}

type gotdMTProtoClient struct{}

type telegramSecretStore struct {
	db  *gorm.DB
	sec security.Service
}

type TelegramProxyConfig = infratelegram.ProxyConfig

func (s telegramSecretStore) SecretValue(ctx context.Context, name string) (string, error) {
	return telegramSecretValue(s.db.WithContext(ctx), s.sec, name)
}

func (s telegramSecretStore) UpsertSecret(ctx context.Context, name string, value string) error {
	return upsertTelegramSecret(s.db.WithContext(ctx), s.sec, name, value)
}

func parseTelegramProxyURL(raw string) (*TelegramProxyConfig, error) {
	return infratelegram.ParseProxyURL(raw)
}

func telegramRuntimeProxy(db *gorm.DB, settings config.Settings) *TelegramProxyConfig {
	cfg := runtimeproxy.Load(db, settings)
	if !cfg.EnabledTelegram || strings.TrimSpace(cfg.ProxyURL) == "" {
		return nil
	}
	proxyCfg, err := parseTelegramProxyURL(cfg.ProxyURL)
	if err != nil {
		return nil
	}
	return proxyCfg
}

func telegramProxyDialer(cfg *TelegramProxyConfig) (func(context.Context, string, string) (net.Conn, error), bool) {
	return infratelegram.ProxyDialer(cfg)
}

func TelegramMTProtoStatus(db *gorm.DB) map[string]bool {
	return map[string]bool{
		"has_app_id":   telegramSecretExists(db, telegramSecretAppID),
		"has_app_hash": telegramSecretExists(db, telegramSecretAppHash),
		"has_session":  telegramSecretExists(db, telegramSecretMTSession),
	}
}

func SaveTelegramMTProtoAppConfig(db *gorm.DB, sec security.Service, appID string, appHash string) error {
	appID = strings.TrimSpace(appID)
	appHash = strings.TrimSpace(appHash)
	if appID == "" {
		return errors.New("telegram app_id is required")
	}
	if _, err := strconv.Atoi(appID); err != nil {
		return fmt.Errorf("telegram app_id is invalid")
	}
	if appHash == "" {
		return errors.New("telegram app_hash is required")
	}
	if err := upsertTelegramSecret(db, sec, telegramSecretAppID, appID); err != nil {
		return err
	}
	return upsertTelegramSecret(db, sec, telegramSecretAppHash, appHash)
}

func StartTelegramMTProtoLogin(db *gorm.DB, sec security.Service, phone string) (string, error) {
	ctx, cancel := context.WithTimeout(dbContext(db), 60*time.Second)
	defer cancel()
	hash, err := telegramMTProto.StartLogin(ctx, db, sec, phone)
	if err != nil {
		return "", err
	}
	if err := upsertTelegramSecret(db, sec, telegramSecretLoginHash, hash); err != nil {
		return "", err
	}
	return hash, nil
}

func CompleteTelegramMTProtoLogin(db *gorm.DB, sec security.Service, phone string, code string, phoneCodeHash string, password string) error {
	ctx, cancel := context.WithTimeout(dbContext(db), 90*time.Second)
	defer cancel()
	if strings.TrimSpace(phone) == "" {
		if value, err := telegramSecretValue(db, sec, telegramSecretLoginPhone); err == nil {
			phone = value
		}
	}
	if strings.TrimSpace(phoneCodeHash) == "" {
		if value, err := telegramSecretValue(db, sec, telegramSecretLoginHash); err == nil {
			phoneCodeHash = value
		}
	}
	if err := telegramMTProto.CompleteLogin(ctx, db, sec, phone, code, phoneCodeHash, password); err != nil {
		return err
	}
	return gormrepo.NewTelegramRepository(db).DeleteSecretsByNames(dbContext(db), domainkernel.SecretKindTelegram, []string{telegramSecretLoginHash})
}

func GetLatestTelegramMessage(db *gorm.DB, sec security.Service, channelRef string) (map[string]any, error) {
	if _, err := publicTelegramUsername(NormalizeTelegramChannelRef(channelRef)); err == nil {
		result, err := FetchPublicTelegramMessagesWithClient(channelRef, 1, nil, runtimeproxy.NewHTTPClient(db, config.Load(), runtimeproxy.ModuleTelegram, 20*time.Second))
		if err != nil {
			return nil, err
		}
		if len(result.Messages) == 0 {
			return map[string]any{"status": "empty", "channel_ref": result.ChannelRef, "title": result.Title, "message_id": nil, "message_time": nil, "text": nil}, nil
		}
		message := result.Messages[0]
		return map[string]any{"status": "ok", "channel_ref": result.ChannelRef, "title": result.Title, "message_id": message.MessageID, "message_time": message.MessageTime, "text": message.Text}, nil
	}
	ctx, cancel := context.WithTimeout(dbContext(db), 45*time.Second)
	defer cancel()
	return telegramMTProto.LatestMessage(ctx, db, sec, channelRef)
}

func GetLatestTelegramMTProtoMessage(db *gorm.DB, sec security.Service, channelRef string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(dbContext(db), 45*time.Second)
	defer cancel()
	return telegramMTProto.LatestMessage(ctx, db, sec, channelRef)
}

func CollectTelegramChannel(db *gorm.DB, channel *domaintelegram.Channel, limit int, sec security.Service, settings config.Settings) (int, int, error) {
	if _, err := publicTelegramUsername(channel.ChannelRef); err == nil {
		return CollectPublicTelegramChannel(db, channel, limit, sec, settings)
	}
	return CollectMTProtoTelegramChannel(db, channel, limit, sec, settings)
}

func ReconcileTelegramChannelBackfill(db *gorm.DB, channel *domaintelegram.Channel, previousBackfillLimit int, previousChannelRef string, sec security.Service, settings config.Settings) (map[string]int, error) {
	if _, err := publicTelegramUsername(channel.ChannelRef); err == nil {
		return ReconcilePublicTelegramChannelBackfill(db, channel, previousBackfillLimit, previousChannelRef, sec, settings)
	}
	return ReconcileMTProtoTelegramChannelBackfill(db, channel, previousBackfillLimit, sec, settings)
}

func CollectMTProtoTelegramChannel(db *gorm.DB, channel *domaintelegram.Channel, limit int, sec security.Service, settings config.Settings) (int, int, error) {
	var latest persistmodel.TelegramMessage
	var minID *int64
	if err := db.Where("channel_id = ?", channel.ID).Order("message_id desc").First(&latest).Error; err == nil {
		minID = &latest.MessageID
	}
	fetchLimit := limit
	if fetchLimit < 100 {
		fetchLimit = 100
	}
	ctx, cancel := context.WithTimeout(dbContext(db), 90*time.Second)
	defer cancel()
	result, err := telegramMTProto.RecentMessages(ctx, db, sec, channel.ChannelRef, fetchLimit, minID)
	if err != nil {
		return 0, 0, err
	}
	if strings.TrimSpace(result.Title) != "" && result.Title != channel.Title {
		channel.Title = result.Title
		_ = gormrepo.NewTelegramRepository(db).SaveChannel(dbContext(db), channel)
	}
	collected := 0
	filtered := 0
	for i := len(result.Messages) - 1; i >= 0; i-- {
		item := result.Messages[i]
		if !channel.CollectFrom.IsZero() && item.MessageTime.Before(channel.CollectFrom) {
			continue
		}
		message, created, err := IngestTelegramChannelMessage(db, channel, item, "mtproto", nil, sec, settings)
		if err != nil {
			return collected, filtered, err
		}
		if !created || message == nil {
			continue
		}
		collected++
		if message.FilterDecision != nil || message.FilterReason != nil {
			filtered++
		}
	}
	return collected, filtered, nil
}

func ReconcileMTProtoTelegramChannelBackfill(db *gorm.DB, channel *domaintelegram.Channel, previousBackfillLimit int, sec security.Service, settings config.Settings) (map[string]int, error) {
	result := map[string]int{"added": 0, "removed": 0}
	if channel.BackfillLimit <= 0 {
		channel.BackfillLimit = 0
	}
	var storedRows []persistmodel.TelegramMessage
	if err := db.Where("channel_id = ?", channel.ID).Find(&storedRows).Error; err != nil {
		return result, err
	}
	stored := telegramMessagesFromModel(storedRows)
	oldBackfillIDs := map[int64]struct{}{}
	if previousBackfillLimit > 0 {
		sort.Slice(stored, func(i, j int) bool { return stored[i].MessageID > stored[j].MessageID })
		count := 0
		for _, message := range stored {
			if telegramRawBool(message.Raw, "manual") {
				continue
			}
			oldBackfillIDs[message.MessageID] = struct{}{}
			count++
			if count >= previousBackfillLimit {
				break
			}
		}
	}
	if channel.BackfillLimit == 0 {
		return removeUndesiredBackfillMessages(db, stored, oldBackfillIDs, map[int64]struct{}{}, result)
	}
	ctx, cancel := context.WithTimeout(dbContext(db), 90*time.Second)
	defer cancel()
	fetched, err := telegramMTProto.RecentMessages(ctx, db, sec, channel.ChannelRef, channel.BackfillLimit, nil)
	if err != nil {
		return result, err
	}
	if strings.TrimSpace(fetched.Title) != "" && fetched.Title != channel.Title {
		channel.Title = fetched.Title
		_ = gormrepo.NewTelegramRepository(db).SaveChannel(dbContext(db), channel)
	}
	desiredIDs := map[int64]struct{}{}
	managed := true
	for i := len(fetched.Messages) - 1; i >= 0; i-- {
		item := fetched.Messages[i]
		desiredIDs[item.MessageID] = struct{}{}
		message, created, err := IngestTelegramChannelMessage(db, channel, item, "mtproto_backfill", &managed, sec, settings)
		if err != nil {
			return result, err
		}
		if created && message != nil {
			result["added"]++
		}
	}
	return removeUndesiredBackfillMessages(db, stored, oldBackfillIDs, desiredIDs, result)
}

func removeUndesiredBackfillMessages(db *gorm.DB, stored []domaintelegram.Message, oldBackfillIDs map[int64]struct{}, desiredIDs map[int64]struct{}, result map[string]int) (map[string]int, error) {
	refs := []string{}
	ids := []uint{}
	for _, message := range stored {
		if telegramRawBool(message.Raw, "manual") {
			continue
		}
		_, wasOldBackfill := oldBackfillIDs[message.MessageID]
		_, isDesired := desiredIDs[message.MessageID]
		if (telegramRawBool(message.Raw, "backfill_managed") || wasOldBackfill) && !isDesired {
			refs = append(refs, TelegramMessageExternalRef(message))
			ids = append(ids, message.ID)
		}
	}
	if len(ids) == 0 {
		return result, nil
	}
	if err := gormrepo.NewTelegramRepository(db).DeleteMessagesByIDsWithRefs(dbContext(db), ids, refs); err != nil {
		return result, err
	}
	result["removed"] = len(ids)
	return result, nil
}

func (gotdMTProtoClient) StartLogin(ctx context.Context, db *gorm.DB, sec security.Service, phone string) (string, error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return "", errors.New("telegram phone is required")
	}
	client, err := telegramMTProtoRuntimeClient(db, sec)
	if err != nil {
		return "", err
	}
	hash, err := client.StartLogin(ctx, phone)
	if err != nil {
		return "", err
	}
	if err := upsertTelegramSecret(db, sec, telegramSecretLoginPhone, phone); err != nil {
		return "", err
	}
	return hash, nil
}

func (gotdMTProtoClient) CompleteLogin(ctx context.Context, db *gorm.DB, sec security.Service, phone string, code string, phoneCodeHash string, password string) error {
	phone = strings.TrimSpace(phone)
	code = strings.TrimSpace(code)
	phoneCodeHash = strings.TrimSpace(phoneCodeHash)
	if phone == "" || code == "" || phoneCodeHash == "" {
		return errors.New("phone, code, and phone_code_hash are required")
	}
	client, err := telegramMTProtoRuntimeClient(db, sec)
	if err != nil {
		return err
	}
	session, err := client.CompleteLogin(ctx, phone, code, phoneCodeHash, password)
	if err != nil {
		return err
	}
	if err := upsertTelegramSecret(db, sec, telegramSecretMTSession, session); err != nil {
		return err
	}
	return gormrepo.NewTelegramRepository(db).DeleteSecretsByNames(dbContext(db), domainkernel.SecretKindTelegram, []string{telegramSecretLoginSession, telegramSecretLoginPhone, telegramSecretLoginHash})
}

func (c gotdMTProtoClient) LatestMessage(ctx context.Context, db *gorm.DB, sec security.Service, channelRef string) (map[string]any, error) {
	client, err := telegramMTProtoRuntimeClient(db, sec)
	if err != nil {
		return nil, err
	}
	return client.LatestMessage(ctx, channelRef)
}

func (gotdMTProtoClient) RecentMessages(ctx context.Context, db *gorm.DB, sec security.Service, channelRef string, limit int, minMessageID *int64) (TelegramPublicResult, error) {
	client, err := telegramMTProtoRuntimeClient(db, sec)
	if err != nil {
		return TelegramPublicResult{}, err
	}
	return client.RecentMessages(ctx, channelRef, limit, minMessageID)
}

func telegramMTProtoRuntimeClient(db *gorm.DB, sec security.Service) (infratelegram.MTProtoClient, error) {
	appID, appHash, err := telegramAppCredentials(db, sec)
	if err != nil {
		return infratelegram.MTProtoClient{}, err
	}
	store := telegramSecretStore{db: db, sec: sec}
	return infratelegram.MTProtoClient{
		Credentials:    infratelegram.MTProtoCredentials{AppID: appID, AppHash: appHash},
		LoginStorage:   &infratelegram.MTProtoSessionStorage{Store: store, SecretName: telegramSecretLoginSession},
		SessionStorage: &infratelegram.MTProtoSessionStorage{Store: store, SecretName: telegramSecretMTSession},
		Proxy:          telegramRuntimeProxy(db, config.Load()),
		Location:       appTZ,
	}, nil
}

func telegramAppCredentials(db *gorm.DB, sec security.Service) (int, string, error) {
	appIDText, err := telegramSecretValue(db, sec, telegramSecretAppID)
	if err != nil {
		return 0, "", fmt.Errorf("telegram secret app_id is not configured")
	}
	appID, err := strconv.Atoi(strings.TrimSpace(appIDText))
	if err != nil || appID <= 0 {
		return 0, "", fmt.Errorf("telegram app_id is invalid")
	}
	appHash, err := telegramSecretValue(db, sec, telegramSecretAppHash)
	if err != nil || strings.TrimSpace(appHash) == "" {
		return 0, "", fmt.Errorf("telegram secret app_hash is not configured")
	}
	return appID, strings.TrimSpace(appHash), nil
}

func telegramSecretValue(db *gorm.DB, sec security.Service, name string) (string, error) {
	var row persistmodel.Secret
	if err := db.Where("kind = ? AND name = ?", domainkernel.SecretKindTelegram, name).First(&row).Error; err != nil {
		return "", err
	}
	return sec.DecryptSecret(row.EncryptedValue)
}

func telegramSecretExists(db *gorm.DB, name string) bool {
	_, found, err := gormrepo.NewTelegramRepository(db).FindSecret(dbContext(db), domainkernel.SecretKindTelegram, name)
	return err == nil && found
}

func upsertTelegramSecret(db *gorm.DB, sec security.Service, name string, value string) error {
	encrypted, err := sec.EncryptSecret(value)
	if err != nil {
		return err
	}
	return gormrepo.NewTelegramRepository(db).UpsertSecret(dbContext(db), domainkernel.SecretKindTelegram, name, encrypted)
}
