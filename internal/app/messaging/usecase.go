package messaging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainmsg "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/messaging"
	domainprediction "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/prediction"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
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
	rssAuthTypeNone                    = "none"
	rssAuthTypeBasic                   = "basic"
	rssAuthTypeBearer                  = "bearer"
	defaultTelegramPollIntervalSeconds = 30
	defaultRSSPollIntervalSeconds      = 900

	feedbackTrainingSnapshotsSettingKey = "MESSAGE_FEEDBACK_TRAINING_SNAPSHOTS"
	feedbackTrainingExportsSettingKey   = "MESSAGE_FEEDBACK_TRAINING_EXPORTS"
	feedbackTrainingSnapshotLimit       = 20
	feedbackTrainingExportLimit         = 10
	feedbackTrainingSampleLimit         = 10000
	feedbackTrainingSnapshotRefLimit    = 200
	sourceTrustPolicySettingKey         = "MESSAGE_SOURCE_TRUST_POLICY"
)

var (
	ErrTelegramAppIDNotConfigured   = errors.New("message subscription secret telegram app_id is not configured")
	ErrTelegramAppHashNotConfigured = errors.New("message subscription secret telegram app_hash is not configured")
	ErrTelegramSessionNotConfigured = errors.New("message subscription secret telegram mtproto_session is not configured")
	ErrTelegramAppIDInvalid         = errors.New("telegram app_id is invalid")
	ErrSubscriptionFilterNotFound   = errors.New("message subscription filter not found")
)

type filterCallFailedError struct {
	err error
}

func (e filterCallFailedError) Error() string {
	return fmt.Sprintf("message filter failed: %v", e.err)
}

func (e filterCallFailedError) Unwrap() error {
	return e.err
}

func isFilterCallFailed(err error) bool {
	var target filterCallFailedError
	return errors.As(err, &target)
}

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

var DefaultPredictionFilterSeed = domainmsg.MessageSubscriptionFilter{
	Name:        "默认预测市场消息过滤器",
	Description: "判断新到消息是否对应可研究的预测市场事件或对赌盘，并决定忽略、观察还是立即触发会议。",
	PromptTemplate: `你只负责预测市场消息分诊，不负责完整投研分析。必须只输出JSON：{"decision":"ignore|observe|meeting","reason":"...","related_symbols":[],"related_prediction_markets":["..."],"match_confidence":0.0,"match_reason":"..."}

判定框架：
1. 先判断消息是否描述了可被 Polymarket 这类预测市场表达的可裁定事件：明确主体、结果、时间窗口、裁定来源或可验证证据。只有情绪、观点、泛泛预测、营销稿或无法裁定的叙述，应输出 ignore。
2. 再判断消息是否可能改变市场概率：新事实、官方表态、法院/监管/选举/体育赛程/公司事件/宏观数据/链上或加密事件的增量，高于重复报道、二手评论和低可信传闻。
3. decision=meeting 只用于高时效、高影响、市场映射明确或需要多角色核验结算规则/赔率变化的消息；decision=observe 用于可能相关但市场、时间窗或证据仍需人工确认的消息；decision=ignore 用于低置信、过期、不可裁定或无预测市场映射的消息。
4. related_prediction_markets 要写可用于 Gamma 搜索的候选表达，例如事件名、market slug、英文/中文问题句、关键实体+结果+时间窗；没有候选就返回空数组。
5. match_confidence 使用0到1：>=0.75 表示高度可能有关联并可触发会议；0.45到0.75 表示进入人工确认/观察；<0.45 不应关联。match_reason 必须说明主体、结果、时间窗、证据强弱和不确定点。
6. 不输出任何真实交易、钱包、API key、充值提现、仓位或模拟盘动作建议；预测市场 v1 只能观察、关注、核验和安排后续唤醒。`,
	Enabled:   true,
	IsDefault: false,
}

