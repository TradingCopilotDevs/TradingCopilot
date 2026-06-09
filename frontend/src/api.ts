import axios from 'axios'
import type { components } from './generated/api-types'
import { clearAuthToken, getAuthToken, isAuthTokenUsable } from './auth'
import { router } from './router'

export const api = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL || '/api'
})

export type JsonApiDocument = components['schemas']['JsonApiDocument']
export type JsonApiErrorDocument = components['schemas']['JsonApiErrorDocument']
export type JsonApiResource = components['schemas']['Resource']
export type JsonApiError = components['schemas']['ErrorObject']
export type ApiSchema<K extends keyof components['schemas']> = components['schemas'][K]
export type ApiResourceAttributes<K extends keyof components['schemas']> =
  ApiSchema<K> extends { attributes: infer Attributes } ? Attributes : never
export type ApiResourceModel<
  K extends keyof components['schemas'],
  Id extends string | number = number
> = Required<ApiSchema<K>> & { id: Id }
export type ApiResourceModelFromResource<
  K extends keyof components['schemas'],
  Id extends string | number = number
> = Required<ApiResourceAttributes<K>> & { id: Id }
type Override<T, U> = Omit<T, keyof U> & U
type ApiDecimal = string | number
type ApiNullableDecimal = ApiDecimal | null

api.interceptors.request.use((config) => {
  const token = getAuthToken()
  if (isAuthTokenUsable(token)) {
    config.headers.Authorization = `Bearer ${token}`
  }
  if (config.data?.data) {
    config.headers['Content-Type'] = 'application/vnd.api+json'
  }
  return config
})

api.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401 && !isAuthRequest(error.config?.url)) {
      clearAuthToken()
      const current = router.currentRoute.value
      if (current.path !== '/login') {
        router.replace({ path: '/login', query: { redirect: current.fullPath } })
      }
    }
    return Promise.reject(error)
  }
)

function isAuthRequest(url?: string) {
  if (!url) return false
  return url.includes('/auth/login') || url.includes('/auth/bootstrap')
}

export function jsonapiResource(
  type: string,
  attributes: Record<string, unknown> = {},
  id?: string
) {
  return {
    data: {
      type,
      ...(id ? { id } : {}),
      attributes: camelizeRecord(attributes)
    }
  }
}

export function unwrapJsonApiCollection<T = Record<string, unknown>>(
  document: unknown
): T[] {
  const data = (document as any)?.data
  if (!Array.isArray(data)) return []
  return data.map((resource) => unwrapJsonApiResource<T>({ data: resource })).filter(Boolean) as T[]
}

export function unwrapJsonApiResource<T = Record<string, unknown>>(document: unknown): T | null {
  const data = (document as any)?.data
  if (!data || Array.isArray(data)) return null
  const attributes = (data.attributes ?? {}) as Record<string, unknown>
  return { id: numericId(data.id), ...attributes } as T
}

export function numericId(id: unknown): string | number | undefined {
  if (typeof id !== 'string') return id as any
  return /^\d+$/.test(id) ? Number(id) : id
}

export function camelizeRecord<T = unknown>(value: T): T {
  return transformRecordKeys(value) as T
}

export function apiErrorText(error: unknown, fallback = '操作失败') {
  const data = (error as { response?: { data?: { detail?: string; errors?: Array<{ detail?: string; message?: string }> } } }).response?.data
  return data?.errors?.[0]?.detail || data?.errors?.[0]?.message || data?.detail || fallback
}

function transformRecordKeys(value: unknown): unknown {
  if (Array.isArray(value)) return value.map((item) => transformRecordKeys(item))
  if (!value || typeof value !== 'object') return value
  return Object.fromEntries(
    Object.entries(value as Record<string, unknown>).map(([key, item]) => [
      key.replace(/_([a-z])/g, (_, char: string) => char.toUpperCase()),
      transformRecordKeys(item)
    ])
  )
}

