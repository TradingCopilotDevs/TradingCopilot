package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	domainai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/ai"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/meeting"
	domainmsg "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/messaging"
	domainsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/settings"
)

const (
	SubscriptionListenerServiceName = "message_subscription_listener"
	SubscriptionListenerLeaseKey    = "message_subscription_listener_lease"

	proxySecretName                    = "outbound_proxy_url"
	proxyEnabledAI                     = "OUTBOUND_PROXY_ENABLED_AI"
	proxyEnabledTelegram               = "OUTBOUND_PROXY_ENABLED_TELEGRAM"
	proxyEnabledMarket                 = "OUTBOUND_PROXY_ENABLED_MARKET"
	proxyEnabledWeb                    = "OUTBOUND_PROXY_ENABLED_WEB"
	proxyNoProxy                       = "OUTBOUND_PROXY_NO_PROXY"
	proxyRevision                      = "OUTBOUND_PROXY_REVISION"
	telegramSecretAppID                = "telegram:app_id"
	telegramSecretAppHash              = "telegram:app_hash"
	telegramSecretSession              = "telegram:mtproto_session"
	telegramLoginSession               = "telegram:mtproto_login_session"
	telegramLoginPhone                 = "telegram:mtproto_login_phone"
	telegramLoginHash                  = "telegram:mtproto_login_phone_code_hash"
	defaultTelegramPollIntervalSeconds = 30
	defaultRSSPollIntervalSeconds      = 900
)

var (
	ErrTelegramAppIDNotConfigured   = errors.New("message subscription secret telegram app_id is not configured")
	ErrTelegramAppHashNotConfigured = errors.New("message subscription secret telegram app_hash is not configured")
	ErrTelegramSessionNotConfigured = errors.New("message subscription secret telegram mtproto_session is not configured")
	ErrTelegramAppIDInvalid         = errors.New("telegram app_id is invalid")
	ErrSubscriptionFilterNotFound   = errors.New("message subscription filter not found")
)

var DefaultFilterSeed = domainmsg.MessageSubscriptionFilter{
	Name:        "默认消息过滤器",
	Description: "判断新到消息是否足以影响A股市场、行业、主题或个股，并决定忽略、观察还是立即触发会议。",
	PromptTemplate: `你只负责第一道消息分诊，不负责完整投研分析。必须只输出JSON：{"decision":"ignore|observe|meeting","reason":"...","related_symbols":["..."]}

判定原则：
1. 看事件本身是否可能引发价格、预期、风险偏好、监管、基本面或流动性的显著变化。
2. 看消息的可信度、来源层级、时效性和传播范围。
3. 看是否对A股市场、行业链条、核心公司或高关注题材有实际映射。
4. 不使用关键词硬规则，必须基于语义和市场影响判断。
5. 只有在值得立刻召集多角色进一步分析时，才输出 meeting。
6. related_symbols 只填写与该消息直接相关的A股6位代码；没有就返回空数组。`,
	Enabled:   true,
	IsDefault: true,
}

var DefaultFilterToolNames = []string{"telegram.recent_messages"}
var DefaultFilterSkillNames = []string{"news-impact-filtering"}

type Usecase struct {
	repo     Repository
	service  Service
	security SecurityService
	tx       Transactor
	queue    TaskQueue
}

func NewUsecase(repo Repository, service Service, sec SecurityService, tx ...Transactor) Usecase {
	u := Usecase{repo: repo, service: service, security: sec}
	if len(tx) > 0 {
		u.tx = tx[0]
	}
	return u
}

func (u Usecase) WithTaskQueue(queue TaskQueue) Usecase {
	u.queue = queue
	return u
}

type SecurityService interface {
	EncryptSecret(value string) (string, error)
	DecryptSecret(value string) (string, error)
}

type Repository interface {
	FindAppSetting(ctx context.Context, key string) (*domainsettings.AppSetting, bool, error)
	SaveAppSetting(ctx context.Context, setting *domainsettings.AppSetting) error
	TryAcquireLease(ctx context.Context, key string, value domainkernel.JSON, description string, now time.Time, cutoff time.Time, claimable func(domainsettings.AppSetting) bool) (bool, error)
	FindSecret(ctx context.Context, kind domainkernel.SecretKind, name string) (*domainsettings.Secret, bool, error)
	CreateSecret(ctx context.Context, secret *domainsettings.Secret) error
	SaveSecret(ctx context.Context, secret *domainsettings.Secret) error
	UpsertSecret(ctx context.Context, kind domainkernel.SecretKind, name string, encryptedValue string) error
	DeleteSecretsByNames(ctx context.Context, kind domainkernel.SecretKind, names []string) error
	ListSubscriptionFilters(ctx context.Context) ([]domainmsg.MessageSubscriptionFilter, error)
	CreateSubscriptionFilter(ctx context.Context, filter *domainmsg.MessageSubscriptionFilter) error
	FindSubscriptionFilter(ctx context.Context, id uint) (*domainmsg.MessageSubscriptionFilter, bool, error)
	FindDefaultSubscriptionFilter(ctx context.Context) (*domainmsg.MessageSubscriptionFilter, bool, error)
	SaveSubscriptionFilter(ctx context.Context, filter *domainmsg.MessageSubscriptionFilter) error
	ClearDefaultSubscriptionFiltersExcept(ctx context.Context, id uint) error
	AssignDefaultFilterToUnboundSubscriptions(ctx context.Context, id uint) error
	DeleteLegacyNewsFilterRole(ctx context.Context) error
	ListSubscriptions(ctx context.Context) ([]domainmsg.MessageSubscription, error)
	CreateSubscription(ctx context.Context, subscription *domainmsg.MessageSubscription) error
	FindSubscription(ctx context.Context, id uint) (*domainmsg.MessageSubscription, bool, error)
	FindSubscriptionByProviderAndSourceRef(ctx context.Context, provider string, sourceRef string) (*domainmsg.MessageSubscription, bool, error)
	SaveSubscription(ctx context.Context, subscription *domainmsg.MessageSubscription) error
	ReplaceSubscriptionTeams(ctx context.Context, subscriptionID uint, teamIDs []uint) error
	ListSubscriptionTeamIDs(ctx context.Context, subscriptionID uint) ([]uint, error)
	ResearchTeamReady(ctx context.Context, teamID uint) (bool, string, error)
	ListSubscriptionsForCollect(ctx context.Context, subscriptionID *uint) ([]domainmsg.MessageSubscription, error)
	DeleteSubscriptionGraph(ctx context.Context, id uint, externalRefs []string) error
	ListMessagesBySubscription(ctx context.Context, subscriptionID uint) ([]domainmsg.IngestedMessage, error)
	ListMessages(ctx context.Context, filter RepositoryMessageFilter) ([]domainmsg.IngestedMessage, error)
	CreateMessage(ctx context.Context, message *domainmsg.IngestedMessage) error
	FindMessage(ctx context.Context, id uint) (*domainmsg.IngestedMessage, bool, error)
	FindMessageBySource(ctx context.Context, subscriptionID uint, sourceMessageID string) (*domainmsg.IngestedMessage, bool, error)
	SaveMessage(ctx context.Context, message *domainmsg.IngestedMessage) error
	DeleteMessage(ctx context.Context, message *domainmsg.IngestedMessage, externalRefs []string) error
	DeleteMessagesByIDsWithRefs(ctx context.Context, ids []uint, externalRefs []string) error
	ListMessagesForRefilter(ctx context.Context, ids []uint, onlyUnfiltered bool, limit int) ([]domainmsg.IngestedMessage, error)
	FindSubscriptionForMessage(ctx context.Context, subscriptionID uint) (*domainmsg.MessageSubscription, bool, error)
	RecentMeetings(ctx context.Context, limit int) ([]domainmeeting.Meeting, error)
	CreateMeeting(ctx context.Context, meeting *domainmeeting.Meeting) error
	AppendMeetingEvent(ctx context.Context, event *domainmeeting.Event) error
	CreateMeetingReference(ctx context.Context, ref *domainmeeting.Reference) error
	FindMeetingReferenceByExternalRef(ctx context.Context, referenceType string, externalRef string) (*domainmeeting.Reference, bool, error)
	FindMeeting(ctx context.Context, id uint) (*domainmeeting.Meeting, bool, error)
	FindAgentRole(ctx context.Context, key string) (*domainai.AgentRole, bool, error)
	FindAIProvider(ctx context.Context, id uint) (*domainai.Provider, bool, error)
	ListPlatformAdapters(ctx context.Context) ([]domainmsg.PlatformAdapter, error)
	CreatePlatformAdapter(ctx context.Context, adapter *domainmsg.PlatformAdapter) error
	FindPlatformAdapter(ctx context.Context, id uint) (*domainmsg.PlatformAdapter, bool, error)
	SavePlatformAdapter(ctx context.Context, adapter *domainmsg.PlatformAdapter) error
	DeletePlatformAdapter(ctx context.Context, id uint) error
}

type Service interface {
	StartSubscriptionLogin(ctx context.Context, credentials TelegramCredentials, phone string) (string, string, error)
	CompleteSubscriptionLogin(ctx context.Context, credentials TelegramCredentials, phone string, code string, phoneCodeHash string, password string) (string, error)
	NormalizeSourceRef(provider string, value string) string
	TestSubscription(ctx context.Context, credentials TelegramCredentials, provider string, sourceRef string) (map[string]any, error)
	FetchSubscriptionMessages(ctx context.Context, credentials TelegramCredentials, subscription domainmsg.MessageSubscription, limit int, afterSourceMessageID *string) (FetchedMessages, error)
	SendAdapterTest(ctx context.Context, token string, chatID string, proxy ProxyConfig) (map[string]any, error)
	JSON(value any) domainkernel.JSON
	ExtractRelatedSymbols(text string) []string
	ApplyFilter(ctx context.Context, message domainmsg.IngestedMessage, sec SecurityService, filter domainmsg.MessageSubscriptionFilter, provider domainai.Provider, proxy ProxyConfig) (FilterResult, error)
}

type Transactor interface {
	WithTx(ctx context.Context, fn func(Repository) error) error
}

