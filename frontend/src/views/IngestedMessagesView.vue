<template>
  <h1 class="page-title">消息库</h1>

  <div class="panel feedback-governance-panel">
    <div class="section-head">
      <h2>反馈治理</h2>
      <div class="toolbar compact-toolbar">
        <el-button :loading="feedbackGovernanceLoading" @click="loadFeedbackGovernance">刷新</el-button>
        <el-button :loading="running.createFeedbackExport" @click="createFeedbackExport">创建导出</el-button>
        <el-button type="primary" :loading="running.recomputeSourceTrust" @click="recomputeSourceTrust">重算</el-button>
        <el-button type="warning" :loading="running.applySourceTrustGovernance" @click="applySourceTrustGovernance">应用治理</el-button>
      </div>
    </div>
    <div class="feedback-evaluation-strip">
      <div class="feedback-metric">
        <span>样本</span>
        <strong>{{ feedbackEvaluation?.sampleCount || 0 }}</strong>
      </div>
      <div class="feedback-metric">
        <span>验证集</span>
        <strong>{{ feedbackEvaluation?.validationCount || 0 }}</strong>
      </div>
      <div class="feedback-metric">
        <span>一致率</span>
        <strong>{{ formatPercent(feedbackEvaluation?.agreementRate) }}</strong>
      </div>
      <div class="feedback-metric">
        <span>噪声率</span>
        <strong>{{ formatPercent(feedbackEvaluation?.noiseRate) }}</strong>
      </div>
      <div class="feedback-metric feedback-metric--action">
        <span>{{ evaluationStatusLabel(feedbackEvaluation?.status) }}</span>
        <el-button size="small" :loading="running.createFeedbackSnapshot" @click="createFeedbackSnapshot">创建快照</el-button>
      </div>
    </div>
    <div class="feedback-governance-grid">
      <div>
        <div class="table-title">来源可信</div>
        <el-table v-loading="feedbackGovernanceLoading" :data="sourceTrustRows" size="small" empty-text="暂无反馈">
          <el-table-column prop="sourceRef" label="来源" min-width="180" show-overflow-tooltip />
          <el-table-column prop="feedbackCount" label="样本" width="90" />
          <el-table-column label="评分" width="100">
            <template #default="{ row }">
              <el-progress :percentage="Number(row.trustScore || 0)" :stroke-width="8" :show-text="false" />
            </template>
          </el-table-column>
          <el-table-column label="状态" width="140">
            <template #default="{ row }">
              <el-tag :type="trustStatusType(row.status)" effect="plain">{{ sourceTrustStatusLabel(row.status) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="动作" min-width="180" show-overflow-tooltip>
            <template #default="{ row }">{{ (row.recommendedActions || []).join(', ') }}</template>
          </el-table-column>
          <el-table-column label="自动策略" min-width="170" show-overflow-tooltip>
            <template #default="{ row }">{{ sourceTrustAutoActionLabel(row.autoAction) }}</template>
          </el-table-column>
        </el-table>
      </div>
      <div>
        <div class="table-title">训练样本</div>
        <el-table v-loading="feedbackGovernanceLoading" :data="feedbackSamples" size="small" empty-text="暂无样本">
          <el-table-column prop="sourceRef" label="来源" width="150" show-overflow-tooltip />
          <el-table-column prop="text" label="内容" min-width="220" show-overflow-tooltip />
          <el-table-column label="标签" width="110">
            <template #default="{ row }">{{ feedbackLabel(row.feedbackLabel) }}</template>
          </el-table-column>
          <el-table-column prop="trainingUse" label="用途" width="170" show-overflow-tooltip />
          <el-table-column prop="split" label="集合" width="90" />
        </el-table>
      </div>
      <div>
        <div class="table-title">训练快照</div>
        <el-table v-loading="feedbackGovernanceLoading" :data="feedbackSnapshots" size="small" empty-text="暂无快照">
          <el-table-column prop="version" label="版本" min-width="190" show-overflow-tooltip />
          <el-table-column label="样本" width="90">
            <template #default="{ row }">{{ row.sampleCount || 0 }}</template>
          </el-table-column>
          <el-table-column label="来源" width="90">
            <template #default="{ row }">{{ row.sourceCount || 0 }}</template>
          </el-table-column>
          <el-table-column label="一致率" width="100">
            <template #default="{ row }">{{ formatPercent(row.evaluation?.agreementRate) }}</template>
          </el-table-column>
          <el-table-column prop="fingerprint" label="指纹" min-width="150" show-overflow-tooltip />
        </el-table>
      </div>
      <div>
        <div class="table-title">训练导出</div>
        <el-table v-loading="feedbackGovernanceLoading" :data="feedbackExports" size="small" empty-text="暂无导出">
          <el-table-column prop="version" label="版本" min-width="190" show-overflow-tooltip />
          <el-table-column label="行数" width="80">
            <template #default="{ row }">{{ row.lineCount || 0 }}</template>
          </el-table-column>
          <el-table-column label="大小" width="90">
            <template #default="{ row }">{{ formatBytes(row.byteCount) }}</template>
          </el-table-column>
          <el-table-column prop="contentSha256" label="SHA256" min-width="150" show-overflow-tooltip />
          <el-table-column label="操作" width="90">
            <template #default="{ row }">
              <el-button link type="primary" :loading="running[`downloadFeedbackExport:${row.id}`]" @click="downloadFeedbackExport(row)">下载</el-button>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </div>
  </div>

  <div class="panel">
    <div class="section-head">
      <h2>已采集消息</h2>
      <div class="toolbar compact-toolbar">
        <el-dropdown trigger="click" :disabled="selectedMessageIds.length === 0" @command="batchFeedbackCommand">
          <el-button :disabled="selectedMessageIds.length === 0" :loading="running.batchFeedback">批量反馈 {{ selectedMessageIds.length || '' }}</el-button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="helpful">有用</el-dropdown-item>
              <el-dropdown-item command="noise">噪声</el-dropdown-item>
              <el-dropdown-item command="misclassified">误判</el-dropdown-item>
              <el-dropdown-item command="neutral">中性</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-button :loading="refiltering" @click="refilterUnfiltered">重过滤未过滤消息</el-button>
        <el-button type="primary" @click="openCreate">补录消息</el-button>
      </div>
    </div>
    <div class="toolbar">
      <el-input v-model="query.q" clearable placeholder="搜索订阅源或消息内容" @keyup.enter="loadMessages()" />
      <el-select v-model="query.subscriptionId" clearable placeholder="订阅源" style="width: 200px">
        <el-option v-for="item in subscriptions" :key="item.id" :label="item.title" :value="item.id" />
      </el-select>
      <el-select v-model="query.researchTeamId" clearable placeholder="投研团队" style="width: 200px">
        <el-option v-for="team in teams" :key="team.id" :label="team.name" :value="team.id" />
      </el-select>
      <el-select v-model="query.filterDecision" clearable placeholder="过滤结果" style="width: 180px">
        <el-option label="未过滤" value="unfiltered" />
        <el-option label="忽略" value="ignore" />
        <el-option label="观察" value="observe" />
        <el-option label="触发会议" value="meeting" />
      </el-select>
      <el-button :loading="loading" @click="loadMessages()">查询</el-button>
    </div>

    <el-alert
      v-if="historyMode"
      class="history-mode-alert"
      type="info"
      :closable="false"
      show-icon
      title="正在查看历史消息，自动刷新已暂停。"
    >
      <template #default>
        <el-button link type="primary" @click="returnToLatest">回到最新</el-button>
      </template>
    </el-alert>

    <div v-if="isMobile" v-loading="loading" class="mobile-card-list">
      <div v-for="message in messages" :key="message.id" class="mobile-card">
        <div class="mobile-card__header">
          <div>
            <h3 class="mobile-card__title">{{ message.subscriptionTitle || '未知订阅源' }}</h3>
            <div class="muted">{{ formatDateTimeUtc8(message.messageTime) }}</div>
          </div>
          <el-tag :type="decisionType(message)">{{ decisionLabel(message) }}</el-tag>
        </div>
        <div class="mobile-card__meta">
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">内容</span>
            <span class="mobile-card__content">{{ message.text }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">原因</span>
            <span>{{ message.filterReason || '-' }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">反馈</span>
            <el-tag :type="feedbackType(message.feedbackLabel)" effect="plain">{{ feedbackLabel(message.feedbackLabel) }}</el-tag>
          </div>
        </div>
        <div class="mobile-card__actions">
          <el-button plain :loading="running[`refilter:${message.id}`]" @click="refilterOne(message)">重过滤</el-button>
          <el-dropdown trigger="click" @command="feedbackCommand(message, $event)">
            <el-button plain :loading="feedbackRunning(message.id)">反馈</el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="helpful">有用</el-dropdown-item>
                <el-dropdown-item command="noise">噪声</el-dropdown-item>
                <el-dropdown-item command="misclassified">误判</el-dropdown-item>
                <el-dropdown-item command="neutral">中性</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
          <el-popconfirm title="删除这条消息？" @confirm="deleteMessage(message.id)">
            <template #reference><el-button plain type="danger">删除</el-button></template>
          </el-popconfirm>
        </div>
      </div>
      <el-empty v-if="!messages.length" description="暂无消息" />
    </div>

    <el-table v-else v-loading="loading" :data="messages" row-key="id" empty-text="暂无消息" @selection-change="handleSelectionChange">
      <el-table-column type="selection" width="48" />
      <el-table-column label="时间" width="190">
        <template #default="{ row }">{{ formatDateTimeUtc8(row.messageTime) }}</template>
      </el-table-column>
      <el-table-column prop="subscriptionTitle" label="订阅源" width="160" />
      <el-table-column prop="text" label="内容" min-width="320" show-overflow-tooltip />
      <el-table-column label="过滤" width="120">
        <template #default="{ row }">
          <el-tag :type="decisionType(row)">{{ decisionLabel(row) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="filterReason" label="原因" min-width="220" show-overflow-tooltip />
      <el-table-column label="反馈" width="110">
        <template #default="{ row }">
          <el-tag :type="feedbackType(row.feedbackLabel)" effect="plain">{{ feedbackLabel(row.feedbackLabel) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="230">
        <template #default="{ row }">
          <el-button link type="primary" :loading="running[`refilter:${row.id}`]" @click="refilterOne(row)">重过滤</el-button>
          <el-dropdown trigger="click" @command="feedbackCommand(row, $event)">
            <el-button link type="primary" :loading="feedbackRunning(row.id)">反馈</el-button>
            <template #dropdown>
              <el-dropdown-menu>
                <el-dropdown-item command="helpful">有用</el-dropdown-item>
                <el-dropdown-item command="noise">噪声</el-dropdown-item>
                <el-dropdown-item command="misclassified">误判</el-dropdown-item>
                <el-dropdown-item command="neutral">中性</el-dropdown-item>
              </el-dropdown-menu>
            </template>
          </el-dropdown>
          <el-popconfirm title="删除这条消息？" @confirm="deleteMessage(row.id)">
            <template #reference>
              <el-button link type="danger">删除</el-button>
            </template>
          </el-popconfirm>
        </template>
      </el-table-column>
    </el-table>

    <div class="cursor-pagination-footer">
      <span class="muted">已加载 {{ messages.length }} 条</span>
      <el-button v-if="nextCursor" plain :loading="loadingMore" @click="loadMore">加载更多</el-button>
    </div>
  </div>

  <el-dialog v-model="dialogVisible" title="补录消息" :width="dialogWidth" :fullscreen="isMobile">
    <el-form :model="form" :label-width="isMobile ? 'auto' : '110px'" :label-position="isMobile ? 'top' : 'right'">
      <el-form-item label="订阅源">
        <el-select v-model="form.subscriptionId" style="width: 100%">
          <el-option v-for="item in subscriptions" :key="item.id" :label="item.title" :value="item.id" />
        </el-select>
      </el-form-item>
      <el-form-item label="消息时间">
        <el-date-picker v-model="form.messageTime" type="datetime" value-format="YYYY-MM-DD HH:mm:ss" style="width: 100%" />
      </el-form-item>
      <el-form-item label="内容">
        <el-input v-model="form.text" type="textarea" :rows="6" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="dialogVisible = false">取消</el-button>
      <el-button type="primary" :loading="running.saveMessage" @click="saveMessage">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { computed, onMounted, reactive, ref } from 'vue'
import {
  api,
  apiErrorText,
  jsonapiResource,
  unwrapJsonApiCollection,
  unwrapJsonApiResource,
  type IngestedMessage,
  type MessageFeedbackEvaluation,
  type MessageFeedbackTrainingExport,
  type MessageFeedbackTrainingSnapshot,
  type MessageFeedbackTrainingSample,
  type MessageSourceTrust,
  type MessageSubscription
} from '../api'
import { useAutoRefresh } from '../composables/useAutoRefresh'
import { useAsyncAction } from '../composables/useAsyncAction'
import { nextCursorFromDocument, useCursorPagination } from '../composables/useCursorPagination'
import { useResponsive } from '../composables/useResponsive'
import { formatDateTimeUtc8 } from '../utils/datetime'

const subscriptions = ref<MessageSubscription[]>([])
const teams = ref<any[]>([])
const sourceTrustRows = ref<MessageSourceTrust[]>([])
const feedbackSamples = ref<MessageFeedbackTrainingSample[]>([])
const feedbackEvaluation = ref<MessageFeedbackEvaluation | null>(null)
const feedbackSnapshots = ref<MessageFeedbackTrainingSnapshot[]>([])
const feedbackExports = ref<MessageFeedbackTrainingExport[]>([])
const refiltering = ref(false)
const feedbackGovernanceLoading = ref(false)
const selectedMessageIds = ref<number[]>([])
const dialogVisible = ref(false)
const historyMode = ref(false)
const { isMobile, isTablet } = useResponsive()
const { running, runAction } = useAsyncAction()
const query = reactive({ q: '', subscriptionId: undefined as number | undefined, researchTeamId: undefined as number | undefined, filterDecision: '' })
const form = reactive({ subscriptionId: undefined as number | undefined, messageTime: '', text: '' })
const dialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '720px'))
const autoRefreshActive = computed(() => !historyMode.value)
type FeedbackLabel = 'helpful' | 'noise' | 'misclassified' | 'neutral'

const {
  items: messages,
  nextCursor,
  loading,
  loadingMore,
  loadFirstPage,
  refreshFirstPage,
  loadMore: loadMorePage
} = useCursorPagination<IngestedMessage>(fetchMessagePage)

function decisionType(message: IngestedMessage) {
  if (message.filterStatus === 'filtering') return 'warning'
  if (message.filterStatus === 'failed') return 'danger'
  if (message.filterStatus === 'unfiltered') return 'info'
  if (message.filterDecision === 'meeting') return 'danger'
  if (message.filterDecision === 'observe') return 'warning'
  return 'info'
}

function decisionLabel(message: IngestedMessage) {
  if (message.filterStatus === 'filtering') return '\u8fc7\u6ee4\u4e2d'
  if (message.filterStatus === 'failed') return '\u8fc7\u6ee4\u5931\u8d25'
  if (message.filterStatus === 'unfiltered') return '\u672a\u8fc7\u6ee4'
  return ({
    ignore: '\u5ffd\u7565',
    observe: '\u89c2\u5bdf',
    meeting: '\u89e6\u53d1\u4f1a\u8bae'
  } as Record<string, string>)[String(message.filterDecision || '')] || '\u672a\u8fc7\u6ee4'
}

function feedbackLabel(value: string | null | undefined) {
  return ({
    helpful: '有用',
    noise: '噪声',
    misclassified: '误判',
    neutral: '中性'
  } as Record<string, string>)[String(value || '')] || '未反馈'
}

function feedbackType(value: string | null | undefined) {
  return ({
    helpful: 'success',
    noise: 'danger',
    misclassified: 'warning',
    neutral: 'info'
  } as Record<string, 'success' | 'danger' | 'warning' | 'info'>)[String(value || '')] || 'info'
}

function trustStatusType(value: string | null | undefined) {
  return ({
    trusted: 'success',
    watch: 'warning',
    low_confidence: 'danger',
    insufficient_feedback: 'info'
  } as Record<string, 'success' | 'danger' | 'warning' | 'info'>)[String(value || '')] || 'info'
}

function sourceTrustStatusLabel(value: string | null | undefined) {
  return ({
    trusted: '可信',
    watch: '观察',
    low_confidence: '低置信',
    insufficient_feedback: '样本不足'
  } as Record<string, string>)[String(value || '')] || '未知'
}

function sourceTrustAutoActionLabel(value: string | null | undefined) {
  return ({
    downrank_meeting_to_observe: '会议降为观察',
    downrank_meeting_to_observe_and_observe_to_ignore: '会议/观察降权',
    downrank_observe_to_ignore: '观察降为忽略'
  } as Record<string, string>)[String(value || '')] || '不自动干预'
}

function evaluationStatusLabel(value: string | null | undefined) {
  return ({
    empty: '暂无样本',
    needs_more_feedback: '需要更多反馈',
    healthy: '健康',
    noisy: '噪声偏高',
    needs_review: '需要复核'
  } as Record<string, string>)[String(value || '')] || '未知'
}

function formatPercent(value: number | string | null | undefined) {
  const numberValue = Number(value || 0)
  if (!Number.isFinite(numberValue)) return '0%'
  return `${Math.round(numberValue * 100)}%`
}

function formatBytes(value: number | string | null | undefined) {
  const bytes = Number(value || 0)
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  if (bytes < 1024) return `${Math.round(bytes)} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`
}

function feedbackRunning(id: number) {
  return Boolean(['helpful', 'noise', 'misclassified', 'neutral'].some((label) => running[`feedback:${id}:${label}`]))
}

async function loadSubscriptions() {
  const { data } = await api.get('/message-subscriptions')
  subscriptions.value = unwrapJsonApiCollection(data)
}

async function loadTeams() {
  teams.value = unwrapJsonApiCollection((await api.get('/research-teams')).data)
}

async function loadFeedbackGovernance() {
  feedbackGovernanceLoading.value = true
  try {
    const [trustResponse, samplesResponse, evaluationResponse, snapshotsResponse, exportsResponse] = await Promise.all([
      api.get('/message-feedback/source-trust', { params: { 'page[limit]': 20 } }),
      api.get('/message-feedback/training-samples', { params: { 'page[limit]': 20 } }),
      api.get('/message-feedback/evaluation'),
      api.get('/message-feedback/training-snapshots', { params: { 'page[limit]': 5 } }),
      api.get('/message-feedback/training-exports', { params: { 'page[limit]': 5 } })
    ])
    sourceTrustRows.value = unwrapJsonApiCollection<MessageSourceTrust>(trustResponse.data)
    feedbackSamples.value = unwrapJsonApiCollection<MessageFeedbackTrainingSample>(samplesResponse.data)
    feedbackEvaluation.value = unwrapJsonApiResource<MessageFeedbackEvaluation>(evaluationResponse.data)
    feedbackSnapshots.value = unwrapJsonApiCollection<MessageFeedbackTrainingSnapshot>(snapshotsResponse.data)
    feedbackExports.value = unwrapJsonApiCollection<MessageFeedbackTrainingExport>(exportsResponse.data)
  } catch (error) {
    ElMessage.error(apiErrorText(error))
  } finally {
    feedbackGovernanceLoading.value = false
  }
}

async function recomputeSourceTrust() {
  await runAction('recomputeSourceTrust', async () => {
    await api.post('/message-feedback/source-trust/recompute')
    await loadFeedbackGovernance()
  }, { success: '来源可信已重算' })
}

function sourceTrustGovernanceMessage(result: any) {
  const skipped = Array.isArray(result?.skipped) ? result.skipped.length : 0
  return `检查 ${result?.checked ?? 0}，更新 ${result?.updated ?? 0}，暂停 ${result?.paused ?? 0}${skipped ? `，跳过 ${skipped}` : ''}`
}

async function applySourceTrustGovernance() {
  await runAction('applySourceTrustGovernance', async () => {
    const { data } = await api.post('/message-subscriptions/maintenance', jsonapiResource('message-subscription-maintenance-results', { action: 'apply_source_trust_governance' }, 'current'))
    const result = unwrapJsonApiResource<any>(data)
    await loadFeedbackGovernance()
    ElMessage.success(sourceTrustGovernanceMessage(result))
  })
}

async function createFeedbackSnapshot() {
  await runAction('createFeedbackSnapshot', async () => {
    await api.post('/message-feedback/training-snapshots')
    await loadFeedbackGovernance()
  }, { success: '训练快照已创建' })
}

async function createFeedbackExport() {
  await runAction('createFeedbackExport', async () => {
    const { data } = await api.post('/message-feedback/training-exports')
    const created = unwrapJsonApiResource<MessageFeedbackTrainingExport>(data)
    await loadFeedbackGovernance()
    if (created) saveFeedbackExportFile(created)
  }, { success: '训练导出已创建' })
}

async function downloadFeedbackExport(row: MessageFeedbackTrainingExport) {
  await runAction(`downloadFeedbackExport:${row.id}`, async () => {
    const { data } = await api.get(`/message-feedback/training-exports/${row.id}`)
    const exportRow = unwrapJsonApiResource<MessageFeedbackTrainingExport>(data)
    if (!exportRow?.content) {
      ElMessage.warning('导出内容为空')
      return
    }
    saveFeedbackExportFile(exportRow)
  })
}

function saveFeedbackExportFile(row: MessageFeedbackTrainingExport) {
  const content = String(row.content || '')
  const blob = new Blob([content], { type: row.contentType || 'application/x-ndjson' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = `${String(row.version || 'message-feedback-training-export').replace(/[^a-zA-Z0-9_.-]/g, '_')}.jsonl`
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

async function fetchMessagePage(cursor?: string) {
  const { data } = await api.get('/ingested-messages', {
    params: {
      q: query.q || undefined,
      subscriptionId: query.subscriptionId,
      researchTeamId: query.researchTeamId,
      filterDecision: query.filterDecision || undefined,
      'page[limit]': 100,
      'page[cursor]': cursor || undefined
    }
  })
  return {
    items: unwrapJsonApiCollection<IngestedMessage>(data),
    nextCursor: nextCursorFromDocument(data)
  }
}

async function loadMessages(background = false) {
  if (background && historyMode.value) return
  if (background) {
    await refreshFirstPage()
    return
  }
  historyMode.value = false
  selectedMessageIds.value = []
  await loadFirstPage()
}

async function loadMore() {
  await loadMorePage()
  historyMode.value = true
}

async function returnToLatest() {
  historyMode.value = false
  await loadMessages()
}

function openCreate() {
  const now = new Date()
  const pad = (value: number) => String(value).padStart(2, '0')
  Object.assign(form, {
    subscriptionId: subscriptions.value[0]?.id,
    messageTime: `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())} ${pad(now.getHours())}:${pad(now.getMinutes())}:${pad(now.getSeconds())}`,
    text: ''
  })
  dialogVisible.value = true
}

async function saveMessage() {
  if (!form.subscriptionId || !form.text.trim()) {
    ElMessage.warning('订阅源和内容都必须填写')
    return
  }
  await runAction('saveMessage', async () => {
    await api.post('/ingested-messages', jsonapiResource('ingested-messages', form))
    dialogVisible.value = false
    await loadMessages()
  }, { success: '消息已补录' })
}

async function refilterOne(row: IngestedMessage) {
  await runAction(`refilter:${row.id}`, async () => {
    await api.post(`/ingested-messages/${row.id}/filter`)
    await loadMessages()
  }, { success: '消息已重过滤' })
}

async function feedbackMessage(row: IngestedMessage, label: FeedbackLabel) {
  await runAction(`feedback:${row.id}:${label}`, async () => {
    await api.post(`/ingested-messages/${row.id}/feedback`, jsonapiResource('ingested-message-feedbacks', { label }))
    ElMessage.success('反馈已记录')
    await loadMessages(true)
    await loadFeedbackGovernance()
  })
}

function feedbackCommand(row: IngestedMessage, label: unknown) {
  const value = String(label || '') as FeedbackLabel
  if (!['helpful', 'noise', 'misclassified', 'neutral'].includes(value)) return
  void feedbackMessage(row, value)
}

function handleSelectionChange(rows: IngestedMessage[]) {
  selectedMessageIds.value = rows.map((row) => row.id)
}

async function batchFeedback(label: FeedbackLabel) {
  const messageIds = [...selectedMessageIds.value]
  if (!messageIds.length) {
    ElMessage.warning('请先选择消息')
    return
  }
  await runAction('batchFeedback', async () => {
    const { data } = await api.post('/ingested-messages/feedback/batch', jsonapiResource('ingested-message-feedback-batches', { messageIds, label }))
    const result = unwrapJsonApiResource<any>(data) || {}
    selectedMessageIds.value = []
    await loadMessages(true)
    await loadFeedbackGovernance()
    ElMessage.success(`已更新 ${result.updatedCount || 0} 条反馈`)
  })
}

function batchFeedbackCommand(label: unknown) {
  const value = String(label || '') as FeedbackLabel
  if (!['helpful', 'noise', 'misclassified', 'neutral'].includes(value)) return
  void batchFeedback(value)
}

async function refilterUnfiltered() {
  refiltering.value = true
  try {
    await api.post('/ingested-messages/refilter', jsonapiResource('ingested-message-refilters', { onlyUnfiltered: true, limit: 100 }))
    await loadMessages()
    ElMessage.success('未过滤消息已重过滤')
  } catch (error) {
    ElMessage.error(apiErrorText(error))
  } finally {
    refiltering.value = false
  }
}

async function deleteMessage(id: number) {
  await runAction(`deleteMessage:${id}`, async () => {
    await api.delete(`/ingested-messages/${id}`)
    await loadMessages()
  }, { success: '消息已删除' })
}

onMounted(async () => {
  await Promise.all([loadSubscriptions(), loadTeams(), loadFeedbackGovernance()])
  await loadMessages()
})

useAutoRefresh({
  enabled: autoRefreshActive,
  intervalMs: 3000,
  refresh: () => loadMessages(true)
})
</script>

<style scoped>
.feedback-evaluation-strip {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 12px;
  margin-bottom: 16px;
}

.feedback-metric {
  min-width: 0;
  padding: 10px 0;
  border-bottom: 1px solid var(--el-border-color-lighter);
}

.feedback-metric span {
  display: block;
  color: var(--el-text-color-secondary);
  font-size: 12px;
}

.feedback-metric strong {
  display: block;
  margin-top: 4px;
  color: var(--el-text-color-primary);
  font-size: 18px;
  line-height: 1.2;
  overflow-wrap: anywhere;
}

.feedback-metric--action {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.feedback-metric--action strong {
  font-size: 14px;
}

.feedback-governance-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 16px;
}

.table-title {
  margin-bottom: 8px;
  color: var(--el-text-color-secondary);
  font-size: 13px;
  font-weight: 600;
}

@media (max-width: 960px) {
  .feedback-evaluation-strip {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .feedback-governance-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 560px) {
  .feedback-evaluation-strip {
    grid-template-columns: 1fr;
  }
}
</style>
