package kernel

type SecretKind string

const (
	SecretKindAIProvider          SecretKind = "ai_provider"
	SecretKindTelegram            SecretKind = "telegram"
	SecretKindMarketData          SecretKind = "market_data"
	SecretKindApp                 SecretKind = "app"
	SecretKindMessageSubscription SecretKind = "message_subscription"
	SecretKindPlatformAdapter     SecretKind = "platform_adapter"
)

type MeetingStatus string

const (
	MeetingQueued    MeetingStatus = "queued"
	MeetingRunning   MeetingStatus = "running"
	MeetingCompleted MeetingStatus = "completed"
	MeetingFailed    MeetingStatus = "failed"
	MeetingCancelled MeetingStatus = "cancelled"
)

type MeetingEventType string

const (
	EventSystem      MeetingEventType = "system"
	EventRoleMessage MeetingEventType = "role_message"
	EventToolCall    MeetingEventType = "tool_call"
	EventToolResult  MeetingEventType = "tool_result"
	EventConclusion  MeetingEventType = "conclusion"
	EventError       MeetingEventType = "error"
)

type NewsDecision string

const (
	NewsIgnore  NewsDecision = "ignore"
	NewsObserve NewsDecision = "observe"
	NewsMeeting NewsDecision = "meeting"
)

type WakeTriggerType string

const (
	WakeTime      WakeTriggerType = "time"
	WakeIndicator WakeTriggerType = "indicator"
	WakeEvent     WakeTriggerType = "event"
)

type WakePlanStatus string

const (
	WakeActive    WakePlanStatus = "active"
	WakePaused    WakePlanStatus = "paused"
	WakeFired     WakePlanStatus = "fired"
	WakeCancelled WakePlanStatus = "cancelled"
)

type OrderSide string

const (
	OrderBuy  OrderSide = "buy"
	OrderSell OrderSide = "sell"
)

type OrderStatus string

const (
	OrderSuggested OrderStatus = "suggested"
	OrderPending   OrderStatus = "pending"
	OrderFilled    OrderStatus = "filled"
	OrderRejected  OrderStatus = "rejected"
	OrderCancelled OrderStatus = "cancelled"
	OrderExpired   OrderStatus = "expired"
)
