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

export interface Meeting {
  id: number
  researchTeamId: number
  topic: string
  status: string
  triggerSource: string
  summary?: string
  conclusion?: string
  tags: string[]
  recapStatus?: string
  recapUpdatedAt?: string
  runAttempt?: number
  heartbeatAt?: string | null
  autoRequeueCount?: number
  createdAt: string
  startedAt?: string
  completedAt?: string
}

export interface DashboardStatusItem {
  key: string
  title: string
  status: 'ok' | 'warning' | 'error' | 'disabled'
  summary: string
  detail: string
  checkedAt: string
}

export interface DashboardSummary {
  appName: string
  appEnv: string
  deploymentModeLabel: string
  meetingDispatchMode: string
  databaseBackend: string
  databaseTarget?: string
  paperExecutionMode: string
}

export interface DashboardBusinessMetrics {
  meetingsTotal: number
  meetingsRunning: number
  meetingsFailed24h: number
  messageSubscriptionEnabledCount: number
  ingestedMessages24h: number
  ingestedUnfilteredCount: number
  platformAdapterEnabledCount: number
  paperAccountCount: number
  paperActiveAccountCount: number
  paperTotalEquity: string | number
  paperPendingOrderCount: number
  wakeActiveCount: number
  wakeOverdueCount: number
  aiEnabledProviderCount: number
  aiReadyProviderCount: number
  newsFilterReady: boolean
  marketSymbolCount: number
  marketWatchlistCount: number
  marketActiveWatchlistCount: number
  paperIsTradingTime: boolean
}

export interface ProxySettings {
  proxyUrl: string
  enabledAi: boolean
  enabledTelegram: boolean
  enabledMarket: boolean
  enabledWeb: boolean
  noProxy: string[]
  revision?: string
  updatedAt?: string
}

export interface DashboardRecentIngestedMessage {
  id: number
  subscriptionId: number
  subscriptionTitle?: string | null
  sourceRef?: string | null
  messageTime: string
  text: string
  filterDecision?: string | null
}

export interface DashboardRecentMeeting {
  id: number
  topic: string
  status: string
  triggerSource: string
  summary?: string | null
  conclusion?: string | null
  tags: string[]
  recapStatus?: string | null
  recapUpdatedAt?: string | null
  runAttempt?: number
  heartbeatAt?: string | null
  autoRequeueCount?: number
  createdAt: string
  startedAt?: string | null
  completedAt?: string | null
}

export interface DashboardRecentActivity {
  recentMeetings: DashboardRecentMeeting[]
  recentIngestedMessages: DashboardRecentIngestedMessage[]
}

export interface DashboardAlert {
  level: 'warning' | 'error'
  title: string
  detail: string
  link: string
}

export interface DashboardOverview {
  summary: DashboardSummary
  systemStatus: {
    database: DashboardStatusItem
    redis: DashboardStatusItem
    worker: DashboardStatusItem
    scheduler: DashboardStatusItem
    messageSubscriptionListener: DashboardStatusItem
    platformAdapter: DashboardStatusItem
    paperEngine: DashboardStatusItem
  }
  businessMetrics: DashboardBusinessMetrics
  recentActivity: DashboardRecentActivity
  alerts: DashboardAlert[]
}

export interface LogEntry {
  id: string
  time?: string | null
  level: string
  role: string
  source: string
  event: string
  message: string
  caller: string
  file: string
  group: string
  method: string
  path: string
  status: string
  durationMs?: number | null
  noise: boolean
  fields: Record<string, unknown>
  raw: Record<string, unknown>
  stacktrace?: string
}

export interface LogFile {
  id: string
  name: string
  sizeBytes: number
  modified: string
  active: boolean
  role: string
}

export interface LogConfigSummary {
  dir?: string
  level?: string
  rotationMode?: string
  rotationSizeMB?: number
  rotationTotalSizeMB?: number
  rotationMaxAgeDays?: number
}

export interface MeetingEvent {
  id: number
  meetingId: number
  sequence: number
  type: string
  roleKey?: string
  content: string
  payload: Record<string, unknown>
  createdAt: string
}

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

export interface MessageSubscription {
  id: number
  provider: string
  title: string
  sourceRef: string
  enabled: boolean
  filterId: number
  filterName?: string
  teamIds: number[]
  backfillLimit: number
  pollIntervalSeconds: number
  collectFrom: string
  lastCollectedAt?: string | null
  nextCollectAt?: string | null
  lastCollectError?: string | null
  config?: Record<string, unknown>
  createdAt?: string
  updatedAt?: string
}

export interface MessageSubscriptionFilter {
  id: number
  name: string
  description: string
  promptTemplate: string
  providerId?: number | null
  model?: string | null
  enabled: boolean
  isDefault: boolean
  toolNames: string[]
  skillNames: string[]
  createdAt?: string
  updatedAt?: string
}