type Usecase struct {
	repo              Repository
	service           Service
	security          SecurityService
	tx                Transactor
	queue             TaskQueue
	predictionMatcher PredictionMatcher
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

func (u Usecase) WithPredictionMatcher(matcher PredictionMatcher) Usecase {
	u.predictionMatcher = matcher
	return u
}

type PredictionMatcher interface {
	MatchNews(ctx context.Context, messageID *uint, text string) ([]domainprediction.Match, error)
	ListMatchesForMessage(ctx context.Context, messageID uint) ([]domainprediction.Match, error)
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
	ResearchTeamAssetClasses(ctx context.Context, teamIDs []uint) (map[uint]string, error)
	ResearchTeamReady(ctx context.Context, teamID uint) (bool, string, error)
	ListSubscriptionsForCollect(ctx context.Context, subscriptionID *uint) ([]domainmsg.MessageSubscription, error)
	DeleteSubscriptionGraph(ctx context.Context, id uint, externalRefs []string) error
	ListMessagesBySubscription(ctx context.Context, subscriptionID uint) ([]domainmsg.IngestedMessage, error)
	ListMessages(ctx context.Context, filter RepositoryMessageFilter) ([]domainmsg.IngestedMessage, error)
	ListFeedbackMessages(ctx context.Context, filter RepositoryFeedbackMessageFilter) ([]domainmsg.IngestedMessage, error)
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
	TestSubscription(ctx context.Context, credentials SubscriptionCredentials, provider string, sourceRef string) (map[string]any, error)
	FetchSubscriptionMessages(ctx context.Context, credentials SubscriptionCredentials, subscription domainmsg.MessageSubscription, limit int, afterSourceMessageID *string) (FetchedMessages, error)
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

type SubscriptionCredentials struct {
	Telegram TelegramCredentials
	RSSAuth  RSSAuthCredentials
	Proxy    ProxyConfig
}

type RSSAuthCredentials struct {
	Type     string
	Username string
	Password string
}

type RSSAuthConfig struct {
	Type               string `json:"type,omitempty"`
	Username           string `json:"username,omitempty"`
	PasswordSecretName string `json:"passwordSecretName,omitempty"`
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

type RepositoryFeedbackMessageFilter struct {
	SubscriptionID string
	Provider       string
	Label          string
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
	RSSAuthType            string
	RSSAuthTypeSet         bool
	RSSUsername            string
	RSSUsernameSet         bool
	RSSPassword            string
	RSSPasswordSet         bool
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

type SubscriptionMaintenanceInput struct {
	Action          string
	SubscriptionIDs []uint
	Provider        string
	OnlyBlocked     bool
	OnlyWarnings    bool
	RSSAuthType     string
	RSSAuthTypeSet  bool
	RSSUsername     string
	RSSUsernameSet  bool
	RSSPassword     string
	RSSPasswordSet  bool
}

type SubscriptionMaintenanceResult struct {
	Action  string
	Matched int
	Checked int
	Passed  int
	Failed  int
	Updated int
	Queued  int
	Paused  int
	Skipped []SubscriptionMaintenanceSkip
}

type SubscriptionMaintenanceSkip struct {
	SubscriptionID uint   `json:"subscriptionId"`
	Title          string `json:"title,omitempty"`
	Reason         string `json:"reason"`
}

type SetupStatus struct {
	SubscriptionCount int
	FilterCount       int
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

type SubscriptionDiagnosticCheck struct {
	Key     string `json:"key"`
	Status  string `json:"status"`
	Title   string `json:"title"`
	Detail  string `json:"detail"`
	Action  string `json:"action,omitempty"`
	Route   string `json:"route,omitempty"`
	Depends string `json:"depends,omitempty"`
}

type SubscriptionDiagnostic struct {
	SubscriptionID     uint                          `json:"subscriptionId"`
	Provider           string                        `json:"provider"`
	Title              string                        `json:"title"`
	SourceRef          string                        `json:"sourceRef"`
	Enabled            bool                          `json:"enabled"`
	SourceKind         string                        `json:"sourceKind"`
	Status             string                        `json:"status"`
	Severity           string                        `json:"severity"`
	Ready              bool                          `json:"ready"`
	PrivateCapable     bool                          `json:"privateCapable"`
	ProxyRoute         string                        `json:"proxyRoute"`
	LastCollectError   *string                       `json:"lastCollectError"`
	NextCollectAt      *time.Time                    `json:"nextCollectAt"`
	LastCollectedAt    *time.Time                    `json:"lastCollectedAt"`
	Checks             []SubscriptionDiagnosticCheck `json:"checks"`
	RecommendedActions []string                      `json:"recommendedActions"`
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
	Message           domainmsg.IngestedMessage
	Subscription      domainmsg.MessageSubscription
	PredictionMatches []domainprediction.Match
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

type MessageFeedbackInput struct {
	Label      string
	Comment    string
	HasComment bool
}

type MessageFeedbackBatchInput struct {
	IDs []uint
	MessageFeedbackInput
}

type MessageFeedbackBatchResult struct {
	RequestedCount int
	UpdatedCount   int
	UpdatedIDs     []uint
	MissingIDs     []uint
	Label          string
	Comment        *string
}

type FeedbackTrainingFilter struct {
	SubscriptionID string
	Provider       string
	Label          string
	Limit          int
	Cursor         string
}

type FeedbackTrainingSample struct {
	Message      domainmsg.IngestedMessage
	Subscription domainmsg.MessageSubscription
	Split        string
	SampleWeight float64
	TrainingUse  string
	DedupeKey    string
}

type FeedbackTrainingSampleList struct {
	Rows       []FeedbackTrainingSample
	Limit      int
	NextCursor string
}

type FeedbackEvaluationReport struct {
	GeneratedAt                  time.Time
	Status                       string
	SampleCount                  int
	TrainCount                   int
	ValidationCount              int
	HelpfulCount                 int
	NoiseCount                   int
	MisclassifiedCount           int
	NeutralCount                 int
	AgreementEligibleCount       int
	AgreementCount               int
	PositiveSignalCount          int
	NoiseSuppressionCount        int
	FalseMeetingFromNoiseCount   int
	NeedsDecisionCorrectionCount int
	AgreementRate                float64
	NoiseRate                    float64
	MisclassificationRate        float64
	LabelBreakdown               map[string]int
	DecisionBreakdown            map[string]int
	Recommendations              []string
	Truncated                    bool
	SampleLimit                  int
}

type FeedbackTrainingSnapshotFilter struct {
	SubscriptionID string `json:"subscriptionId,omitempty"`
	Provider       string `json:"provider,omitempty"`
	FeedbackLabel  string `json:"feedbackLabel,omitempty"`
}

type FeedbackTrainingSnapshotSampleRef struct {
	MessageID     uint    `json:"messageId"`
	DedupeKey     string  `json:"dedupeKey"`
	FeedbackLabel string  `json:"feedbackLabel"`
	Split         string  `json:"split"`
	SampleWeight  float64 `json:"sampleWeight"`
}

type FeedbackTrainingSnapshotSourceTrustSummary struct {
	Trusted              int `json:"trusted"`
	Watch                int `json:"watch"`
	LowConfidence        int `json:"lowConfidence"`
	InsufficientFeedback int `json:"insufficientFeedback"`
}

type FeedbackTrainingSnapshot struct {
	Version               string                                     `json:"version"`
	CreatedAt             time.Time                                  `json:"createdAt"`
	Filter                FeedbackTrainingSnapshotFilter             `json:"filter"`
	SampleCount           int                                        `json:"sampleCount"`
	TrainCount            int                                        `json:"trainCount"`
	ValidationCount       int                                        `json:"validationCount"`
	SourceCount           int                                        `json:"sourceCount"`
	Fingerprint           string                                     `json:"fingerprint"`
	LabelBreakdown        map[string]int                             `json:"labelBreakdown"`
	DecisionBreakdown     map[string]int                             `json:"decisionBreakdown"`
	SourceStatusBreakdown map[string]int                             `json:"sourceStatusBreakdown"`
	SourceTrustSummary    FeedbackTrainingSnapshotSourceTrustSummary `json:"sourceTrustSummary"`
	Evaluation            FeedbackEvaluationReport                   `json:"evaluation"`
	SampleRefCount        int                                        `json:"sampleRefCount"`
	SampleRefsTruncated   bool                                       `json:"sampleRefsTruncated"`
	SampleRefs            []FeedbackTrainingSnapshotSampleRef        `json:"sampleRefs"`
}

type FeedbackTrainingExport struct {
	Version               string                                     `json:"version"`
	CreatedAt             time.Time                                  `json:"createdAt"`
	Filter                FeedbackTrainingSnapshotFilter             `json:"filter"`
	Format                string                                     `json:"format"`
	ContentType           string                                     `json:"contentType"`
	SampleCount           int                                        `json:"sampleCount"`
	TrainCount            int                                        `json:"trainCount"`
	ValidationCount       int                                        `json:"validationCount"`
	SourceCount           int                                        `json:"sourceCount"`
	Fingerprint           string                                     `json:"fingerprint"`
	ContentSHA256         string                                     `json:"contentSha256"`
	ByteCount             int                                        `json:"byteCount"`
	LineCount             int                                        `json:"lineCount"`
	Truncated             bool                                       `json:"truncated"`
	SampleLimit           int                                        `json:"sampleLimit"`
	LabelBreakdown        map[string]int                             `json:"labelBreakdown"`
	DecisionBreakdown     map[string]int                             `json:"decisionBreakdown"`
	SourceStatusBreakdown map[string]int                             `json:"sourceStatusBreakdown"`
	SourceTrustSummary    FeedbackTrainingSnapshotSourceTrustSummary `json:"sourceTrustSummary"`
	Evaluation            FeedbackEvaluationReport                   `json:"evaluation"`
	Content               string                                     `json:"content"`
}

type SourceTrustFilter struct {
	SubscriptionID string
	Provider       string
}

type SourceTrustReport struct {
	GeneratedAt time.Time
	Status      string
	SampleCount int
	SourceCount int
	SampleLimit int
	Truncated   bool
	Items       []SourceTrustItem
}

type SourceTrustItem struct {
	SubscriptionID     uint
	SubscriptionTitle  string
	Provider           string
	SourceRef          string
	FeedbackCount      int
	HelpfulCount       int
	NoiseCount         int
	MisclassifiedCount int
	NeutralCount       int
	TrustScore         int
	Status             string
	Explanation        string
	RecommendedActions []string
	LastFeedbackAt     *time.Time
	PositiveRate       float64
	NegativeRate       float64
	SampleWeight       float64
	AutoAction         string
	AutoActionReason   string
}

type SourceTrustPolicy struct {
	Enabled                  bool    `json:"enabled"`
	MinFeedback              int     `json:"minFeedback"`
	MeetingToObserveMaxScore int     `json:"meetingToObserveMaxScore"`
	ObserveToIgnoreMaxScore  int     `json:"observeToIgnoreMaxScore"`
	SevereNegativeRate       float64 `json:"severeNegativeRate"`
	ModerateNegativeRate     float64 `json:"moderateNegativeRate"`
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

func (u Usecase) EnsureDefaultPredictionSubscriptionFilter(ctx context.Context) (*domainmsg.MessageSubscriptionFilter, error) {
	var out *domainmsg.MessageSubscriptionFilter
	if err := u.withTx(ctx, func(repo Repository) error {
		filters, err := repo.ListSubscriptionFilters(ctx)
		if err != nil {
			return err
		}
		for i := range filters {
			if filters[i].Name == DefaultPredictionFilterSeed.Name {
				out = &filters[i]
				return nil
			}
		}
		seed := DefaultPredictionFilterSeed
		if err := repo.CreateSubscriptionFilter(ctx, &seed); err != nil {
			return err
		}
		out = &seed
		return nil
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

func (u Usecase) SetupStatus(ctx context.Context) (SetupStatus, error) {
	filters, err := u.repo.ListSubscriptionFilters(ctx)
	if err != nil {
		return SetupStatus{}, err
	}
	subscriptions, err := u.repo.ListSubscriptions(ctx)
	if err != nil {
		return SetupStatus{}, err
	}
	return SetupStatus{SubscriptionCount: len(subscriptions), FilterCount: len(filters)}, nil
}

func (u Usecase) SubscriptionDiagnostics(ctx context.Context) ([]SubscriptionDiagnostic, error) {
	rows, err := u.ListSubscriptions(ctx)
	if err != nil {
		return nil, err
	}
	status := u.SubscriptionStatus(ctx)
	proxy := u.ProxyConfig(ctx)
	out := make([]SubscriptionDiagnostic, 0, len(rows))
	for _, row := range rows {
		out = append(out, u.subscriptionDiagnostic(ctx, row, status, proxy))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return diagnosticSeverityRank(out[i].Severity) > diagnosticSeverityRank(out[j].Severity)
		}
		if out[i].Enabled != out[j].Enabled {
			return out[i].Enabled
		}
		return out[i].SubscriptionID < out[j].SubscriptionID
	})
	return out, nil
}

func (u Usecase) resolveSubscriptionFilterID(ctx context.Context, filterID uint, teamIDs []uint) (uint, error) {
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
	if u.subscriptionShouldUsePredictionFilter(ctx, teamIDs) {
		row, err := u.EnsureDefaultPredictionSubscriptionFilter(ctx)
		if err != nil {
			return 0, err
		}
		if row == nil || row.ID == 0 {
			return 0, errors.New("default prediction market message filter is not configured")
		}
		return row.ID, nil
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

func (u Usecase) subscriptionShouldUsePredictionFilter(ctx context.Context, teamIDs []uint) bool {
	teamIDs = uniqueUintIDs(teamIDs)
	if len(teamIDs) == 0 {
		return false
	}
	assetClasses, err := u.repo.ResearchTeamAssetClasses(ctx, teamIDs)
	if err != nil || len(assetClasses) == 0 {
		return false
	}
	hasPrediction := false
	for _, teamID := range teamIDs {
		switch strings.TrimSpace(assetClasses[teamID]) {
		case "prediction_market":
			hasPrediction = true
		case "":
			return false
		default:
			return false
		}
	}
	return hasPrediction
}

func (u Usecase) CreateSubscription(ctx context.Context, input SubscriptionInput) (*SubscriptionMutation, error) {
	provider, err := normalizeSubscriptionProvider(input.Provider)
	if err != nil {
		return nil, err
	}
	filterID, err := u.resolveSubscriptionFilterID(ctx, input.FilterID, input.TeamIDs)
	if err != nil {
		return nil, err
	}
	sourceRef := u.service.NormalizeSourceRef(provider, input.SourceRef)
	if err := validateSubscriptionSourceRef(provider, sourceRef); err != nil {
		return nil, err
	}
	config, err := u.subscriptionConfigForMutation(provider, input, nil, true)
	if err != nil {
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
		Config:              config,
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
			if err := u.applyRSSAuthMutation(ctx, repo, &row, input, nil); err != nil {
				return err
			}
			row.TeamIDs = teamIDs
			return repo.ReplaceSubscriptionTeams(ctx, row.ID, teamIDs)
		}
		previous := *existing
		config, err := u.subscriptionConfigForMutation(provider, input, &previous, input.Config != nil)
		if err != nil {
			return err
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
		existing.Config = config
		if !existing.CollectFrom.IsZero() {
			existing.CollectFrom = time.Time{}
		}
		teamIDs := append([]uint(nil), existing.TeamIDs...)
		if err := repo.SaveSubscription(ctx, existing); err != nil {
			return err
		}
		if err := u.applyRSSAuthMutation(ctx, repo, existing, input, &previous); err != nil {
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
	previous := *row
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
		filterID, err := u.resolveSubscriptionFilterID(ctx, input.FilterID, firstNonEmptyUintSlice(input.TeamIDs, row.TeamIDs))
		if err != nil {
			return nil, true, err
		}
		row.FilterID = filterID
	}
	if fields["teamIds"] {
		row.TeamIDs = uniqueUintIDs(input.TeamIDs)
		if !fields["filterId"] && input.FilterID == 0 {
			if filterID, err := u.resolveSubscriptionFilterID(ctx, 0, row.TeamIDs); err == nil {
				row.FilterID = filterID
			} else {
				return nil, true, err
			}
		}
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
	if fields["config"] || previous.Provider != row.Provider || input.RSSAuthTypeSet || input.RSSUsernameSet || input.RSSPasswordSet {
		config, err := u.subscriptionConfigForMutation(row.Provider, input, &previous, fields["config"])
		if err != nil {
			return nil, true, err
		}
		row.Config = config
	}
	if row.Enabled && len(row.TeamIDs) == 0 {
		return nil, true, errors.New("enabled message subscription requires at least one research team")
	}
	if err := u.withTx(ctx, func(repo Repository) error {
		teamIDs := append([]uint(nil), row.TeamIDs...)
		if err := repo.SaveSubscription(ctx, row); err != nil {
			return err
		}
		if err := u.applyRSSAuthMutation(ctx, repo, row, input, &previous); err != nil {
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
		row, _, err := repo.FindSubscription(ctx, id)
		if err != nil {
			return err
		}
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
		if err := repo.DeleteSubscriptionGraph(ctx, id, refs); err != nil {
			return err
		}
		if row != nil {
			return repo.DeleteSecretsByNames(ctx, domainkernel.SecretKindMessageSubscription, rssSecretNamesForSubscription(row, id))
		}
		return nil
	})
}

func (u Usecase) TestSubscriptionRef(ctx context.Context, provider string, sourceRef string) (map[string]any, error) {
	return u.TestSubscriptionDraft(ctx, SubscriptionInput{Provider: provider, SourceRef: sourceRef})
}

func (u Usecase) TestSubscriptionDraft(ctx context.Context, input SubscriptionInput) (map[string]any, error) {
	provider := input.Provider
	provider, err := normalizeSubscriptionProvider(provider)
	if err != nil {
		return nil, err
	}
	sourceRef := u.service.NormalizeSourceRef(provider, input.SourceRef)
	if err := validateSubscriptionSourceRef(provider, sourceRef); err != nil {
		return nil, err
	}
	credentials, err := u.subscriptionDraftCredentials(ctx, provider, input)
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
	credentials, err := u.subscriptionCredentials(ctx, row, false, false)
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

func (u Usecase) RunSubscriptionMaintenance(ctx context.Context, input SubscriptionMaintenanceInput) (SubscriptionMaintenanceResult, error) {
	action := normalizeSubscriptionMaintenanceAction(input.Action)
	if action == "" {
		return SubscriptionMaintenanceResult{}, fmt.Errorf("unsupported message subscription maintenance action: %s", input.Action)
	}
	rows, err := u.subscriptionMaintenanceRows(ctx, input)
	if err != nil {
		return SubscriptionMaintenanceResult{}, err
	}
	result := SubscriptionMaintenanceResult{Action: action, Matched: len(rows)}
	switch action {
	case "repair_defaults":
		return u.repairSubscriptionDefaults(ctx, rows, result)
	case "clear_collect_error":
		return u.clearSubscriptionCollectErrors(ctx, rows, result)
	case "queue_collect":
		return u.queueSubscriptionMaintenanceCollect(ctx, rows, result, "maintenance_collect")
	case "rotate_rss_auth":
		return u.rotateRSSSubscriptionAuth(ctx, rows, input, result)
	case "audit_telegram_access":
		return u.auditTelegramSubscriptionAccess(ctx, rows, result)
	case "apply_source_trust_governance":
		return u.applySourceTrustGovernance(ctx, rows, result)
	default:
		return SubscriptionMaintenanceResult{}, fmt.Errorf("unsupported message subscription maintenance action: %s", action)
	}
}

func normalizeSubscriptionMaintenanceAction(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "repair_defaults", "repair-defaults", "repair":
		return "repair_defaults"
	case "clear_collect_error", "clear-collect-error":
		return "clear_collect_error"
	case "queue_collect", "queue-collect", "collect":
		return "queue_collect"
	case "rotate_rss_auth", "rotate-rss-auth":
		return "rotate_rss_auth"
	case "audit_telegram_access", "audit-telegram-access", "telegram_access_audit", "telegram-access-audit":
		return "audit_telegram_access"
	case "apply_source_trust_governance", "apply-source-trust-governance", "source_trust_governance", "source-trust-governance":
		return "apply_source_trust_governance"
	default:
		return ""
	}
}

func (u Usecase) subscriptionMaintenanceRows(ctx context.Context, input SubscriptionMaintenanceInput) ([]domainmsg.MessageSubscription, error) {
	rows, err := u.ListSubscriptions(ctx)
	if err != nil {
		return nil, err
	}
	idSet := map[uint]bool{}
	for _, id := range uniqueUintIDs(input.SubscriptionIDs) {
		if id != 0 {
			idSet[id] = true
		}
	}
	provider := strings.TrimSpace(input.Provider)
	if provider != "" {
		normalized, err := normalizeSubscriptionProvider(provider)
		if err != nil {
			return nil, err
		}
		provider = normalized
	}
	status := u.SubscriptionStatus(ctx)
	proxy := u.ProxyConfig(ctx)
	out := make([]domainmsg.MessageSubscription, 0, len(rows))
	for _, row := range rows {
		if len(idSet) > 0 && !idSet[row.ID] {
			continue
		}
		if provider != "" && row.Provider != provider {
			continue
		}
		if input.OnlyBlocked || input.OnlyWarnings {
			diagnostic := u.subscriptionDiagnostic(ctx, row, status, proxy)
			if input.OnlyBlocked && diagnostic.Status != "blocked" && !diagnosticsHaveStatus(diagnostic.Checks, "blocked") {
				continue
			}
			if input.OnlyWarnings && diagnostic.Status != "warning" && !diagnosticsHaveStatus(diagnostic.Checks, "warning") {
				continue
			}
		}
		out = append(out, row)
	}
	return out, nil
}

func (u Usecase) repairSubscriptionDefaults(ctx context.Context, rows []domainmsg.MessageSubscription, result SubscriptionMaintenanceResult) (SubscriptionMaintenanceResult, error) {
	filter, err := u.EnsureDefaultSubscriptionFilter(ctx)
	if err != nil {
		return result, err
	}
	if filter == nil || filter.ID == 0 {
		return result, errors.New("default message subscription filter is not configured")
	}
	collectIDs := []uint{}
	if err := u.withTx(ctx, func(repo Repository) error {
		for i := range rows {
			row := rows[i]
			changed := false
			needsFilter, err := subscriptionNeedsDefaultFilter(ctx, repo, row)
			if err != nil {
				return err
			}
			if needsFilter {
				row.FilterID = filter.ID
				changed = true
			}
			if row.LastCollectError != nil && strings.TrimSpace(*row.LastCollectError) != "" {
				row.LastCollectError = nil
				changed = true
			}
			if changed {
				if err := repo.SaveSubscription(ctx, &row); err != nil {
					return err
				}
				result.Updated++
			}
			if row.Enabled {
				collectIDs = append(collectIDs, row.ID)
			} else if !changed {
				result.Skipped = append(result.Skipped, SubscriptionMaintenanceSkip{SubscriptionID: row.ID, Title: row.Title, Reason: "subscription is disabled and already has defaults"})
			}
		}
		return nil
	}); err != nil {
		return result, err
	}
	return u.enqueueSubscriptionMaintenanceCollect(ctx, collectIDs, result, "maintenance_repair")
}

func subscriptionNeedsDefaultFilter(ctx context.Context, repo Repository, row domainmsg.MessageSubscription) (bool, error) {
	if row.FilterID == 0 {
		return true, nil
	}
	filter, found, err := repo.FindSubscriptionFilter(ctx, row.FilterID)
	if err != nil {
		return false, err
	}
	return !found || filter == nil || !filter.Enabled, nil
}

func (u Usecase) clearSubscriptionCollectErrors(ctx context.Context, rows []domainmsg.MessageSubscription, result SubscriptionMaintenanceResult) (SubscriptionMaintenanceResult, error) {
	if err := u.withTx(ctx, func(repo Repository) error {
		for i := range rows {
			row := rows[i]
			if row.LastCollectError == nil || strings.TrimSpace(*row.LastCollectError) == "" {
				result.Skipped = append(result.Skipped, SubscriptionMaintenanceSkip{SubscriptionID: row.ID, Title: row.Title, Reason: "no collect error"})
				continue
			}
			row.LastCollectError = nil
			if err := repo.SaveSubscription(ctx, &row); err != nil {
				return err
			}
			result.Updated++
		}
		return nil
	}); err != nil {
		return result, err
	}
	return result, nil
}

func (u Usecase) queueSubscriptionMaintenanceCollect(ctx context.Context, rows []domainmsg.MessageSubscription, result SubscriptionMaintenanceResult, reason string) (SubscriptionMaintenanceResult, error) {
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		if !row.Enabled {
			result.Skipped = append(result.Skipped, SubscriptionMaintenanceSkip{SubscriptionID: row.ID, Title: row.Title, Reason: "subscription is disabled"})
			continue
		}
		ids = append(ids, row.ID)
	}
	return u.enqueueSubscriptionMaintenanceCollect(ctx, ids, result, reason)
}

func (u Usecase) enqueueSubscriptionMaintenanceCollect(ctx context.Context, subscriptionIDs []uint, result SubscriptionMaintenanceResult, reason string) (SubscriptionMaintenanceResult, error) {
	for _, id := range uniqueUintIDs(subscriptionIDs) {
		if err := u.enqueueCollect(ctx, CollectTask{SubscriptionID: id, Reason: reason}); err != nil {
			return result, err
		}
		result.Queued++
	}
	return result, nil
}

func (u Usecase) rotateRSSSubscriptionAuth(ctx context.Context, rows []domainmsg.MessageSubscription, input SubscriptionMaintenanceInput, result SubscriptionMaintenanceResult) (SubscriptionMaintenanceResult, error) {
	if !input.RSSPasswordSet || strings.TrimSpace(input.RSSPassword) == "" {
		return result, errors.New("rss auth password/token is required for rotation")
	}
	mutation := SubscriptionInput{
		RSSAuthType:    input.RSSAuthType,
		RSSAuthTypeSet: input.RSSAuthTypeSet,
		RSSUsername:    input.RSSUsername,
		RSSUsernameSet: input.RSSUsernameSet,
		RSSPassword:    input.RSSPassword,
		RSSPasswordSet: true,
	}
	if err := u.withTx(ctx, func(repo Repository) error {
		for i := range rows {
			row := rows[i]
			if row.Provider != domainmsg.ProviderRSSFeed {
				result.Skipped = append(result.Skipped, SubscriptionMaintenanceSkip{SubscriptionID: row.ID, Title: row.Title, Reason: "not an RSS/Atom subscription"})
				continue
			}
			previous := row
			config, err := u.subscriptionConfigForMutation(row.Provider, mutation, &previous, false)
			if err != nil {
				result.Skipped = append(result.Skipped, SubscriptionMaintenanceSkip{SubscriptionID: row.ID, Title: row.Title, Reason: err.Error()})
				continue
			}
			row.Config = config
			if err := u.applyRSSAuthMutation(ctx, repo, &row, mutation, &previous); err != nil {
				result.Skipped = append(result.Skipped, SubscriptionMaintenanceSkip{SubscriptionID: row.ID, Title: row.Title, Reason: err.Error()})
				continue
			}
			result.Updated++
		}
		return nil
	}); err != nil {
		return result, err
	}
	return result, nil
}

func (u Usecase) auditTelegramSubscriptionAccess(ctx context.Context, rows []domainmsg.MessageSubscription, result SubscriptionMaintenanceResult) (SubscriptionMaintenanceResult, error) {
	for i := range rows {
		row := rows[i]
		if row.Provider != domainmsg.ProviderTelegramChannel {
			result.Skipped = append(result.Skipped, SubscriptionMaintenanceSkip{SubscriptionID: row.ID, Title: row.Title, Reason: "not a Telegram subscription"})
			continue
		}
		result.Checked++
		credentials, err := u.subscriptionCredentials(ctx, &row, false, false)
		if err == nil {
			_, err = u.service.TestSubscription(ctx, credentials, row.Provider, row.SourceRef)
		}
		if err != nil {
			result.Failed++
			changed, saveErr := u.updateSubscriptionAuditError(ctx, row, err)
			if saveErr != nil {
				return result, saveErr
			}
			if changed {
				result.Updated++
			}
			continue
		}
		result.Passed++
		if row.LastCollectError != nil && strings.TrimSpace(*row.LastCollectError) != "" {
			row.LastCollectError = nil
			if err := u.repo.SaveSubscription(ctx, &row); err != nil {
				return result, err
			}
			result.Updated++
		}
	}
	return result, nil
}

func (u Usecase) updateSubscriptionAuditError(ctx context.Context, row domainmsg.MessageSubscription, auditErr error) (bool, error) {
	message := "telegram access audit failed: " + strings.TrimSpace(auditErr.Error())
	if strings.TrimSpace(auditErr.Error()) == "" {
		message = "telegram access audit failed"
	}
	if row.LastCollectError != nil && strings.TrimSpace(*row.LastCollectError) == message {
		return false, nil
	}
	row.LastCollectError = &message
	if err := u.repo.SaveSubscription(ctx, &row); err != nil {
		return false, err
	}
	return true, nil
}

func (u Usecase) applySourceTrustGovernance(ctx context.Context, rows []domainmsg.MessageSubscription, result SubscriptionMaintenanceResult) (SubscriptionMaintenanceResult, error) {
	if len(rows) == 0 {
		return result, nil
	}
	report, err := u.SourceTrustReport(ctx, SourceTrustFilter{})
	if err != nil {
		return result, err
	}
	items := map[uint]SourceTrustItem{}
	for _, item := range report.Items {
		items[item.SubscriptionID] = item
	}
	now := time.Now()
	if err := u.withTx(ctx, func(repo Repository) error {
		for i := range rows {
			row := rows[i]
			item, ok := items[row.ID]
			if !ok || item.FeedbackCount == 0 {
				result.Skipped = append(result.Skipped, SubscriptionMaintenanceSkip{SubscriptionID: row.ID, Title: row.Title, Reason: "no source trust feedback samples"})
				continue
			}
			result.Checked++
			if strings.TrimSpace(item.AutoAction) == "" {
				result.Passed++
				result.Skipped = append(result.Skipped, SubscriptionMaintenanceSkip{SubscriptionID: row.ID, Title: row.Title, Reason: "source trust does not require governance"})
				continue
			}
			governanceAction := sourceTrustGovernanceAction(item)
			if governanceAction == "" {
				result.Passed++
				result.Skipped = append(result.Skipped, SubscriptionMaintenanceSkip{SubscriptionID: row.ID, Title: row.Title, Reason: "source trust action is already handled by filter downranking"})
				continue
			}
			if governanceAction == "pause_source" && row.Enabled {
				row.Enabled = false
				result.Paused++
			}
			reason := sourceTrustGovernanceReason(governanceAction, item)
			row.LastCollectError = &reason
			row.Config = u.sourceTrustGovernanceConfig(row, item, governanceAction, reason, now)
			if err := repo.SaveSubscription(ctx, &row); err != nil {
				return err
			}
			result.Updated++
		}
		return nil
	}); err != nil {
		return result, err
	}
	return result, nil
}

func sourceTrustGovernanceAction(item SourceTrustItem) string {
	switch strings.TrimSpace(item.AutoAction) {
	case "downrank_meeting_to_observe_and_observe_to_ignore":
		return "pause_source"
	case "downrank_meeting_to_observe":
		return "watch_source"
	default:
		return ""
	}
}

func sourceTrustGovernanceReason(action string, item SourceTrustItem) string {
	prefix := "source trust governance watch"
	if action == "pause_source" {
		prefix = "source trust governance paused source"
	}
	reason := strings.TrimSpace(item.AutoActionReason)
	if reason == "" {
		reason = sourceTrustAutoActionReason(item)
	}
	return prefix + ": " + reason
}

func (u Usecase) sourceTrustGovernanceConfig(row domainmsg.MessageSubscription, item SourceTrustItem, action string, reason string, now time.Time) domainkernel.JSON {
	cfg := sanitizeSubscriptionConfig(row.Provider, row.Config)
	cfg["sourceTrustGovernance"] = map[string]any{
		"action":        action,
		"autoAction":    item.AutoAction,
		"status":        item.Status,
		"trustScore":    item.TrustScore,
		"feedbackCount": item.FeedbackCount,
		"negativeRate":  item.NegativeRate,
		"reason":        reason,
		"appliedAt":     now.UTC().Format(time.RFC3339Nano),
	}
	return u.service.JSON(cfg)
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

func (u Usecase) FeedbackMessage(ctx context.Context, id uint, input MessageFeedbackInput) (*MessageMutation, bool, error) {
	row, found, err := u.repo.FindMessage(ctx, id)
	if err != nil || !found {
		return nil, found, err
	}
	label, comment, err := normalizeMessageFeedback(input)
	if err != nil {
		return nil, true, err
	}
	now := time.Now()
	applyMessageFeedback(row, label, comment, input.HasComment, now)
	if err := u.repo.SaveMessage(ctx, row); err != nil {
		return nil, true, err
	}
	return &MessageMutation{Row: u.hydrateMessage(ctx, *row)}, true, nil
}

func (u Usecase) FeedbackMessages(ctx context.Context, input MessageFeedbackBatchInput) (MessageFeedbackBatchResult, error) {
	ids := uniqueUintIDs(input.IDs)
	if len(ids) == 0 {
		return MessageFeedbackBatchResult{}, errors.New("message feedback batch requires at least one message id")
	}
	if len(ids) > 500 {
		return MessageFeedbackBatchResult{}, errors.New("message feedback batch supports at most 500 message ids")
	}
	label, comment, err := normalizeMessageFeedback(input.MessageFeedbackInput)
	if err != nil {
		return MessageFeedbackBatchResult{}, err
	}
	result := MessageFeedbackBatchResult{
		RequestedCount: len(ids),
		UpdatedIDs:     []uint{},
		MissingIDs:     []uint{},
		Label:          label,
		Comment:        comment,
	}
	now := time.Now()
	if err := u.withTx(ctx, func(repo Repository) error {
		for _, id := range ids {
			row, found, err := repo.FindMessage(ctx, id)
			if err != nil {
				return err
			}
			if !found || row == nil {
				result.MissingIDs = append(result.MissingIDs, id)
				continue
			}
			applyMessageFeedback(row, label, comment, input.HasComment, now)
			if err := repo.SaveMessage(ctx, row); err != nil {
				return err
			}
			result.UpdatedIDs = append(result.UpdatedIDs, id)
		}
		return nil
	}); err != nil {
		return MessageFeedbackBatchResult{}, err
	}
	result.UpdatedCount = len(result.UpdatedIDs)
	return result, nil
}

func (u Usecase) ListFeedbackTrainingSamples(ctx context.Context, filter FeedbackTrainingFilter) (FeedbackTrainingSampleList, error) {
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 500 {
		filter.Limit = 500
	}
	var cursorID uint64
	if filter.Cursor != "" {
		cursorID, _ = strconv.ParseUint(filter.Cursor, 10, 64)
	}
	rows, err := u.repo.ListFeedbackMessages(ctx, RepositoryFeedbackMessageFilter{
		SubscriptionID: filter.SubscriptionID,
		Provider:       filter.Provider,
		Label:          filter.Label,
		Limit:          filter.Limit + 1,
		CursorID:       cursorID,
	})
	if err != nil {
		return FeedbackTrainingSampleList{}, err
	}
	nextCursor := ""
	if len(rows) > filter.Limit {
		nextCursor = fmt.Sprint(rows[filter.Limit-1].ID)
		rows = rows[:filter.Limit]
	}
	out := make([]FeedbackTrainingSample, 0, len(rows))
	for _, row := range rows {
		messageRow := u.hydrateMessage(ctx, row)
		out = append(out, feedbackTrainingSample(messageRow))
	}
	return FeedbackTrainingSampleList{Rows: out, Limit: filter.Limit, NextCursor: nextCursor}, nil
}

func (u Usecase) SourceTrustReport(ctx context.Context, filter SourceTrustFilter) (SourceTrustReport, error) {
	const sampleLimit = 10000
	rows, err := u.repo.ListFeedbackMessages(ctx, RepositoryFeedbackMessageFilter{
		SubscriptionID: filter.SubscriptionID,
		Provider:       filter.Provider,
		Limit:          sampleLimit + 1,
	})
	if err != nil {
		return SourceTrustReport{}, err
	}
	truncated := false
	if len(rows) > sampleLimit {
		truncated = true
		rows = rows[:sampleLimit]
	}
	policy := u.sourceTrustPolicy(ctx, u.repo)
	type bucket struct {
		item SourceTrustItem
	}
	buckets := map[string]*bucket{}
	for _, row := range rows {
		messageRow := u.hydrateMessage(ctx, row)
		subscription := messageRow.Subscription
		sourceRef := strings.TrimSpace(subscription.SourceRef)
		if sourceRef == "" {
			sourceRef = fmt.Sprintf("subscription:%d", row.SubscriptionID)
		}
		provider := strings.TrimSpace(row.Provider)
		if provider == "" {
			provider = strings.TrimSpace(subscription.Provider)
		}
		if provider == "" {
			provider = "unknown"
		}
		key := provider + "\x00" + strconv.FormatUint(uint64(row.SubscriptionID), 10) + "\x00" + sourceRef
		entry := buckets[key]
		if entry == nil {
			entry = &bucket{item: SourceTrustItem{
				SubscriptionID:    row.SubscriptionID,
				SubscriptionTitle: subscription.Title,
				Provider:          provider,
				SourceRef:         sourceRef,
			}}
			buckets[key] = entry
		}
		entry.item.FeedbackCount++
		if row.FeedbackAt != nil && (entry.item.LastFeedbackAt == nil || row.FeedbackAt.After(*entry.item.LastFeedbackAt)) {
			at := *row.FeedbackAt
			entry.item.LastFeedbackAt = &at
		}
		label := ""
		if row.FeedbackLabel != nil {
			label = strings.ToLower(strings.TrimSpace(*row.FeedbackLabel))
		}
		switch label {
		case domainmsg.FeedbackHelpful:
			entry.item.HelpfulCount++
		case domainmsg.FeedbackNoise:
			entry.item.NoiseCount++
		case domainmsg.FeedbackMisclassified:
			entry.item.MisclassifiedCount++
		case domainmsg.FeedbackNeutral:
			entry.item.NeutralCount++
		}
	}
	items := make([]SourceTrustItem, 0, len(buckets))
	for _, entry := range buckets {
		item := finalizeSourceTrustItem(entry.item)
		item.AutoAction, item.AutoActionReason = sourceTrustAutoAction(item, policy)
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].TrustScore != items[j].TrustScore {
			return items[i].TrustScore > items[j].TrustScore
		}
		if items[i].FeedbackCount != items[j].FeedbackCount {
			return items[i].FeedbackCount > items[j].FeedbackCount
		}
		return items[i].Provider+items[i].SourceRef < items[j].Provider+items[j].SourceRef
	})
	status := "insufficient_feedback"
	if len(items) > 0 {
		status = "computed"
	}
	return SourceTrustReport{
		GeneratedAt: time.Now(),
		Status:      status,
		SampleCount: len(rows),
		SourceCount: len(items),
		SampleLimit: sampleLimit,
		Truncated:   truncated,
		Items:       items,
	}, nil
}

func (u Usecase) FeedbackEvaluation(ctx context.Context, filter FeedbackTrainingFilter) (FeedbackEvaluationReport, error) {
	samples, truncated, err := u.feedbackTrainingSamples(ctx, filter, feedbackTrainingSampleLimit)
	if err != nil {
		return FeedbackEvaluationReport{}, err
	}
	return feedbackEvaluationReport(samples, truncated, feedbackTrainingSampleLimit), nil
}

func (u Usecase) ListFeedbackTrainingSnapshots(ctx context.Context, filter FeedbackTrainingFilter) ([]FeedbackTrainingSnapshot, error) {
	snapshots, err := u.loadFeedbackTrainingSnapshots(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]FeedbackTrainingSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if snapshotMatchesFilter(snapshot.Filter, filter) {
			out = append(out, snapshot)
		}
	}
	return out, nil
}

func (u Usecase) CreateFeedbackTrainingSnapshot(ctx context.Context, filter FeedbackTrainingFilter) (FeedbackTrainingSnapshot, error) {
	samples, truncated, err := u.feedbackTrainingSamples(ctx, filter, feedbackTrainingSampleLimit)
	if err != nil {
		return FeedbackTrainingSnapshot{}, err
	}
	sourceTrust, err := u.SourceTrustReport(ctx, SourceTrustFilter{
		SubscriptionID: filter.SubscriptionID,
		Provider:       filter.Provider,
	})
	if err != nil {
		return FeedbackTrainingSnapshot{}, err
	}
	evaluation := feedbackEvaluationReport(samples, truncated, feedbackTrainingSampleLimit)
	now := time.Now()
	snapshot := feedbackTrainingSnapshot(snapshotFilterFromTrainingFilter(filter), samples, sourceTrust, evaluation, now)
	snapshots, err := u.loadFeedbackTrainingSnapshots(ctx)
	if err != nil {
		return FeedbackTrainingSnapshot{}, err
	}
	for _, existing := range snapshots {
		if existing.Fingerprint == snapshot.Fingerprint && sameSnapshotFilter(existing.Filter, snapshot.Filter) {
			return existing, nil
		}
	}
	snapshots = append([]FeedbackTrainingSnapshot{snapshot}, snapshots...)
	if len(snapshots) > feedbackTrainingSnapshotLimit {
		snapshots = snapshots[:feedbackTrainingSnapshotLimit]
	}
	if err := u.saveFeedbackTrainingSnapshots(ctx, snapshots, now); err != nil {
		return FeedbackTrainingSnapshot{}, err
	}
	return snapshot, nil
}

func (u Usecase) ListFeedbackTrainingExports(ctx context.Context, filter FeedbackTrainingFilter) ([]FeedbackTrainingExport, error) {
	exports, err := u.loadFeedbackTrainingExports(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]FeedbackTrainingExport, 0, len(exports))
	for _, export := range exports {
		if snapshotMatchesFilter(export.Filter, filter) {
			out = append(out, export)
		}
	}
	return out, nil
}

func (u Usecase) FindFeedbackTrainingExport(ctx context.Context, version string) (FeedbackTrainingExport, bool, error) {
	version = strings.TrimSpace(version)
	if version == "" {
		return FeedbackTrainingExport{}, false, nil
	}
	exports, err := u.loadFeedbackTrainingExports(ctx)
	if err != nil {
		return FeedbackTrainingExport{}, false, err
	}
	for _, export := range exports {
		if export.Version == version {
			return export, true, nil
		}
	}
	return FeedbackTrainingExport{}, false, nil
}

func (u Usecase) CreateFeedbackTrainingExport(ctx context.Context, filter FeedbackTrainingFilter) (FeedbackTrainingExport, error) {
	samples, truncated, err := u.feedbackTrainingSamples(ctx, filter, feedbackTrainingSampleLimit)
	if err != nil {
		return FeedbackTrainingExport{}, err
	}
	sourceTrust, err := u.SourceTrustReport(ctx, SourceTrustFilter{
		SubscriptionID: filter.SubscriptionID,
		Provider:       filter.Provider,
	})
	if err != nil {
		return FeedbackTrainingExport{}, err
	}
	evaluation := feedbackEvaluationReport(samples, truncated, feedbackTrainingSampleLimit)
	now := time.Now()
	export, err := feedbackTrainingExport(snapshotFilterFromTrainingFilter(filter), samples, sourceTrust, evaluation, truncated, feedbackTrainingSampleLimit, now)
	if err != nil {
		return FeedbackTrainingExport{}, err
	}
	exports, err := u.loadFeedbackTrainingExports(ctx)
	if err != nil {
		return FeedbackTrainingExport{}, err
	}
	for _, existing := range exports {
		if existing.Fingerprint == export.Fingerprint && sameSnapshotFilter(existing.Filter, export.Filter) {
			return existing, nil
		}
	}
	exports = append([]FeedbackTrainingExport{export}, exports...)
	if len(exports) > feedbackTrainingExportLimit {
		exports = exports[:feedbackTrainingExportLimit]
	}
	if err := u.saveFeedbackTrainingExports(ctx, exports, now); err != nil {
		return FeedbackTrainingExport{}, err
	}
	return export, nil
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
			if err := u.applyFilter(ctx, repo, &rows[i]); err != nil && !isFilterCallFailed(err) {
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
		if err := u.applyFilter(ctx, repo, row); err != nil && !isFilterCallFailed(err) {
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

func normalizeMessageFeedback(input MessageFeedbackInput) (string, *string, error) {
	label := strings.ToLower(strings.TrimSpace(input.Label))
	switch label {
	case domainmsg.FeedbackHelpful, domainmsg.FeedbackNoise, domainmsg.FeedbackMisclassified, domainmsg.FeedbackNeutral:
	default:
		return "", nil, errors.New("message feedback label must be helpful, noise, misclassified, or neutral")
	}
	commentText := strings.TrimSpace(input.Comment)
	if !input.HasComment || commentText == "" {
		return label, nil, nil
	}
	return label, &commentText, nil
}

func applyMessageFeedback(row *domainmsg.IngestedMessage, label string, comment *string, hasComment bool, now time.Time) {
	row.FeedbackLabel = &label
	if hasComment {
		row.FeedbackComment = comment
	}
	row.FeedbackAt = &now
	row.UpdatedAt = now
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
	var filterErr error
	if err := u.withTx(ctx, func(repo Repository) error {
		locked, found, err := repo.FindMessage(ctx, task.MessageID)
		if err != nil || !found {
			return err
		}
		row = locked
		row.FilterStatus = domainmsg.FilterStatusFiltering
		if err := u.applyFilter(ctx, repo, row); err != nil {
			if isFilterCallFailed(err) {
				filterErr = err
				return nil
			}
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
	if filterErr != nil {
		return &MessageMutation{Row: u.hydrateMessage(ctx, *row)}, true, filterErr
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
	credentials, err := u.subscriptionCredentials(ctx, subscription, false, false)
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
		credentials, err := u.subscriptionCredentials(ctx, subscription, false, false)
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
		credentials, err := u.subscriptionCredentials(ctx, subscription, false, false)
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
		if err := u.applySourceTrustPolicy(ctx, repo, row); err != nil {
			return err
		}
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
	if saveErr := repo.SaveMessage(ctx, row); saveErr != nil {
		return saveErr
	}
	if err != nil {
		return filterCallFailedError{err: err}
	}
	if u.predictionMatcher != nil && row.FilterDecision != nil && *row.FilterDecision != domainkernel.NewsIgnore {
		messageID := row.ID
		_, _ = u.predictionMatcher.MatchNews(ctx, &messageID, row.Text)
	}
	return nil
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
	predictionMatches := u.predictionMatchesForMessage(ctx, row.ID)
	predictionMarketIDs := linkedPredictionMarketIDs(predictionMatches)
	if len(predictionMarketIDs) > 0 && symbolPart == "message-event" {
		symbolPart = fmt.Sprintf("prediction markets %s", joinUintIDs(predictionMarketIDs, 3))
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
		content := fmt.Sprintf("Subscription %s triggered a meeting.\nFilter reason: %s\nRelated symbols: %s\nRelated prediction markets: %s\n\n%s", subscription.Title, stringValue(row.FilterReason), strings.Join(symbols, ", "), joinUintIDs(predictionMarketIDs, 10), row.Text)
		if err := repo.AppendMeetingEvent(ctx, &domainmeeting.Event{MeetingID: meeting.ID, Type: domainkernel.EventSystem, Content: "Meeting submitted for execution.", Payload: u.service.JSON(map[string]any{"status": "queued"})}); err != nil {
			return nil, err
		}
		if err := repo.AppendMeetingEvent(ctx, &domainmeeting.Event{MeetingID: meeting.ID, Type: domainkernel.EventSystem, Content: content, Payload: u.service.JSON(map[string]any{"status": "message_subscription_triggered", "ingested_message_id": row.ID, "subscription_id": subscription.ID, "research_team_id": teamID, "decision": *row.FilterDecision, "related_symbols": symbols, "prediction_market_ids": predictionMarketIDs, "prediction_market_matches": predictionMatchPayload(predictionMatches)})}); err != nil {
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
		for _, match := range predictionMatches {
			if !isLinkedPredictionMatch(match) {
				continue
			}
			marketExternalRef := fmt.Sprintf("prediction_market:%d:message:%d:team:%d", match.MarketID, row.ID, teamID)
			if _, ok, err := repo.FindMeetingReferenceByExternalRef(ctx, "prediction_market", marketExternalRef); err != nil {
				return nil, err
			} else if ok {
				continue
			}
			note := fmt.Sprintf("Prediction market match score %s / message#%s", match.Score.String(), row.SourceMessageID)
			topic := fmt.Sprintf("Prediction market #%d", match.MarketID)
			summary := match.NewsSnippet
			if len(summary) > 2000 {
				summary = summary[:2000]
			}
			marketRef := domainmeeting.Reference{SourceMeetingID: meeting.ID, ReferenceType: "prediction_market", Note: &note, TargetTopicSnapshot: topic, TargetSummarySnapshot: &summary, ExternalRef: &marketExternalRef}
			if err := repo.CreateMeetingReference(ctx, &marketRef); err != nil {
				return nil, err
			}
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

func (u Usecase) subscriptionDiagnostic(ctx context.Context, row domainmsg.MessageSubscription, status map[string]bool, proxy ProxyConfig) SubscriptionDiagnostic {
	checks := []SubscriptionDiagnosticCheck{}
	sourceKind := subscriptionSourceKind(row.Provider, row.SourceRef)
	proxyRoute := subscriptionProxyRoute(row.Provider, proxy)
	privateCapable := false
	switch row.Provider {
	case domainmsg.ProviderTelegramChannel:
		privateCapable = status["has_session"]
		checks = append(checks,
			diagnosticBoolCheck("telegram_app_id", status["has_app_id"], "Telegram App ID", "Telegram MTProto app_id is configured.", "Configure Telegram App ID.", "/message-subscriptions"),
			diagnosticBoolCheck("telegram_app_hash", status["has_app_hash"], "Telegram App Hash", "Telegram MTProto app_hash is configured.", "Configure Telegram App Hash.", "/message-subscriptions"),
			diagnosticBoolCheck("telegram_session", status["has_session"], "Telegram session", "Telegram MTProto session is available for private channel access.", "Complete Telegram login and save a session.", "/message-subscriptions"),
		)
		checks = append(checks, telegramSourceCheck(sourceKind))
		checks = append(checks, proxyDiagnosticCheck(row.Provider, proxy))
	case domainmsg.ProviderRSSFeed:
		authCheck := u.rssAuthCheck(ctx, row)
		if rssAuthConfigFromSubscription(row).Type != rssAuthTypeNone && sourceKind == "rss_public_feed" {
			sourceKind = "rss_private_auth"
		}
		privateCapable = authCheck.Status == "ok" && rssAuthConfigFromSubscription(row).Type != rssAuthTypeNone
		checks = append(checks, rssSourceCheck(row.SourceRef))
		checks = append(checks, authCheck)
		checks = append(checks, proxyDiagnosticCheck(row.Provider, proxy))
	default:
		checks = append(checks, SubscriptionDiagnosticCheck{Key: "provider", Status: "blocked", Title: "Provider", Detail: "Unsupported message subscription provider.", Action: "Use telegram_channel or rss_feed."})
	}
	checks = append(checks, u.subscriptionFilterCheck(ctx, row))
	if row.Enabled && len(row.TeamIDs) == 0 {
		checks = append(checks, SubscriptionDiagnosticCheck{Key: "team_binding", Status: "blocked", Title: "Team binding", Detail: "Enabled subscriptions must be bound to at least one research team.", Action: "Bind this source to a research team.", Route: "/message-subscriptions"})
	} else if len(row.TeamIDs) == 0 {
		checks = append(checks, SubscriptionDiagnosticCheck{Key: "team_binding", Status: "warning", Title: "Team binding", Detail: "This disabled source is not bound to a research team.", Action: "Bind a team before enabling the source.", Route: "/message-subscriptions"})
	} else {
		checks = append(checks, SubscriptionDiagnosticCheck{Key: "team_binding", Status: "ok", Title: "Team binding", Detail: "At least one research team is bound."})
	}
	if row.LastCollectError != nil && strings.TrimSpace(*row.LastCollectError) != "" {
		checks = append(checks, SubscriptionDiagnosticCheck{Key: "last_collect_error", Status: "warning", Title: "Last collect error", Detail: strings.TrimSpace(*row.LastCollectError), Action: "Run source test or collect again after fixing the reported issue.", Route: "/message-subscriptions"})
	}
	overall := diagnosticOverall(checks, row.Enabled)
	return SubscriptionDiagnostic{
		SubscriptionID:     row.ID,
		Provider:           row.Provider,
		Title:              row.Title,
		SourceRef:          row.SourceRef,
		Enabled:            row.Enabled,
		SourceKind:         sourceKind,
		Status:             overall,
		Severity:           overall,
		Ready:              overall == "ready" || (!row.Enabled && !diagnosticsHaveStatus(checks, "blocked")),
		PrivateCapable:     privateCapable,
		ProxyRoute:         proxyRoute,
		LastCollectError:   row.LastCollectError,
		NextCollectAt:      row.NextCollectAt,
		LastCollectedAt:    row.LastCollectedAt,
		Checks:             checks,
		RecommendedActions: diagnosticRecommendedActions(checks),
	}
}

func (u Usecase) subscriptionFilterCheck(ctx context.Context, row domainmsg.MessageSubscription) SubscriptionDiagnosticCheck {
	filterID := row.FilterID
	if filterID == 0 {
		return SubscriptionDiagnosticCheck{Key: "filter", Status: "blocked", Title: "Message filter", Detail: "No message subscription filter is bound.", Action: "Bind an enabled filter.", Route: "/message-subscriptions"}
	}
	filter, found, err := u.repo.FindSubscriptionFilter(ctx, filterID)
	if err != nil {
		return SubscriptionDiagnosticCheck{Key: "filter", Status: "warning", Title: "Message filter", Detail: err.Error(), Action: "Check filter configuration.", Route: "/message-subscriptions"}
	}
	if !found || filter == nil {
		return SubscriptionDiagnosticCheck{Key: "filter", Status: "blocked", Title: "Message filter", Detail: "Bound filter was not found.", Action: "Bind an existing enabled filter.", Route: "/message-subscriptions"}
	}
	if !filter.Enabled {
		return SubscriptionDiagnosticCheck{Key: "filter", Status: "blocked", Title: "Message filter", Detail: "Bound filter is disabled.", Action: "Enable the filter or bind another filter.", Route: "/message-subscriptions"}
	}
	if filter.ProviderID == nil {
		return SubscriptionDiagnosticCheck{Key: "filter_provider", Status: "warning", Title: "Filter AI provider", Detail: "Filter has no AI provider bound; automatic filtering will fail until a provider/model is configured.", Action: "Bind an enabled AI provider.", Route: "/message-subscriptions"}
	}
	provider, found, err := u.repo.FindAIProvider(ctx, *filter.ProviderID)
	if err != nil {
		return SubscriptionDiagnosticCheck{Key: "filter_provider", Status: "warning", Title: "Filter AI provider", Detail: err.Error(), Action: "Check AI provider configuration.", Route: "/ai/providers"}
	}
	if !found || provider == nil || !provider.Enabled || provider.APIKeySecret == nil {
		return SubscriptionDiagnosticCheck{Key: "filter_provider", Status: "blocked", Title: "Filter AI provider", Detail: "Filter provider is missing, disabled, or has no API key.", Action: "Enable provider and save API key.", Route: "/ai/providers"}
	}
	return SubscriptionDiagnosticCheck{Key: "filter", Status: "ok", Title: "Message filter", Detail: "Filter and AI provider are ready."}
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

func subscriptionSourceKind(provider string, sourceRef string) string {
	sourceRef = strings.TrimSpace(sourceRef)
	switch provider {
	case domainmsg.ProviderRSSFeed:
		parsed, err := url.Parse(sourceRef)
		if err != nil || parsed.Hostname() == "" {
			return "invalid_url"
		}
		if parsed.User != nil {
			return "rss_url_credentials"
		}
		return "rss_public_feed"
	case domainmsg.ProviderTelegramChannel:
		lower := strings.ToLower(sourceRef)
		switch {
		case strings.HasPrefix(sourceRef, "-100"):
			return "telegram_private_numeric"
		case strings.Contains(lower, "joinchat") || strings.Contains(lower, "t.me/+") || strings.Contains(lower, "telegram.me/+"):
			return "telegram_private_invite"
		case strings.HasPrefix(sourceRef, "@"):
			return "telegram_public_handle"
		case strings.Contains(lower, "t.me/") || strings.Contains(lower, "telegram.me/"):
			return "telegram_public_link"
		case sourceRef != "":
			return "telegram_peer_ref"
		default:
			return "missing_source_ref"
		}
	default:
		return "unknown"
	}
}

func subscriptionProxyRoute(provider string, proxy ProxyConfig) string {
	if strings.TrimSpace(proxy.ProxyURL) == "" {
		return "direct"
	}
	switch provider {
	case domainmsg.ProviderTelegramChannel:
		if proxy.EnabledTelegram {
			return "proxy:telegram"
		}
	case domainmsg.ProviderRSSFeed:
		if proxy.EnabledWeb {
			return "proxy:web"
		}
	}
	return "proxy_configured_but_not_enabled"
}

func diagnosticBoolCheck(key string, ok bool, title string, okDetail string, action string, route string) SubscriptionDiagnosticCheck {
	if ok {
		return SubscriptionDiagnosticCheck{Key: key, Status: "ok", Title: title, Detail: okDetail}
	}
	return SubscriptionDiagnosticCheck{Key: key, Status: "blocked", Title: title, Detail: title + " is missing.", Action: action, Route: route}
}

func telegramSourceCheck(sourceKind string) SubscriptionDiagnosticCheck {
	switch sourceKind {
	case "telegram_private_numeric", "telegram_private_invite":
		return SubscriptionDiagnosticCheck{Key: "telegram_private_source", Status: "warning", Title: "Telegram private source", Detail: "Private or numeric Telegram refs require a logged-in MTProto session with access to the peer.", Action: "Confirm the saved Telegram account can access this channel.", Route: "/message-subscriptions", Depends: "telegram_session"}
	case "telegram_public_handle", "telegram_public_link", "telegram_peer_ref":
		return SubscriptionDiagnosticCheck{Key: "telegram_source", Status: "ok", Title: "Telegram source", Detail: "Telegram source reference is syntactically usable."}
	default:
		return SubscriptionDiagnosticCheck{Key: "telegram_source", Status: "blocked", Title: "Telegram source", Detail: "Telegram source reference is empty or invalid.", Action: "Enter @handle, t.me link, or -100 numeric peer.", Route: "/message-subscriptions"}
	}
}

func rssSourceCheck(sourceRef string) SubscriptionDiagnosticCheck {
	parsed, err := url.Parse(strings.TrimSpace(sourceRef))
	if err != nil || parsed.Hostname() == "" {
		return SubscriptionDiagnosticCheck{Key: "rss_url", Status: "blocked", Title: "RSS URL", Detail: "RSS/Atom source must be a valid http or https URL.", Action: "Enter a public RSS/Atom feed URL.", Route: "/message-subscriptions"}
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return SubscriptionDiagnosticCheck{Key: "rss_url", Status: "blocked", Title: "RSS URL", Detail: "RSS/Atom source must use http or https.", Action: "Use an http or https feed URL.", Route: "/message-subscriptions"}
	}
	if parsed.User != nil {
		return SubscriptionDiagnosticCheck{Key: "rss_url_credentials", Status: "blocked", Title: "RSS URL credentials", Detail: "RSS/Atom subscriptions reject URL-embedded credentials. Use the dedicated RSS auth fields instead.", Action: "Remove credentials from the URL and configure Basic or Bearer auth on the subscription.", Route: "/message-subscriptions"}
	}
	return SubscriptionDiagnosticCheck{Key: "rss_url", Status: "ok", Title: "RSS URL", Detail: "RSS/Atom URL is public and syntactically valid."}
}

func (u Usecase) rssAuthCheck(ctx context.Context, row domainmsg.MessageSubscription) SubscriptionDiagnosticCheck {
	auth := rssAuthConfigFromSubscription(row)
	switch auth.Type {
	case rssAuthTypeNone:
		return SubscriptionDiagnosticCheck{Key: "rss_auth", Status: "ok", Title: "RSS auth", Detail: "No RSS authentication is configured; collection treats this as a public feed."}
	case rssAuthTypeBasic:
		if strings.TrimSpace(auth.Username) == "" {
			return SubscriptionDiagnosticCheck{Key: "rss_auth", Status: "blocked", Title: "RSS auth", Detail: "Basic auth requires a username.", Action: "Enter the RSS username and password.", Route: "/message-subscriptions"}
		}
		if !u.hasRSSAuthSecret(ctx, row, auth) {
			return SubscriptionDiagnosticCheck{Key: "rss_auth", Status: "blocked", Title: "RSS auth", Detail: "Basic auth password is not configured.", Action: "Save the RSS password again.", Route: "/message-subscriptions"}
		}
		return SubscriptionDiagnosticCheck{Key: "rss_auth", Status: "ok", Title: "RSS auth", Detail: "Basic auth credentials are configured and stored as a secret."}
	case rssAuthTypeBearer:
		if !u.hasRSSAuthSecret(ctx, row, auth) {
			return SubscriptionDiagnosticCheck{Key: "rss_auth", Status: "blocked", Title: "RSS auth", Detail: "Bearer token is not configured.", Action: "Save the RSS bearer token again.", Route: "/message-subscriptions"}
		}
		return SubscriptionDiagnosticCheck{Key: "rss_auth", Status: "ok", Title: "RSS auth", Detail: "Bearer token is configured and stored as a secret."}
	default:
		return SubscriptionDiagnosticCheck{Key: "rss_auth", Status: "blocked", Title: "RSS auth", Detail: "RSS auth type is invalid.", Action: "Choose no auth, Basic, or Bearer.", Route: "/message-subscriptions"}
	}
}

func proxyDiagnosticCheck(provider string, proxy ProxyConfig) SubscriptionDiagnosticCheck {
	if strings.TrimSpace(proxy.ProxyURL) == "" {
		return SubscriptionDiagnosticCheck{Key: "proxy_route", Status: "info", Title: "Proxy route", Detail: "No outbound proxy URL is configured; source tests and collection use direct network access.", Action: "Configure proxy only if this host cannot reach the source directly.", Route: "/settings"}
	}
	switch provider {
	case domainmsg.ProviderTelegramChannel:
		if proxy.EnabledTelegram {
			return SubscriptionDiagnosticCheck{Key: "proxy_route", Status: "ok", Title: "Proxy route", Detail: "Telegram traffic is routed through the configured outbound proxy."}
		}
		return SubscriptionDiagnosticCheck{Key: "proxy_route", Status: "warning", Title: "Proxy route", Detail: "A proxy URL is configured but Telegram proxy routing is disabled.", Action: "Enable Telegram proxy routing or clear the proxy URL.", Route: "/settings"}
	case domainmsg.ProviderRSSFeed:
		if proxy.EnabledWeb {
			return SubscriptionDiagnosticCheck{Key: "proxy_route", Status: "ok", Title: "Proxy route", Detail: "RSS/Atom web traffic is routed through the configured outbound proxy."}
		}
		return SubscriptionDiagnosticCheck{Key: "proxy_route", Status: "warning", Title: "Proxy route", Detail: "A proxy URL is configured but Web/RSS proxy routing is disabled.", Action: "Enable Web proxy routing or clear the proxy URL.", Route: "/settings"}
	default:
		return SubscriptionDiagnosticCheck{Key: "proxy_route", Status: "info", Title: "Proxy route", Detail: "Proxy route is not evaluated for this provider."}
	}
}

func diagnosticOverall(checks []SubscriptionDiagnosticCheck, enabled bool) string {
	if !enabled {
		return "disabled"
	}
	if diagnosticsHaveStatus(checks, "blocked") {
		return "blocked"
	}
	if diagnosticsHaveStatus(checks, "warning") {
		return "warning"
	}
	return "ready"
}

func diagnosticsHaveStatus(checks []SubscriptionDiagnosticCheck, status string) bool {
	for _, check := range checks {
		if check.Status == status {
			return true
		}
	}
	return false
}

func diagnosticRecommendedActions(checks []SubscriptionDiagnosticCheck) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, check := range checks {
		action := strings.TrimSpace(check.Action)
		if action == "" || seen[action] {
			continue
		}
		seen[action] = true
		out = append(out, action)
	}
	return out
}

func diagnosticSeverityRank(severity string) int {
	switch severity {
	case "blocked":
		return 4
	case "warning":
		return 3
	case "ready":
		return 2
	case "disabled":
		return 1
	default:
		return 0
	}
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
	matches := u.predictionMatchesForMessage(ctx, message.ID)
	if message.Subscription != nil {
		return MessageRow{Message: message, Subscription: *message.Subscription, PredictionMatches: matches}
	}
	subscription, _, _ := repo.FindSubscriptionForMessage(ctx, message.SubscriptionID)
	if subscription == nil {
		subscription = &domainmsg.MessageSubscription{}
	}
	return MessageRow{Message: message, Subscription: *subscription, PredictionMatches: matches}
}

func (u Usecase) applySourceTrustPolicy(ctx context.Context, repo Repository, row *domainmsg.IngestedMessage) error {
	if row == nil || row.FilterDecision == nil {
		return nil
	}
	policy := u.sourceTrustPolicy(ctx, repo)
	if !policy.Enabled {
		return nil
	}
	item, ok, err := u.sourceTrustItemForMessage(ctx, repo, row)
	if err != nil || !ok {
		return err
	}
	nextDecision, action, reason := sourceTrustDownrankDecision(item, policy, *row.FilterDecision)
	if nextDecision == nil {
		return nil
	}
	original := *row.FilterDecision
	row.FilterDecision = nextDecision
	note := fmt.Sprintf("Source trust auto-downrank applied: %s; action=%s, originalDecision=%s, finalDecision=%s.", reason, action, original, *row.FilterDecision)
	if strings.TrimSpace(stringValue(row.FilterReason)) != "" {
		merged := strings.TrimSpace(*row.FilterReason) + " " + note
		row.FilterReason = &merged
	} else {
		row.FilterReason = &note
	}
	return nil
}

func (u Usecase) sourceTrustItemForMessage(ctx context.Context, repo Repository, row *domainmsg.IngestedMessage) (SourceTrustItem, bool, error) {
	if row == nil || row.SubscriptionID == 0 {
		return SourceTrustItem{}, false, nil
	}
	subscription, _, err := repo.FindSubscriptionForMessage(ctx, row.SubscriptionID)
	if err != nil {
		return SourceTrustItem{}, false, err
	}
	provider := strings.TrimSpace(row.Provider)
	if provider == "" && subscription != nil {
		provider = strings.TrimSpace(subscription.Provider)
	}
	rows, err := repo.ListFeedbackMessages(ctx, RepositoryFeedbackMessageFilter{
		SubscriptionID: strconv.FormatUint(uint64(row.SubscriptionID), 10),
		Provider:       provider,
		Limit:          feedbackTrainingSampleLimit,
	})
	if err != nil {
		return SourceTrustItem{}, false, err
	}
	sourceRef := fmt.Sprintf("subscription:%d", row.SubscriptionID)
	title := ""
	if subscription != nil {
		title = subscription.Title
		if strings.TrimSpace(subscription.SourceRef) != "" {
			sourceRef = strings.TrimSpace(subscription.SourceRef)
		}
	}
	item := SourceTrustItem{
		SubscriptionID:    row.SubscriptionID,
		SubscriptionTitle: title,
		Provider:          firstNonEmpty(provider, "unknown"),
		SourceRef:         sourceRef,
	}
	for _, feedback := range rows {
		item.FeedbackCount++
		if feedback.FeedbackAt != nil && (item.LastFeedbackAt == nil || feedback.FeedbackAt.After(*item.LastFeedbackAt)) {
			at := *feedback.FeedbackAt
			item.LastFeedbackAt = &at
		}
		switch feedbackLabelValue(feedback.FeedbackLabel) {
		case domainmsg.FeedbackHelpful:
			item.HelpfulCount++
		case domainmsg.FeedbackNoise:
			item.NoiseCount++
		case domainmsg.FeedbackMisclassified:
			item.MisclassifiedCount++
		case domainmsg.FeedbackNeutral:
			item.NeutralCount++
		}
	}
	item = finalizeSourceTrustItem(item)
	return item, item.FeedbackCount > 0, nil
}

func (u Usecase) sourceTrustPolicy(ctx context.Context, repo Repository) SourceTrustPolicy {
	policy := defaultSourceTrustPolicy()
	setting, found, err := repo.FindAppSetting(ctx, sourceTrustPolicySettingKey)
	if err != nil || !found || setting == nil || len(setting.Value) == 0 {
		return policy
	}
	var raw map[string]any
	if err := json.Unmarshal(setting.Value, &raw); err != nil {
		return policy
	}
	if value, ok := raw["enabled"].(bool); ok {
		policy.Enabled = value
	}
	policy.MinFeedback = intFromMap(raw, "minFeedback", policy.MinFeedback)
	policy.MeetingToObserveMaxScore = intFromMap(raw, "meetingToObserveMaxScore", policy.MeetingToObserveMaxScore)
	policy.ObserveToIgnoreMaxScore = intFromMap(raw, "observeToIgnoreMaxScore", policy.ObserveToIgnoreMaxScore)
	policy.ModerateNegativeRate = floatFromMap(raw, "moderateNegativeRate", policy.ModerateNegativeRate)
	policy.SevereNegativeRate = floatFromMap(raw, "severeNegativeRate", policy.SevereNegativeRate)
	return normalizeSourceTrustPolicy(policy)
}

func defaultSourceTrustPolicy() SourceTrustPolicy {
	return SourceTrustPolicy{
		Enabled:                  true,
		MinFeedback:              3,
		MeetingToObserveMaxScore: 45,
		ObserveToIgnoreMaxScore:  25,
		ModerateNegativeRate:     0.5,
		SevereNegativeRate:       0.75,
	}
}

func normalizeSourceTrustPolicy(policy SourceTrustPolicy) SourceTrustPolicy {
	if policy.MinFeedback <= 0 {
		policy.MinFeedback = 3
	}
	if policy.MeetingToObserveMaxScore <= 0 {
		policy.MeetingToObserveMaxScore = 45
	}
	if policy.ObserveToIgnoreMaxScore <= 0 {
		policy.ObserveToIgnoreMaxScore = 25
	}
	if policy.ModerateNegativeRate <= 0 || policy.ModerateNegativeRate > 1 {
		policy.ModerateNegativeRate = 0.5
	}
	if policy.SevereNegativeRate <= 0 || policy.SevereNegativeRate > 1 {
		policy.SevereNegativeRate = 0.75
	}
	return policy
}

func sourceTrustAutoAction(item SourceTrustItem, policy SourceTrustPolicy) (string, string) {
	if !policy.Enabled || item.FeedbackCount < policy.MinFeedback {
		return "", ""
	}
	negativeRate := item.NegativeRate
	if item.TrustScore <= policy.ObserveToIgnoreMaxScore || negativeRate >= policy.SevereNegativeRate {
		return "downrank_meeting_to_observe_and_observe_to_ignore", sourceTrustAutoActionReason(item)
	}
	if item.TrustScore < policy.MeetingToObserveMaxScore || negativeRate >= policy.ModerateNegativeRate {
		return "downrank_meeting_to_observe", sourceTrustAutoActionReason(item)
	}
	return "", ""
}

func sourceTrustDownrankDecision(item SourceTrustItem, policy SourceTrustPolicy, original domainkernel.NewsDecision) (*domainkernel.NewsDecision, string, string) {
	action, reason := sourceTrustAutoAction(item, policy)
	if action == "" {
		return nil, "", ""
	}
	switch original {
	case domainkernel.NewsMeeting:
		decision := domainkernel.NewsObserve
		return &decision, "downrank_meeting_to_observe", reason
	case domainkernel.NewsObserve:
		if action == "downrank_meeting_to_observe_and_observe_to_ignore" {
			decision := domainkernel.NewsIgnore
			return &decision, "downrank_observe_to_ignore", reason
		}
	}
	return nil, "", ""
}

func sourceTrustAutoActionReason(item SourceTrustItem) string {
	return fmt.Sprintf("sourceTrust=%s, score=%d, feedback=%d, negativeRate=%.0f%%", item.Status, item.TrustScore, item.FeedbackCount, item.NegativeRate*100)
}

func (u Usecase) feedbackTrainingSamples(ctx context.Context, filter FeedbackTrainingFilter, limit int) ([]FeedbackTrainingSample, bool, error) {
	if limit <= 0 {
		limit = feedbackTrainingSampleLimit
	}
	rows, err := u.repo.ListFeedbackMessages(ctx, RepositoryFeedbackMessageFilter{
		SubscriptionID: filter.SubscriptionID,
		Provider:       filter.Provider,
		Label:          filter.Label,
		Limit:          limit + 1,
	})
	if err != nil {
		return nil, false, err
	}
	truncated := false
	if len(rows) > limit {
		truncated = true
		rows = rows[:limit]
	}
	out := make([]FeedbackTrainingSample, 0, len(rows))
	for _, row := range rows {
		messageRow := u.hydrateMessage(ctx, row)
		out = append(out, feedbackTrainingSample(messageRow))
	}
	return out, truncated, nil
}

func feedbackEvaluationReport(samples []FeedbackTrainingSample, truncated bool, sampleLimit int) FeedbackEvaluationReport {
	report := FeedbackEvaluationReport{
		GeneratedAt:       time.Now(),
		Status:            "empty",
		SampleCount:       len(samples),
		LabelBreakdown:    map[string]int{},
		DecisionBreakdown: map[string]int{},
		Recommendations:   []string{},
		Truncated:         truncated,
		SampleLimit:       sampleLimit,
	}
	for _, label := range []string{domainmsg.FeedbackHelpful, domainmsg.FeedbackNoise, domainmsg.FeedbackMisclassified, domainmsg.FeedbackNeutral} {
		report.LabelBreakdown[label] = 0
	}
	for _, sample := range samples {
		switch sample.Split {
		case "validation":
			report.ValidationCount++
		default:
			report.TrainCount++
		}
		label := feedbackLabelValue(sample.Message.FeedbackLabel)
		report.LabelBreakdown[label]++
		decision := feedbackDecisionValue(sample.Message)
		report.DecisionBreakdown[decision]++
		switch label {
		case domainmsg.FeedbackHelpful:
			report.HelpfulCount++
			report.AgreementEligibleCount++
			if decision == string(domainkernel.NewsMeeting) || decision == string(domainkernel.NewsObserve) {
				report.AgreementCount++
				report.PositiveSignalCount++
			}
		case domainmsg.FeedbackNoise:
			report.NoiseCount++
			report.AgreementEligibleCount++
			if decision == string(domainkernel.NewsIgnore) {
				report.AgreementCount++
				report.NoiseSuppressionCount++
			}
			if decision == string(domainkernel.NewsMeeting) {
				report.FalseMeetingFromNoiseCount++
			}
		case domainmsg.FeedbackMisclassified:
			report.MisclassifiedCount++
			report.NeedsDecisionCorrectionCount++
		case domainmsg.FeedbackNeutral:
			report.NeutralCount++
		}
	}
	report.AgreementRate = ratio(report.AgreementCount, report.AgreementEligibleCount)
	report.NoiseRate = ratio(report.NoiseCount, report.SampleCount)
	report.MisclassificationRate = ratio(report.MisclassifiedCount, report.SampleCount)
	report.Status = feedbackEvaluationStatus(report)
	report.Recommendations = feedbackEvaluationRecommendations(report)
	return report
}

func feedbackEvaluationStatus(report FeedbackEvaluationReport) string {
	switch {
	case report.SampleCount == 0:
		return "empty"
	case report.SampleCount < 10:
		return "needs_more_feedback"
	case report.FalseMeetingFromNoiseCount > 0 || report.NoiseRate >= 0.4:
		return "noisy"
	case report.MisclassificationRate >= 0.2 || (report.AgreementEligibleCount > 0 && report.AgreementRate < 0.65):
		return "needs_review"
	default:
		return "healthy"
	}
}

func feedbackEvaluationRecommendations(report FeedbackEvaluationReport) []string {
	out := []string{}
	if report.SampleCount < 10 {
		out = append(out, "collect_more_feedback")
	}
	if report.ValidationCount == 0 && report.SampleCount > 0 {
		out = append(out, "add_validation_samples")
	}
	if report.FalseMeetingFromNoiseCount > 0 {
		out = append(out, "review_noise_meeting_triggers")
	}
	if report.MisclassifiedCount > 0 {
		out = append(out, "review_misclassified_samples")
	}
	if report.NoiseRate >= 0.4 {
		out = append(out, "tighten_source_filters")
	}
	if len(out) == 0 {
		out = append(out, "continue_feedback_collection")
	}
	return out
}

func feedbackTrainingSnapshot(filter FeedbackTrainingSnapshotFilter, samples []FeedbackTrainingSample, sourceTrust SourceTrustReport, evaluation FeedbackEvaluationReport, now time.Time) FeedbackTrainingSnapshot {
	refs := make([]FeedbackTrainingSnapshotSampleRef, 0, len(samples))
	for _, sample := range samples {
		refs = append(refs, FeedbackTrainingSnapshotSampleRef{
			MessageID:     sample.Message.ID,
			DedupeKey:     sample.DedupeKey,
			FeedbackLabel: feedbackLabelValue(sample.Message.FeedbackLabel),
			Split:         sample.Split,
			SampleWeight:  sample.SampleWeight,
		})
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].DedupeKey != refs[j].DedupeKey {
			return refs[i].DedupeKey < refs[j].DedupeKey
		}
		return refs[i].MessageID < refs[j].MessageID
	})
	sourceBreakdown := sourceTrustStatusBreakdown(sourceTrust.Items)
	labelBreakdown := cloneIntMap(evaluation.LabelBreakdown)
	decisionBreakdown := cloneIntMap(evaluation.DecisionBreakdown)
	fingerprint := feedbackTrainingFingerprint(filter, refs, labelBreakdown, decisionBreakdown, sourceBreakdown)
	storedRefs := refs
	refsTruncated := false
	if len(storedRefs) > feedbackTrainingSnapshotRefLimit {
		refsTruncated = true
		storedRefs = append([]FeedbackTrainingSnapshotSampleRef(nil), storedRefs[:feedbackTrainingSnapshotRefLimit]...)
	}
	return FeedbackTrainingSnapshot{
		Version:               fmt.Sprintf("feedback-%s-%s", now.UTC().Format("20060102T150405Z"), fingerprint[:8]),
		CreatedAt:             now,
		Filter:                filter,
		SampleCount:           evaluation.SampleCount,
		TrainCount:            evaluation.TrainCount,
		ValidationCount:       evaluation.ValidationCount,
		SourceCount:           sourceTrust.SourceCount,
		Fingerprint:           fingerprint,
		LabelBreakdown:        labelBreakdown,
		DecisionBreakdown:     decisionBreakdown,
		SourceStatusBreakdown: sourceBreakdown,
		SourceTrustSummary:    sourceTrustSummary(sourceBreakdown),
		Evaluation:            evaluation,
		SampleRefCount:        len(refs),
		SampleRefsTruncated:   refsTruncated,
		SampleRefs:            storedRefs,
	}
}

func feedbackTrainingExport(filter FeedbackTrainingSnapshotFilter, samples []FeedbackTrainingSample, sourceTrust SourceTrustReport, evaluation FeedbackEvaluationReport, truncated bool, sampleLimit int, now time.Time) (FeedbackTrainingExport, error) {
	content, lineCount, err := feedbackTrainingExportJSONL(samples)
	if err != nil {
		return FeedbackTrainingExport{}, err
	}
	sum := sha256.Sum256([]byte(content))
	contentSHA := hex.EncodeToString(sum[:])
	sourceBreakdown := sourceTrustStatusBreakdown(sourceTrust.Items)
	labelBreakdown := cloneIntMap(evaluation.LabelBreakdown)
	decisionBreakdown := cloneIntMap(evaluation.DecisionBreakdown)
	return FeedbackTrainingExport{
		Version:               fmt.Sprintf("feedback-export-%s-%s", now.UTC().Format("20060102T150405Z"), contentSHA[:8]),
		CreatedAt:             now,
		Filter:                filter,
		Format:                "jsonl",
		ContentType:           "application/x-ndjson",
		SampleCount:           evaluation.SampleCount,
		TrainCount:            evaluation.TrainCount,
		ValidationCount:       evaluation.ValidationCount,
		SourceCount:           sourceTrust.SourceCount,
		Fingerprint:           contentSHA,
		ContentSHA256:         contentSHA,
		ByteCount:             len([]byte(content)),
		LineCount:             lineCount,
		Truncated:             truncated,
		SampleLimit:           sampleLimit,
		LabelBreakdown:        labelBreakdown,
		DecisionBreakdown:     decisionBreakdown,
		SourceStatusBreakdown: sourceBreakdown,
		SourceTrustSummary:    sourceTrustSummary(sourceBreakdown),
		Evaluation:            evaluation,
		Content:               content,
	}, nil
}

func feedbackTrainingExportJSONL(samples []FeedbackTrainingSample) (string, int, error) {
	var builder strings.Builder
	for _, sample := range samples {
		message := sample.Message
		subscription := sample.Subscription
		payload := map[string]any{
			"messageId":         message.ID,
			"subscriptionId":    message.SubscriptionID,
			"subscriptionTitle": subscription.Title,
			"provider":          message.Provider,
			"sourceRef":         subscription.SourceRef,
			"sourceMessageId":   message.SourceMessageID,
			"messageTime":       message.MessageTime,
			"text":              message.Text,
			"filterDecision":    newsDecisionPtrValue(message.FilterDecision),
			"filterReason":      stringPtrValue(message.FilterReason),
			"filterStatus":      message.FilterStatus,
			"relatedSymbols":    jsonValueFromDomainJSON(message.RelatedSymbols),
			"feedbackLabel":     feedbackLabelValue(message.FeedbackLabel),
			"feedbackComment":   stringPtrValue(message.FeedbackComment),
			"feedbackAt":        message.FeedbackAt,
			"split":             sample.Split,
			"sampleWeight":      sample.SampleWeight,
			"trainingUse":       sample.TrainingUse,
			"dedupeKey":         sample.DedupeKey,
		}
		raw, err := json.Marshal(payload)
		if err != nil {
			return "", 0, err
		}
		builder.Write(raw)
		builder.WriteByte('\n')
	}
	return builder.String(), len(samples), nil
}

func feedbackTrainingFingerprint(filter FeedbackTrainingSnapshotFilter, refs []FeedbackTrainingSnapshotSampleRef, labelBreakdown map[string]int, decisionBreakdown map[string]int, sourceBreakdown map[string]int) string {
	payload := map[string]any{
		"filter":                filter,
		"sampleRefs":            refs,
		"labelBreakdown":        labelBreakdown,
		"decisionBreakdown":     decisionBreakdown,
		"sourceStatusBreakdown": sourceBreakdown,
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (u Usecase) loadFeedbackTrainingSnapshots(ctx context.Context) ([]FeedbackTrainingSnapshot, error) {
	setting, found, err := u.repo.FindAppSetting(ctx, feedbackTrainingSnapshotsSettingKey)
	if err != nil || !found || setting == nil || len(setting.Value) == 0 {
		return nil, err
	}
	var snapshots []FeedbackTrainingSnapshot
	if err := json.Unmarshal(setting.Value, &snapshots); err != nil {
		return nil, err
	}
	sort.Slice(snapshots, func(i, j int) bool {
		return snapshots[i].CreatedAt.After(snapshots[j].CreatedAt)
	})
	return snapshots, nil
}

func (u Usecase) loadFeedbackTrainingExports(ctx context.Context) ([]FeedbackTrainingExport, error) {
	setting, found, err := u.repo.FindAppSetting(ctx, feedbackTrainingExportsSettingKey)
	if err != nil || !found || setting == nil || len(setting.Value) == 0 {
		return nil, err
	}
	var exports []FeedbackTrainingExport
	if err := json.Unmarshal(setting.Value, &exports); err != nil {
		return nil, err
	}
	sort.Slice(exports, func(i, j int) bool {
		return exports[i].CreatedAt.After(exports[j].CreatedAt)
	})
	return exports, nil
}

func (u Usecase) saveFeedbackTrainingExports(ctx context.Context, exports []FeedbackTrainingExport, now time.Time) error {
	raw, err := json.Marshal(exports)
	if err != nil {
		return err
	}
	description := "Message feedback training export archive."
	return u.repo.SaveAppSetting(ctx, &domainsettings.AppSetting{
		Key:         feedbackTrainingExportsSettingKey,
		Value:       domainkernel.JSON(raw),
		Description: &description,
		UpdatedAt:   now,
	})
}

func (u Usecase) saveFeedbackTrainingSnapshots(ctx context.Context, snapshots []FeedbackTrainingSnapshot, now time.Time) error {
	raw, err := json.Marshal(snapshots)
	if err != nil {
		return err
	}
	description := "Message feedback training snapshot index."
	return u.repo.SaveAppSetting(ctx, &domainsettings.AppSetting{
		Key:         feedbackTrainingSnapshotsSettingKey,
		Value:       domainkernel.JSON(raw),
		Description: &description,
		UpdatedAt:   now,
	})
}

func sourceTrustStatusBreakdown(items []SourceTrustItem) map[string]int {
	out := map[string]int{
		"trusted":               0,
		"watch":                 0,
		"low_confidence":        0,
		"insufficient_feedback": 0,
	}
	for _, item := range items {
		status := strings.TrimSpace(item.Status)
		if status == "" {
			status = "insufficient_feedback"
		}
		out[status]++
	}
	return out
}

func sourceTrustSummary(breakdown map[string]int) FeedbackTrainingSnapshotSourceTrustSummary {
	return FeedbackTrainingSnapshotSourceTrustSummary{
		Trusted:              breakdown["trusted"],
		Watch:                breakdown["watch"],
		LowConfidence:        breakdown["low_confidence"],
		InsufficientFeedback: breakdown["insufficient_feedback"],
	}
}

func snapshotFilterFromTrainingFilter(filter FeedbackTrainingFilter) FeedbackTrainingSnapshotFilter {
	return FeedbackTrainingSnapshotFilter{
		SubscriptionID: strings.TrimSpace(filter.SubscriptionID),
		Provider:       strings.TrimSpace(filter.Provider),
		FeedbackLabel:  strings.ToLower(strings.TrimSpace(filter.Label)),
	}
}

func snapshotMatchesFilter(snapshotFilter FeedbackTrainingSnapshotFilter, filter FeedbackTrainingFilter) bool {
	if value := strings.TrimSpace(filter.SubscriptionID); value != "" && snapshotFilter.SubscriptionID != value {
		return false
	}
	if value := strings.TrimSpace(filter.Provider); value != "" && snapshotFilter.Provider != value {
		return false
	}
	if value := strings.ToLower(strings.TrimSpace(filter.Label)); value != "" && snapshotFilter.FeedbackLabel != value {
		return false
	}
	return true
}

func sameSnapshotFilter(a FeedbackTrainingSnapshotFilter, b FeedbackTrainingSnapshotFilter) bool {
	return a.SubscriptionID == b.SubscriptionID && a.Provider == b.Provider && a.FeedbackLabel == b.FeedbackLabel
}

func feedbackLabelValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(*value))
}

func stringPtrValue(value *string) any {
	if value == nil {
		return nil
	}
	return strings.TrimSpace(*value)
}

func newsDecisionPtrValue(value *domainkernel.NewsDecision) any {
	if value == nil {
		return nil
	}
	return strings.TrimSpace(string(*value))
}

func jsonValueFromDomainJSON(raw domainkernel.JSON) any {
	if len(raw) == 0 {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil
	}
	return value
}

func feedbackDecisionValue(message domainmsg.IngestedMessage) string {
	if message.FilterDecision != nil && strings.TrimSpace(string(*message.FilterDecision)) != "" {
		return strings.TrimSpace(string(*message.FilterDecision))
	}
	status := strings.TrimSpace(message.FilterStatus)
	if status == "" {
		return domainmsg.FilterStatusUnfiltered
	}
	return status
}

func cloneIntMap(value map[string]int) map[string]int {
	out := make(map[string]int, len(value))
	for key, item := range value {
		out[key] = item
	}
	return out
}

func ratio(numerator int, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func intFromMap(raw map[string]any, key string, fallback int) int {
	value, ok := raw[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func floatFromMap(raw map[string]any, key string, fallback float64) float64 {
	value, ok := raw[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err == nil {
			return parsed
		}
	}
	return fallback
}

func feedbackTrainingSample(row MessageRow) FeedbackTrainingSample {
	label := ""
	if row.Message.FeedbackLabel != nil {
		label = strings.ToLower(strings.TrimSpace(*row.Message.FeedbackLabel))
	}
	trainingUse := "classifier_training"
	switch label {
	case domainmsg.FeedbackHelpful:
		trainingUse = "positive_source_signal"
	case domainmsg.FeedbackNoise:
		trainingUse = "noise_suppression"
	case domainmsg.FeedbackMisclassified:
		trainingUse = "decision_correction"
	case domainmsg.FeedbackNeutral:
		trainingUse = "calibration"
	}
	sourceRef := strings.TrimSpace(row.Subscription.SourceRef)
	if sourceRef == "" {
		sourceRef = fmt.Sprintf("subscription:%d", row.Message.SubscriptionID)
	}
	return FeedbackTrainingSample{
		Message:      row.Message,
		Subscription: row.Subscription,
		Split:        feedbackSampleSplit(row.Message.ID),
		SampleWeight: feedbackSampleWeight(label),
		TrainingUse:  trainingUse,
		DedupeKey:    row.Message.Provider + ":" + sourceRef + ":" + strings.TrimSpace(row.Message.SourceMessageID),
	}
}

func feedbackSampleSplit(id uint) string {
	if id > 0 && id%10 == 0 {
		return "validation"
	}
	return "train"
}

func feedbackSampleWeight(label string) float64 {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case domainmsg.FeedbackHelpful, domainmsg.FeedbackNoise, domainmsg.FeedbackMisclassified:
		return 1
	case domainmsg.FeedbackNeutral:
		return 0.5
	default:
		return 0
	}
}

func finalizeSourceTrustItem(item SourceTrustItem) SourceTrustItem {
	count := item.FeedbackCount
	score := 50
	if count > 0 {
		net := item.HelpfulCount - item.NoiseCount - item.MisclassifiedCount
		score = 50 + int(float64(net)/float64(count)*50)
	}
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	item.TrustScore = score
	if count > 0 {
		item.PositiveRate = float64(item.HelpfulCount) / float64(count)
		item.NegativeRate = float64(item.NoiseCount+item.MisclassifiedCount) / float64(count)
	}
	item.SampleWeight = sourceTrustSampleWeight(item)
	item.Status, item.Explanation, item.RecommendedActions = sourceTrustStatus(item)
	return item
}

func sourceTrustSampleWeight(item SourceTrustItem) float64 {
	switch {
	case item.FeedbackCount < 3:
		return 0.5
	case item.TrustScore >= 75:
		return 1.2
	case item.TrustScore < 45:
		return 0.2
	default:
		return 0.8
	}
}

func sourceTrustStatus(item SourceTrustItem) (string, string, []string) {
	negative := item.NoiseCount + item.MisclassifiedCount
	switch {
	case item.FeedbackCount < 3:
		return "insufficient_feedback", "Fewer than 3 feedback samples are available for this source.", []string{"collect_more_feedback", "keep_current_filter"}
	case item.TrustScore >= 75 && item.HelpfulCount >= 2:
		return "trusted", "Helpful feedback materially outweighs noise and misclassification reports.", []string{"include_positive_samples", "keep_source_enabled"}
	case item.TrustScore < 45 || negative*2 >= item.FeedbackCount:
		return "low_confidence", "Noise or misclassification feedback is high enough to require source review.", []string{"review_recent_samples", "tighten_filter_prompt", "consider_source_downrank"}
	default:
		return "watch", "Feedback is mixed and should remain visible for manual governance.", []string{"review_feedback_mix", "collect_more_feedback"}
	}
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

func (u Usecase) subscriptionConfigForMutation(provider string, input SubscriptionInput, existing *domainmsg.MessageSubscription, configFieldSet bool) (domainkernel.JSON, error) {
	var raw any = input.Config
	if !configFieldSet && existing != nil {
		raw = existing.Config
	}
	cfg := sanitizeSubscriptionConfig(provider, raw)
	if provider != domainmsg.ProviderRSSFeed {
		delete(cfg, "rssAuth")
		return u.service.JSON(cfg), nil
	}
	auth := RSSAuthConfig{Type: rssAuthTypeNone}
	if existing != nil {
		auth = rssAuthConfigFromSubscription(*existing)
	}
	if auth.Type == "" {
		auth.Type = rssAuthTypeNone
	}
	if input.RSSAuthTypeSet {
		normalized, err := normalizeRSSAuthType(input.RSSAuthType)
		if err != nil {
			return nil, err
		}
		auth.Type = normalized
	}
	if input.RSSUsernameSet {
		auth.Username = strings.TrimSpace(input.RSSUsername)
	}
	if auth.Type == rssAuthTypeNone {
		auth.Username = ""
		auth.PasswordSecretName = ""
	}
	if input.RSSPasswordSet && strings.TrimSpace(input.RSSPassword) != "" && auth.Type == rssAuthTypeNone {
		return nil, errors.New("rss auth type must be basic or bearer when a password/token is provided")
	}
	setRSSAuthConfig(cfg, auth)
	return u.service.JSON(cfg), nil
}

func (u Usecase) applyRSSAuthMutation(ctx context.Context, repo Repository, row *domainmsg.MessageSubscription, input SubscriptionInput, previous *domainmsg.MessageSubscription) error {
	if row == nil || row.Provider != domainmsg.ProviderRSSFeed {
		deleteNames := rssSecretNamesForSubscription(previous, 0)
		if len(deleteNames) > 0 {
			return repo.DeleteSecretsByNames(ctx, domainkernel.SecretKindMessageSubscription, deleteNames)
		}
		return nil
	}
	cfg := sanitizeSubscriptionConfig(row.Provider, row.Config)
	auth := rssAuthConfigFromSubscription(*row)
	authType, err := normalizeRSSAuthType(auth.Type)
	if err != nil {
		return err
	}
	auth.Type = authType
	if auth.Type == "" {
		auth.Type = rssAuthTypeNone
	}
	if auth.Type == rssAuthTypeNone {
		deleteNames := rssSecretNamesForSubscription(previous, row.ID)
		setRSSAuthConfig(cfg, auth)
		row.Config = u.service.JSON(cfg)
		if err := repo.SaveSubscription(ctx, row); err != nil {
			return err
		}
		return repo.DeleteSecretsByNames(ctx, domainkernel.SecretKindMessageSubscription, deleteNames)
	}
	if auth.Type == rssAuthTypeBasic && strings.TrimSpace(auth.Username) == "" {
		return errors.New("rss basic auth username is required")
	}
	if input.RSSPasswordSet && strings.TrimSpace(input.RSSPassword) == "" {
		return errors.New("rss auth password/token cannot be empty")
	}
	secretName := rssPasswordSecretName(row.ID)
	if input.RSSPasswordSet {
		if err := u.upsertEncryptedSecret(ctx, repo, domainkernel.SecretKindMessageSubscription, secretName, strings.TrimSpace(input.RSSPassword)); err != nil {
			return err
		}
	}
	if _, ok := u.decryptedSecretWithRepo(ctx, repo, domainkernel.SecretKindMessageSubscription, secretName); !ok {
		return errors.New("rss auth password/token is required")
	}
	auth.PasswordSecretName = secretName
	setRSSAuthConfig(cfg, auth)
	row.Config = u.service.JSON(cfg)
	if err := repo.SaveSubscription(ctx, row); err != nil {
		return err
	}
	deleteNames := staleRSSSecretNames(previous, secretName)
	if len(deleteNames) > 0 {
		return repo.DeleteSecretsByNames(ctx, domainkernel.SecretKindMessageSubscription, deleteNames)
	}
	return nil
}

func (u Usecase) subscriptionDraftCredentials(ctx context.Context, provider string, input SubscriptionInput) (SubscriptionCredentials, error) {
	credentials := SubscriptionCredentials{Proxy: u.ProxyConfig(ctx)}
	if provider != domainmsg.ProviderRSSFeed {
		telegram, err := u.telegramRuntimeCredentials(ctx, false, false)
		if err != nil {
			return credentials, err
		}
		credentials.Telegram = telegram
		credentials.Proxy = telegram.Proxy
		return credentials, nil
	}
	authType, err := normalizeRSSAuthType(input.RSSAuthType)
	if err != nil {
		return credentials, err
	}
	credentials.RSSAuth = RSSAuthCredentials{
		Type:     authType,
		Username: strings.TrimSpace(input.RSSUsername),
		Password: strings.TrimSpace(input.RSSPassword),
	}
	if credentials.RSSAuth.Type == rssAuthTypeNone && credentials.RSSAuth.Password != "" {
		return credentials, errors.New("rss auth type must be basic or bearer when a password/token is provided")
	}
	if credentials.RSSAuth.Type == rssAuthTypeBasic && credentials.RSSAuth.Username == "" {
		return credentials, errors.New("rss basic auth username is required")
	}
	if credentials.RSSAuth.Type != rssAuthTypeNone && credentials.RSSAuth.Password == "" {
		return credentials, errors.New("rss auth password/token is required")
	}
	return credentials, nil
}

func (u Usecase) subscriptionCredentials(ctx context.Context, subscription *domainmsg.MessageSubscription, requireTelegramSession bool, requireTelegramApp bool) (SubscriptionCredentials, error) {
	credentials := SubscriptionCredentials{Proxy: u.ProxyConfig(ctx)}
	if subscription == nil {
		return credentials, errors.New("message subscription is required")
	}
	if subscription.Provider != domainmsg.ProviderRSSFeed {
		telegram, err := u.telegramRuntimeCredentials(ctx, requireTelegramSession, requireTelegramApp)
		if err != nil {
			return credentials, err
		}
		credentials.Telegram = telegram
		credentials.Proxy = telegram.Proxy
		return credentials, nil
	}
	auth := rssAuthConfigFromSubscription(*subscription)
	authType, err := normalizeRSSAuthType(auth.Type)
	if err != nil {
		return credentials, err
	}
	auth.Type = authType
	credentials.RSSAuth.Type = auth.Type
	credentials.RSSAuth.Username = strings.TrimSpace(auth.Username)
	if auth.Type == rssAuthTypeNone {
		return credentials, nil
	}
	if auth.Type == rssAuthTypeBasic && credentials.RSSAuth.Username == "" {
		return credentials, errors.New("rss basic auth username is required")
	}
	secretName := strings.TrimSpace(auth.PasswordSecretName)
	if secretName == "" && subscription.ID != 0 {
		secretName = rssPasswordSecretName(subscription.ID)
	}
	password, ok := u.decryptedSecret(ctx, domainkernel.SecretKindMessageSubscription, secretName)
	if !ok || strings.TrimSpace(password) == "" {
		return credentials, errors.New("rss auth password/token is not configured")
	}
	credentials.RSSAuth.Password = strings.TrimSpace(password)
	return credentials, nil
}

func normalizeRSSAuthType(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", rssAuthTypeNone:
		return rssAuthTypeNone, nil
	case rssAuthTypeBasic:
		return rssAuthTypeBasic, nil
	case rssAuthTypeBearer:
		return rssAuthTypeBearer, nil
	default:
		return "", fmt.Errorf("unsupported rss auth type: %s", value)
	}
}

func rssPasswordSecretName(subscriptionID uint) string {
	return fmt.Sprintf("rss:%d:password", subscriptionID)
}

func rssAuthConfigFromSubscription(row domainmsg.MessageSubscription) RSSAuthConfig {
	auth := rssAuthConfigFromRaw(row.Config)
	authType, err := normalizeRSSAuthType(auth.Type)
	if err != nil {
		auth.Type = strings.TrimSpace(auth.Type)
		return auth
	}
	auth.Type = authType
	return auth
}

func rssAuthConfigFromRaw(raw any) RSSAuthConfig {
	cfg := subscriptionConfigMap(raw)
	authRaw, ok := cfg["rssAuth"]
	if !ok || authRaw == nil {
		return RSSAuthConfig{Type: rssAuthTypeNone}
	}
	authMap, ok := authRaw.(map[string]any)
	if !ok {
		return RSSAuthConfig{Type: rssAuthTypeNone}
	}
	auth := RSSAuthConfig{
		Type:               mapStringValue(authMap, "type"),
		Username:           mapStringValue(authMap, "username"),
		PasswordSecretName: mapStringValue(authMap, "passwordSecretName"),
	}
	if auth.Type == "" {
		auth.Type = rssAuthTypeNone
	}
	return auth
}

func mapStringValue(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func sanitizeSubscriptionConfig(provider string, raw any) map[string]any {
	cfg := subscriptionConfigMap(raw)
	if provider != domainmsg.ProviderRSSFeed {
		delete(cfg, "rssAuth")
		return cfg
	}
	delete(cfg, "rssPassword")
	delete(cfg, "rssToken")
	if rawAuth, ok := cfg["rssAuth"].(map[string]any); ok {
		clean := map[string]any{}
		for _, key := range []string{"type", "username", "passwordSecretName"} {
			if value, ok := rawAuth[key]; ok {
				clean[key] = value
			}
		}
		cfg["rssAuth"] = clean
	}
	return cfg
}

func subscriptionConfigMap(raw any) map[string]any {
	if raw == nil {
		return map[string]any{}
	}
	var value map[string]any
	switch typed := raw.(type) {
	case domainkernel.JSON:
		if len(typed) == 0 {
			return map[string]any{}
		}
		_ = json.Unmarshal(typed, &value)
	case []byte:
		if len(typed) == 0 {
			return map[string]any{}
		}
		_ = json.Unmarshal(typed, &value)
	case map[string]any:
		value = typed
	default:
		rawJSON, err := json.Marshal(raw)
		if err == nil {
			_ = json.Unmarshal(rawJSON, &value)
		}
	}
	if value == nil {
		return map[string]any{}
	}
	return cloneStringAnyMap(value)
}

func cloneStringAnyMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, value := range in {
		if nested, ok := value.(map[string]any); ok {
			out[key] = cloneStringAnyMap(nested)
			continue
		}
		out[key] = value
	}
	return out
}

func setRSSAuthConfig(cfg map[string]any, auth RSSAuthConfig) {
	authType, err := normalizeRSSAuthType(auth.Type)
	if err != nil || authType == rssAuthTypeNone {
		delete(cfg, "rssAuth")
		return
	}
	out := map[string]any{"type": authType}
	if strings.TrimSpace(auth.Username) != "" {
		out["username"] = strings.TrimSpace(auth.Username)
	}
	if strings.TrimSpace(auth.PasswordSecretName) != "" {
		out["passwordSecretName"] = strings.TrimSpace(auth.PasswordSecretName)
	}
	cfg["rssAuth"] = out
}

func rssSecretNamesForSubscription(row *domainmsg.MessageSubscription, fallbackID uint) []string {
	names := []string{}
	if row != nil {
		auth := rssAuthConfigFromSubscription(*row)
		if strings.TrimSpace(auth.PasswordSecretName) != "" {
			names = append(names, strings.TrimSpace(auth.PasswordSecretName))
		}
		if row.ID != 0 {
			names = append(names, rssPasswordSecretName(row.ID))
		}
	}
	if fallbackID != 0 {
		names = append(names, rssPasswordSecretName(fallbackID))
	}
	return uniqueStrings(names)
}

func staleRSSSecretNames(previous *domainmsg.MessageSubscription, keep string) []string {
	names := rssSecretNamesForSubscription(previous, 0)
	out := names[:0]
	for _, name := range names {
		if name != "" && name != keep {
			out = append(out, name)
		}
	}
	return out
}

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func (u Usecase) hasRSSAuthSecret(ctx context.Context, row domainmsg.MessageSubscription, auth RSSAuthConfig) bool {
	name := strings.TrimSpace(auth.PasswordSecretName)
	if name == "" && row.ID != 0 {
		name = rssPasswordSecretName(row.ID)
	}
	return u.hasSecret(ctx, domainkernel.SecretKindMessageSubscription, name)
}

func (u Usecase) decryptedSecretWithRepo(ctx context.Context, repo Repository, kind domainkernel.SecretKind, name string) (string, bool) {
	if strings.TrimSpace(name) == "" {
		return "", false
	}
	secret, found, err := repo.FindSecret(ctx, kind, name)
	if err != nil || !found || secret == nil {
		return "", false
	}
	value, err := u.security.DecryptSecret(secret.EncryptedValue)
	return value, err == nil && strings.TrimSpace(value) != ""
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

func firstNonEmptyUintSlice(values ...[]uint) []uint {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

func (u Usecase) predictionMatchesForMessage(ctx context.Context, messageID uint) []domainprediction.Match {
	if u.predictionMatcher == nil || messageID == 0 {
		return nil
	}
	rows, err := u.predictionMatcher.ListMatchesForMessage(ctx, messageID)
	if err != nil {
		return nil
	}
	return rows
}

func linkedPredictionMarketIDs(matches []domainprediction.Match) []uint {
	ids := make([]uint, 0, len(matches))
	for _, match := range matches {
		if isLinkedPredictionMatch(match) {
			ids = append(ids, match.MarketID)
		}
	}
	return uniqueUintIDs(ids)
}

func isLinkedPredictionMatch(match domainprediction.Match) bool {
	return match.Status == "linked" || match.Status == "confirmed"
}

func predictionMatchPayload(matches []domainprediction.Match) []map[string]any {
	out := make([]map[string]any, 0, len(matches))
	for _, match := range matches {
		out = append(out, map[string]any{
			"id":       match.ID,
			"marketId": match.MarketID,
			"score":    match.Score,
			"status":   match.Status,
			"query":    match.Query,
			"reason":   match.Reason,
		})
	}
	return out
}

func joinUintIDs(values []uint, limit int) string {
	values = uniqueUintIDs(values)
	if len(values) == 0 {
		return ""
	}
	if limit > 0 && len(values) > limit {
		values = values[:limit]
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.FormatUint(uint64(value), 10))
	}
	return strings.Join(parts, ", ")
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
