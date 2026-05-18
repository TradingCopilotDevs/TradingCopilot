<template>
  <h1 class="page-title">系统日志</h1>

  <div class="panel">
    <div class="section-head">
      <div>
        <h2>日志诊断</h2>
        <p class="panel-subtitle">{{ configSummary }}</p>
      </div>
      <div class="toolbar compact-toolbar">
        <el-switch v-model="autoRefreshEnabled" active-text="自动刷新" />
        <el-button :loading="loading || isRefreshing" @click="historyMode ? returnToLatest() : refreshNow()">
          {{ historyMode ? '回到最新' : '刷新' }}
        </el-button>
      </div>
    </div>

    <div class="toolbar logs-filter-toolbar">
      <el-date-picker
        v-model="query.timeRange"
        type="datetimerange"
        unlink-panels
        range-separator="至"
        start-placeholder="开始时间"
        end-placeholder="结束时间"
        class="logs-time-range"
      />
      <el-select v-model="query.level" clearable placeholder="级别" style="width: 120px">
        <el-option label="debug" value="debug" />
        <el-option label="info" value="info" />
        <el-option label="warn" value="warn" />
        <el-option label="error" value="error" />
      </el-select>
      <el-select v-model="query.event" clearable filterable placeholder="事件" style="width: 190px">
        <el-option v-for="event in events" :key="event" :label="eventLabel(event)" :value="event" />
      </el-select>
      <el-select v-model="query.group" clearable filterable placeholder="分组" style="width: 160px">
        <el-option v-for="group in groups" :key="group" :label="group" :value="group" />
      </el-select>
      <el-select v-model="query.method" clearable placeholder="方法" style="width: 110px">
        <el-option v-for="method in methods" :key="method" :label="method" :value="method" />
      </el-select>
      <el-input v-model="query.path" clearable placeholder="路径关键词" style="width: 220px" @keyup.enter="loadLogs()" />
      <el-select v-model="query.status" clearable filterable placeholder="状态" style="width: 140px">
        <el-option v-for="status in statuses" :key="status" :label="statusLabel(status)" :value="status" />
      </el-select>
      <el-checkbox v-model="query.slowOnly">慢请求</el-checkbox>
      <el-checkbox v-model="query.includeNoise">显示噪声</el-checkbox>
      <el-select v-model="query.role" clearable filterable placeholder="进程" style="width: 180px">
        <el-option v-for="role in roles" :key="role" :label="role" :value="role" />
      </el-select>
      <el-select v-model="query.file" clearable filterable placeholder="日志文件" style="width: 220px">
        <el-option v-for="file in logFiles" :key="file.name" :label="file.name" :value="file.name" />
      </el-select>
      <el-input v-model="query.q" clearable placeholder="全文关键词" class="logs-search" @keyup.enter="loadLogs()" />
      <el-button type="primary" :loading="loading" @click="loadLogs()">查询</el-button>
    </div>

    <el-alert
      v-if="historyMode"
      class="history-mode-alert"
      type="info"
      :closable="false"
      show-icon
      title="正在查看历史日志，自动刷新已暂停。"
    >
      <template #default>
        <el-button link type="primary" @click="returnToLatest">回到最新</el-button>
      </template>
    </el-alert>

    <div v-if="isMobile" v-loading="loading" class="mobile-card-list">
      <div v-for="entry in entries" :key="entry.id" class="mobile-card" @click="openEntry(entry)">
        <div class="mobile-card__header">
          <div>
            <h3 class="mobile-card__title">{{ primaryText(entry) }}</h3>
            <div class="muted">{{ formatDateTimeUtc8(entry.time) }}</div>
          </div>
          <el-tag :type="levelType(entry.level)">{{ entry.level }}</el-tag>
        </div>
        <div class="mobile-card__meta">
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">事件</span>
            <span>{{ eventLabel(entry.event) }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">分组</span>
            <span>{{ displayValue(entry.group) }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">方法</span>
            <span>{{ displayValue(entry.method) }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">进程</span>
            <span>{{ entry.role || '-' }}</span>
          </div>
        </div>
      </div>
      <el-empty v-if="!entries.length" description="暂无日志" />
    </div>

    <div v-else class="table-scroll">
      <el-table v-loading="loading" :data="entries" empty-text="暂无日志" @row-click="openEntry">
        <el-table-column label="时间" width="210">
          <template #default="{ row }">{{ formatDateTimeUtc8(row.time) }}</template>
        </el-table-column>
        <el-table-column label="级别" width="90">
          <template #default="{ row }">
            <el-tag :type="levelType(row.level)" effect="plain">{{ row.level }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="事件" width="160">
          <template #default="{ row }">{{ eventLabel(row.event) }}</template>
        </el-table-column>
        <el-table-column label="分组" width="110">
          <template #default="{ row }">{{ displayValue(row.group) }}</template>
        </el-table-column>
        <el-table-column label="方法" width="90">
          <template #default="{ row }">{{ displayValue(row.method) }}</template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="statusType(row.status)" effect="plain">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="耗时" width="100">
          <template #default="{ row }">{{ durationText(row.durationMs) }}</template>
        </el-table-column>
        <el-table-column label="路径 / 消息" min-width="360" show-overflow-tooltip>
          <template #default="{ row }">{{ primaryText(row) }}</template>
        </el-table-column>
      </el-table>
    </div>

    <div class="logs-footer">
      <span class="muted">已加载 {{ entries.length }} 条，默认隐藏静态资源和日志页自身请求</span>
      <el-button v-if="nextCursor" plain :loading="loadingMore" @click="loadMore">加载更多</el-button>
    </div>
  </div>

  <el-drawer v-model="drawerVisible" title="日志详情" :size="drawerSize">
    <template v-if="selectedEntry">
      <div class="log-detail-grid">
        <span>时间</span>
        <strong>{{ formatDateTimeUtc8(selectedEntry.time) }}</strong>
        <span>级别</span>
        <el-tag :type="levelType(selectedEntry.level)" effect="plain">{{ selectedEntry.level }}</el-tag>
        <span>事件</span>
        <strong>{{ displayValue(selectedEntry.event) }}</strong>
        <span>进程</span>
        <strong>{{ selectedEntry.role || '-' }}</strong>
        <span>来源</span>
        <strong>{{ selectedEntry.source || '-' }}</strong>
        <span>分组</span>
        <strong>{{ displayValue(selectedEntry.group) }}</strong>
        <span>方法</span>
        <strong>{{ displayValue(selectedEntry.method) }}</strong>
        <span>状态</span>
        <strong>{{ statusLabel(selectedEntry.status) }}</strong>
        <span>耗时</span>
        <strong>{{ durationText(selectedEntry.durationMs) }}</strong>
        <span>请求 ID</span>
        <strong>{{ fieldText(selectedEntry, 'requestId') }}</strong>
        <span>远端地址</span>
        <strong>{{ fieldText(selectedEntry, 'remoteAddr') }}</strong>
        <span>响应字节</span>
        <strong>{{ fieldText(selectedEntry, 'bytes') }}</strong>
        <span>文件</span>
        <strong>{{ selectedEntry.file }}</strong>
        <span>调用点</span>
        <strong>{{ selectedEntry.caller || '-' }}</strong>
      </div>
      <el-divider />
      <h3 class="log-detail-title">消息</h3>
      <p class="log-message">{{ selectedEntry.message || '-' }}</p>
      <h3 class="log-detail-title">结构化字段</h3>
      <pre class="json-block">{{ prettyJSON(selectedEntry.fields) }}</pre>
      <h3 class="log-detail-title">原始日志</h3>
      <pre class="json-block">{{ prettyJSON(selectedEntry.raw) }}</pre>
      <template v-if="selectedEntry.stacktrace">
        <h3 class="log-detail-title">堆栈</h3>
        <pre class="json-block">{{ selectedEntry.stacktrace }}</pre>
      </template>
      <div class="drawer-actions">
        <el-button type="primary" @click="copyEntry">复制 JSON</el-button>
      </div>
    </template>
  </el-drawer>
</template>

<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api, apiErrorText, unwrapJsonApiCollection, type LogConfigSummary, type LogEntry, type LogFile } from '../api'
import { useAutoRefresh } from '../composables/useAutoRefresh'
import { nextCursorFromDocument, useCursorPagination } from '../composables/useCursorPagination'
import { useResponsive } from '../composables/useResponsive'
import { formatDateTimeUtc8 } from '../utils/datetime'

const { isMobile, isTablet } = useResponsive()
const logFiles = ref<LogFile[]>([])
const config = ref<LogConfigSummary>({})
const selectedEntry = ref<LogEntry | null>(null)
const drawerVisible = ref(false)
const autoRefreshEnabled = ref(true)
const historyMode = ref(false)
const placeholderText = '不适用'
const events = ['http.request', 'http.panic', 'gorm.slow_query', 'gorm.query_failed', 'gorm.warning', 'app.lifecycle', 'app.seed_defaults', 'queue.enqueue', 'queue.task', 'ai.chat', 'ai.models', 'market.symbols', 'market.quotes', 'market.series', 'market.daily', 'messaging.listener', 'messaging.live_message', 'meeting.dispatch', 'meeting.recovery', 'wake.process_due', 'paper.maintenance', 'system.heartbeat']
const methods = ['GET', 'POST', 'PUT', 'DELETE', 'OPTIONS', 'setup', 'serve', 'query', 'list', 'sync', 'fetch', 'refresh', 'enqueue', 'run', 'collect', 'filter', 'listen', 'ingest', 'call', 'evaluate', 'recover', 'dispatch', 'record', 'stop', 'not_applicable']
const groups = ['app', 'auth', 'settings', 'logs', 'market', 'meetings', 'meeting', 'messaging', 'paper', 'wake', 'dashboard', 'ai', 'research', 'database', 'scheduler', 'system', 'static', 'frontend', 'unknown', 'not_applicable']
const statuses = ['ok', 'warning', 'error', 'slow', 'skipped', 'not_applicable', '200', '201', '204', '400', '401', '403', '404', '409', '422', '500', '502', '503']

const now = new Date()
const oneHourAgo = new Date(now.getTime() - 60 * 60 * 1000)
const query = reactive({
  timeRange: [oneHourAgo, now] as [Date, Date],
  level: '',
  event: '',
  group: '',
  method: '',
  path: '',
  status: '',
  slowOnly: false,
  includeNoise: false,
  role: '',
  file: '',
  q: ''
})

const roles = computed(() => Array.from(new Set(logFiles.value.map((file) => file.role).filter(Boolean))).sort())
const drawerSize = computed(() => (isMobile.value ? '100%' : isTablet.value ? '80%' : '620px'))
const autoRefreshActive = computed(() => autoRefreshEnabled.value && !historyMode.value)
const configSummary = computed(() => {
  const cfg = config.value
  if (!cfg.dir) return '日志配置加载中'
  if (cfg.rotationMode === 'time') {
    return `${cfg.dir} | ${cfg.level || 'info'} | 每日切分 | 保留 ${cfg.rotationMaxAgeDays || 7} 天`
  }
  return `${cfg.dir} | ${cfg.level || 'info'} | 单文件 ${cfg.rotationSizeMB || 20}MB | 总量 ${cfg.rotationTotalSizeMB || 100}MB`
})

const {
  items: entries,
  nextCursor,
  loading,
  loadingMore,
  loadFirstPage,
  refreshFirstPage,
  loadMore: loadMorePage
} = useCursorPagination<LogEntry>(fetchLogPage)

const { isRefreshing, refreshNow } = useAutoRefresh({
  enabled: autoRefreshActive,
  immediate: false,
  intervalMs: 5000,
  refresh: () => loadLogs(true)
})

watch(
  () => query.includeNoise,
  () => {
    void loadLogs(true)
  }
)

function params(cursor?: string) {
  const [from, to] = query.timeRange || []
  return {
    from: from ? from.toISOString() : undefined,
    to: to ? to.toISOString() : undefined,
    level: query.level || undefined,
    event: query.event || undefined,
    group: query.group || undefined,
    method: query.method || undefined,
    path: query.path || undefined,
    status: query.status || undefined,
    slowOnly: query.slowOnly || undefined,
    includeNoise: query.includeNoise || undefined,
    role: query.role || undefined,
    file: query.file || undefined,
    q: query.q || undefined,
    'page[limit]': 200,
    'page[cursor]': cursor || undefined
  }
}

async function loadFiles() {
  const { data } = await api.get('/logs/files')
  logFiles.value = unwrapJsonApiCollection<LogFile>(data)
  config.value = (data?.meta?.config || {}) as LogConfigSummary
}

async function fetchLogPage(cursor?: string) {
  const { data } = await api.get('/logs', { params: params(cursor) })
  return {
    items: unwrapJsonApiCollection<LogEntry>(data),
    nextCursor: nextCursorFromDocument(data)
  }
}

async function loadLogs(background = false) {
  if (background && historyMode.value) return
  try {
    await loadFiles()
    if (background) {
      await refreshFirstPage()
      return
    }
    historyMode.value = false
    await loadFirstPage()
  } catch (error) {
    if (!background) ElMessage.error(apiErrorText(error))
  }
}

async function loadMore() {
  if (!nextCursor.value) return
  try {
    await loadMorePage()
    historyMode.value = true
  } catch (error) {
    ElMessage.error(apiErrorText(error))
  }
}

async function returnToLatest() {
  historyMode.value = false
  await loadLogs()
}

function openEntry(entry: LogEntry) {
  selectedEntry.value = entry
  drawerVisible.value = true
}

function levelType(level: string) {
  const map: Record<string, 'success' | 'warning' | 'danger' | 'info'> = {
    debug: 'info',
    info: 'success',
    warn: 'warning',
    error: 'danger'
  }
  return map[String(level).toLowerCase()] || 'info'
}

function statusType(status: string) {
  const numeric = Number(status)
  if (status === 'error' || numeric >= 500) return 'danger'
  if (status === 'warning' || status === 'slow' || numeric >= 400) return 'warning'
  if (status === 'skipped' || status === 'not_applicable' || numeric >= 300) return 'info'
  return 'success'
}

function eventLabel(value: string) {
  const map: Record<string, string> = {
    'http.request': 'HTTP 请求',
    'http.panic': 'HTTP Panic',
    'gorm.slow_query': '慢 SQL',
    'gorm.query_failed': 'SQL 失败',
    'app.lifecycle': '生命周期'
  }
  return map[value] || displayValue(value)
}

function statusLabel(value: string) {
  return displayValue(value)
}

function displayValue(value?: string | null) {
  return !value || value === 'not_applicable' ? placeholderText : value
}

function primaryText(entry: LogEntry) {
  return entry.path || entry.message || '-'
}

function durationText(value?: number | null) {
  return typeof value === 'number' ? `${value}ms` : placeholderText
}

function fieldText(entry: LogEntry, key: string) {
  const value = entry.raw?.[key] ?? entry.fields?.[key]
  if (value === undefined || value === null || value === '') return '-'
  return String(value)
}

function prettyJSON(value: unknown) {
  return JSON.stringify(value || {}, null, 2)
}

async function copyEntry() {
  if (!selectedEntry.value) return
  await navigator.clipboard.writeText(prettyJSON(selectedEntry.value.raw))
  ElMessage.success('日志 JSON 已复制')
}

onMounted(() => {
  void loadLogs()
})
</script>

<style scoped>
.logs-filter-toolbar {
  align-items: flex-start;
}

.logs-time-range {
  width: 360px;
}

.logs-search {
  min-width: 220px;
  flex: 1;
}

.status-input {
  width: 98px;
}

.logs-footer {
  align-items: center;
  display: flex;
  justify-content: space-between;
  margin-top: 14px;
}

.history-mode-alert {
  margin-bottom: 12px;
}

.log-detail-grid {
  display: grid;
  grid-template-columns: 88px minmax(0, 1fr);
  gap: 10px 14px;
}

.log-detail-grid span {
  color: var(--el-text-color-secondary);
}

.log-detail-grid strong {
  overflow-wrap: anywhere;
}

.log-detail-title {
  font-size: 14px;
  margin: 18px 0 8px;
}

.log-message {
  line-height: 1.6;
  margin: 0;
  overflow-wrap: anywhere;
}

.json-block {
  background: var(--el-fill-color-light);
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  font-size: 12px;
  line-height: 1.5;
  margin: 0;
  max-height: 360px;
  overflow: auto;
  padding: 12px;
  white-space: pre-wrap;
  word-break: break-word;
}

.drawer-actions {
  display: flex;
  justify-content: flex-end;
  margin-top: 16px;
}

@media (max-width: 768px) {
  .logs-time-range,
  .logs-search {
    width: 100%;
  }
}
</style>