type TaskQueue interface {
	EnqueueCollect(ctx context.Context, task CollectTask) error
	EnqueueFilter(ctx context.Context, task FilterTask) error
}

type CollectTask struct {
	SubscriptionID        uint   `json:"subscription_id,omitempty"`
	Reason                string `json:"reason,omitempty"`
	ReconcileBackfill     bool   `json:"reconcile_backfill,omitempty"`
	PreviousBackfillLimit int    `json:"previous_backfill_limit,omitempty"`
	PreviousSourceRef     string `json:"previous_source_ref,omitempty"`
}

type FilterTask struct {
	MessageID uint `json:"message_id"`
	Force     bool `json:"force,omitempty"`
}

type ProxyConfig struct {
	ProxyURL        string
	EnabledAI       bool
	EnabledTelegram bool
	EnabledMarket   bool
	EnabledWeb      bool
	NoProxy         []string
	Revision        string
	UpdatedAt       time.Time
}

type TelegramCredentials struct {
	AppID        int
	AppHash      string
	Session      string
	LoginSession string
	Proxy        ProxyConfig
}

type RepositoryMessageFilter struct {
	SubscriptionID string
	ResearchTeamID string
	Query          string
	OnlyUnfiltered bool
	Decision       string
	Limit          int
	CursorID       uint64
}

type SubscriptionInput struct {
	Provider               string
	Title                  string
	SourceRef              string
	Enabled                bool
	FilterID               uint
	TeamIDs                []uint
	TeamIDsSet             bool
	BackfillLimit          int
	BackfillLimitSpecified bool
	PollIntervalSeconds    int
	PollIntervalSpecified  bool
	Config                 any
}

type SubscriptionFilterInput struct {
	Name           string
	Description    string
	PromptTemplate string
	ProviderID     *uint
	Model          *string
	Enabled        bool
	IsDefault      bool
}

type CollectInput struct {
	SubscriptionID *uint
	Limit          int
}

type CollectResult struct {
	Subscriptions   int
	Collected       int
	Filtered        int
	Queued          int
	Status          string
	CreatedMeetings []*domainmeeting.Meeting
}

type SubscriptionMutation struct {
	domainmsg.MessageSubscription
	CreatedMeetings []*domainmeeting.Meeting
}

type FetchedMessages struct {
	SourceRef string
	Title     string
	Messages  []FetchedMessage
}

type FetchedMessage struct {
	SourceMessageID string
	MessageTime     time.Time
	Text            string
	Raw             domainkernel.JSON
}

type FilterResult struct {
	Decision       *domainkernel.NewsDecision
	Reason         *string
	RelatedSymbols domainkernel.JSON
	FilterID       uint
}

type MessageFilter struct {
	SubscriptionID string
	ResearchTeamID string
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
	Message      domainmsg.IngestedMessage
	Subscription domainmsg.MessageSubscription
}