export interface IngestedMessage {
  id: number
  subscriptionId: number
  subscriptionTitle?: string
  sourceRef?: string
  provider: string
  sourceMessageId: string
  messageTime: string
  text: string
  filterDecision?: string | null
  filterReason?: string | null
  filterStatus?: 'unfiltered' | 'filtering' | 'filtered' | 'failed'
  relatedSymbols: string[]
  filteredAt?: string | null
  filterId?: number | null
  createdAt: string
  updatedAt: string
}

export interface ResearchTeam {
  id: number
  name: string
  description: string
  paperAccountId: number
  active: boolean
  createdAt?: string
  updatedAt?: string
}

export interface ResearchTeamRole {
  id: number | string
  researchTeamId: number
  key: string
  name: string
  responsibility: string
  promptTemplate: string
  providerId?: number | null
  model?: string | null
  toolNames: string[]
  skillNames: string[]
  enabled: boolean
  sortOrder: number
}

export interface PlatformAdapter {
  id: number
  provider: string
  displayName: string
  enabled: boolean
  config?: Record<string, unknown>
  hasBotToken: boolean
  hasChatId: boolean
  chatId?: string
  createdAt?: string
  updatedAt?: string
}

export interface MeetingReference {
  id: number
  sourceMeetingId: number
  targetMeetingId?: number | null
  referenceType: string
  note?: string | null
  targetTopicSnapshot: string
  targetSummarySnapshot?: string | null
  targetDeleted: boolean
  externalRef?: string | null
  createdAt: string
}

export interface WakePlan {
  id: number
  researchTeamId: number
  meetingId?: number | null
  triggerType: string
  triggerConfig: Record<string, unknown>
  reason: string
  sourceMeetingEventId?: number | null
  sourceRoleKey?: string | null
  status: string
  nextCheckAt?: string | null
  firedAt?: string | null
  lastRunAt?: string | null
  resultSummary?: string | null
}

export interface PaperOrder {
  id: number
  accountId: number
  meetingId?: number | null
  code: string
  symbolName?: string | null
  side: string
  quantity: number
  status: string
  suggestedPrice?: string | number | null
  filledPrice?: string | number | null
  reason?: string | null
  submittedAt?: string | null
  executeAfter?: string | null
  expireAt?: string | null
  sourceMeetingEventId?: number | null
  executionNote?: string | null
  commission?: string | number | null
  stampDuty?: string | number | null
  transferFee?: string | number | null
  netAmount?: string | number | null
  createdAt: string
  filledAt?: string | null
}

export interface RiskConfig {
  id: number
  name: string
  initialCash: string | number
  maxPositionPct: string | number
  maxOrderPct: string | number
  allowShort: boolean
  allowMargin: boolean
  allowShMain: boolean
  allowSzMain: boolean
  allowBj: boolean
  allowStar: boolean
  allowChinext: boolean
  allowEtfLof: boolean
  commissionRate: string | number
  minCommission: string | number
  stampDutyRate: string | number
  transferFeeRate: string | number
  enabled: boolean
  accountIds?: number[]
  accountCount?: number
}

export interface PaperAccount {
  id: number
  name: string
  initialCash: string | number
  cash: string | number
  riskConfigId?: number | null
  researchTeamId?: number | null
  researchTeamName?: string | null
  active: boolean
  marketValue: string | number
  totalEquity: string | number
  unrealizedPnl: string | number
  realizedPnl: string | number
  totalReturnPct: string | number
  positionCount: number
  pendingOrderCount: number
}

export interface PaperPosition {
  id: number
  accountId: number
  code: string
  symbolName?: string | null
  quantity: number
  avgCost: string | number
  costAmount: string | number
  lastPrice?: string | number | null
  marketValue: string | number
  unrealizedPnl: string | number
  realizedPnl: string | number
  updatedAt: string
}

export interface PaperFill {
  id: number
  orderId: number
  accountId: number
  code: string
  symbolName?: string | null
  side: string
  quantity: number
  price: string | number
  grossAmount: string | number
  commission: string | number
  stampDuty: string | number
  transferFee: string | number
  netAmount: string | number
  realizedPnl: string | number
  filledAt: string
}

export interface PaperPerformancePoint {
  id: number
  accountId: number
  snapshotTime: string
  cash: string | number
  marketValue: string | number
  totalEquity: string | number
  unrealizedPnl: string | number
  realizedPnl: string | number
  dailyPnl: string | number
}

export interface PaperPerformance {
  accountId: number
  initialCash: string | number
  latestEquity: string | number
  latestCash: string | number
  latestMarketValue: string | number
  unrealizedPnl: string | number
  realizedPnl: string | number
  totalReturnPct: string | number
  maxDrawdownPct: string | number
  winRatePct: string | number
  fillsCount: number
  series: PaperPerformancePoint[]
}

export interface PaperOverview {
  accountCount: number
  activeAccountCount: number
  totalCash: string | number
  totalMarketValue: string | number
  totalEquity: string | number
  totalUnrealizedPnl: string | number
  totalRealizedPnl: string | number
  totalReturnPct: string | number
  positionCount: number
  fillCount: number
  suggestedOrderCount: number
  pendingOrderCount: number
  filledOrderCount: number
  rejectedOrderCount: number
  cancelledOrderCount: number
}
