<template>
  <h1 class="page-title">运维状态</h1>

  <div class="section-head">
    <div class="muted">Provider 健康、后台任务心跳与备份恢复状态</div>
    <el-button :loading="loading" @click="load">刷新</el-button>
  </div>

  <div class="panel">
    <div class="section-head">
      <h2>Provider Health</h2>
      <div class="muted">检查时间：{{ formatDateTimeUtc8(providerHealth?.generatedAt) }}</div>
    </div>

    <div class="ops-provider-grid">
      <div class="ops-card">
        <div class="ops-card__head">
          <strong>AI 服务商</strong>
          <el-tag :type="providerCount.ready > 0 ? 'success' : 'warning'" effect="plain">
            {{ providerCount.ready }} / {{ providerCount.total }} 可用
          </el-tag>
        </div>
        <div class="ops-provider-list">
          <div v-for="item in aiProviders" :key="String(item.id || item.name)" class="ops-provider-row">
            <span>{{ item.name || item.id || '-' }}</span>
            <el-tag :type="statusTagType(String(item.status || (item.ready ? 'ok' : 'warning')))" effect="plain">
              {{ statusLabel(String(item.status || (item.ready ? 'ok' : 'warning'))) }}
            </el-tag>
          </div>
          <el-empty v-if="!aiProviders.length" description="暂无 AI 服务商" />
        </div>
        <div class="ops-kv">
          <span>24h 调用</span>
          <strong>{{ formatNumber(aiUsage.modelCalls24h) }}</strong>
          <span>24h Token</span>
          <strong>{{ formatNumber(aiUsage.totalTokens24h) }}</strong>
          <span>累计 Token</span>
          <strong>{{ formatNumber(aiUsage.totalTokensTotal) }}</strong>
          <span>成本来源</span>
          <strong>{{ costSourceLabel(String(aiUsage.costAmountSource || 'not_configured')) }}</strong>
          <span>24h 成本</span>
          <strong>{{ formatCost(aiUsage.costAmount24h, String(aiUsage.costCurrency || '')) }}</strong>
          <span>成本预算</span>
          <strong>
            <el-tag :type="costBudgetTagType(String(aiUsage.costBudgetStatus || 'not_configured'))" effect="plain">
              {{ costBudgetLabel(String(aiUsage.costBudgetStatus || 'not_configured')) }}
            </el-tag>
          </strong>
          <span>预算用量</span>
          <strong>{{ formatBudgetUsed(aiUsage.costBudgetUsedPct) }}</strong>
        </div>
      </div>

      <div class="ops-card">
        <div class="ops-card__head">
          <strong>会议运行</strong>
          <el-tag :type="statusTagType(String(meetingRuntime.status || 'empty'))" effect="plain">
            {{ statusLabel(String(meetingRuntime.status || 'empty')) }}
          </el-tag>
        </div>
        <p>{{ meetingRuntime.summary || '-' }}</p>
        <div class="ops-kv">
          <span>24h 会议</span>
          <strong>{{ formatNumber(meetingRuntime.meetingCount24h) }}</strong>
          <span>24h 失败</span>
          <strong>{{ formatNumber(meetingRuntime.failedCount24h) }}</strong>
          <span>排队均值</span>
          <strong>{{ formatDurationSeconds(meetingRuntime.queueWaitAvgSeconds) }}</strong>
          <span>运行 P95</span>
          <strong>{{ formatDurationSeconds(meetingRuntime.runP95Seconds) }}</strong>
        </div>
      </div>

      <div class="ops-card">
        <div class="ops-card__head">
          <strong>行情源</strong>
          <el-tag effect="plain">{{ marketProvider.defaultProvider || '-' }}</el-tag>
        </div>
        <div class="ops-kv">
          <span>实时源</span>
          <strong>{{ marketProvider.realtimeProvider || '-' }}</strong>
          <span>缓存 TTL</span>
          <strong>{{ marketProvider.realtimeCacheTtlSeconds ?? '-' }}s</strong>
          <span>最新报价源</span>
          <strong>{{ marketProvider.lastQuoteProvider || '-' }}</strong>
        </div>
      </div>

      <div class="ops-card">
        <div class="ops-card__head">
          <strong>消息接入</strong>
          <el-tag :type="messageListenerTag" effect="plain">{{ messageListenerStatus }}</el-tag>
        </div>
        <div class="ops-kv">
          <span>启用订阅</span>
          <strong>{{ messagingSubscriptions.enabled ?? '-' }}</strong>
          <span>订阅总数</span>
          <strong>{{ messagingSubscriptions.total ?? '-' }}</strong>
          <span>未过滤消息</span>
          <strong>{{ messagingSubscriptions.unfilteredMessages ?? '-' }}</strong>
        </div>
      </div>
    </div>
    <div class="table-scroll ops-ai-cost-table">
      <el-table :data="aiUsageRows" empty-text="暂无 AI 用量明细" size="small">
        <el-table-column prop="providerName" label="Provider" min-width="140" />
        <el-table-column prop="model" label="模型" min-width="160" />
        <el-table-column label="24h 调用" width="100">
          <template #default="{ row }">{{ formatNumber(row.modelCalls24h) }}</template>
        </el-table-column>
        <el-table-column label="24h Token" width="120">
          <template #default="{ row }">{{ formatNumber(row.totalTokens24h) }}</template>
        </el-table-column>
        <el-table-column label="24h 成本" width="130">
          <template #default="{ row }">{{ formatCost(row.costAmount24h, String(aiUsage.costCurrency || '')) }}</template>
        </el-table-column>
        <el-table-column label="来源" width="130">
          <template #default="{ row }">{{ costSourceLabel(String(row.costAmountSource || 'not_configured')) }}</template>
        </el-table-column>
        <el-table-column label="缺价格" width="90">
          <template #default="{ row }">{{ formatNumber(row.costMissingCalls24h) }}</template>
        </el-table-column>
      </el-table>
    </div>
  </div>

  <div class="panel">
    <div class="section-head">
      <h2>后台任务</h2>
      <div class="toolbar compact-toolbar">
        <el-tag :type="riskTagType(String(backlogRisk.level || 'ok'))" effect="plain">
          {{ riskLabel(String(backlogRisk.level || 'ok')) }}
        </el-tag>
        <el-tag :type="recentErrors.status === 'warning' ? 'warning' : 'success'" effect="plain">
          {{ recentErrors.count ?? 0 }} 个近期错误
        </el-tag>
        <el-button :loading="queueRetrying" :disabled="!retryFailedEnabled" @click="retryFailedJobs">
          重试失败任务
        </el-button>
      </div>
    </div>
    <div class="ops-queue-band">
      <div class="ops-card">
        <div class="ops-card__head">
          <strong>Redis 队列</strong>
          <el-tag :type="statusTagType(String(queue.status || 'disabled'))" effect="plain">
            {{ statusLabel(String(queue.status || 'disabled')) }}
          </el-tag>
        </div>
        <p>{{ queue.summary || '-' }}</p>
        <div class="muted">{{ queue.detail || '-' }}</div>
      </div>
      <div class="ops-kv ops-kv--wide ops-queue-kv">
        <span>队列数</span>
        <strong>{{ queue.queueCount ?? queueRows.length }}</strong>
        <span>总任务</span>
        <strong>{{ queueTotals.size ?? 0 }}</strong>
        <span>等待</span>
        <strong>{{ queueTotals.pending ?? 0 }}</strong>
        <span>执行中</span>
        <strong>{{ queueTotals.active ?? 0 }}</strong>
        <span>重试</span>
        <strong>{{ queueTotals.retry ?? 0 }}</strong>
        <span>归档</span>
        <strong>{{ queueTotals.archived ?? 0 }}</strong>
        <span>今日失败</span>
        <strong>{{ queueTotals.failedToday ?? 0 }}</strong>
        <span>积压风险</span>
        <strong>{{ backlogRisk.reason || '-' }}</strong>
        <span>内存</span>
        <strong>{{ formatBytes(queueTotals.memoryUsageBytes) }}</strong>
      </div>
    </div>
    <div class="table-scroll ops-queue-table">
      <el-table :data="queueRows" empty-text="暂无队列快照" size="small">
        <el-table-column prop="name" label="队列" min-width="120" />
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="statusTagType(row.status || 'disabled')" effect="plain">{{ statusLabel(row.status || 'disabled') }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="size" label="总数" width="80" />
        <el-table-column prop="pending" label="等待" width="80" />
        <el-table-column prop="active" label="执行中" width="90" />
        <el-table-column prop="scheduled" label="计划" width="80" />
        <el-table-column prop="retry" label="重试" width="80" />
        <el-table-column prop="archived" label="归档" width="80" />
        <el-table-column label="延迟" width="100">
          <template #default="{ row }">{{ formatDurationSeconds(row.latencySeconds) }}</template>
        </el-table-column>
        <el-table-column label="内存" width="110">
          <template #default="{ row }">{{ formatBytes(row.memoryUsageBytes) }}</template>
        </el-table-column>
      </el-table>
    </div>
    <div class="ops-filter-row">
      <el-select v-model="failedTaskQueueFilter" placeholder="队列" clearable size="small">
        <el-option v-for="row in queueRows" :key="row.name || 'default'" :label="row.name || 'default'" :value="row.name || 'default'" />
      </el-select>
      <el-input v-model="failedTaskTypeFilter" placeholder="任务类型" clearable size="small" />
      <el-select v-model="failedTaskStateFilter" placeholder="状态" clearable size="small">
        <el-option label="retry" value="retry" />
        <el-option label="archived" value="archived" />
      </el-select>
      <el-input-number v-model="failedTaskLimit" :min="1" :max="50" size="small" controls-position="right" />
      <el-button size="small" @click="load">筛选</el-button>
      <el-button size="small" @click="clearQueueFilters">清空</el-button>
    </div>
    <div class="table-scroll ops-queue-table">
      <el-table :data="failedTasks" empty-text="暂无失败任务样本" size="small">
        <el-table-column prop="queue" label="队列" width="120" />
        <el-table-column prop="type" label="类型" min-width="180" />
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="row.state === 'archived' ? 'danger' : 'warning'" effect="plain">{{ row.state || '-' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="重试" width="90">
          <template #default="{ row }">{{ row.retried ?? 0 }} / {{ row.maxRetry ?? 0 }}</template>
        </el-table-column>
        <el-table-column prop="lastError" label="最近错误" min-width="260" show-overflow-tooltip />
        <el-table-column label="失败时间" width="190">
          <template #default="{ row }">{{ formatDateTimeUtc8(row.lastFailedAt) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="110" fixed="right">
          <template #default="{ row }">
            <el-button
              size="small"
              :disabled="!row.queue || !row.id"
              :loading="taskRunning === `${row.queue}:${row.id}`"
              @click="runFailedTask(row)"
            >
              重试
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
    <div class="table-scroll ops-queue-table">
      <el-table :data="recentErrorRows" empty-text="暂无近期错误" size="small">
        <el-table-column label="时间" width="190">
          <template #default="{ row }">{{ formatDateTimeUtc8(row.time) }}</template>
        </el-table-column>
        <el-table-column prop="group" label="分组" width="110" />
        <el-table-column prop="event" label="事件" min-width="170" />
        <el-table-column prop="message" label="消息" min-width="260" show-overflow-tooltip />
        <el-table-column prop="path" label="路径" min-width="180" show-overflow-tooltip />
        <el-table-column prop="file" label="文件" min-width="160" show-overflow-tooltip />
        <el-table-column label="操作" width="100" fixed="right">
          <template #default="{ row }">
            <el-button size="small" @click="openLogsForError(row)">日志</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
    <div class="ops-status-grid">
      <div v-for="item in jobStatusItems" :key="item.key" class="ops-card">
        <div class="ops-card__head">
          <strong>{{ item.title }}</strong>
          <el-tag :type="statusTagType(item.status)" effect="plain">{{ statusLabel(item.status) }}</el-tag>
        </div>
        <p>{{ item.summary || '-' }}</p>
        <div class="muted">{{ item.detail || '-' }}</div>
      </div>
    </div>
  </div>

  <div class="panel">
    <div class="section-head">
      <h2>备份与恢复</h2>
      <div class="toolbar compact-toolbar">
        <el-tag :type="backups?.status === 'manual' ? 'warning' : 'success'" effect="plain">{{ backupStatusLabel }}</el-tag>
        <el-button type="primary" :loading="backupCreating" @click="createBackup">创建备份</el-button>
      </div>
    </div>
    <div class="ops-backup-grid">
      <div class="ops-kv ops-kv--wide">
        <span>数据库</span>
        <strong>{{ backups?.databaseBackend || '-' }}</strong>
        <span>目标</span>
        <strong>{{ backups?.databaseTarget || '-' }}</strong>
        <span>备份目录</span>
        <strong>{{ backups?.backupDir || '-' }}</strong>
        <span>归档提供方</span>
        <strong>{{ archiveProviderLabel }}</strong>
        <span>外部归档</span>
        <strong>
          <el-tag :type="archiveExternalTagType" effect="plain">{{ archiveExternalStatusLabel }}</el-tag>
        </strong>
        <span v-if="archiveMissingExternalConfig.length">缺失配置</span>
        <strong v-if="archiveMissingExternalConfig.length">{{ archiveMissingExternalConfig.join(' / ') }}</strong>
        <span v-if="archiveExternalHint">归档提示</span>
        <strong v-if="archiveExternalHint">{{ archiveExternalHint }}</strong>
        <span>最新备份</span>
        <strong>{{ backups?.latestBackup?.name || '-' }}</strong>
        <span>保留策略</span>
        <strong>{{ backups?.retentionPolicy || '-' }}</strong>
        <span>保留窗口</span>
        <strong>{{ retentionWindowLabel }}</strong>
        <span>上次清理</span>
        <strong>{{ retentionRunLabel }}</strong>
        <span>恢复演练</span>
        <strong>{{ restoreDrillLabel(backups?.restoreDrill) }}</strong>
      </div>
      <div>
        <div class="muted ops-list-title">支持的备份动作</div>
        <el-tag v-for="item in backups?.supportedActions || []" :key="item" class="tag-gap" effect="plain">
          {{ backupActionLabel(item) }}
        </el-tag>
      </div>
    </div>
    <div class="table-scroll ops-backup-table">
      <el-table :data="backups?.backups || []" empty-text="暂无备份归档">
        <el-table-column prop="name" label="归档" min-width="260" />
        <el-table-column label="大小" width="120">
          <template #default="{ row }">{{ formatBytes(row.sizeBytes) }}</template>
        </el-table-column>
        <el-table-column label="创建时间" width="190">
          <template #default="{ row }">{{ formatDateTimeUtc8(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column prop="path" label="路径" min-width="320" show-overflow-tooltip />
        <el-table-column label="Dry-run" width="120">
          <template #default="{ row }">
            <el-button size="small" :loading="backupDryRunning === row.name" @click="dryRunBackup(row.name)">
              Dry-run
            </el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>
    <div v-if="restoreDryRun" class="ops-dry-run">
      <div class="ops-card__head">
        <strong>{{ restoreDryRun.backupName }}</strong>
        <el-tag :type="restoreDryRun.status === 'valid' ? 'success' : 'warning'" effect="plain">
          {{ restoreDryRun.status }}
        </el-tag>
      </div>
      <div class="ops-kv">
        <span>entries</span>
        <strong>{{ restoreDryRun.entryCount }}</strong>
        <span>database</span>
        <strong>{{ restoreDryRun.databaseEntryCount }}</strong>
        <span>logs</span>
        <strong>{{ restoreDryRun.logEntryCount }}</strong>
        <span>sandbox</span>
        <strong>{{ restoreDryRun.sandboxStatus }}</strong>
        <span>files</span>
        <strong>{{ restoreDryRun.sandboxFileCount }}</strong>
        <span>size</span>
        <strong>{{ formatBytes(restoreDryRun.sandboxSizeBytes) }}</strong>
        <span>path</span>
        <strong>{{ restoreDryRun.sandboxDir }}</strong>
      </div>
      <div v-if="restoreDryRun.sandboxChecks?.length" class="ops-checks">
        <el-tag
          v-for="item in restoreDryRun.sandboxChecks"
          :key="item.name"
          :type="item.status === 'pass' ? 'success' : 'warning'"
          effect="plain"
        >
          {{ item.name }}: {{ item.status }}
        </el-tag>
      </div>
      <div v-if="restoreDryRun.notes?.length" class="muted ops-dry-run__notes">
        {{ restoreDryRun.notes.join(' / ') }}
      </div>
    </div>
  </div>

  <div class="panel">
    <div class="section-head">
      <h2>原始诊断</h2>
      <div class="muted">用于排障和后续接入监控系统</div>
    </div>
    <pre class="ops-json">{{ prettyJSON({ providerHealth, jobs, backups, restoreDryRun }) }}</pre>
  </div>
</template>

<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  api,
  apiErrorText,
  unwrapJsonApiResource,
  type OpsBackups,
  type OpsBackupRun,
  type OpsBackupRestoreDryRun,
  type OpsJobAction,
  type OpsJobs,
  type OpsQueueBacklogRisk,
  type OpsQueueFailedTask,
  type OpsRecentErrorEntry,
  type OpsProviderHealth
} from '../api'
import { formatDateTimeUtc8 } from '../utils/datetime'

const router = useRouter()
const providerHealth = ref<OpsProviderHealth | null>(null)
const jobs = ref<OpsJobs | null>(null)
const backups = ref<OpsBackups | null>(null)
const restoreDryRun = ref<OpsBackupRestoreDryRun | null>(null)
const loading = ref(false)
const backupCreating = ref(false)
const backupDryRunning = ref('')
const queueRetrying = ref(false)
const taskRunning = ref('')
const failedTaskQueueFilter = ref('')
const failedTaskTypeFilter = ref('')
const failedTaskStateFilter = ref('')
const failedTaskLimit = ref(8)

const aiProviders = computed<Record<string, any>[]>(() => listValue(providerHealth.value?.ai?.providers))
const aiUsage = computed<Record<string, any>>(() => objectValue(providerHealth.value?.ai?.usage))
const aiUsageRows = computed<Record<string, any>[]>(() => listValue(aiUsage.value.byProviderModel))
const meetingRuntime = computed<Record<string, any>>(() => objectValue(objectValue(providerHealth.value?.meeting).runtime))
const providerCount = computed(() => ({
  total: aiProviders.value.length,
  ready: aiProviders.value.filter((item) => Boolean(item.ready)).length
}))
const marketProvider = computed<Record<string, any>>(() => objectValue(providerHealth.value?.market?.provider))
const messagingProvider = computed<Record<string, any>>(() => objectValue(providerHealth.value?.messaging?.provider))
const messagingSubscriptions = computed<Record<string, any>>(() => objectValue(messagingProvider.value.subscriptions))
const messageListener = computed<Record<string, any>>(() => objectValue(objectValue(messagingProvider.value.mtproto).status))
const messageListenerStatus = computed(() => statusLabel(String(messageListener.value.status || 'disabled')))
const messageListenerTag = computed(() => statusTagType(String(messageListener.value.status || 'disabled')))
const queue = computed<Record<string, any>>(() => objectValue(jobs.value?.queue))
const queueTotals = computed<Record<string, any>>(() => objectValue(queue.value.totals))
const queueRows = computed<Record<string, any>[]>(() => listValue(queue.value.queues))
const failedTasks = computed<OpsQueueFailedTask[]>(() => listValue(queue.value.failedTasks) as OpsQueueFailedTask[])
const backlogRisk = computed<OpsQueueBacklogRisk>(() => objectValue(queue.value.backlogRisk) as OpsQueueBacklogRisk)
const recentErrors = computed<Record<string, any>>(() => objectValue(jobs.value?.recentErrors))
const recentErrorRows = computed<OpsRecentErrorEntry[]>(() => listValue(recentErrors.value.entries) as OpsRecentErrorEntry[])
const retentionPolicyDetail = computed<Record<string, any>>(() => objectValue(backups.value?.retentionPolicyDetail))
const retentionLastRun = computed<Record<string, any>>(() => objectValue(backups.value?.retentionLastRun))
const archiveProviderDetail = computed<Record<string, any>>(() => objectValue(backups.value?.archiveProviderDetail))
const retryFailedEnabled = computed(() => {
  const actions = objectValue(queue.value.actions)
  const action = objectValue(actions.retryFailed)
  return Boolean(action.enabled)
})

const jobStatusItems = computed(() => [
  statusItem('redis', 'Redis', jobs.value?.redis),
  statusItem('worker', '后台工作进程', jobs.value?.worker),
  statusItem('scheduler', '定时调度器', jobs.value?.scheduler),
  statusItem('messageListener', '消息监听', jobs.value?.messageListener),
  statusItem('paperEngine', '模拟盘引擎', jobs.value?.paperEngine)
])

const backupStatusLabel = computed(() => {
  const map: Record<string, string> = {
    manual: '手动备份',
    ready: '已配置',
    disabled: '未启用'
  }
  return map[backups.value?.status || ''] || backups.value?.status || '-'
})
const archiveProviderLabel = computed(() => {
  const provider = archiveProviderDetail.value
  if (!Object.keys(provider).length) return backups.value?.archiveProvider || '-'
  const key = String(provider.key || backups.value?.archiveProvider || '')
  const root = String(provider.root || backups.value?.backupDir || '')
  const configuredExternal = String(provider.configuredExternalProvider || '')
  const label = key === 'local_filesystem'
    ? '本地文件系统'
    : key === 's3'
      ? 'S3 对象存储'
      : key === 'oss'
        ? 'OSS 对象存储'
        : key || String(provider.kind || '-')
  const suffix = configuredExternal && !provider.external ? ` / ${configuredExternal.toUpperCase()} 诊断` : ''
  return root ? `${label}${suffix} (${root})` : `${label}${suffix}`
})
const archiveMissingExternalConfig = computed(() => {
  const value = archiveProviderDetail.value.missingExternalConfig
  return Array.isArray(value) ? value.map(String).filter(Boolean) : []
})
const archiveExternalHint = computed(() => String(archiveProviderDetail.value.setupHint || ''))
const archiveExternalStatusLabel = computed(() => {
  const provider = archiveProviderDetail.value
  const configuredExternal = String(provider.configuredExternalProvider || '')
  if (!configuredExternal) return '未配置外部归档'
  if (provider.external && provider.externalReady) return `${configuredExternal.toUpperCase()} 已启用`
  if (provider.externalReady) return `${configuredExternal.toUpperCase()} 配置已完整`
  if (archiveMissingExternalConfig.value.length) return `${configuredExternal.toUpperCase()} 缺少配置`
  return `${configuredExternal.toUpperCase()} 待检查`
})
const archiveExternalTagType = computed<'success' | 'warning' | 'danger' | 'info'>(() => {
  const provider = archiveProviderDetail.value
  if (!provider.configuredExternalProvider) return 'info'
  return provider.externalReady ? 'success' : 'warning'
})
const retentionWindowLabel = computed(() => {
  const copies = retentionPolicyDetail.value.recommendedCopies || '-'
  const days = retentionPolicyDetail.value.recommendedDays || '-'
  return `${copies} 份 / ${days} 天`
})
const retentionRunLabel = computed(() => {
  if (!retentionLastRun.value.appliedAt) return '-'
  const prunedBackups = retentionLastRun.value.prunedBackupCount || 0
  const prunedDrills = retentionLastRun.value.prunedSandboxDirCount || 0
  return `${formatDateTimeUtc8(String(retentionLastRun.value.appliedAt))}，清理 ${prunedBackups} 个归档 / ${prunedDrills} 个沙箱`
})

async function load() {
  loading.value = true
  try {
    const jobParams = queueFilterParams()
    const [healthResp, jobsResp, backupsResp] = await Promise.all([
      api.get('/ops/provider-health'),
      api.get('/ops/jobs', { params: jobParams }),
      api.get('/ops/backups')
    ])
    providerHealth.value = unwrapJsonApiResource<OpsProviderHealth>(healthResp.data)
    jobs.value = unwrapJsonApiResource<OpsJobs>(jobsResp.data)
    backups.value = unwrapJsonApiResource<OpsBackups>(backupsResp.data)
  } catch (error) {
    ElMessage.error(apiErrorText(error, '运维状态加载失败'))
  } finally {
    loading.value = false
  }
}

function queueFilterParams() {
  const params: Record<string, string | number> = { failedLimit: failedTaskLimit.value || 8 }
  if (failedTaskQueueFilter.value) params.queue = failedTaskQueueFilter.value
  if (failedTaskTypeFilter.value) params.type = failedTaskTypeFilter.value
  if (failedTaskStateFilter.value) params.state = failedTaskStateFilter.value
  return params
}

async function clearQueueFilters() {
  failedTaskQueueFilter.value = ''
  failedTaskTypeFilter.value = ''
  failedTaskStateFilter.value = ''
  failedTaskLimit.value = 8
  await load()
}

async function createBackup() {
  backupCreating.value = true
  try {
    const { data } = await api.post('/ops/backups')
    const result = unwrapJsonApiResource<OpsBackupRun>(data)
    const retentionRun = objectValue(result?.retentionRun)
    const prunedBackups = Number(retentionRun.prunedBackupCount || 0)
    ElMessage.success(
      result?.backupName
        ? `备份已创建：${result.backupName}${prunedBackups > 0 ? `，已清理 ${prunedBackups} 个旧归档` : ''}`
        : '备份已创建'
    )
    await load()
  } catch (error) {
    ElMessage.error(apiErrorText(error, '备份创建失败'))
  } finally {
    backupCreating.value = false
  }
}

async function dryRunBackup(name: string) {
  if (!name) return
  backupDryRunning.value = name
  try {
    const { data } = await api.post(`/ops/backups/${encodeURIComponent(name)}/restore-dry-run`)
    restoreDryRun.value = unwrapJsonApiResource<OpsBackupRestoreDryRun>(data)
    ElMessage.success(`备份恢复演练已完成：${name}`)
  } catch (error) {
    ElMessage.error(apiErrorText(error, `备份恢复演练失败：${name}`))
  } finally {
    backupDryRunning.value = ''
  }
}

async function retryFailedJobs() {
  try {
    await ElMessageBox.confirm('确认将 retry 和 archived 队列任务重新提交处理？', '重试失败任务', {
      type: 'warning',
      confirmButtonText: '重试',
      cancelButtonText: '取消'
    })
  } catch {
    return
  }
  queueRetrying.value = true
  try {
    const { data } = await api.post('/ops/jobs/retry-failed')
    const result = unwrapJsonApiResource<OpsJobAction>(data)
    ElMessage.success(`已提交 ${result?.totalSubmitted ?? 0} 个失败任务`)
    await load()
  } catch (error) {
    ElMessage.error(apiErrorText(error, '失败任务重试提交失败'))
  } finally {
    queueRetrying.value = false
  }
}

async function runFailedTask(row: OpsQueueFailedTask) {
  if (!row.queue || !row.id) return
  try {
    await ElMessageBox.confirm(`确认重新提交任务 ${row.id}？`, '重试单个任务', {
      type: 'warning',
      confirmButtonText: '重试',
      cancelButtonText: '取消'
    })
  } catch {
    return
  }
  const key = `${row.queue}:${row.id}`
  taskRunning.value = key
  try {
    const { data } = await api.post(`/ops/jobs/${encodeURIComponent(row.queue)}/tasks/${encodeURIComponent(row.id)}/run`)
    const result = unwrapJsonApiResource<OpsJobAction>(data)
    ElMessage.success(result?.taskId ? `任务已重新提交：${result.taskId}` : '任务已重新提交')
    await load()
  } catch (error) {
    ElMessage.error(apiErrorText(error, '单任务重试失败'))
  } finally {
    taskRunning.value = ''
  }
}

function openLogsForError(row: OpsRecentErrorEntry) {
  const query: Record<string, string> = {
    level: 'error',
    includeNoise: 'true'
  }
  if (row.event) query.event = row.event
  if (row.group) query.group = row.group
  if (row.path) query.path = row.path
  if (row.message) query.q = row.message.slice(0, 120)
  void router.push({ path: '/logs', query })
}

function statusItem(key: string, title: string, value: unknown) {
  const item = objectValue(value)
  return {
    key,
    title,
    status: String(item.status || 'disabled'),
    summary: String(item.summary || ''),
    detail: String(item.detail || '')
  }
}

function riskLabel(value: string) {
  const map: Record<string, string> = {
    ok: '队列正常',
    warning: '队列告警',
    danger: '队列高危'
  }
  return map[value] || value || '-'
}

function riskTagType(value: string) {
  const map: Record<string, 'success' | 'warning' | 'danger' | 'info'> = {
    ok: 'success',
    warning: 'warning',
    danger: 'danger'
  }
  return map[value] || 'info'
}

function objectValue(value: unknown): Record<string, any> {
  return value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, any>) : {}
}

function listValue(value: unknown): Record<string, any>[] {
  return Array.isArray(value) ? value.map(objectValue) : []
}

function statusLabel(value: string) {
  const map: Record<string, string> = {
    ok: '正常',
    warning: '警告',
    error: '异常',
    empty: '暂无数据',
    disabled: '未启用',
    skipped: '跳过'
  }
  return map[value] || value || '-'
}

function statusTagType(value: string) {
  const map: Record<string, 'success' | 'warning' | 'danger' | 'info'> = {
    ok: 'success',
    warning: 'warning',
    error: 'danger',
    empty: 'info',
    disabled: 'info',
    skipped: 'info'
  }
  return map[value] || 'info'
}

function backupActionLabel(value: string) {
  const map: Record<string, string> = {
    database_snapshot: '数据库快照',
    runtime_env_export: '运行配置导出',
    logs_archive: '日志归档',
    restore_dry_run: '恢复预检',
    archive_provider_local: '本地归档提供方',
    archive_provider_s3: 'S3 归档提供方',
    archive_provider_oss: 'OSS 归档提供方',
    archive_provider_external_config_check: '外部归档配置检查'
  }
  return map[value] || value
}

function restoreDrillLabel(value: string | null | undefined) {
  const map: Record<string, string> = {
    not_ready: '暂无备份',
    dry_run_available: '可预检',
    drilled: '已演练',
    drill_warning: '演练有警告',
    stale_drill: '演练已过期'
  }
  return map[String(value || '')] || value || '-'
}

function costSourceLabel(value: string) {
  const map: Record<string, string> = {
    provider_payload: 'Provider payload',
    configured_rates: '配置价格',
    mixed: '混合来源',
    not_configured: '未配置',
    provider_payload_or_not_configured: '按事件'
  }
  return map[value] || value || '-'
}

function costBudgetLabel(value: string) {
  const map: Record<string, string> = {
    ok: '正常',
    warning: '接近预算',
    exceeded: '已超预算',
    no_cost_data: '无成本数据',
    not_configured: '未配置'
  }
  return map[value] || value || '-'
}

function costBudgetTagType(value: string) {
  const map: Record<string, 'success' | 'warning' | 'danger' | 'info'> = {
    ok: 'success',
    warning: 'warning',
    exceeded: 'danger',
    no_cost_data: 'info',
    not_configured: 'info'
  }
  return map[value] || 'info'
}

function formatCost(value: number | string | null | undefined, currency: string) {
  if (value == null || value === '') return '-'
  const number = Number(value)
  if (!Number.isFinite(number)) return '-'
  const suffix = currency ? ` ${currency}` : ''
  return `${number.toLocaleString('zh-CN', { minimumFractionDigits: 4, maximumFractionDigits: 6 })}${suffix}`
}

function formatBudgetUsed(value: number | string | null | undefined) {
  if (value == null || value === '') return '-'
  const number = Number(value)
  if (!Number.isFinite(number)) return '-'
  return `${number.toFixed(2)}%`
}

function formatNumber(value: number | string | null | undefined) {
  const number = Number(value || 0)
  if (!Number.isFinite(number)) return '0'
  return Math.round(number).toLocaleString('zh-CN')
}

function formatBytes(value: number | string | null | undefined) {
  const bytes = Number(value || 0)
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let size = bytes
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) {
    size /= 1024
    unit++
  }
  return `${size.toFixed(unit === 0 ? 0 : 1)} ${units[unit]}`
}