type MessageInput struct {
	SubscriptionID  uint
	SourceMessageID string
	MessageTime     time.Time
	Text            string
	FilterDecision  any
	FilterReason    any
	RelatedSymbols  any
	HasMessageTime  bool
	HasDecision     bool
	HasReason       bool
	HasSymbols      bool
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

type PlatformAdapterInput struct {
	Provider    string
	DisplayName string
	Enabled     bool
	Config      any
	BotToken    string
	ChatID      string
}

type PlatformAdapterRow struct {
	Adapter     domainmsg.PlatformAdapter
	HasBotToken bool
	HasChatID   bool
	ChatID      string
}

func (u Usecase) SubscriptionStatus(ctx context.Context) map[string]bool {
	return map[string]bool{
		"has_app_id":   u.hasSecret(ctx, domainkernel.SecretKindMessageSubscription, telegramSecretAppID),
		"has_app_hash": u.hasSecret(ctx, domainkernel.SecretKindMessageSubscription, telegramSecretAppHash),
		"has_session":  u.hasSecret(ctx, domainkernel.SecretKindMessageSubscription, telegramSecretSession),
	}
}

func (u Usecase) EnsureDefaultSubscriptionFilter(ctx context.Context) (*domainmsg.MessageSubscriptionFilter, error) {
	var out *domainmsg.MessageSubscriptionFilter
	if err := u.withTx(ctx, func(repo Repository) error {
		row, found, err := repo.FindDefaultSubscriptionFilter(ctx)
		if err != nil {
			return err
		}
		if found && row != nil {
			out = row
			if err := repo.AssignDefaultFilterToUnboundSubscriptions(ctx, row.ID); err != nil {
				return err
			}
			return repo.DeleteLegacyNewsFilterRole(ctx)
		}
		filters, err := repo.ListSubscriptionFilters(ctx)
		if err != nil {
			return err
		}
		if len(filters) > 0 {
			return repo.DeleteLegacyNewsFilterRole(ctx)
		}
		seed := DefaultFilterSeed
		if err := repo.CreateSubscriptionFilter(ctx, &seed); err != nil {
			return err
		}
		if err := repo.ClearDefaultSubscriptionFiltersExcept(ctx, seed.ID); err != nil {
			return err
		}
		out = &seed
		if err := repo.AssignDefaultFilterToUnboundSubscriptions(ctx, seed.ID); err != nil {
			return err
		}
		return repo.DeleteLegacyNewsFilterRole(ctx)
	}); err != nil {
		return nil, err
	}
	return out, nil
}

func (u Usecase) ListSubscriptionFilters(ctx context.Context) ([]domainmsg.MessageSubscriptionFilter, error) {
	if _, err := u.EnsureDefaultSubscriptionFilter(ctx); err != nil {
		return nil, err
	}
	return u.repo.ListSubscriptionFilters(ctx)
}

func (u Usecase) CreateSubscriptionFilter(ctx context.Context, input SubscriptionFilterInput) (*domainmsg.MessageSubscriptionFilter, error) {
	row := domainmsg.MessageSubscriptionFilter{
		Name:           strings.TrimSpace(input.Name),
		Description:    strings.TrimSpace(input.Description),
		PromptTemplate: strings.TrimSpace(input.PromptTemplate),
		ProviderID:     input.ProviderID,
		Model:          cleanStringPtr(input.Model),
		Enabled:        input.Enabled,
		IsDefault:      input.IsDefault,
	}
	if row.Name == "" {
		return nil, errors.New("message subscription filter name is required")
	}
	if row.PromptTemplate == "" {
		return nil, errors.New("message subscription filter prompt template is required")
	}
	if err := u.withTx(ctx, func(repo Repository) error {
		if err := repo.CreateSubscriptionFilter(ctx, &row); err != nil {
			return err
		}
		if row.IsDefault {
			return repo.ClearDefaultSubscriptionFiltersExcept(ctx, row.ID)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &row, nil
}

func (u Usecase) UpdateSubscriptionFilter(ctx context.Context, id uint, input SubscriptionFilterInput, fields map[string]bool) (*domainmsg.MessageSubscriptionFilter, bool, error) {
	var row *domainmsg.MessageSubscriptionFilter
	if err := u.withTx(ctx, func(repo Repository) error {
		foundRow, found, err := repo.FindSubscriptionFilter(ctx, id)
		if err != nil || !found {
			return err
		}
		row = foundRow
		if fields["name"] {
			row.Name = strings.TrimSpace(input.Name)
		}
		if fields["description"] {
			row.Description = strings.TrimSpace(input.Description)
		}
		if fields["promptTemplate"] {
			row.PromptTemplate = strings.TrimSpace(input.PromptTemplate)
		}
		if fields["providerId"] {
			row.ProviderID = input.ProviderID
		}
		if fields["model"] {
			row.Model = cleanStringPtr(input.Model)
		}
		if fields["enabled"] {
			row.Enabled = input.Enabled
		}
		if fields["isDefault"] {
			row.IsDefault = input.IsDefault
		}
		if row.Name == "" {
			return errors.New("message subscription filter name is required")
		}
		if row.PromptTemplate == "" {
			return errors.New("message subscription filter prompt template is required")
		}
		if err := repo.SaveSubscriptionFilter(ctx, row); err != nil {
			return err
		}
		if row.IsDefault {
			return repo.ClearDefaultSubscriptionFiltersExcept(ctx, row.ID)
		}
		return nil
	}); err != nil {
		if row == nil && err == nil {
			return nil, false, nil
		}
		return nil, row != nil, err
	}
	return row, row != nil, nil
}

func (u Usecase) SaveTelegramAppConfig(ctx context.Context, appID string, appHash string) error {
	return u.withTx(ctx, func(repo Repository) error {
		if strings.TrimSpace(appID) != "" {
			if err := u.upsertEncryptedSecret(ctx, repo, domainkernel.SecretKindMessageSubscription, "telegram:app_id", appID); err != nil {
				return err
			}
		}
		if strings.TrimSpace(appHash) != "" {
			if err := u.upsertEncryptedSecret(ctx, repo, domainkernel.SecretKindMessageSubscription, "telegram:app_hash", appHash); err != nil {
				return err
			}
		}
		return nil
	})
}

func (u Usecase) StartTelegramLogin(ctx context.Context, phone string) (string, error) {
	credentials, err := u.TelegramRuntimeCredentials(ctx, false)
	if err != nil {
		return "", err
	}
	hash, loginSession, err := u.service.StartSubscriptionLogin(ctx, credentials, phone)
	if err != nil {
		return "", err
	}
	if err := u.withTx(ctx, func(repo Repository) error {
		if err := u.upsertEncryptedSecret(ctx, repo, domainkernel.SecretKindMessageSubscription, telegramLoginPhone, phone); err != nil {
			return err
		}
		if err := u.upsertEncryptedSecret(ctx, repo, domainkernel.SecretKindMessageSubscription, telegramLoginHash, hash); err != nil {
			return err
		}
		return u.upsertEncryptedSecret(ctx, repo, domainkernel.SecretKindMessageSubscription, telegramLoginSession, loginSession)
	}); err != nil {
		return "", err
	}
	return hash, nil
}

func (u Usecase) VerifyTelegramLogin(ctx context.Context, phone string, code string, phoneCodeHash string, password string) error {
	if strings.TrimSpace(phone) == "" {
		if value, ok := u.decryptedSecret(ctx, domainkernel.SecretKindMessageSubscription, telegramLoginPhone); ok {
			phone = value
		}
	}
	if strings.TrimSpace(phoneCodeHash) == "" {
		if value, ok := u.decryptedSecret(ctx, domainkernel.SecretKindMessageSubscription, telegramLoginHash); ok {
			phoneCodeHash = value
		}
	}
	credentials, err := u.TelegramRuntimeCredentials(ctx, false)
	if err != nil {
		return err
	}
	if value, ok := u.decryptedSecret(ctx, domainkernel.SecretKindMessageSubscription, telegramLoginSession); ok {
		credentials.LoginSession = value
	}
	session, err := u.service.CompleteSubscriptionLogin(ctx, credentials, phone, code, phoneCodeHash, password)
	if err != nil {
		return err
	}
	return u.withTx(ctx, func(repo Repository) error {
		if err := u.upsertEncryptedSecret(ctx, repo, domainkernel.SecretKindMessageSubscription, "telegram:mtproto_session", session); err != nil {
			return err
		}
		return repo.DeleteSecretsByNames(ctx, domainkernel.SecretKindMessageSubscription, []string{telegramLoginSession, telegramLoginPhone, telegramLoginHash})
	})
}

func (u Usecase) ListSubscriptions(ctx context.Context) ([]domainmsg.MessageSubscription, error) {
	if _, err := u.EnsureDefaultSubscriptionFilter(ctx); err != nil {
		return nil, err
	}
	return u.repo.ListSubscriptions(ctx)
}

func (u Usecase) resolveSubscriptionFilterID(ctx context.Context, filterID uint) (uint, error) {
	if filterID > 0 {
		row, found, err := u.repo.FindSubscriptionFilter(ctx, filterID)
		if err != nil {
			return 0, err
		}
		if !found || row == nil {
			return 0, ErrSubscriptionFilterNotFound
		}
		return filterID, nil
	}
	row, err := u.EnsureDefaultSubscriptionFilter(ctx)
	if err != nil {
		return 0, err
	}
	if row == nil || row.ID == 0 {
		return 0, errors.New("default message subscription filter is not configured")
	}
	return row.ID, nil
}

func (u Usecase) CreateSubscription(ctx context.Context, input SubscriptionInput) (*SubscriptionMutation, error) {
	provider, err := normalizeSubscriptionProvider(input.Provider)
	if err != nil {
		return nil, err
	}
	filterID, err := u.resolveSubscriptionFilterID(ctx, input.FilterID)
	if err != nil {
		return nil, err
	}
	sourceRef := u.service.NormalizeSourceRef(provider, input.SourceRef)
	if err := validateSubscriptionSourceRef(provider, sourceRef); err != nil {
		return nil, err
	}
	row := domainmsg.MessageSubscription{
		Provider:            provider,
		Title:               strings.TrimSpace(input.Title),
		SourceRef:           sourceRef,
		Enabled:             input.Enabled,
		FilterID:            filterID,
		TeamIDs:             uniqueUintIDs(input.TeamIDs),
		BackfillLimit:       input.BackfillLimit,
		PollIntervalSeconds: input.PollIntervalSeconds,
		Config:              u.service.JSON(input.Config),
	}
	if !input.BackfillLimitSpecified && row.BackfillLimit == 0 {
		row.BackfillLimit = 20
	}
	if !input.PollIntervalSpecified || row.PollIntervalSeconds <= 0 {
		row.PollIntervalSeconds = defaultPollIntervalSeconds(provider)
	}
	if row.Title == "" {
		row.Title = defaultSubscriptionTitle(provider, row.SourceRef)
	}
	if row.Enabled && len(row.TeamIDs) == 0 {
		return nil, errors.New("enabled message subscription requires at least one research team")
	}
	previousBackfillLimit := 0
	previousSourceRef := ""
	if err := u.withTx(ctx, func(repo Repository) error {
		existing, found, err := repo.FindSubscriptionByProviderAndSourceRef(ctx, provider, sourceRef)
		if err != nil {
			return err
		}
		if !found || existing == nil {
			teamIDs := append([]uint(nil), row.TeamIDs...)
			if err := repo.CreateSubscription(ctx, &row); err != nil {
				return err
			}
			row.TeamIDs = teamIDs
			return repo.ReplaceSubscriptionTeams(ctx, row.ID, teamIDs)
		}
		previousBackfillLimit = existing.BackfillLimit
		previousSourceRef = existing.SourceRef
		existing.Provider = provider
		existing.FilterID = filterID
		if strings.TrimSpace(input.Title) != "" {
			existing.Title = strings.TrimSpace(input.Title)
		} else if strings.TrimSpace(existing.Title) == "" {
			existing.Title = strings.TrimPrefix(sourceRef, "@")
		}
		existing.SourceRef = sourceRef
		existing.Enabled = input.Enabled
		existing.TeamIDs = row.TeamIDs
		existing.BackfillLimit = row.BackfillLimit
		existing.PollIntervalSeconds = row.PollIntervalSeconds
		existing.Config = row.Config
		if !existing.CollectFrom.IsZero() {
			existing.CollectFrom = time.Time{}
		}
		teamIDs := append([]uint(nil), existing.TeamIDs...)
		if err := repo.SaveSubscription(ctx, existing); err != nil {
			return err
		}
		existing.TeamIDs = teamIDs
		if err := repo.ReplaceSubscriptionTeams(ctx, existing.ID, teamIDs); err != nil {
			return err
		}
		row = *existing
		return nil
	}); err != nil {
		return nil, err
	}
	if row.Enabled {
		if err := u.enqueueCollect(ctx, CollectTask{
			SubscriptionID:        row.ID,
			Reason:                "subscription_saved",
			ReconcileBackfill:     true,
			PreviousBackfillLimit: previousBackfillLimit,
			PreviousSourceRef:     previousSourceRef,
		}); err != nil {
			return nil, err
		}
	}
	return &SubscriptionMutation{MessageSubscription: row}, nil
}

func (u Usecase) UpdateSubscription(ctx context.Context, id uint, input SubscriptionInput, fields map[string]bool) (*SubscriptionMutation, bool, error) {
	row, found, err := u.repo.FindSubscription(ctx, id)
	if err != nil || !found {
		return nil, found, err
	}
	previousBackfillLimit := row.BackfillLimit
	previousSourceRef := row.SourceRef
	if fields["provider"] && input.Provider != "" {
		provider, err := normalizeSubscriptionProvider(input.Provider)
		if err != nil {
			return nil, true, err
		}
		row.Provider = provider
	}
	if fields["title"] && input.Title != "" {
		row.Title = input.Title
	}
	if fields["sourceRef"] && input.SourceRef != "" {
		row.SourceRef = u.service.NormalizeSourceRef(row.Provider, input.SourceRef)
	}
	if err := validateSubscriptionSourceRef(row.Provider, row.SourceRef); err != nil {
		return nil, true, err
	}
	if fields["enabled"] {
		row.Enabled = input.Enabled
	}
	if fields["filterId"] {
		filterID, err := u.resolveSubscriptionFilterID(ctx, input.FilterID)
		if err != nil {
			return nil, true, err
		}
		row.FilterID = filterID
	}
	if fields["teamIds"] {
		row.TeamIDs = uniqueUintIDs(input.TeamIDs)
	}
	if fields["backfillLimit"] {
		row.BackfillLimit = input.BackfillLimit
	}
	if fields["pollIntervalSeconds"] {
		row.PollIntervalSeconds = input.PollIntervalSeconds
	}
	if row.PollIntervalSeconds <= 0 {
		row.PollIntervalSeconds = defaultPollIntervalSeconds(row.Provider)
	}
	if fields["config"] {
		row.Config = u.service.JSON(input.Config)
	}
	if row.Enabled && len(row.TeamIDs) == 0 {
		return nil, true, errors.New("enabled message subscription requires at least one research team")
	}
	if err := u.withTx(ctx, func(repo Repository) error {
		teamIDs := append([]uint(nil), row.TeamIDs...)
		if err := repo.SaveSubscription(ctx, row); err != nil {
			return err
		}
		if fields["teamIds"] {
			row.TeamIDs = teamIDs
			return repo.ReplaceSubscriptionTeams(ctx, row.ID, teamIDs)
		}
		return nil
	}); err != nil {
		return nil, true, err
	}
	if row.Enabled {
		if err := u.enqueueCollect(ctx, CollectTask{
			SubscriptionID:        row.ID,
			Reason:                "subscription_updated",
			ReconcileBackfill:     previousBackfillLimit != row.BackfillLimit || previousSourceRef != row.SourceRef,
			PreviousBackfillLimit: previousBackfillLimit,
			PreviousSourceRef:     previousSourceRef,
		}); err != nil {
			return nil, true, err
		}
	}
	return &SubscriptionMutation{MessageSubscription: *row}, true, nil
}

func (u Usecase) DeleteSubscription(ctx context.Context, id uint) error {
	return u.withTx(ctx, func(repo Repository) error {
		messages, err := repo.ListMessagesBySubscription(ctx, id)
		if err != nil {
			return err
		}
		teamIDs, err := repo.ListSubscriptionTeamIDs(ctx, id)
		if err != nil {
			return err
		}
		refs := make([]string, 0, len(messages)*len(teamIDs))
		for _, message := range messages {
			refs = append(refs, messageExternalRefsForTeams(message, teamIDs)...)
		}
		return repo.DeleteSubscriptionGraph(ctx, id, refs)
	})
}

func (u Usecase) TestSubscriptionRef(ctx context.Context, provider string, sourceRef string) (map[string]any, error) {
	provider, err := normalizeSubscriptionProvider(provider)
	if err != nil {
		return nil, err
	}
	sourceRef = u.service.NormalizeSourceRef(provider, sourceRef)
	if err := validateSubscriptionSourceRef(provider, sourceRef); err != nil {
		return nil, err
	}
	credentials, err := u.telegramRuntimeCredentials(ctx, false, false)
	if err != nil {
		return nil, err
	}
	return u.service.TestSubscription(ctx, credentials, provider, sourceRef)
}

func (u Usecase) TestSubscription(ctx context.Context, id uint) (map[string]any, bool, error) {
	row, found, err := u.repo.FindSubscription(ctx, id)
	if err != nil || !found {
		return nil, found, err
	}
	credentials, err := u.telegramRuntimeCredentials(ctx, false, false)
	if err != nil {
		return nil, true, err
	}
	result, err := u.service.TestSubscription(ctx, credentials, row.Provider, row.SourceRef)
	return result, true, err
}

func (u Usecase) CollectSubscriptions(ctx context.Context, input CollectInput) (CollectResult, bool, error) {
	if input.Limit <= 0 {
		input.Limit = 20
	}
	if input.Limit > 200 {
		input.Limit = 200
	}
	rows, err := u.repo.ListSubscriptionsForCollect(ctx, input.SubscriptionID)
	if err != nil {
		return CollectResult{}, false, err
	}
	if len(rows) == 0 {
		return CollectResult{}, false, nil
	}
	result := CollectResult{Subscriptions: len(rows)}
	for i := range rows {
		collected, filtered, meetings, err := u.collectSubscription(ctx, &rows[i], input.Limit)
		if err != nil {
			return CollectResult{}, true, fmt.Errorf("%s: %w", rows[i].Title, err)
		}
		result.Collected += collected
		result.Filtered += filtered
		result.CreatedMeetings = append(result.CreatedMeetings, meetings...)
	}
	return result, true, nil
}

func (u Usecase) ProcessCollectTask(ctx context.Context, task CollectTask) (CollectResult, bool, error) {
	var subscriptionID *uint
	if task.SubscriptionID != 0 {
		subscriptionID = &task.SubscriptionID
	}
	rows, err := u.repo.ListSubscriptionsForCollect(ctx, subscriptionID)
	if err != nil {
		return CollectResult{}, false, err
	}
	if len(rows) == 0 {
		return CollectResult{}, false, nil
	}
	result := CollectResult{Subscriptions: len(rows), Status: "processed"}
	for i := range rows {
		if task.ReconcileBackfill {
			_, _, err := u.reconcileSubscriptionBackfill(ctx, &rows[i], task.PreviousBackfillLimit, task.PreviousSourceRef)
			if err != nil {
				_ = u.updateSubscriptionCollectStatus(ctx, &rows[i], time.Now(), err)
				return CollectResult{}, true, fmt.Errorf("%s: %w", rows[i].Title, err)
			}
			_ = u.updateSubscriptionCollectStatus(ctx, &rows[i], time.Now(), nil)
			continue
		}
		collected, filtered, meetings, err := u.collectSubscription(ctx, &rows[i], rows[i].BackfillLimit)
		if err != nil {
			return CollectResult{}, true, fmt.Errorf("%s: %w", rows[i].Title, err)
		}
		result.Collected += collected
		result.Filtered += filtered
		result.CreatedMeetings = append(result.CreatedMeetings, meetings...)
	}
	return result, true, nil
}

func (u Usecase) QueueCollectSubscriptions(ctx context.Context, input CollectInput) (CollectResult, bool, error) {
	rows, err := u.repo.ListSubscriptionsForCollect(ctx, input.SubscriptionID)
	if err != nil {
		return CollectResult{}, false, err
	}
	if len(rows) == 0 {
		return CollectResult{}, false, nil
	}
	for _, row := range rows {
		if err := u.enqueueCollect(ctx, CollectTask{SubscriptionID: row.ID, Reason: "manual_collect"}); err != nil {
			return CollectResult{}, true, err
		}
	}
	return CollectResult{Subscriptions: len(rows), Queued: len(rows), Status: "queued"}, true, nil
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
		SubscriptionID: filter.SubscriptionID, ResearchTeamID: filter.ResearchTeamID, Query: filter.Query, OnlyUnfiltered: filter.OnlyUnfiltered,
		Decision: filter.Decision, Limit: filter.Limit + 1, CursorID: cursorID,
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
	subscription, found, err := u.repo.FindSubscription(ctx, input.SubscriptionID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errors.New("message subscription not found")
	}
	row := domainmsg.IngestedMessage{
		SubscriptionID:  input.SubscriptionID,
		Provider:        subscription.Provider,
		SourceMessageID: strings.TrimSpace(input.SourceMessageID),
		MessageTime:     messageTime,
		Text:            input.Text,
		Raw:             u.service.JSON(map[string]any{"manual": true}),
	}
	u.applyMessageFilterInput(&row, input)
	if row.SourceMessageID == "" {
		row.SourceMessageID = fmt.Sprintf("manual:%d", time.Now().UnixMilli())
	}
	if row.RelatedSymbols == nil {
		row.RelatedSymbols = u.service.JSON(u.service.ExtractRelatedSymbols(row.Text))
	}
	row.FilterStatus = filterStatusFromManualInput(row)
	var created []*domainmeeting.Meeting
	if err := u.withTx(ctx, func(repo Repository) error {
		if err := repo.CreateMessage(ctx, &row); err != nil {
			return err
		}
		meetings, err := u.ensureMeetingsForMessage(ctx, repo, &row, "message_subscription_manual")
		if err != nil {
			return err
		}
		created = append(created, meetings...)
		return nil
	}); err != nil {
		return nil, err
	}
	return &MessageMutation{Row: u.hydrateMessage(ctx, row), CreatedMeetings: created}, nil
}

func (u Usecase) UpdateMessage(ctx context.Context, id uint, input MessageInput) (*MessageMutation, bool, error) {
	row, found, err := u.repo.FindMessage(ctx, id)
	if err != nil || !found {
		return nil, found, err
	}
	u.applyMessageFilterInput(row, input)
	if row.RelatedSymbols == nil {
		row.RelatedSymbols = u.service.JSON(u.service.ExtractRelatedSymbols(row.Text))
	}
	row.FilterStatus = filterStatusFromManualInput(*row)
	now := time.Now()
	row.FilteredAt = &now
	var created []*domainmeeting.Meeting
	if err := u.withTx(ctx, func(repo Repository) error {
		if err := repo.SaveMessage(ctx, row); err != nil {
			return err
		}
		meetings, err := u.ensureMeetingsForMessage(ctx, repo, row, "message_subscription_manual")
		if err != nil {
			return err
		}
		created = append(created, meetings...)
		return nil
	}); err != nil {
		return nil, true, err
	}
	return &MessageMutation{Row: u.hydrateMessage(ctx, *row), CreatedMeetings: created}, true, nil
}

func (u Usecase) DeleteMessage(ctx context.Context, id uint) (bool, error) {
	row, found, err := u.repo.FindMessage(ctx, id)
	if err != nil || !found {
		return found, err
	}
	if err := u.withTx(ctx, func(repo Repository) error {
		teamIDs, err := repo.ListSubscriptionTeamIDs(ctx, row.SubscriptionID)
		if err != nil {
			return err
		}
		return repo.DeleteMessage(ctx, row, messageExternalRefsForTeams(*row, teamIDs))
	}); err != nil {
		return true, err
	}
	return true, nil
}

func (u Usecase) RefilterMessages(ctx context.Context, input RefilterInput) (RefilterResult, error) {
	if input.Limit <= 0 {
		input.Limit = 100
	}
	rows, err := u.repo.ListMessagesForRefilter(ctx, input.IDs, input.OnlyUnfiltered, input.Limit)
	if err != nil {
		return RefilterResult{}, err
	}
	var created []*domainmeeting.Meeting
	if err := u.withTx(ctx, func(repo Repository) error {
		for i := range rows {
			if err := u.applyFilter(ctx, repo, &rows[i]); err != nil {
				return err
			}
			meetings, err := u.ensureMeetingsForMessage(ctx, repo, &rows[i], "message_subscription_refilter")
			if err != nil {
				return err
			}
			created = append(created, meetings...)
		}
		return nil
	}); err != nil {
		return RefilterResult{}, err
	}
	return RefilterResult{Rows: u.hydrateMessages(ctx, rows), CreatedMeetings: created}, nil
}

func (u Usecase) QueueRefilterMessages(ctx context.Context, input RefilterInput) (RefilterResult, error) {
	if input.Limit <= 0 {
		input.Limit = 100
	}
	rows, err := u.repo.ListMessagesForRefilter(ctx, input.IDs, input.OnlyUnfiltered, input.Limit)
	if err != nil {
		return RefilterResult{}, err
	}
	for i := range rows {
		rows[i].FilterStatus = domainmsg.FilterStatusUnfiltered
		rows[i].FilterDecision = nil
		rows[i].FilteredAt = nil
		if err := u.repo.SaveMessage(ctx, &rows[i]); err != nil {
			return RefilterResult{}, err
		}
		if err := u.enqueueFilter(ctx, FilterTask{MessageID: rows[i].ID, Force: true}); err != nil {
			return RefilterResult{}, err
		}
	}
	return RefilterResult{Rows: u.hydrateMessages(ctx, rows)}, nil
}

func (u Usecase) FilterMessage(ctx context.Context, id uint) (*MessageMutation, bool, error) {
	row, found, err := u.repo.FindMessage(ctx, id)
	if err != nil || !found {
		return nil, found, err
	}
	var created []*domainmeeting.Meeting
	if err := u.withTx(ctx, func(repo Repository) error {
		if err := u.applyFilter(ctx, repo, row); err != nil {
			return err
		}
		meetings, err := u.ensureMeetingsForMessage(ctx, repo, row, "message_subscription_refilter")
		if err != nil {
			return err
		}
		created = append(created, meetings...)
		return nil
	}); err != nil {
		return nil, true, err
	}
	return &MessageMutation{Row: u.hydrateMessage(ctx, *row), CreatedMeetings: created}, true, nil
}

func (u Usecase) QueueFilterMessage(ctx context.Context, id uint) (*MessageMutation, bool, error) {
	row, found, err := u.repo.FindMessage(ctx, id)
	if err != nil || !found {
		return nil, found, err
	}
	row.FilterStatus = domainmsg.FilterStatusUnfiltered
	row.FilterDecision = nil
	row.FilteredAt = nil
	now := time.Now()
	row.UpdatedAt = now
	if err := u.repo.SaveMessage(ctx, row); err != nil {
		return nil, true, err
	}
	if err := u.enqueueFilter(ctx, FilterTask{MessageID: row.ID, Force: true}); err != nil {
		return nil, true, err
	}
	return &MessageMutation{Row: u.hydrateMessage(ctx, *row)}, true, nil
}

func (u Usecase) ProcessFilterTask(ctx context.Context, task FilterTask) (*MessageMutation, bool, error) {
	row, found, err := u.repo.FindMessage(ctx, task.MessageID)
	if err != nil || !found {
		return nil, found, err
	}
	if !task.Force && row.FilterStatus == domainmsg.FilterStatusFiltered {
		return &MessageMutation{Row: u.hydrateMessage(ctx, *row)}, true, nil
	}
	row.FilterStatus = domainmsg.FilterStatusFiltering
	now := time.Now()
	row.UpdatedAt = now
	if err := u.repo.SaveMessage(ctx, row); err != nil {
		return nil, true, err
	}
	var created []*domainmeeting.Meeting
	if err := u.withTx(ctx, func(repo Repository) error {
		locked, found, err := repo.FindMessage(ctx, task.MessageID)
		if err != nil || !found {
			return err
		}
		row = locked
		row.FilterStatus = domainmsg.FilterStatusFiltering
		if err := u.applyFilter(ctx, repo, row); err != nil {
			return err
		}
		meetings, err := u.ensureMeetingsForMessage(ctx, repo, row, "message_subscription_refilter")
		if err != nil {
			return err
		}
		created = append(created, meetings...)
		return nil
	}); err != nil {
		return nil, true, err
	}
	return &MessageMutation{Row: u.hydrateMessage(ctx, *row), CreatedMeetings: created}, true, nil
}

func (u Usecase) ListPlatformAdapters(ctx context.Context) ([]PlatformAdapterRow, error) {
	rows, err := u.repo.ListPlatformAdapters(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]PlatformAdapterRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, u.platformAdapterRow(ctx, row))
	}
	return out, nil
}

func (u Usecase) CreatePlatformAdapter(ctx context.Context, input PlatformAdapterInput) (*PlatformAdapterRow, error) {
	provider := strings.TrimSpace(input.Provider)
	if provider == "" {
		provider = domainmsg.ProviderTelegramBot
	}
	row := domainmsg.PlatformAdapter{
		Provider:    provider,
		DisplayName: firstNonEmpty(input.DisplayName, "Telegram Bot"),
		Enabled:     input.Enabled,
		Config:      u.service.JSON(map[string]any{"capabilities": []string{"outbound_notifications"}}),
	}
	if input.Config != nil {
		row.Config = u.service.JSON(input.Config)
	}
	if err := u.withTx(ctx, func(repo Repository) error {
		if err := repo.CreatePlatformAdapter(ctx, &row); err != nil {
			return err
		}
		return u.savePlatformAdapterSecrets(ctx, repo, row.ID, input.BotToken, input.ChatID)
	}); err != nil {
		return nil, err
	}
	out := u.platformAdapterRow(ctx, row)
	return &out, nil
}

func (u Usecase) UpdatePlatformAdapter(ctx context.Context, id uint, input PlatformAdapterInput, fields map[string]bool) (*PlatformAdapterRow, bool, error) {
	row, found, err := u.repo.FindPlatformAdapter(ctx, id)
	if err != nil || !found {
		return nil, found, err
	}
	if fields["provider"] && input.Provider != "" {
		row.Provider = input.Provider
	}
	if fields["displayName"] && input.DisplayName != "" {
		row.DisplayName = input.DisplayName
	}
	if fields["enabled"] {
		row.Enabled = input.Enabled
	}
	if fields["config"] {
		row.Config = u.service.JSON(input.Config)
	}
	if err := u.withTx(ctx, func(repo Repository) error {
		if err := repo.SavePlatformAdapter(ctx, row); err != nil {
			return err
		}
		return u.savePlatformAdapterSecrets(ctx, repo, row.ID, input.BotToken, input.ChatID)
	}); err != nil {
		return nil, true, err
	}
	out := u.platformAdapterRow(ctx, *row)
	return &out, true, nil
}

func (u Usecase) DeletePlatformAdapter(ctx context.Context, id uint) error {
	return u.withTx(ctx, func(repo Repository) error {
		if err := repo.DeleteSecretsByNames(ctx, domainkernel.SecretKindPlatformAdapter, []string{platformSecretName(id, "bot_token"), platformSecretName(id, "chat_id")}); err != nil {
			return err
		}
		return repo.DeletePlatformAdapter(ctx, id)
	})
}

func (u Usecase) TestPlatformAdapter(ctx context.Context, id uint) (map[string]any, bool, error) {
	_, found, err := u.repo.FindPlatformAdapter(ctx, id)
	if err != nil || !found {
		return nil, found, err
	}
	token, chatID, err := u.platformAdapterSecrets(ctx, id)
	if err != nil {
		return nil, true, err
	}
	result, err := u.service.SendAdapterTest(ctx, token, chatID, u.ProxyConfig(ctx))
	return result, true, err
}

func (u Usecase) applyMessageFilterInput(row *domainmsg.IngestedMessage, input MessageInput) {
	if input.HasDecision {
		if input.FilterDecision == nil {
			row.FilterDecision = nil
		} else {
			decision := domainkernel.NewsDecision(fmt.Sprint(input.FilterDecision))
			row.FilterDecision = &decision
		}
	}
	if input.HasReason {
		if input.FilterReason == nil {
			row.FilterReason = nil
		} else {
			reason := fmt.Sprint(input.FilterReason)
			row.FilterReason = &reason
		}
	}
	if input.HasSymbols {
		row.RelatedSymbols = u.service.JSON(input.RelatedSymbols)
	}
}

func filterStatusFromManualInput(row domainmsg.IngestedMessage) string {
	if row.FilterDecision != nil || row.FilterReason != nil {
		return domainmsg.FilterStatusFiltered
	}
	return domainmsg.FilterStatusUnfiltered
}

func (u Usecase) UpdateSubscriptionTitle(ctx context.Context, id uint, title string) error {
	title = strings.TrimSpace(title)
	if title == "" {
		return nil
	}
	return u.withTx(ctx, func(repo Repository) error {
		row, found, err := repo.FindSubscription(ctx, id)
		if err != nil || !found {
			return err
		}
		if row.Title == title {
			return nil
		}
		row.Title = title
		return repo.SaveSubscription(ctx, row)
	})
}

func (u Usecase) IngestFetchedMessage(ctx context.Context, subscription domainmsg.MessageSubscription, item FetchedMessage, ingestSource string, backfillManaged *bool) (*MessageMutation, bool, error) {
	var mutation *MessageMutation
	var created bool
	err := u.withTx(ctx, func(repo Repository) error {
		var err error
		mutation, created, err = u.ingestFetchedMessage(ctx, repo, &subscription, item, ingestSource, backfillManaged)
		return err
	})
	if err == nil && created && mutation != nil {
		err = u.enqueueFilter(ctx, FilterTask{MessageID: mutation.Row.Message.ID})
	}
	return mutation, created, err
}

func (u Usecase) collectSubscription(ctx context.Context, subscription *domainmsg.MessageSubscription, limit int) (int, int, []*domainmeeting.Meeting, error) {
	if limit <= 0 {
		return 0, 0, nil, nil
	}
	messages, err := u.repo.ListMessagesBySubscription(ctx, subscription.ID)
	if err != nil {
		return 0, 0, nil, err
	}
	seen := map[string]struct{}{}
	var afterSourceMessageID *string
	for _, message := range messages {
		seen[message.SourceMessageID] = struct{}{}
	}
	if value, ok := latestSourceMessageID(subscription.Provider, messages); ok {
		afterSourceMessageID = &value
	}
	fetchLimit := limit
	if fetchLimit < 100 {
		fetchLimit = 100
	}
	credentials, err := u.telegramRuntimeCredentials(ctx, false, false)
	if err != nil {
		return 0, 0, nil, err
	}
	fetched, err := u.service.FetchSubscriptionMessages(ctx, credentials, *subscription, fetchLimit, afterSourceMessageID)
	if err != nil {
		_ = u.updateSubscriptionCollectStatus(ctx, subscription, time.Now(), err)
		return 0, 0, nil, err
	}
	_ = u.updateSubscriptionCollectStatus(ctx, subscription, time.Now(), nil)
	if strings.TrimSpace(fetched.Title) != "" && fetched.Title != subscription.Title {
		_ = u.UpdateSubscriptionTitle(ctx, subscription.ID, fetched.Title)
		subscription.Title = fetched.Title
	}
	items := sortFetchedMessagesAsc(fetched.Messages)
	newItems := items[:0]
	for _, item := range items {
		if strings.TrimSpace(item.SourceMessageID) == "" {
			continue
		}
		if _, exists := seen[item.SourceMessageID]; exists {
			continue
		}
		newItems = append(newItems, item)
	}
	items = newItems
	if len(items) > limit {
		items = items[len(items)-limit:]
	}
	collected, filtered := 0, 0
	var createdMeetings []*domainmeeting.Meeting
	for _, item := range items {
		mutation, created, err := u.IngestFetchedMessage(ctx, *subscription, item, "message_subscription_collect", nil)
		if err != nil {
			return collected, filtered, createdMeetings, err
		}
		if !created || mutation == nil {
			continue
		}
		collected++
		createdMeetings = append(createdMeetings, mutation.CreatedMeetings...)
		if mutation.Row.Message.FilterDecision != nil || mutation.Row.Message.FilterReason != nil {
			filtered++
		}
	}
	return collected, filtered, createdMeetings, nil
}

func (u Usecase) reconcileSubscriptionBackfill(ctx context.Context, subscription *domainmsg.MessageSubscription, previousBackfillLimit int, previousSourceRef string) (map[string]int, []*domainmeeting.Meeting, error) {
	result := map[string]int{"added": 0, "removed": 0}
	oldBackfillIDs := map[string]struct{}{}
	oldLimit := max(previousBackfillLimit, 0)
	oldRef := strings.TrimSpace(previousSourceRef)
	if oldRef == "" {
		oldRef = subscription.SourceRef
	}
	if oldLimit > 0 {
		credentials, err := u.telegramRuntimeCredentials(ctx, false, false)
		if err != nil {
			return result, nil, err
		}
		oldSubscription := *subscription
		oldSubscription.SourceRef = oldRef
		oldFetched, err := u.service.FetchSubscriptionMessages(ctx, credentials, oldSubscription, oldLimit, nil)
		if err != nil {
			return result, nil, err
		}
		for _, item := range latestFetchedMessages(oldFetched.Messages, oldLimit) {
			oldBackfillIDs[item.SourceMessageID] = struct{}{}
		}
	}
	desiredIDs := map[string]struct{}{}
	var desiredItems []FetchedMessage
	if subscription.BackfillLimit > 0 {
		credentials, err := u.telegramRuntimeCredentials(ctx, false, false)
		if err != nil {
			return result, nil, err
		}
		fetched, err := u.service.FetchSubscriptionMessages(ctx, credentials, *subscription, subscription.BackfillLimit, nil)
		if err != nil {
			return result, nil, err
		}
		if strings.TrimSpace(fetched.Title) != "" && fetched.Title != subscription.Title {
			subscription.Title = fetched.Title
			_ = u.UpdateSubscriptionTitle(ctx, subscription.ID, fetched.Title)
		}
		desiredItems = latestFetchedMessages(fetched.Messages, subscription.BackfillLimit)
		for _, item := range desiredItems {
			desiredIDs[item.SourceMessageID] = struct{}{}
		}
	}
	return u.reconcileBackfillItems(ctx, subscription, oldBackfillIDs, desiredIDs, desiredItems, result)
}

func (u Usecase) reconcileBackfillItems(ctx context.Context, subscription *domainmsg.MessageSubscription, oldBackfillIDs map[string]struct{}, desiredIDs map[string]struct{}, desiredItems []FetchedMessage, result map[string]int) (map[string]int, []*domainmeeting.Meeting, error) {
	stored, err := u.repo.ListMessagesBySubscription(ctx, subscription.ID)
	if err != nil {
		return result, nil, err
	}
	managed := true
	var createdMeetings []*domainmeeting.Meeting
	createdMessageIDs := []uint{}
	if err := u.withTx(ctx, func(repo Repository) error {
		storedBySourceID := map[string]domainmsg.IngestedMessage{}
		for _, message := range stored {
			storedBySourceID[message.SourceMessageID] = message
		}
		for sourceID := range oldBackfillIDs {
			message, ok := storedBySourceID[sourceID]
			if !ok || rawBool(message.Raw, "manual") {
				continue
			}
			raw := mergeRaw(message.Raw, "", &managed, u.service.JSON)
			if string(raw) != string(message.Raw) {
				message.Raw = raw
				if err := repo.SaveMessage(ctx, &message); err != nil {
					return err
				}
			}
		}
		for _, item := range sortFetchedMessagesAsc(desiredItems) {
			mutation, created, err := u.ingestFetchedMessage(ctx, repo, subscription, item, "message_subscription_backfill", &managed)
			if err != nil {
				return err
			}
			if created {
				result["added"]++
				if mutation != nil {
					createdMessageIDs = append(createdMessageIDs, mutation.Row.Message.ID)
					createdMeetings = append(createdMeetings, mutation.CreatedMeetings...)
				}
			}
		}
		ids := []uint{}
		refs := []string{}
		teamIDs := uniqueUintIDs(subscription.TeamIDs)
		if len(teamIDs) == 0 {
			var err error
			teamIDs, err = repo.ListSubscriptionTeamIDs(ctx, subscription.ID)
			if err != nil {
				return err
			}
		}
		for _, message := range stored {
			if rawBool(message.Raw, "manual") {
				continue
			}
			_, wasOldBackfill := oldBackfillIDs[message.SourceMessageID]
			_, isDesired := desiredIDs[message.SourceMessageID]
			if (rawBool(message.Raw, "backfill_managed") || wasOldBackfill) && !isDesired {
				ids = append(ids, message.ID)
				refs = append(refs, messageExternalRefsForTeams(message, teamIDs)...)
			}
		}
		if len(ids) == 0 {
			return nil
		}
		result["removed"] = len(ids)
		return repo.DeleteMessagesByIDsWithRefs(ctx, ids, refs)
	}); err != nil {
		return result, createdMeetings, err
	}
	for _, id := range createdMessageIDs {
		if err := u.enqueueFilter(ctx, FilterTask{MessageID: id}); err != nil {
			return result, createdMeetings, err
		}
	}
	return result, createdMeetings, nil
}

func (u Usecase) ingestFetchedMessage(ctx context.Context, repo Repository, subscription *domainmsg.MessageSubscription, item FetchedMessage, ingestSource string, backfillManaged *bool) (*MessageMutation, bool, error) {
	if !subscription.CollectFrom.IsZero() && item.MessageTime.Before(subscription.CollectFrom) {
		return nil, false, nil
	}
	if existing, found, err := repo.FindMessageBySource(ctx, subscription.ID, item.SourceMessageID); err != nil {
		return nil, false, err
	} else if found && existing != nil {
		raw := mergeRaw(existing.Raw, ingestSource, backfillManaged, u.service.JSON)
		if string(raw) != string(existing.Raw) {
			existing.Raw = raw
			if err := repo.SaveMessage(ctx, existing); err != nil {
				return nil, false, err
			}
		}
		return &MessageMutation{Row: u.hydrateMessageWithRepo(ctx, repo, *existing)}, false, nil
	}
	message := domainmsg.IngestedMessage{
		SubscriptionID:  subscription.ID,
		Provider:        subscription.Provider,
		SourceMessageID: item.SourceMessageID,
		MessageTime:     item.MessageTime,
		Text:            item.Text,
		Raw:             mergeRaw(item.Raw, ingestSource, backfillManaged, u.service.JSON),
		FilterStatus:    domainmsg.FilterStatusUnfiltered,
		UpdatedAt:       time.Now(),
	}
	if err := repo.CreateMessage(ctx, &message); err != nil {
		return nil, false, err
	}
	return &MessageMutation{Row: u.hydrateMessageWithRepo(ctx, repo, message)}, true, nil
}

func (u Usecase) applyFilter(ctx context.Context, repo Repository, row *domainmsg.IngestedMessage) error {
	filter, provider, cfgErr := u.filterRuntimeConfig(ctx, repo, row.SubscriptionID)
	var result FilterResult
	var err error
	if cfgErr != nil {
		err = cfgErr
	} else {
		result, err = u.service.ApplyFilter(ctx, *row, u.security, filter, provider, u.ProxyConfig(ctx))
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		row.FilterDecision = nil
		reason := fmt.Sprintf("message filter call failed: %v", err)
		row.FilterReason = &reason
		row.RelatedSymbols = u.service.JSON(nil)
		row.FilterStatus = domainmsg.FilterStatusFailed
	} else {
		row.FilterDecision = result.Decision
		row.FilterReason = result.Reason
		row.RelatedSymbols = result.RelatedSymbols
		row.FilterStatus = domainmsg.FilterStatusFiltered
	}
	filterID := result.FilterID
	if filterID == 0 && filter.ID != 0 {
		filterID = filter.ID
	}
	if filterID != 0 {
		row.FilterID = &filterID
	}
	now := time.Now()
	row.FilteredAt = &now
	row.UpdatedAt = now
	return repo.SaveMessage(ctx, row)
}

func (u Usecase) ensureMeetingsForMessage(ctx context.Context, repo Repository, row *domainmsg.IngestedMessage, triggerSource string) ([]*domainmeeting.Meeting, error) {
	if row.FilterDecision == nil || *row.FilterDecision != domainkernel.NewsMeeting {
		return nil, nil
	}
	subscription, found, err := repo.FindSubscriptionForMessage(ctx, row.SubscriptionID)
	if err != nil {
		return nil, err
	}
	if !found || subscription == nil {
		return nil, errors.New("message subscription not found")
	}
	symbols := relatedSymbolsFromJSON(row.RelatedSymbols)
	if len(symbols) == 0 {
		symbols = u.service.ExtractRelatedSymbols(row.Text)
	}
	symbolPart := "message-event"
	if len(symbols) > 0 {
		limit := len(symbols)
		if limit > 3 {
			limit = 3
		}
		symbolPart = strings.Join(symbols[:limit], ", ")
	}
	teamIDs := uniqueUintIDs(subscription.TeamIDs)
	if len(teamIDs) == 0 {
		var err error
		teamIDs, err = repo.ListSubscriptionTeamIDs(ctx, subscription.ID)
		if err != nil {
			return nil, err
		}
	}
	created := []*domainmeeting.Meeting{}
	for _, teamID := range teamIDs {
		ready, reason, err := repo.ResearchTeamReady(ctx, teamID)
		if err != nil {
			return nil, err
		}
		if !ready {
			return nil, errors.New(reason)
		}
		externalRef := messageExternalRefForTeam(*row, teamID)
		if existing, ok, err := repo.FindMeetingReferenceByExternalRef(ctx, "ingested_message", externalRef); err != nil {
			return nil, err
		} else if ok && existing != nil {
			continue
		}
		meeting := domainmeeting.Meeting{
			ResearchTeamID: teamID,
			Topic:          fmt.Sprintf("Message subscription trigger: %s / %s", subscription.Title, symbolPart),
			TriggerSource:  firstNonEmpty(triggerSource, "message_subscription"),
			Status:         domainkernel.MeetingQueued,
			Tags:           u.service.JSON(nil),
		}
		if err := repo.CreateMeeting(ctx, &meeting); err != nil {
			return nil, err
		}
		content := fmt.Sprintf("Subscription %s triggered a meeting.\nFilter reason: %s\nRelated symbols: %s\n\n%s", subscription.Title, stringValue(row.FilterReason), strings.Join(symbols, ", "), row.Text)
		if err := repo.AppendMeetingEvent(ctx, &domainmeeting.Event{MeetingID: meeting.ID, Type: domainkernel.EventSystem, Content: "Meeting submitted for execution.", Payload: u.service.JSON(map[string]any{"status": "queued"})}); err != nil {
			return nil, err
		}
		if err := repo.AppendMeetingEvent(ctx, &domainmeeting.Event{MeetingID: meeting.ID, Type: domainkernel.EventSystem, Content: content, Payload: u.service.JSON(map[string]any{"status": "message_subscription_triggered", "ingested_message_id": row.ID, "subscription_id": subscription.ID, "research_team_id": teamID, "decision": *row.FilterDecision, "related_symbols": symbols})}); err != nil {
			return nil, err
		}
		note := fmt.Sprintf("%s / team#%d / message#%s", subscription.Title, teamID, row.SourceMessageID)
		topic := fmt.Sprintf("Ingested message %s / #%s", subscription.Title, row.SourceMessageID)
		summary := row.Text
		if len(summary) > 2000 {
			summary = summary[:2000]
		}
		ref := domainmeeting.Reference{SourceMeetingID: meeting.ID, ReferenceType: "ingested_message", Note: &note, TargetTopicSnapshot: topic, TargetSummarySnapshot: &summary, ExternalRef: &externalRef}
		if err := repo.CreateMeetingReference(ctx, &ref); err != nil {
			return nil, err
		}
		created = append(created, &meeting)
	}
	return created, nil
}

func (u Usecase) updateSubscriptionCollectStatus(ctx context.Context, subscription *domainmsg.MessageSubscription, now time.Time, collectErr error) error {
	if subscription == nil || subscription.ID == 0 {
		return nil
	}
	interval := subscription.PollIntervalSeconds
	if interval <= 0 {
		interval = defaultPollIntervalSeconds(subscription.Provider)
	}
	subscription.LastCollectedAt = &now
	next := now.Add(time.Duration(interval) * time.Second)
	subscription.NextCollectAt = &next
	if collectErr != nil {
		message := collectErr.Error()
		subscription.LastCollectError = &message
	} else {
		subscription.LastCollectError = nil
	}
	return u.repo.SaveSubscription(ctx, subscription)
}

func normalizeSubscriptionProvider(provider string) (string, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return domainmsg.ProviderTelegramChannel, nil
	}
	switch provider {
	case domainmsg.ProviderTelegramChannel, domainmsg.ProviderRSSFeed:
		return provider, nil
	default:
		return "", fmt.Errorf("unsupported message subscription provider: %s", provider)
	}
}

func defaultPollIntervalSeconds(provider string) int {
	if provider == domainmsg.ProviderRSSFeed {
		return defaultRSSPollIntervalSeconds
	}
	return defaultTelegramPollIntervalSeconds
}

func defaultSubscriptionTitle(provider string, sourceRef string) string {
	sourceRef = strings.TrimSpace(sourceRef)
	if provider == domainmsg.ProviderTelegramChannel {
		return strings.TrimPrefix(sourceRef, "@")
	}
	return sourceRef
}

func validateSubscriptionSourceRef(provider string, sourceRef string) error {
	sourceRef = strings.TrimSpace(sourceRef)
	if sourceRef == "" {
		return errors.New("message subscription source ref is required")
	}
	if provider == domainmsg.ProviderRSSFeed {
		parsed, err := url.Parse(sourceRef)
		if err != nil || parsed.Hostname() == "" {
			return errors.New("rss feed source ref must be an http or https URL")
		}
		scheme := strings.ToLower(parsed.Scheme)
		if scheme != "http" && scheme != "https" {
			return errors.New("rss feed source ref must be an http or https URL")
		}
		if parsed.User != nil {
			return errors.New("rss feed source ref must not include credentials")
		}
	}
	return nil
}

func latestSourceMessageID(provider string, messages []domainmsg.IngestedMessage) (string, bool) {
	if provider != domainmsg.ProviderTelegramChannel {
		return "", false
	}
	var latest int64
	found := false
	for _, message := range messages {
		value, err := strconv.ParseInt(strings.TrimSpace(message.SourceMessageID), 10, 64)
		if err != nil {
			continue
		}
		if !found || value > latest {
			latest = value
			found = true
		}
	}
	if !found {
		return "", false
	}
	return strconv.FormatInt(latest, 10), true
}

func (u Usecase) hydrateMessages(ctx context.Context, messages []domainmsg.IngestedMessage) []MessageRow {
	out := make([]MessageRow, 0, len(messages))
	for _, message := range messages {
		out = append(out, u.hydrateMessage(ctx, message))
	}
	return out
}

func (u Usecase) hydrateMessage(ctx context.Context, message domainmsg.IngestedMessage) MessageRow {
	return u.hydrateMessageWithRepo(ctx, u.repo, message)
}

func (u Usecase) hydrateMessageWithRepo(ctx context.Context, repo Repository, message domainmsg.IngestedMessage) MessageRow {
	subscription, _, _ := repo.FindSubscriptionForMessage(ctx, message.SubscriptionID)
	if subscription == nil {
		subscription = &domainmsg.MessageSubscription{}
	}
	return MessageRow{Message: message, Subscription: *subscription}
}

func (u Usecase) upsertEncryptedSecret(ctx context.Context, repo Repository, kind domainkernel.SecretKind, name string, value string) error {
	encrypted, err := u.security.EncryptSecret(value)
	if err != nil {
		return err
	}
	row, found, err := repo.FindSecret(ctx, kind, name)
	if err != nil {
		return err
	}
	if !found {
		return repo.CreateSecret(ctx, &domainsettings.Secret{Kind: kind, Name: name, EncryptedValue: encrypted})
	}
	row.EncryptedValue = encrypted
	return repo.SaveSecret(ctx, row)
}

func (u Usecase) TelegramRuntimeCredentials(ctx context.Context, requireSession bool) (TelegramCredentials, error) {
	return u.telegramRuntimeCredentials(ctx, requireSession, true)
}

func (u Usecase) telegramRuntimeCredentials(ctx context.Context, requireSession bool, requireApp bool) (TelegramCredentials, error) {
	credentials := TelegramCredentials{Proxy: u.ProxyConfig(ctx)}
	appIDText, hasAppID := u.decryptedSecret(ctx, domainkernel.SecretKindMessageSubscription, telegramSecretAppID)
	appHash, hasAppHash := u.decryptedSecret(ctx, domainkernel.SecretKindMessageSubscription, telegramSecretAppHash)
	if !hasAppID || strings.TrimSpace(appIDText) == "" {
		if requireApp {
			return credentials, ErrTelegramAppIDNotConfigured
		}
	} else {
		appID, err := strconv.Atoi(strings.TrimSpace(appIDText))
		if err != nil || appID <= 0 {
			if requireApp {
				return credentials, ErrTelegramAppIDInvalid
			}
		} else {
			credentials.AppID = appID
		}
	}
	if !hasAppHash || strings.TrimSpace(appHash) == "" {
		if requireApp {
			return credentials, ErrTelegramAppHashNotConfigured
		}
	} else {
		credentials.AppHash = strings.TrimSpace(appHash)
	}
	if session, ok := u.decryptedSecret(ctx, domainkernel.SecretKindMessageSubscription, telegramSecretSession); ok {
		credentials.Session = strings.TrimSpace(session)
	}
	if requireSession && credentials.Session == "" {
		return credentials, ErrTelegramSessionNotConfigured
	}
	return credentials, nil
}

func IsTelegramRuntimeConfigError(err error) bool {
	return errors.Is(err, ErrTelegramAppIDNotConfigured) ||
		errors.Is(err, ErrTelegramAppHashNotConfigured) ||
		errors.Is(err, ErrTelegramSessionNotConfigured) ||
		errors.Is(err, ErrTelegramAppIDInvalid)
}

func (u Usecase) ProxyConfig(ctx context.Context) ProxyConfig {
	cfg := ProxyConfig{}
	if value, ok := u.decryptedSecret(ctx, domainkernel.SecretKindApp, proxySecretName); ok {
		cfg.ProxyURL = strings.TrimSpace(value)
	}
	cfg.EnabledAI = u.boolSetting(ctx, proxyEnabledAI)
	cfg.EnabledTelegram = u.boolSetting(ctx, proxyEnabledTelegram)
	cfg.EnabledMarket = u.boolSetting(ctx, proxyEnabledMarket)
	cfg.EnabledWeb = u.boolSetting(ctx, proxyEnabledWeb)
	if values, ok := u.stringListSetting(ctx, proxyNoProxy); ok {
		cfg.NoProxy = values
	}
	if value, updatedAt, ok := u.stringSettingWithUpdatedAt(ctx, proxyRevision); ok {
		cfg.Revision = value
		cfg.UpdatedAt = updatedAt
	}
	return cfg
}

func (u Usecase) filterRuntimeConfig(ctx context.Context, repo Repository, subscriptionID uint) (domainmsg.MessageSubscriptionFilter, domainai.Provider, error) {
	subscription, found, err := repo.FindSubscriptionForMessage(ctx, subscriptionID)
	if err != nil {
		return domainmsg.MessageSubscriptionFilter{}, domainai.Provider{}, err
	}
	if !found || subscription == nil {
		return domainmsg.MessageSubscriptionFilter{}, domainai.Provider{}, errors.New("message subscription not found")
	}
	filter, found, err := repo.FindSubscriptionFilter(ctx, subscription.FilterID)
	if err != nil {
		return domainmsg.MessageSubscriptionFilter{}, domainai.Provider{}, err
	}
	if !found || filter == nil || !filter.Enabled {
		return domainmsg.MessageSubscriptionFilter{}, domainai.Provider{}, errors.New("message subscription filter is not enabled")
	}
	if filter.ProviderID == nil {
		return domainmsg.MessageSubscriptionFilter{}, domainai.Provider{}, errors.New("message subscription filter has no available provider")
	}
	provider, found, err := repo.FindAIProvider(ctx, *filter.ProviderID)
	if err != nil {
		return domainmsg.MessageSubscriptionFilter{}, domainai.Provider{}, err
	}
	if !found || provider == nil || !provider.Enabled || provider.APIKeySecret == nil {
		return domainmsg.MessageSubscriptionFilter{}, domainai.Provider{}, errors.New("message subscription filter has no available provider or api key")
	}
	model := provider.DefaultModel
	if filter.Model != nil && strings.TrimSpace(*filter.Model) != "" {
		model = strings.TrimSpace(*filter.Model)
	}
	if strings.TrimSpace(model) == "" {
		return domainmsg.MessageSubscriptionFilter{}, domainai.Provider{}, errors.New("message subscription filter has no model configured")
	}
	return *filter, *provider, nil
}

func (u Usecase) withTx(ctx context.Context, fn func(Repository) error) error {
	if u.tx == nil {
		return errors.New("messaging unit of work is not configured")
	}
	return u.tx.WithTx(ctx, fn)
}

func (u Usecase) enqueueCollect(ctx context.Context, task CollectTask) error {
	if u.queue == nil {
		return nil
	}
	return u.queue.EnqueueCollect(ctx, task)
}

func (u Usecase) enqueueFilter(ctx context.Context, task FilterTask) error {
	if u.queue == nil {
		return nil
	}
	return u.queue.EnqueueFilter(ctx, task)
}

func (u Usecase) secret(ctx context.Context, kind domainkernel.SecretKind, name string) (domainsettings.Secret, bool) {
	row, ok, _ := u.repo.FindSecret(ctx, kind, name)
	if !ok || row == nil {
		return domainsettings.Secret{}, false
	}
	return *row, true
}

func (u Usecase) hasSecret(ctx context.Context, kind domainkernel.SecretKind, name string) bool {
	value, ok := u.decryptedSecret(ctx, kind, name)
	if !ok || strings.TrimSpace(value) == "" {
		return false
	}
	if kind == domainkernel.SecretKindMessageSubscription && name == telegramSecretAppID {
		appID, err := strconv.Atoi(strings.TrimSpace(value))
		return err == nil && appID > 0
	}
	return true
}

func (u Usecase) decryptedSecret(ctx context.Context, kind domainkernel.SecretKind, name string) (string, bool) {
	secret, ok := u.secret(ctx, kind, name)
	if !ok {
		return "", false
	}
	value, err := u.security.DecryptSecret(secret.EncryptedValue)
	return value, err == nil
}

func (u Usecase) boolSetting(ctx context.Context, key string) bool {
	value, ok := u.scalarSetting(ctx, key)
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return fmt.Sprint(value) == "true"
	}
}