export type Meeting = ApiResourceModel<'MeetingAttributes'>
export type MeetingTrustReport = ApiSchema<'MeetingTrustReport'>
export type MeetingTrustReview = ApiResourceModel<'MeetingTrustReviewAttributes', string | number>
export type MeetingTrustReviewAttributes = ApiSchema<'MeetingTrustReviewAttributes'>
export type MeetingRecapActionSuggestion = ApiSchema<'MeetingRecapActionSuggestion'>
export type MeetingRecapActionReviewAttributes = ApiSchema<'MeetingRecapActionReviewAttributes'>
export type MessageFeedbackTrainingSample = ApiResourceModel<'MessageFeedbackTrainingSampleAttributes'>
export type MessageFeedbackEvaluation = ApiResourceModel<'MessageFeedbackEvaluationAttributes', string | number>
export type MessageFeedbackTrainingSnapshot = ApiResourceModel<'MessageFeedbackTrainingSnapshotAttributes', string | number>
export type MessageFeedbackTrainingExport = ApiResourceModel<'MessageFeedbackTrainingExportAttributes', string | number>
export type MessageSourceTrust = ApiResourceModel<'MessageSourceTrustAttributes', string | number>
export type MessageSourceTrustReport = ApiResourceModel<'MessageSourceTrustReportAttributes', string | number>

export type DashboardStatusItem = ApiSchema<'DashboardStatusItem'>

export type DashboardSummary = ApiSchema<'DashboardSummary'>

export type DashboardBusinessMetrics = Override<ApiSchema<'DashboardBusinessMetrics'>, {
  paperTotalEquity: ApiDecimal
  aiCostAmount24h?: ApiDecimal
}>

export type ProxySettings = ApiSchema<'ProxySettingAttributes'>

export type SetupStep = ApiSchema<'SetupStep'>

export type SetupReadiness = ApiResourceModel<'SetupReadinessAttributes', string | number>

export type SetupActionResult = ApiResourceModel<'SetupActionResultAttributes', string | number>

export type AdminUser = ApiResourceModel<'AdminUserAttributes'>

export type AuthSession = ApiResourceModel<'AuthSessionAttributes'>

export type AuditEvent = ApiResourceModel<'AuditEventAttributes'>

export type MarketTask = ApiResourceModel<'MarketTaskAttributes', string | number>

export type OpsProviderHealth = ApiResourceModelFromResource<'OpsProviderHealthResource', string | number>

export type OpsJobs = ApiResourceModelFromResource<'OpsJobsResource', string | number>

export type OpsRecentErrors = ApiSchema<'OpsRecentErrors'>

export type OpsRecentErrorEntry = ApiSchema<'OpsRecentErrorEntry'>

export interface OpsJobAction {
  id?: string | number
  status?: string
  action?: string
  retriedRetryCount?: number
  retriedArchiveCount?: number
  totalSubmitted?: number
  queues?: Record<string, unknown>[]
  queue?: string
  taskId?: string
  taskType?: string
  state?: string
  ranAt?: string
  destructive?: boolean
  detail?: string
}

export type OpsQueueDiagnostics = ApiSchema<'OpsQueueDiagnostics'> & {
  filters?: OpsQueueFilters
}

export interface OpsQueueFilters {
  queue?: string
  type?: string
  state?: 'retry' | 'archived' | ''
  failedLimit?: number
}

export type OpsQueueInfo = ApiSchema<'OpsQueueInfo'>

export type OpsQueueTotals = ApiSchema<'OpsQueueTotals'>

export type OpsQueueFailedTask = ApiSchema<'OpsQueueFailedTask'>

export type OpsQueueBacklogRisk = ApiSchema<'OpsQueueBacklogRisk'>

export type OpsBackups = Override<ApiResourceModelFromResource<'OpsBackupsResource', string | number>, {
  generatedAt: string
  status: string
  backupDir: string
  archiveProvider: string
  archiveProviderDetail: OpsBackupArchiveProvider
  databaseBackend?: string
  databaseTarget?: string
  retentionPolicy: string
  retentionPolicyDetail?: Record<string, unknown>
  retentionLastRun?: Record<string, unknown> | null
  restoreDrill: string
  restoreDrillDetail?: Record<string, unknown>
  restoreDrillSchedule?: Record<string, unknown>
  restoreDrillScheduleHint?: string
  supportedActions: string[]
  latestBackup?: OpsBackupArchive | null
  backups: OpsBackupArchive[]
}>

export type OpsBackupArchive = ApiSchema<'OpsBackupArchive'>

export type OpsBackupArchiveProvider = ApiSchema<'OpsBackupArchiveProvider'>

export type OpsBackupRun = Override<ApiResourceModelFromResource<'OpsBackupRunResource', string | number>, {
  databaseBackend?: string
  databaseTarget?: string
}>