function formatDurationSeconds(value: number | string | null | undefined) {
  const seconds = Number(value || 0)
  if (!Number.isFinite(seconds) || seconds <= 0) return '0s'
  if (seconds < 60) return `${Math.round(seconds)}s`
  const minutes = Math.floor(seconds / 60)
  const rest = Math.round(seconds % 60)
  return `${minutes}m ${rest}s`
}

function prettyJSON(value: unknown) {
  return JSON.stringify(value || {}, null, 2)
}

onMounted(load)
</script>

<style scoped>
.ops-provider-grid,
.ops-status-grid,
.ops-backup-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(240px, 1fr));
  gap: 14px;
}

.ops-card {
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  background: var(--surface-subtle);
  padding: 14px;
  min-width: 0;
}

.ops-card__head,
.ops-provider-row {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.ops-card p {
  margin: 12px 0 8px;
  font-weight: 600;
}

.ops-provider-list {
  display: grid;
  gap: 10px;
  margin-top: 12px;
}

.ops-provider-row span {
  overflow-wrap: anywhere;
}

.ops-kv {
  display: grid;
  grid-template-columns: 92px minmax(0, 1fr);
  gap: 10px 12px;
  margin-top: 12px;
}

.ops-kv--wide {
  margin-top: 0;
}

.ops-kv span,
.ops-list-title {
  color: var(--text-muted);
}

.ops-kv strong {
  overflow-wrap: anywhere;
}

.ops-json {
  background: var(--code-bg);
  border-radius: 8px;
  color: var(--code-text);
  font-size: 12px;
  line-height: 1.55;
  margin: 0;
  max-height: 460px;
  overflow: auto;
  padding: 14px;
  white-space: pre-wrap;
  word-break: break-word;
}

.ops-backup-table {
  margin-top: 16px;
}

.ops-ai-cost-table {
  margin-top: 16px;
}

.ops-queue-band {
  display: grid;
  grid-template-columns: minmax(220px, 0.8fr) minmax(0, 1.2fr);
  gap: 14px;
  margin-bottom: 14px;
}

.ops-queue-kv {
  align-self: stretch;
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  background: var(--surface-subtle);
  padding: 14px;
}

.ops-queue-table {
  margin-bottom: 14px;
}

.ops-filter-row {
  align-items: center;
  display: grid;
  gap: 10px;
  grid-template-columns: minmax(120px, 0.7fr) minmax(180px, 1fr) minmax(120px, 0.7fr) 120px auto auto;
  margin-bottom: 14px;
}

.ops-dry-run {
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  background: var(--surface-subtle);
  margin-top: 14px;
  padding: 14px;
}

.ops-checks {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin-top: 12px;
}

.ops-dry-run__notes {
  margin-top: 10px;
}

@media (max-width: 900px) {
  .ops-queue-band {
    grid-template-columns: 1fr;
  }

  .ops-filter-row {
    grid-template-columns: 1fr;
  }
}
</style>