func (u Usecase) stringListSetting(ctx context.Context, key string) ([]string, bool) {
	value, ok := u.scalarSetting(ctx, key)
	if !ok {
		return nil, false
	}
	switch typed := value.(type) {
	case []string:
		return typed, true
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			itemText := strings.TrimSpace(fmt.Sprint(item))
			if itemText != "" {
				out = append(out, itemText)
			}
		}
		return out, true
	default:
		out := []string{}
		for _, item := range strings.Split(fmt.Sprint(value), ",") {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out, true
	}
}

func (u Usecase) stringSettingWithUpdatedAt(ctx context.Context, key string) (string, time.Time, bool) {
	setting, found, err := u.repo.FindAppSetting(ctx, key)
	if err != nil || !found || setting == nil {
		return "", time.Time{}, false
	}
	return strings.TrimSpace(fmt.Sprint(scalarFromJSON(setting.Value))), setting.UpdatedAt, true
}

func (u Usecase) scalarSetting(ctx context.Context, key string) (any, bool) {
	setting, found, err := u.repo.FindAppSetting(ctx, key)
	if err != nil || !found || setting == nil {
		return nil, false
	}
	return scalarFromJSON(setting.Value), true
}

func (u Usecase) savePlatformAdapterSecrets(ctx context.Context, repo Repository, adapterID uint, token string, chatID string) error {
	if strings.TrimSpace(token) != "" {
		if err := u.upsertEncryptedSecret(ctx, repo, domainkernel.SecretKindPlatformAdapter, platformSecretName(adapterID, "bot_token"), token); err != nil {
			return err
		}
	}
	if strings.TrimSpace(chatID) != "" {
		return u.upsertEncryptedSecret(ctx, repo, domainkernel.SecretKindPlatformAdapter, platformSecretName(adapterID, "chat_id"), chatID)
	}
	return nil
}