export type OpsBackupRestoreDryRun = ApiResourceModel<'OpsBackupRestoreDryRunAttributes', string | number>

export type DashboardRecentIngestedMessage = ApiSchema<'DashboardRecentIngestedMessage'>

export type DashboardRecentMeeting = ApiSchema<'DashboardRecentMeeting'>

export type DashboardRecentActivity = ApiSchema<'DashboardRecentActivity'>

export type DashboardAlert = ApiSchema<'DashboardAlert'>

export type DashboardOverview = Override<ApiResourceModelFromResource<'DashboardResource', string | number>, {
  summary: DashboardSummary
  businessMetrics: DashboardBusinessMetrics
}>

export type LogEntry = ApiResourceModel<'LogEntryAttributes', string>

export type LogFile = ApiResourceModel<'LogFileAttributes', string>

export type LogConfigSummary = ApiSchema<'LogConfigSummary'>

export type MeetingEvent = ApiResourceModelFromResource<'MeetingEventResource'>

export interface TelegramChannel {
  id: number
  title: string
  channelRef: string
  enabled: boolean
  backfillLimit: number
  collectFrom: string
}

export interface TelegramMessage {
  id: number
  channelId: number
  channelTitle?: string
  channelRef?: string
  messageId: number
  messageTime: string
  text: string
  filterDecision?: string | null
  filterReason?: string | null
  relatedSymbols: string[]
  filteredAt?: string | null
  filterModelRoleKey?: string | null
  createdAt: string
  updatedAt: string
}

export type MessageSubscription = ApiResourceModelFromResource<'MessageSubscriptionResource'>

export type MessageSubscriptionDiagnostic = ApiResourceModel<'MessageSubscriptionDiagnosticAttributes'>

export type MessageSubscriptionFilter = ApiResourceModel<'MessageSubscriptionFilterAttributes'>

export type IngestedMessage = ApiResourceModel<'IngestedMessageAttributes'>

export type ResearchTeam = ApiResourceModel<'ResearchTeamAttributes'>

export type ResearchTeamRole = ApiResourceModel<'ResearchTeamRoleAttributes', string | number>

export type PlatformAdapter = ApiResourceModelFromResource<'PlatformAdapterResource'>

export type MeetingReference = ApiResourceModelFromResource<'MeetingReferenceResource'>

export type WakePlan = ApiResourceModel<'WakePlanAttributes'>

export type PaperOrder = Override<ApiResourceModel<'PaperOrderAttributes'>, {
  filledQuantity: number
  remainingQuantity: number
  partialFillCount: number
  approvalRiskReasons?: string[]
  approvalRiskMetrics?: Record<string, unknown>
  suggestedPrice: ApiNullableDecimal
  filledPrice: ApiNullableDecimal
  commission: ApiNullableDecimal
  stampDuty: ApiNullableDecimal
  transferFee: ApiNullableDecimal
  netAmount: ApiNullableDecimal
}>

export type RiskConfig = Override<ApiResourceModel<'PaperRiskConfigAttributes'>, {
  initialCash: ApiDecimal
  maxPositionPct: ApiDecimal
  maxOrderPct: ApiDecimal
  commissionRate: ApiDecimal
  minCommission: ApiDecimal
  stampDutyRate: ApiDecimal
  transferFeeRate: ApiDecimal
}>

export type PaperAccount = Override<ApiResourceModel<'PaperAccountAttributes'>, {
  initialCash: ApiDecimal
  cash: ApiDecimal
  marketValue: ApiDecimal
  totalEquity: ApiDecimal
  unrealizedPnl: ApiDecimal
  realizedPnl: ApiDecimal
  totalReturnPct: ApiDecimal
}>

export type PaperPosition = Override<ApiResourceModel<'PaperPositionAttributes'>, {
  avgCost: ApiDecimal
  costAmount: ApiDecimal
  lastPrice: ApiNullableDecimal
  marketValue: ApiDecimal
  unrealizedPnl: ApiDecimal
  realizedPnl: ApiDecimal
}>

export type PaperFill = Override<ApiResourceModel<'PaperFillAttributes'>, {
  price: ApiDecimal
  grossAmount: ApiDecimal
  commission: ApiDecimal
  stampDuty: ApiDecimal
  transferFee: ApiDecimal
  netAmount: ApiDecimal
  realizedPnl: ApiDecimal
}>

