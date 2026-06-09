<template>
  <h1 class="page-title">总览</h1>

  <div class="section-head">
    <div class="muted">系统健康、运行模式、业务概况与最近活动</div>
    <el-button :loading="loading" @click="loadDashboard()">刷新</el-button>
  </div>

  <div v-if="alerts.length" class="dashboard-alerts">
    <el-alert
      v-for="(item, index) in alerts"
      :key="`${item.level}-${item.title}-${index}`"
      :title="item.title"
      :type="item.level"
      :closable="false"
      show-icon
      :class="['dashboard-alert', item.actionLabel !== '查看' ? 'dashboard-alert--actionable' : '']"
    >
      <template #default>
        <div class="dashboard-alert__body">
          <div class="dashboard-alert__copy">
            <span>{{ item.detail }}</span>
            <div v-if="item.wakePlanSummaries?.length" class="dashboard-alert__objects">
              <div v-for="plan in item.wakePlanSummaries" :key="plan.id" class="dashboard-alert__object">
                <strong>#{{ plan.id }} {{ wakeTriggerLabel(plan.triggerType) }}</strong>
                <span>
                  投研团队：{{ wakeSummaryTeamLabel(plan) }} · 来源会议：{{ wakeSummaryMeetingLabel(plan) }} · 到期：{{ formatDateTimeUtc8(plan.nextCheckAt) }}
                </span>
                <span>{{ plan.reason || '未填写唤醒原因' }}</span>
              </div>
              <div v-if="item.remainingCount && item.remainingCount > 0" class="muted dashboard-alert__more">
                还有 {{ item.remainingCount }} 个逾期计划，进入唤醒计划页可查看完整列表。
              </div>
            </div>
          </div>
          <el-button link type="primary" @click="router.push(item.link)">{{ item.actionLabel }}</el-button>
        </div>
      </template>
    </el-alert>
  </div>

  <div class="metric-grid dashboard-summary-grid">
    <div v-for="item in summaryCards" :key="item.label" class="metric">
      <span>{{ item.label }}</span>
      <strong>{{ item.value }}</strong>
      <div v-if="item.hint" class="muted dashboard-summary-hint">{{ item.hint }}</div>
    </div>
  </div>

  <div class="panel">
    <div class="section-head">
      <h2>系统状态</h2>
      <div class="muted">启用态与运行态分离显示</div>
    </div>
    <div class="dashboard-status-grid">
      <div v-for="item in statusCards" :key="item.key" class="dashboard-status-card">
        <div class="dashboard-status-card__head">
          <strong>{{ item.title }}</strong>
          <el-tag :type="statusTagType(item.status)" effect="plain">{{ statusLabel(item.status) }}</el-tag>
        </div>
        <div class="dashboard-status-card__summary">{{ item.summary }}</div>
        <div class="muted dashboard-status-card__detail">{{ item.detail }}</div>
        <div class="muted dashboard-status-card__time">检查时间：{{ formatDateTimeUtc8(item.checkedAt) }}</div>
      </div>
    </div>
  </div>

  <div class="dashboard-business-grid">
    <div v-for="section in businessSections" :key="section.title" class="panel dashboard-business-card">
      <div class="section-head">
        <h2>{{ section.title }}</h2>
        <el-button link type="primary" @click="router.push(section.link)">进入</el-button>
      </div>
      <div class="dashboard-kv-grid">
        <div v-for="item in section.items" :key="item.label" class="dashboard-kv">
          <span>{{ item.label }}</span>
          <strong>{{ item.value }}</strong>
        </div>
      </div>
    </div>
  </div>

  <div class="dashboard-activity-grid">
    <div class="panel">
      <div class="section-head">
        <h2>最近会议</h2>
        <el-button link type="primary" @click="router.push('/meetings')">全部会议</el-button>
      </div>

      <div v-if="isMobile" class="mobile-card-list">
        <div v-for="meeting in recentMeetings" :key="meeting.id" class="mobile-card">
          <div class="mobile-card__header">
            <div>
              <h3 class="mobile-card__title">#{{ meeting.id }} {{ meeting.topic }}</h3>
              <div class="muted">{{ formatDateTimeUtc8(meeting.createdAt) }}</div>
            </div>
            <el-tag :type="meetingStatusTagType(meeting.status)" effect="plain">{{ meetingStatusLabel(meeting.status) }}</el-tag>
          </div>
          <div class="mobile-card__meta">
            <div class="mobile-card__meta-row">
              <span class="mobile-card__meta-label">来源</span>
              <span>{{ triggerSourceLabel(meeting.triggerSource) }}</span>
            </div>
            <div class="mobile-card__meta-row">
              <span class="mobile-card__meta-label">重试次数</span>
              <span>{{ meeting.runAttempt ?? 0 }}</span>
            </div>
          </div>
          <div class="mobile-card__actions">
            <el-button type="primary" plain @click="router.push(`/meetings/${meeting.id}`)">查看</el-button>
          </div>
        </div>
        <el-empty v-if="!recentMeetings.length" description="暂无会议" />
      </div>

      <el-table v-else :data="recentMeetings" empty-text="暂无会议">
        <el-table-column prop="id" label="编号" width="80" />
        <el-table-column prop="topic" label="主题" min-width="220" />
        <el-table-column label="状态" width="120">
          <template #default="{ row }">
            <el-tag :type="meetingStatusTagType(row.status)" effect="plain">{{ meetingStatusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="来源" width="140">
          <template #default="{ row }">{{ triggerSourceLabel(row.triggerSource) }}</template>
        </el-table-column>
        <el-table-column label="创建时间" min-width="180">
          <template #default="{ row }">{{ formatDateTimeUtc8(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="100">
          <template #default="{ row }">
            <el-button link type="primary" @click="router.push(`/meetings/${row.id}`)">查看</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="panel">
      <div class="section-head">
        <h2>最近采集消息</h2>
        <el-button link type="primary" @click="router.push('/ingested-messages')">全部消息</el-button>
      </div>

      <div v-if="isMobile" class="mobile-card-list">
        <div v-for="message in recentIngestedMessages" :key="message.id" class="mobile-card">
          <div class="mobile-card__header">
            <div>
              <h3 class="mobile-card__title">{{ message.subscriptionTitle || '未知订阅源' }}</h3>
              <div class="muted">{{ formatDateTimeUtc8(message.messageTime) }}</div>
            </div>
            <el-tag :type="telegramDecisionTagType(message.filterDecision)" effect="plain">
              {{ telegramDecisionLabel(message.filterDecision) }}
            </el-tag>
          </div>
          <div class="mobile-card__meta">
            <div class="mobile-card__meta-row">
              <span class="mobile-card__meta-label">频道</span>
              <span>{{ message.sourceRef || '-' }}</span>
            </div>
            <div class="mobile-card__meta-row">
              <span class="mobile-card__meta-label">内容</span>
              <span class="dashboard-message-text">{{ message.text }}</span>
            </div>
          </div>
        </div>
        <el-empty v-if="!recentIngestedMessages.length" description="暂无消息" />
      </div>

      <el-table v-else :data="recentIngestedMessages" empty-text="暂无消息">
        <el-table-column label="订阅源" min-width="180">
          <template #default="{ row }">
            <div>{{ row.subscriptionTitle || '未知订阅源' }}</div>
            <div class="muted">{{ row.sourceRef || '-' }}</div>
          </template>
        </el-table-column>
        <el-table-column label="过滤结果" width="120">
          <template #default="{ row }">
            <el-tag :type="telegramDecisionTagType(row.filterDecision)" effect="plain">
              {{ telegramDecisionLabel(row.filterDecision) }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="时间" min-width="180">
          <template #default="{ row }">{{ formatDateTimeUtc8(row.messageTime) }}</template>
        </el-table-column>
        <el-table-column label="内容" min-width="280" show-overflow-tooltip>
          <template #default="{ row }">{{ row.text }}</template>
        </el-table-column>
      </el-table>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, type DashboardAlert, type DashboardOverview, type DashboardRecentMeeting, type DashboardStatusItem } from '../api'
import { useAutoRefresh } from '../composables/useAutoRefresh'
import { useResponsive } from '../composables/useResponsive'
import { formatDateTimeUtc8 } from '../utils/datetime'

const router = useRouter()
const { isMobile } = useResponsive()
const dashboard = ref<DashboardOverview | null>(null)
const loading = ref(false)

type DashboardWakePlanSummary = {
  id: number
  researchTeamId?: number
  researchTeamName?: string
  meetingId?: number | null
  triggerType?: string
  reason?: string
  nextCheckAt?: string | null
}

type DashboardAlertView = DashboardAlert & {
  actionLabel: string
  wakePlanSummaries?: DashboardWakePlanSummary[]
  remainingCount?: number
}

const alerts = computed<DashboardAlertView[]>(() => (dashboard.value?.alerts || []).map(localizeDashboardAlert))
const recentMeetings = computed<DashboardRecentMeeting[]>(() => dashboard.value?.recentActivity.recentMeetings || [])
const recentIngestedMessages = computed(() => dashboard.value?.recentActivity.recentIngestedMessages || [])
const statusCards = computed<DashboardStatusItem[]>(() => {
  const systemStatus = dashboard.value?.systemStatus
  if (!systemStatus) return []
  return [
    systemStatus.database,
    systemStatus.redis,
    systemStatus.worker,
    systemStatus.scheduler,
    systemStatus.messageSubscriptionListener,
    systemStatus.platformAdapter,
    systemStatus.paperEngine
  ].map(localizeStatusItem)
})

const summaryCards = computed(() => {
  const summary = dashboard.value?.summary
  const metrics = dashboard.value?.businessMetrics
  if (!summary || !metrics) return []
  return [
    { label: '环境', value: envLabel(summary.appEnv), hint: summary.deploymentModeLabel },
    { label: '会议模式', value: modeLabel(summary.meetingDispatchMode), hint: paperModeLabel(summary.paperExecutionMode) },
    { label: '数据库', value: summary.databaseBackend, hint: summary.databaseTarget || (metrics.paperIsTradingTime ? '当前为交易时段' : '当前非交易时段') },
    { label: '运行中会议', value: String(metrics.meetingsRunning), hint: `总数 ${metrics.meetingsTotal}` },
    { label: '消息订阅器', value: String(metrics.messageSubscriptionEnabledCount), hint: `24 小时消息 ${metrics.ingestedMessages24h}` },
    { label: '模拟盘总资产', value: formatMoney(metrics.paperTotalEquity), hint: `待执行订单 ${metrics.paperPendingOrderCount}` }
  ]
})

const businessSections = computed(() => {
  const metrics = dashboard.value?.businessMetrics
  if (!metrics) return []
  return [
    {
      title: '会议',
      link: '/meetings',
      items: [
        { label: '总会议数', value: String(metrics.meetingsTotal) },
        { label: '运行中', value: String(metrics.meetingsRunning) },
        { label: '24 小时失败', value: String(metrics.meetingsFailed24h) }
      ]
    },
    {
      title: '消息接入',
      link: '/message-subscriptions',
      items: [
        { label: '启用订阅器', value: String(metrics.messageSubscriptionEnabledCount) },
        { label: '平台适配器', value: String(metrics.platformAdapterEnabledCount) },
        { label: '未过滤消息', value: String(metrics.ingestedUnfilteredCount) }
      ]
    },
    {
      title: '模拟盘',
      link: '/paper',
      items: [
        { label: '账户数', value: String(metrics.paperAccountCount) },
        { label: '活跃账户', value: String(metrics.paperActiveAccountCount) },
        { label: '总资产', value: formatMoney(metrics.paperTotalEquity) }
      ]
    },
    {
      title: '模型提供商',
      link: '/model-providers',
      items: [
        { label: '启用服务商', value: String(metrics.aiEnabledProviderCount) },
        { label: '可用服务商', value: String(metrics.aiReadyProviderCount) },
        { label: '消息过滤器', value: metrics.newsFilterReady ? '已就绪' : '未就绪' }
      ]
    },
    {
      title: '唤醒计划',
      link: '/wake',
      items: [
        { label: '活跃计划', value: String(metrics.wakeActiveCount) },
        { label: '逾期计划', value: String(metrics.wakeOverdueCount) },
        { label: '执行方式', value: paperModeLabel(dashboard.value?.summary.paperExecutionMode) }
      ]
    },
    {
      title: '市场',
      link: '/market',
      items: [
        { label: '标的数', value: String(metrics.marketSymbolCount) },
        { label: '自选总数', value: String(metrics.marketWatchlistCount) },
        { label: '活跃自选', value: String(metrics.marketActiveWatchlistCount) }
      ]
    }
  ]
})

function formatMoney(value: string | number | null | undefined) {
  const num = Number(value ?? 0)
  return num.toLocaleString('zh-CN', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2
  })
}

function envLabel(value: string) {
  const map: Record<string, string> = {
    dev: '开发',
    test: '测试',
    prod: '生产'
  }
  return map[value] || value || '-'
}

function modeLabel(value: string) {
  const map: Record<string, string> = {
    local: '本地执行',
    auto: '自动切换',
    redis: 'Redis 队列'
  }
  return map[value] || value || '-'
}

function paperModeLabel(value: string | null | undefined) {
  const map: Record<string, string> = {
    local_loop: '本地循环',
    redis_queue: '队列执行'
  }
  return map[String(value || '')] || value || '-'
}

function statusLabel(value: string) {
  const map: Record<string, string> = {
    ok: '正常',
    warning: '警告',
    error: '异常',
    disabled: '未启用'
  }
  return map[value] || value || '-'
}

function localizeDashboardAlert(item: DashboardAlert): DashboardAlertView {
  if (item.title === 'Overdue wake plans') {
    const summaries = dashboardWakePlanSummaries(item)
    const totalCount = numberFromUnknown((item as Record<string, unknown>).totalCount) ?? numberFromText(item.detail)
    const count = totalCount ? String(totalCount) : overdueWakePlanCount(item.detail)
    return {
      ...item,
      title: '唤醒计划待处理',
      detail: `${count} 个生效中的唤醒计划已经到期但尚未被后台调度处理。下方列出具体计划；进入唤醒计划页后可手动触发、暂停或取消。若计划应自动处理，请检查后台工作进程和定时调度器。`,
      link: '/wake?overdue=true',
      actionLabel: '处理唤醒计划',
      wakePlanSummaries: summaries,
      remainingCount: Math.max(0, (totalCount || summaries.length) - summaries.length)
    }
  }
  if (item.title === 'Message filter AI is not ready') {
    return {
      ...item,
      title: '消息过滤模型未就绪',
      detail: '消息订阅过滤器缺少可用的模型服务商、API Key 或默认模型，自动过滤不会运行。请先配置模型提供商，再回到消息订阅页确认过滤器已启用。',
      link: '/model-providers',
      actionLabel: '配置模型'
    }
  }
  if (item.title === 'Paper engine is unhealthy') {
    return {
      ...item,
      title: '模拟盘引擎异常',
      detail: '存在启用中的模拟盘账户，但模拟盘执行环境不健康。请进入模拟盘查看待执行订单；若当前为队列模式，请检查后台工作进程和定时调度器心跳。',
      link: '/paper',
      actionLabel: '检查模拟盘'
    }
  }
  return localizeDependencyAlert(item)
}

function overdueWakePlanCount(detail: string) {
  const match = String(detail || '').match(/\d+/)
  return match ? match[0] : '有'
}

function localizeDependencyAlert(item: DashboardAlert): DashboardAlertView {
  const key = statusKeyFromAlertTitle(item.title)
  const title = statusTitleLabel(key, item.title)
  return {
    ...item,
    title: `${title}异常`,
    detail: `${statusDetailLabel(item.detail)} ${dashboardAlertFixText(item.title, item.link)}`.trim(),
    actionLabel: dashboardAlertActionLabel(item.title, item.link)
  }
}

function statusKeyFromAlertTitle(title: string) {
  const map: Record<string, string> = {
    Database: 'database',
    Redis: 'redis',
    Worker: 'worker',
    Scheduler: 'scheduler',
    'Message Subscription Listener': 'message_subscription_listener',
    'Platform Adapter': 'platform_adapter',
    'Paper Engine': 'paper_engine'
  }
  return map[title] || title
}

function dashboardAlertFixText(title: string, link: string) {
  const map: Record<string, string> = {
    Redis: '影响：队列模式下会议、唤醒计划和模拟盘任务可能不会自动执行。处理：进入系统设置检查 REDIS_URL 和 Redis 服务状态，或切换为本地执行模式。',
    Worker: '影响：后台队列任务可能堆积，包括自动会议、唤醒处理和模拟盘执行。处理：确认 worker 进程已启动，并能写入心跳。',
    Scheduler: '影响：定时扫描不会按时处理唤醒计划。处理：确认 scheduler 进程已启动，并能写入心跳。',
    'Message Subscription Listener': '影响：实时订阅消息不会自动进入系统。处理：进入消息订阅页检查 Telegram 凭据、MTProto 会话和已启用订阅源。',
    'Platform Adapter': '影响：系统无法发送外部通知。处理：进入平台适配器页配置并启用通知通道。',
    'Paper Engine': '影响：模拟盘订单和维护任务可能无法继续执行。处理：进入模拟盘查看账户与订单，并检查后台 worker/scheduler 心跳。'
  }
  return map[title] || `处理：进入${link || '对应页面'}查看异常详情并修复配置。`
}

function dashboardAlertActionLabel(title: string, link: string) {
  const map: Record<string, string> = {
    Database: '检查数据库',
    Redis: '检查 Redis',
    Worker: '检查后台进程',
    Scheduler: '检查定时调度',
    'Message Subscription Listener': '检查订阅监听',
    'Platform Adapter': '配置通知通道',
    'Paper Engine': '检查模拟盘'
  }
  return map[title] || (link ? '去处理' : '查看')
}

function dashboardWakePlanSummaries(item: DashboardAlert): DashboardWakePlanSummary[] {
  const raw = (item as Record<string, unknown>).wakePlanSummaries
  if (!Array.isArray(raw)) return []
  const summaries: DashboardWakePlanSummary[] = []
  for (const entry of raw) {
    const object = entry as Record<string, unknown>
    const id = numberFromUnknown(object.id)
    if (!id) continue
    summaries.push({
      id,
      researchTeamId: numberFromUnknown(object.researchTeamId),
      researchTeamName: stringFromUnknown(object.researchTeamName),
      meetingId: numberFromUnknown(object.meetingId) ?? null,
      triggerType: stringFromUnknown(object.triggerType),
      reason: stringFromUnknown(object.reason),
      nextCheckAt: stringFromUnknown(object.nextCheckAt)
    })
  }
  return summaries
}

function wakeSummaryTeamLabel(plan: DashboardWakePlanSummary) {
  return plan.researchTeamName || (plan.researchTeamId ? `#${plan.researchTeamId}` : '-')
}

function wakeSummaryMeetingLabel(plan: DashboardWakePlanSummary) {
  return plan.meetingId ? `#${plan.meetingId}` : '-'
}

function numberFromText(value: string) {
  const match = String(value || '').match(/\d+/)
  return match ? Number(match[0]) : undefined
}

function numberFromUnknown(value: unknown) {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : undefined
}

function stringFromUnknown(value: unknown) {
  if (value === null || value === undefined) return undefined
  const text = String(value).trim()
  return text || undefined
}

function localizeStatusItem(item: DashboardStatusItem): DashboardStatusItem {
  return {
    ...item,
    title: statusTitleLabel(item.key, item.title),
    summary: statusSummaryLabel(item.summary),
    detail: statusDetailLabel(item.detail)
  }
}

function statusTitleLabel(key: string, fallback: string) {
  const map: Record<string, string> = {
    database: '数据库',
    redis: 'Redis',
    worker: '后台工作进程',
    scheduler: '定时调度器',
    message_subscription_listener: '消息订阅监听',
    platform_adapter: '平台适配器',
    paper_engine: '模拟盘引擎'
  }
  return map[key] || fallback || '-'
}

function wakeTriggerLabel(value: string | null | undefined) {
  const map: Record<string, string> = {
    time: '定时',
    indicator: '指标',
    event: '事件',
    interval: '周期',
    condition: '条件',
    manual: '手动',
    market: '行情',
    news: '消息'
  }
  return map[String(value || '')] || value || '-'
}

function statusSummaryLabel(value: string) {
  const map: Record<string, string> = {
    connected: '已连接',
    'local mode': '本地模式',
    'invalid Redis URL': 'Redis 地址无效',
    'ping failed': 'Ping 失败',
    'ping ok': 'Ping 正常',
    'not configured': '未配置',
    'partial config': '配置不完整',
    'no enabled channels': '无启用频道',
    'not enabled': '未启用',
    'no heartbeat': '无心跳',
    'invalid heartbeat': '心跳无效',
    stopped: '已停止',
    'stale heartbeat': '心跳超时',
    'running with errors': '运行中但有错误',
    running: '运行中',
    SQLite: 'SQLite',
    PostgreSQL: 'PostgreSQL',
    'local loop': '本地循环',
    'queue mode': '队列模式',
    'queue mode unhealthy': '队列模式异常'
  }
  return map[value] || value || '-'
}

function statusDetailLabel(value: string) {
  const exact: Record<string, string> = {
    'GORM connection is active': '数据库连接正常。',
    'Local dispatch mode does not require Redis.': '本地调度模式不需要 Redis。',
    'Redis queue dependency responded to PING.': 'Redis 队列依赖已响应 PING。',
    'Configure a platform adapter to enable outbound notifications.': '配置平台适配器后启用发送通知。',
    'Configure Telegram App ID, App Hash, and complete MTProto login to enable live subscription listening.':
      '配置 Telegram App ID、App Hash 并完成 MTProto 登录后启用订阅监听。',
    'Live subscription listening requires App ID, App Hash, and a stored MTProto session.':
      '订阅监听需要 App ID、App Hash 和已保存的 MTProto 会话。',
    'MTProto is configured, but no message subscriptions are enabled.': 'MTProto 已配置，但还没有启用的消息订阅源。',
    'Configure Bot Token and Chat ID on the Telegram page to enable polling.': '在 Telegram 页面配置 Bot Token 和 Chat ID 后启用轮询。',
    'Bot polling requires both Bot Token and Chat ID.': 'Bot 轮询需要同时配置 Bot Token 和 Chat ID。',
    'Configure Telegram App ID, App Hash, and complete MTProto login to enable live channel listening.':
      '配置 Telegram App ID、App Hash 并完成 MTProto 登录后启用频道监听。',
    'Live channel listening requires App ID, App Hash, and a stored MTProto session.':
      '实时频道监听需要 App ID、App Hash 和已保存的 MTProto 会话。',
    'MTProto is configured, but no Telegram channels are enabled.': 'MTProto 已配置，但还没有启用的 Telegram 频道。',
    'Current local mode does not require this background process.': '当前本地模式不需要这个后台进程。',
    'No heartbeat has been recorded by this process.': '尚未收到该进程的心跳。',
    'Heartbeat payload is missing a valid timestamp.': '心跳数据缺少有效时间戳。',
    'Paper maintenance runs in the application process in local mode.': '本地模式下，模拟盘维护任务在应用进程内运行。',
    'Worker and scheduler heartbeats are healthy.': '后台工作进程和定时调度器心跳正常。',
    'Paper maintenance requires healthy worker and scheduler heartbeats in queue mode.':
      '队列模式下，模拟盘维护任务需要后台工作进程和定时调度器心跳正常。'
  }
  if (exact[value]) return exact[value]

  const database = value.match(/^Connected to (SQLite|PostgreSQL) database (.+)\.$/)
  if (database) return `已连接到 ${database[1]} 数据库：${database[2]}。`

  const stopped = value.match(/^Last heartbeat reported stopped about (\d+) seconds ago\.$/)
  if (stopped) return `最后一次心跳在约 ${stopped[1]} 秒前报告已停止。`

  const stale = value.match(/^Last heartbeat was about (\d+) seconds ago, exceeding (\d+) seconds\.$/)
  if (stale) return `最后一次心跳约在 ${stale[1]} 秒前，已超过 ${stale[2]} 秒阈值。`

  const running = value.match(/^Last heartbeat was about (\d+) seconds ago, mode ([^.]+)\.(?: Last error: (.*))?$/)
  if (running) {
    const mode = modeLabel(running[2])
    return running[3] ? `最后一次心跳约在 ${running[1]} 秒前，模式：${mode}。最后错误：${running[3]}` : `最后一次心跳约在 ${running[1]} 秒前，模式：${mode}。`
  }

  return value || '-'
}

function statusTagType(value: string) {
  const map: Record<string, 'success' | 'warning' | 'danger' | 'info'> = {
    ok: 'success',
    warning: 'warning',
    error: 'danger',
    disabled: 'info'
  }
  return map[value] || 'info'
}

function meetingStatusLabel(value: string) {
  const map: Record<string, string> = {
    queued: '排队中',
    running: '运行中',
    completed: '已完成',
    failed: '失败',
    cancelled: '已取消'
  }
  return map[value] || value || '-'
}

function meetingStatusTagType(value: string) {
  const map: Record<string, 'primary' | 'success' | 'warning' | 'danger' | 'info'> = {
    queued: 'warning',
    running: 'primary',
    completed: 'success',
    failed: 'danger',
    cancelled: 'info'
  }
  return map[value] || 'info'
}

function triggerSourceLabel(value: string | null | undefined) {
  const map: Record<string, string> = {
    manual: '手动创建',
    telegram: '消息触发',
    wake: '唤醒计划',
    scheduler: '调度器'
  }
  return map[String(value || '')] || value || '-'
}

function telegramDecisionLabel(value: string | null | undefined) {
  const map: Record<string, string> = {
    ignore: '忽略',
    observe: '观察',
    meeting: '触发会议'
  }
  return map[String(value || '')] || '未过滤'
}

function telegramDecisionTagType(value: string | null | undefined) {
  const map: Record<string, 'success' | 'warning' | 'info'> = {
    ignore: 'info',
    observe: 'warning',
    meeting: 'success'
  }
  return map[String(value || '')] || 'info'
}

async function loadDashboard(background = false) {
  if (!background) loading.value = true
  try {
    const { data } = await api.get('/dashboard')
    dashboard.value = data.data?.attributes ?? null
  } finally {
    if (!background) loading.value = false
  }
}

onMounted(loadDashboard)

useAutoRefresh({
  intervalMs: 10000,
  refresh: () => loadDashboard(true)
})
</script>

<style scoped>
.dashboard-alerts {
  display: grid;
  gap: 12px;
  margin-bottom: 16px;
}

.dashboard-alert {
  border-radius: 10px;
}

.dashboard-alert--actionable {
  border-left: 4px solid #d97706;
}

.dashboard-alert__body {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.dashboard-alert__copy {
  display: grid;
  flex: 1 1 auto;
  gap: 8px;
  min-width: 0;
}

.dashboard-alert__body span,
.dashboard-alert__object strong {
  min-width: 0;
  line-height: 1.55;
  overflow-wrap: anywhere;
}

.dashboard-alert__body .el-button {
  flex: 0 0 auto;
}

.dashboard-alert__objects {
  display: grid;
  gap: 8px;
}

.dashboard-alert__object {
  display: grid;
  gap: 2px;
  min-width: 0;
  border: 1px solid #f4d37a;
  border-radius: 8px;
  background: #fffaf0;
  padding: 8px 10px;
}

.dashboard-alert__object strong {
  color: #7c2d12;
}

.dashboard-alert__more {
  font-size: 12px;
}

.dashboard-summary-grid {
  margin-bottom: 16px;
}

.dashboard-summary-hint {
  margin-top: 8px;
}

.dashboard-status-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 14px;
}

.dashboard-status-card {
  border: 1px solid #dbe3ee;
  border-radius: 10px;
  padding: 14px;
  background: linear-gradient(180deg, #ffffff 0%, #f8fbff 100%);
}

.dashboard-status-card__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.dashboard-status-card__summary {
  margin-top: 10px;
  font-size: 16px;
  font-weight: 600;
  color: #18212f;
}

.dashboard-status-card__detail {
  margin-top: 8px;
  min-height: 42px;
  line-height: 1.5;
}

.dashboard-status-card__time {
  margin-top: 10px;
  font-size: 12px;
}

.dashboard-business-grid,
.dashboard-activity-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.dashboard-business-card {
  margin-bottom: 0;
}

.dashboard-kv-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.dashboard-kv {
  border: 1px solid #dbe3ee;
  border-radius: 8px;
  background: #fbfdff;
  padding: 12px;
}

.dashboard-kv span {
  display: block;
  color: #5f6f84;
  font-size: 13px;
}

.dashboard-kv strong {
  display: block;
  margin-top: 8px;
  font-size: 18px;
  color: #18212f;
}

.dashboard-message-text {
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

@media (max-width: 1100px) {
  .dashboard-business-grid,
  .dashboard-activity-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 768px) {
  .dashboard-kv-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .dashboard-alert__body {
    flex-direction: column;
    gap: 8px;
  }
}
</style>
