package telegram

import (
	"context"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domaintelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/telegram"
	"net/http"
	"time"

	apptelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/app/telegram"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"gorm.io/gorm"
)

var BotAPIBase = TelegramBotAPIBase

type PublicResult = TelegramPublicResult

type Service struct {
	db       *gorm.DB
	settings config.Settings
}

func NewService(db *gorm.DB, settings config.Settings) Service {
	return Service{db: db, settings: settings}
}

func (s Service) MTProtoStatus(ctx context.Context) map[string]bool {
	return MTProtoStatus(s.db.WithContext(ctx))
}

func (s Service) StartMTProtoLogin(ctx context.Context, sec apptelegram.SecurityService, phone string) (string, error) {
	return StartMTProtoLogin(s.db.WithContext(ctx), concreteSecurity(sec), phone)
}

func (s Service) CompleteMTProtoLogin(ctx context.Context, sec apptelegram.SecurityService, phone string, code string, phoneCodeHash string, password string) error {
	return CompleteMTProtoLogin(s.db.WithContext(ctx), concreteSecurity(sec), phone, code, phoneCodeHash, password)
}

func (s Service) NormalizeChannelRef(value string) string {
	return NormalizeChannelRef(value)
}

func (s Service) ReconcileChannelBackfill(ctx context.Context, channel *domaintelegram.Channel, previousBackfillLimit int, previousChannelRef string, sec apptelegram.SecurityService) (map[string]int, error) {
	return ReconcileChannelBackfill(s.db.WithContext(ctx), channel, previousBackfillLimit, previousChannelRef, concreteSecurity(sec), s.settings)
}

func (s Service) GetLatestMessage(ctx context.Context, sec apptelegram.SecurityService, channelRef string) (map[string]any, error) {
	return GetLatestMessage(s.db.WithContext(ctx), concreteSecurity(sec), channelRef)
}

func (s Service) CollectChannel(ctx context.Context, channel *domaintelegram.Channel, limit int, sec apptelegram.SecurityService) (int, int, error) {
	return CollectChannel(s.db.WithContext(ctx), channel, limit, concreteSecurity(sec), s.settings)
}

func (s Service) SendBotTest(ctx context.Context, token string, chatID string) (map[string]any, error) {
	return SendBotMessageWithClient(BotAPIBase, token, chatID, "TradingCopilot Telegram Bot test message.", runtimeproxy.NewHTTPClient(s.db.WithContext(ctx), s.settings, runtimeproxy.ModuleTelegram, 15*time.Second))
}

func (s Service) JSON(value any) domainkernel.JSON { return JSON(value) }

func (s Service) ExtractRelatedSymbols(text string) []string { return ExtractRelatedSymbols(text) }

func (s Service) ApplyFilter(ctx context.Context, message *domaintelegram.Message, sec apptelegram.SecurityService) error {
	return ApplyFilter(s.db.WithContext(ctx), message, concreteSecurity(sec), s.settings)
}

func (s Service) EnsureMeetingForMessage(ctx context.Context, message *domaintelegram.Message, triggerSource string) (*domainmeeting.Meeting, bool, error) {
	return EnsureMeetingForMessage(s.db.WithContext(ctx), message, triggerSource)
}

func (s Service) MessageExternalRef(message domaintelegram.Message) string {
	return MessageExternalRef(message)
}

func MTProtoStatus(db *gorm.DB) map[string]bool {
	return TelegramMTProtoStatus(db)
}

func SaveMTProtoAppConfig(db *gorm.DB, sec security.Service, appID string, appHash string) error {
	return SaveTelegramMTProtoAppConfig(db, sec, appID, appHash)
}

func StartMTProtoLogin(db *gorm.DB, sec security.Service, phone string) (string, error) {
	return StartTelegramMTProtoLogin(db, sec, phone)
}

func CompleteMTProtoLogin(db *gorm.DB, sec security.Service, phone string, code string, phoneCodeHash string, password string) error {
	return CompleteTelegramMTProtoLogin(db, sec, phone, code, phoneCodeHash, password)
}

func LatestMTProtoMessage(db *gorm.DB, sec security.Service, channelRef string) (map[string]any, error) {
	return GetLatestTelegramMTProtoMessage(db, sec, channelRef)
}

func NormalizeChannelRef(value string) string {
	return NormalizeTelegramChannelRef(value)
}

func ReconcileChannelBackfill(db *gorm.DB, channel *domaintelegram.Channel, previousBackfillLimit int, previousChannelRef string, sec security.Service, settings config.Settings) (map[string]int, error) {
	return ReconcileTelegramChannelBackfill(db, channel, previousBackfillLimit, previousChannelRef, sec, settings)
}

func MessageExternalRef(message domaintelegram.Message) string {
	return TelegramMessageExternalRef(message)
}

func GetLatestMessage(db *gorm.DB, sec security.Service, channelRef string) (map[string]any, error) {
	return GetLatestTelegramMessage(db, sec, channelRef)
}

func CollectChannel(db *gorm.DB, channel *domaintelegram.Channel, limit int, sec security.Service, settings config.Settings) (int, int, error) {
	return CollectTelegramChannel(db, channel, limit, sec, settings)
}

func SendBotMessageWithClient(apiBase string, token string, chatID string, text string, client *http.Client) (map[string]any, error) {
	return SendTelegramBotMessageWithClient(apiBase, token, chatID, text, client)
}

func ApplyFilter(db *gorm.DB, message *domaintelegram.Message, sec security.Service, settings config.Settings) error {
	return ApplyTelegramFilter(db, message, sec, settings)
}

func EnsureMeetingForMessage(db *gorm.DB, message *domaintelegram.Message, triggerSource string) (*domainmeeting.Meeting, bool, error) {
	return EnsureMeetingForTelegramMessage(db, message, triggerSource)
}

func RunBotListener(ctx context.Context, db *gorm.DB, settings config.Settings, sec security.Service, dispatch MeetingDispatcher) {
	RunTelegramBotListener(ctx, db, settings, sec, dispatch)
}

func RunChannelListener(ctx context.Context, db *gorm.DB, settings config.Settings, sec security.Service, dispatch MeetingDispatcher) {
	RunTelegramChannelListener(ctx, db, settings, sec, dispatch)
}

func concreteSecurity(sec apptelegram.SecurityService) security.Service {
	if typed, ok := sec.(security.Service); ok {
		return typed
	}
	return security.Service{}
}