func (u Usecase) platformAdapterRow(ctx context.Context, row domainmsg.PlatformAdapter) PlatformAdapterRow {
	out := PlatformAdapterRow{Adapter: row}
	if secret, ok := u.secret(ctx, domainkernel.SecretKindPlatformAdapter, platformSecretName(row.ID, "bot_token")); ok {
		out.HasBotToken = secret.EncryptedValue != ""
	}
	if secret, ok := u.secret(ctx, domainkernel.SecretKindPlatformAdapter, platformSecretName(row.ID, "chat_id")); ok {
		out.HasChatID = secret.EncryptedValue != ""
		if value, err := u.security.DecryptSecret(secret.EncryptedValue); err == nil {
			out.ChatID = value
		}
	}
	return out
}

func (u Usecase) platformAdapterSecrets(ctx context.Context, adapterID uint) (string, string, error) {
	tokenSecret, ok := u.secret(ctx, domainkernel.SecretKindPlatformAdapter, platformSecretName(adapterID, "bot_token"))
	if !ok {
		return "", "", errors.New("platform adapter bot token is not configured")
	}
	chatSecret, ok := u.secret(ctx, domainkernel.SecretKindPlatformAdapter, platformSecretName(adapterID, "chat_id"))
	if !ok {
		return "", "", errors.New("platform adapter chat id is not configured")
	}
	token, err := u.security.DecryptSecret(tokenSecret.EncryptedValue)
	if err != nil {
		return "", "", err
	}
	chatID, err := u.security.DecryptSecret(chatSecret.EncryptedValue)
	if err != nil {
		return "", "", err
	}
	return token, chatID, nil
}