export type PaperCorporateAction = Override<ApiResourceModel<'PaperCorporateActionAttributes'>, {
  cashPerShare: ApiNullableDecimal
  shareRatio: ApiNullableDecimal
  cashAmount: ApiNullableDecimal
}>

export type PaperReplayEvent = Required<ApiSchema<'PaperReplayEvent'>>

export type PaperReplay = ApiResourceModel<'PaperReplayAttributes', string | number>

export type PaperBacktestInput = Override<ApiSchema<'PaperBacktestInput'>, {
  initialCash?: ApiDecimal
  buyThresholdPct?: ApiDecimal
  sellThresholdPct?: ApiDecimal
  orderPct?: ApiDecimal
  slippageBps?: ApiDecimal
}>

export type PaperBacktestPolicy = Override<Required<ApiSchema<'PaperBacktestPolicy'>>, {
  limitBandPct: ApiDecimal
  slippageBps: ApiDecimal
  rules: string[]
}>

export type PaperBacktestSummary = Override<Required<ApiSchema<'PaperBacktestSummary'>>, {
  initialCash: ApiDecimal
  finalCash: ApiDecimal
  finalMarketValue: ApiDecimal
  finalEquity: ApiDecimal
  totalReturnPct: ApiDecimal
  maxDrawdownPct: ApiDecimal
}>

export type PaperBacktestPoint = Override<Required<ApiSchema<'PaperBacktestPoint'>>, {
  close: ApiDecimal
  signalPct: ApiDecimal
  cash: ApiDecimal
  marketValue: ApiDecimal
  totalEquity: ApiDecimal
  dailyReturnPct: ApiDecimal
  drawdownPct: ApiDecimal
}>

export type PaperBacktestOrder = Override<Required<ApiSchema<'PaperBacktestOrder'>>, {
  signalPct: ApiDecimal
  referencePrice: ApiDecimal
  filledPrice: ApiDecimal
  grossAmount: ApiDecimal
  fees: ApiDecimal
  cashAfter: ApiDecimal
}>

export type PaperBacktest = Override<Required<ApiSchema<'PaperBacktestAttributes'>>, {
  input: PaperBacktestInput
  policy: PaperBacktestPolicy
  summary: PaperBacktestSummary
  series: PaperBacktestPoint[]
  orders: PaperBacktestOrder[]
}>

export type PaperPerformancePoint = Override<ApiSchema<'PaperPerformancePoint'>, {
  cash: ApiDecimal
  marketValue: ApiDecimal
  totalEquity: ApiDecimal
  unrealizedPnl: ApiDecimal
  realizedPnl: ApiDecimal
  dailyPnl: ApiDecimal
}>

export type PaperAttributionItem = Override<Required<ApiSchema<'PaperAttributionItem'>>, {
  costAmount: ApiDecimal
  marketValue: ApiDecimal
  weightPct: ApiDecimal
  unrealizedPnl: ApiDecimal
  realizedPnl: ApiDecimal
  totalPnl: ApiDecimal
  returnPct: ApiDecimal
  contributionPct: ApiDecimal
}>

export type PaperRiskAlert = Override<ApiSchema<'PaperRiskAlert'>, {
  metric?: ApiNullableDecimal
  threshold?: ApiNullableDecimal
}>

export type PaperRiskSummary = Required<ApiSchema<'PaperRiskSummary'>>

export type PaperPerformance = Override<Required<ApiSchema<'PaperPerformanceAttributes'>>, {
  initialCash: ApiDecimal
  latestEquity: ApiDecimal
  latestCash: ApiDecimal
  latestMarketValue: ApiDecimal
  unrealizedPnl: ApiDecimal
  realizedPnl: ApiDecimal
  totalReturnPct: ApiDecimal
  maxDrawdownPct: ApiDecimal
  winRatePct: ApiDecimal
  series: PaperPerformancePoint[]
  attribution: PaperAttributionItem[]
  riskAlerts: PaperRiskAlert[]
  riskSummary: PaperRiskSummary
}>

export type PaperOverview = Override<Required<ApiSchema<'PaperOverviewAttributes'>>, {
  totalCash: ApiDecimal
  totalMarketValue: ApiDecimal
  totalEquity: ApiDecimal
  totalUnrealizedPnl: ApiDecimal
  totalRealizedPnl: ApiDecimal
  totalReturnPct: ApiDecimal
}>