func platformSecretName(adapterID uint, key string) string {
	return fmt.Sprintf("adapter:%d:%s", adapterID, key)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func messageExternalRef(message domainmsg.IngestedMessage) string {
	return fmt.Sprintf("ingested_message:%d", message.ID)
}

func messageExternalRefForTeam(message domainmsg.IngestedMessage, teamID uint) string {
	return fmt.Sprintf("ingested_message:%d:team:%d", message.ID, teamID)
}

func messageExternalRefsForTeams(message domainmsg.IngestedMessage, teamIDs []uint) []string {
	teamIDs = uniqueUintIDs(teamIDs)
	refs := make([]string, 0, len(teamIDs))
	for _, teamID := range teamIDs {
		refs = append(refs, messageExternalRefForTeam(message, teamID))
	}
	return refs
}

func sortFetchedMessagesAsc(messages []FetchedMessage) []FetchedMessage {
	out := append([]FetchedMessage(nil), messages...)
	sort.Slice(out, func(i, j int) bool {
		if !out[i].MessageTime.Equal(out[j].MessageTime) {
			return out[i].MessageTime.Before(out[j].MessageTime)
		}
		return out[i].SourceMessageID < out[j].SourceMessageID
	})
	return out
}

func latestFetchedMessages(messages []FetchedMessage, limit int) []FetchedMessage {
	if limit <= 0 {
		return nil
	}
	out := sortFetchedMessagesAsc(messages)
	if len(out) <= limit {
		return out
	}
	return out[len(out)-limit:]
}

func mergeRaw(raw domainkernel.JSON, ingestSource string, backfillManaged *bool, jsonValue func(any) domainkernel.JSON) domainkernel.JSON {
	var data map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &data)
	}
	if data == nil {
		data = map[string]any{}
	}
	if ingestSource != "" {
		data["ingest_source"] = ingestSource
	}
	if backfillManaged != nil {
		data["backfill_managed"] = *backfillManaged
	}
	return jsonValue(data)
}

func rawBool(raw domainkernel.JSON, key string) bool {
	var data map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &data) != nil {
		return false
	}
	value, ok := data[key]
	if !ok {
		return false
	}
	if boolValue, ok := value.(bool); ok {
		return boolValue
	}
	if stringValue, ok := value.(string); ok {
		parsed, _ := strconv.ParseBool(stringValue)
		return parsed
	}
	return false
}

func scalarFromJSON(raw []byte) any {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	if obj, ok := value.(map[string]any); ok {
		if inner, exists := obj["value"]; exists {
			return inner
		}
	}
	return value
}

func relatedSymbolsFromJSON(raw []byte) []string {
	var values []string
	if len(raw) == 0 || json.Unmarshal(raw, &values) != nil {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(strings.ToUpper(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func cleanStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(*value)
	if text == "" {
		return nil
	}
	return &text
}

func errOrNil(found bool, err error) error {
	if err != nil {
		return err
	}
	if !found {
		return errors.New("referenced meeting not found")
	}
	return nil
}

func max(a int, b int) int {
	if a > b {
		return a
	}
	return b
}

func uniqueUintIDs(values []uint) []uint {
	seen := map[uint]bool{}
	out := make([]uint, 0, len(values))
	for _, value := range values {
		if value == 0 || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func ListenerLeaseClaimable(setting domainsettings.AppSetting, owner string, now time.Time, ttl time.Duration) bool {
	description := "message subscription listener lease: " + owner
	if setting.Description != nil && *setting.Description == description {
		return true
	}
	var payload struct {
		Owner     string    `json:"owner"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(setting.Value, &payload); err == nil {
		if strings.TrimSpace(payload.Owner) == owner {
			return true
		}
		if !payload.ExpiresAt.IsZero() && !payload.ExpiresAt.After(now) {
			return true
		}
	}
	if setting.UpdatedAt.IsZero() {
		return true
	}
	return !setting.UpdatedAt.After(now.Add(-ttl))
}
