<template>
  <h1 class="page-title">唤醒计划</h1>
  <div class="toolbar">
    <el-select v-model="filters.researchTeamId" clearable placeholder="投研团队" style="width: 200px">
      <el-option v-for="team in teams" :key="team.id" :label="team.name" :value="team.id" />
    </el-select>
    <el-select v-model="filters.status" clearable placeholder="状态" :disabled="filters.overdueOnly" style="width: 180px">
      <el-option label="生效中" value="active" />
      <el-option label="已暂停" value="paused" />
      <el-option label="已触发" value="fired" />
      <el-option label="已取消" value="cancelled" />
    </el-select>
    <el-switch v-model="filters.overdueOnly" active-text="只看待处理" @change="applyOverdueMode" />
    <el-input v-model="filters.meetingId" placeholder="按会议编号过滤" style="max-width: 220px" />
    <el-button :loading="loading" @click="load()">查询</el-button>
    <el-button type="primary" @click="openPlan()">新建计划</el-button>
  </div>

  <el-alert
    v-if="historyMode"
    class="history-mode-alert"
    type="info"
    :closable="false"
    show-icon
    title="正在查看历史唤醒计划，自动刷新已暂停。"
  >
    <template #default>
      <el-button link type="primary" @click="returnToLatest">回到最新</el-button>
    </template>
  </el-alert>

  <el-alert
    v-if="overduePlans.length"
    class="wake-overdue-alert"
    type="warning"
    :closable="false"
    show-icon
    :title="`${overduePlans.length} 个唤醒计划已到期待处理`"
  >
    <template #default>
      <div class="wake-overdue-alert__body">
        <span>涉及 {{ overduePlanIdsText }}。下方“待处理计划”已列出每个逾期计划的编号、来源、原因和到期时长；可直接触发、暂停或取消。</span>
        <div class="wake-overdue-alert__actions">
          <el-button v-if="!filters.overdueOnly" link type="primary" @click="showOnlyOverdue">只看这些计划</el-button>
          <el-button link type="primary" @click="router.push('/ops')">检查调度进程</el-button>
          <el-button link type="primary" @click="router.push('/settings')">检查队列配置</el-button>
        </div>
      </div>
    </template>
  </el-alert>

  <div v-if="overduePlans.length" class="wake-overdue-panel">
    <div class="section-head">
      <div>
        <h2>待处理计划</h2>
        <div class="muted">按到期时间从早到晚排列。这里列出的就是当前需要处理的唤醒计划。</div>
      </div>
      <el-tag type="warning" effect="dark">{{ overduePlans.length }} 个待处理</el-tag>
    </div>
    <div class="wake-overdue-list">
      <div v-for="plan in overduePlans" :key="`overdue-${plan.id}`" class="wake-overdue-card">
        <div class="wake-overdue-card__head">
          <div>
            <strong>#{{ plan.id }} {{ wakeTriggerLabel(plan.triggerType) }}</strong>
            <div class="muted">{{ overdueDurationText(plan) }}</div>
          </div>
          <div class="wake-status-tags">
            <el-tag type="warning" effect="dark">待处理</el-tag>
            <el-tag :type="wakeStatusType(plan.status)" effect="plain">{{ wakeStatusLabel(plan.status) }}</el-tag>
          </div>
        </div>
        <div class="wake-overdue-card__reason">{{ plan.reason || '未填写原因' }}</div>
        <div class="wake-overdue-card__meta">
          <span>来源会议：{{ plan.meetingId ? `#${plan.meetingId}` : '-' }}</span>
          <span>投研团队：{{ teamName(plan.researchTeamId) }}</span>
          <span>下次检查：{{ formatDateTimeUtc8(plan.nextCheckAt) }}</span>
          <span>最后执行：{{ formatDateTimeUtc8(plan.lastRunAt) }}</span>
        </div>
        <div class="toolbar compact-toolbar wake-overdue-card__actions">
          <el-button size="small" type="primary" plain @click="firePlan(plan)">立即触发</el-button>
          <el-button size="small" plain @click="openPlan(plan)">编辑</el-button>
          <el-button size="small" type="warning" plain @click="pausePlan(plan)">暂停</el-button>
          <el-button size="small" type="danger" plain @click="cancelPlan(plan)">取消</el-button>
        </div>
      </div>
    </div>
  </div>

  <div v-if="isMobile" v-loading="loading" class="mobile-card-list">
    <div v-for="plan in displayPlans" :key="plan.id" class="mobile-card">
      <div class="mobile-card__header">
        <div>
          <h3 class="mobile-card__title">#{{ plan.id }} {{ wakeTriggerLabel(plan.triggerType) }}</h3>
          <div class="muted">来源会议：{{ plan.meetingId ? `#${plan.meetingId}` : '-' }}</div>
          <div class="muted">投研团队：{{ teamName(plan.researchTeamId) }}</div>
        </div>
        <div class="wake-status-tags">
          <el-tag :type="wakeStatusType(plan.status)">{{ wakeStatusLabel(plan.status) }}</el-tag>
          <el-tag v-if="isWakePlanOverdue(plan)" type="warning" effect="dark">待处理</el-tag>
        </div>
      </div>
      <div class="mobile-card__meta">
        <div class="mobile-card__meta-row">
          <span class="mobile-card__meta-label">原因</span>
          <span>{{ plan.reason }}</span>
        </div>
        <div class="mobile-card__meta-row">
          <span class="mobile-card__meta-label">下次检查</span>
          <span :class="{ 'wake-overdue-text': isWakePlanOverdue(plan) }">{{ wakeNextCheckText(plan) }}</span>
        </div>
        <div class="mobile-card__meta-row">
          <span class="mobile-card__meta-label">最后执行</span>
          <span>{{ formatDateTimeUtc8(plan.lastRunAt) }}</span>
        </div>
      </div>
      <div class="mobile-card__actions">
        <el-button plain @click="openPlan(plan)">编辑</el-button>
        <el-button v-if="plan.status === 'active'" plain type="primary" @click="firePlan(plan)">立即触发</el-button>
        <el-button v-if="plan.status === 'active'" plain type="warning" @click="pausePlan(plan)">暂停</el-button>
        <el-button v-if="plan.status === 'paused'" plain @click="resumePlan(plan)">恢复</el-button>
        <el-button
          v-if="plan.status !== 'cancelled' && plan.status !== 'fired'"
          plain
          type="danger"
          @click="cancelPlan(plan)"
        >
          取消
        </el-button>
        <el-button plain type="danger" @click="deletePlan(plan)">删除</el-button>
      </div>
    </div>
    <el-empty v-if="!displayPlans.length" :description="emptyWakeText" />
  </div>

  <el-table v-else v-loading="loading" :data="displayPlans" class="panel" :row-class-name="wakeRowClassName">
    <el-table-column prop="id" label="编号" width="80" />
    <el-table-column label="投研团队" min-width="160">
      <template #default="{ row }">{{ teamName(row.researchTeamId) }}</template>
    </el-table-column>
    <el-table-column label="来源会议" width="110">
      <template #default="{ row }">{{ row.meetingId ? `#${row.meetingId}` : '-' }}</template>
    </el-table-column>
    <el-table-column label="类型" width="120">
      <template #default="{ row }">{{ wakeTriggerLabel(row.triggerType) }}</template>
    </el-table-column>
    <el-table-column prop="reason" label="原因" min-width="260" />
    <el-table-column label="状态" width="120">
      <template #default="{ row }">
        <div class="wake-status-tags">
          <el-tag :type="wakeStatusType(row.status)">{{ wakeStatusLabel(row.status) }}</el-tag>
          <el-tag v-if="isWakePlanOverdue(row)" type="warning" effect="dark">待处理</el-tag>
        </div>
      </template>
    </el-table-column>
    <el-table-column label="下次检查" width="220">
      <template #default="{ row }">
        <span :class="{ 'wake-overdue-text': isWakePlanOverdue(row) }">{{ wakeNextCheckText(row) }}</span>
      </template>
    </el-table-column>
    <el-table-column label="最后执行" width="220">
      <template #default="{ row }">{{ formatDateTimeUtc8(row.lastRunAt) }}</template>
    </el-table-column>
    <el-table-column label="操作" width="360">
      <template #default="{ row }">
        <el-button link type="primary" @click="openPlan(row)">编辑</el-button>
        <el-button v-if="row.status === 'active'" link type="primary" @click="firePlan(row)">立即触发</el-button>
        <el-button v-if="row.status === 'active'" link type="warning" @click="pausePlan(row)">暂停</el-button>
        <el-button v-if="row.status === 'paused'" link type="primary" @click="resumePlan(row)">恢复</el-button>
        <el-button v-if="row.status !== 'cancelled' && row.status !== 'fired'" link type="danger" @click="cancelPlan(row)">
          取消
        </el-button>
        <el-button link type="danger" @click="deletePlan(row)">删除</el-button>
      </template>
    </el-table-column>
  </el-table>

  <div class="cursor-pagination-footer">
    <span class="muted">已加载 {{ plans.length }} 条{{ filters.overdueOnly ? '待处理计划' : '' }}</span>
    <el-button v-if="nextCursor" plain :loading="loadingMore" @click="loadMore">加载更多</el-button>
    <el-button v-if="filters.overdueOnly" link type="primary" @click="showAllPlans">查看全部计划</el-button>
  </div>

  <el-dialog v-model="planDialog" title="唤醒计划" :width="planDialogWidth" :fullscreen="isMobile">
    <el-form :model="planForm" :label-width="isMobile ? 'auto' : '120px'" :label-position="isMobile ? 'top' : 'right'">
      <el-form-item label="投研团队">
        <el-select v-model="planForm.researchTeamId" filterable style="width: 100%">
          <el-option v-for="team in teams" :key="team.id" :label="team.name" :value="team.id" />
        </el-select>
      </el-form-item>
      <el-form-item label="类型">
        <el-radio-group v-model="planForm.triggerType">
          <el-radio-button label="time">定时</el-radio-button>
          <el-radio-button label="indicator">指标</el-radio-button>
          <el-radio-button label="event">事件</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <el-form-item label="状态">
        <el-select v-model="planForm.status" style="width: 100%">
          <el-option label="生效中" value="active" />
          <el-option label="已暂停" value="paused" />
          <el-option label="已取消" value="cancelled" />
        </el-select>
      </el-form-item>
      <el-form-item label="原因">
        <el-input v-model="planForm.reason" type="textarea" :rows="3" />
      </el-form-item>
      <el-form-item label="下次检查">
        <el-date-picker
          v-model="planForm.nextCheckAt"
          type="datetime"
          value-format="YYYY-MM-DDTHH:mm:ssZ"
          style="width: 100%"
        />
      </el-form-item>

      <template v-if="planForm.triggerType === 'indicator'">
        <el-form-item label="证券代码"><el-input v-model="planForm.code" placeholder="600519" /></el-form-item>
        <el-form-item label="指标">
          <el-select v-model="planForm.field" style="width: 100%">
            <el-option label="价格" value="price" />
            <el-option label="涨跌幅" value="change_pct" />
            <el-option label="收盘价" value="close" />
            <el-option label="成交量" value="volume" />
          </el-select>
        </el-form-item>
        <el-form-item label="条件">
          <div class="wake-condition-row">
            <el-select v-model="planForm.operator" style="width: 120px">
              <el-option label=">=" value=">=" />
              <el-option label="<=" value="<=" />
              <el-option label=">" value=">" />
              <el-option label="<" value="<" />
              <el-option label="==" value="==" />
            </el-select>
            <el-input-number v-model="planForm.threshold" :precision="4" :step="0.01" style="width: 180px" />
          </div>
        </el-form-item>
      </template>

      <template v-if="planForm.triggerType === 'event'">
        <el-form-item label="关键词">
          <el-input v-model="planForm.keywords" placeholder="多个关键词用逗号分隔" />
        </el-form-item>
        <el-form-item label="相关证券">
          <el-input v-model="planForm.relatedSymbols" placeholder="600519, 000001" />
        </el-form-item>
        <el-form-item label="过滤决策">
          <el-select v-model="planForm.decisions" multiple style="width: 100%">
            <el-option label="触发会议" value="meeting" />
            <el-option label="观察" value="observe" />
            <el-option label="忽略" value="ignore" />
          </el-select>
        </el-form-item>
        <el-form-item label="正则">
          <el-input v-model="planForm.regex" placeholder="可选" />
        </el-form-item>
      </template>

      <el-form-item v-if="planForm.triggerType !== 'time'" label="检查间隔">
        <el-input-number v-model="planForm.intervalSeconds" :min="30" :step="30" />
        <span class="form-suffix">秒</span>
      </el-form-item>
      <el-form-item label="会议主题">
        <el-input v-model="planForm.topic" placeholder="触发后创建会议时使用，可选" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="planDialog = false">取消</el-button>
      <el-button type="primary" :loading="savingPlan" @click="savePlan">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, apiErrorText, jsonapiResource, unwrapJsonApiCollection, type ResearchTeam, type WakePlan } from '../api'
import { useAutoRefresh } from '../composables/useAutoRefresh'
import { nextCursorFromDocument, useCursorPagination } from '../composables/useCursorPagination'
import { useResponsive } from '../composables/useResponsive'
import { formatDateTimeUtc8, toDateUtc8 } from '../utils/datetime'

const { isMobile } = useResponsive()
const route = useRoute()
const router = useRouter()
const teams = ref<ResearchTeam[]>([])
const filters = reactive({
  researchTeamId: undefined as number | undefined,
  status: queryFlag(route.query.overdue) ? 'active' : '',
  meetingId: '',
  overdueOnly: queryFlag(route.query.overdue)
})
const historyMode = ref(false)
const autoRefreshActive = computed(() => !historyMode.value)
const planDialog = ref(false)
const savingPlan = ref(false)
const planDialogWidth = computed(() => (isMobile.value ? '100%' : '680px'))
const planForm = reactive(defaultPlanForm())

const {
  items: plans,
  nextCursor,
  loading,
  loadingMore,
  loadFirstPage,
  refreshFirstPage,
  loadMore: loadMorePage
} = useCursorPagination<WakePlan>(fetchWakePage)

const displayPlans = computed(() => {
  const rows = filters.overdueOnly ? plans.value.filter(isWakePlanOverdue) : plans.value
  return [...rows].sort(compareWakePlanPriority)
})
const overduePlans = computed(() => plans.value.filter(isWakePlanOverdue).sort(compareWakeDueTime))
const overduePlanIdsText = computed(() => {
  const ids = overduePlans.value.map((plan) => `#${plan.id}`)
  const visible = ids.slice(0, 8).join('、')
  return ids.length > 8 ? `${visible} 等 ${ids.length} 个计划` : visible
})
const emptyWakeText = computed(() => filters.overdueOnly ? '暂无到期待处理的生效中唤醒计划' : '暂无唤醒计划')

function teamName(id: number | null | undefined) {
  return teams.value.find((team) => team.id === id)?.name || (id ? `#${id}` : '-')
}

function wakeStatusLabel(value: string | null | undefined) {
  const map: Record<string, string> = {
    active: '生效中',
    paused: '已暂停',
    fired: '已触发',
    cancelled: '已取消'
  }
  return map[String(value || '')] || value || '-'
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

function wakeStatusType(value: string | null | undefined) {
  if (value === 'active') return 'success'
  if (value === 'paused') return 'warning'
  if (value === 'cancelled') return 'info'
  return 'primary'
}

async function fetchWakePage(cursor?: string) {
  const { data } = await api.get('/wake-plans', {
    params: {
      researchTeamId: filters.researchTeamId,
      status: filters.status || undefined,
      meetingId: filters.meetingId || undefined,
      overdue: filters.overdueOnly ? 'true' : undefined,
      'page[limit]': 100,
      'page[cursor]': cursor || undefined
    }
  })
  return {
    items: unwrapJsonApiCollection<WakePlan>(data),
    nextCursor: nextCursorFromDocument(data)
  }
}

async function load(background = false) {
  if (background && historyMode.value) return
  if (background) {
    await refreshFirstPage()
    return
  }
  historyMode.value = false
  await loadFirstPage()
}

async function loadMore() {
  await loadMorePage()
  historyMode.value = true
}

async function returnToLatest() {
  historyMode.value = false
  await load()
}

async function applyOverdueMode() {
  if (filters.overdueOnly) {
    filters.status = 'active'
  }
  await replaceWakeQuery()
  await load()
}

async function showOnlyOverdue() {
  filters.overdueOnly = true
  filters.status = 'active'
  await replaceWakeQuery()
  await load()
}

async function showAllPlans() {
  filters.overdueOnly = false
  await replaceWakeQuery()
  await load()
}

async function replaceWakeQuery() {
  const query = { ...route.query }
  if (filters.overdueOnly) {
    query.overdue = 'true'
  } else {
    delete query.overdue
  }
  await router.replace({ path: '/wake', query })
}

async function loadTeams() {
  teams.value = unwrapJsonApiCollection((await api.get('/research-teams')).data)
}

function isWakePlanOverdue(plan: WakePlan) {
  const next = toDateUtc8(plan.nextCheckAt)
  return plan.status === 'active' && !!next && next.getTime() <= Date.now()
}

function wakeNextCheckText(plan: WakePlan) {
  const base = formatDateTimeUtc8(plan.nextCheckAt)
  return isWakePlanOverdue(plan) ? `${base}（已到期待处理）` : base
}

function overdueDurationText(plan: WakePlan) {
  const next = toDateUtc8(plan.nextCheckAt)
  if (!next) return '没有设置下次检查时间'
  const elapsedMs = Math.max(0, Date.now() - next.getTime())
  const minutes = Math.floor(elapsedMs / 60000)
  if (minutes < 1) return '刚刚到期'
  if (minutes < 60) return `已逾期 ${minutes} 分钟`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `已逾期 ${hours} 小时 ${minutes % 60} 分钟`
  const days = Math.floor(hours / 24)
  return `已逾期 ${days} 天 ${hours % 24} 小时`
}

function wakeRowClassName({ row }: { row: WakePlan }) {
  return isWakePlanOverdue(row) ? 'wake-plan-overdue-row' : ''
}

function compareWakePlanPriority(a: WakePlan, b: WakePlan) {
  const overdueDelta = Number(isWakePlanOverdue(b)) - Number(isWakePlanOverdue(a))
  if (overdueDelta !== 0) return overdueDelta
  return compareWakeDueTime(a, b)
}

function compareWakeDueTime(a: WakePlan, b: WakePlan) {
  return wakeDueTimestamp(a) - wakeDueTimestamp(b)
}

function wakeDueTimestamp(plan: WakePlan) {
  return toDateUtc8(plan.nextCheckAt)?.getTime() ?? Number.MAX_SAFE_INTEGER
}

function queryFlag(value: unknown) {
  const raw = Array.isArray(value) ? value[0] : value
  return raw === true || raw === 'true' || raw === '1'
}

function openPlan(row?: WakePlan) {
  Object.assign(planForm, defaultPlanForm())
  if (!row) {
    planForm.researchTeamId = filters.researchTeamId || teams.value[0]?.id
    planDialog.value = true
    return
  }
  const cfg = row.triggerConfig || {}
  planForm.id = row.id
  planForm.researchTeamId = row.researchTeamId
  planForm.triggerType = normalizeTriggerType(row.triggerType)
  planForm.status = row.status || 'active'
  planForm.reason = row.reason || ''
  planForm.nextCheckAt = row.nextCheckAt || ''
  planForm.topic = stringFromConfig(cfg.topic)
  planForm.intervalSeconds = numberFromConfig(cfg.intervalSeconds, 300)
  planForm.code = stringFromConfig(cfg.code || cfg.symbol || cfg.ticker)
  planForm.field = stringFromConfig(cfg.field || cfg.metric) || 'price'
  planForm.operator = stringFromConfig(cfg.operator || cfg.op) || '>='
  planForm.threshold = numberFromConfig(cfg.threshold || cfg.target || cfg.value, 0)
  planForm.keywords = listFromConfig(cfg.keywords || cfg.keyword || cfg.contains).join(', ')
  planForm.relatedSymbols = listFromConfig(cfg.relatedSymbols || cfg.symbols || cfg.codes).join(', ')
  planForm.decisions = listFromConfig(cfg.decisions || cfg.decision).concat(listFromConfig(cfg.filterDecision))
  if (!planForm.decisions.length) planForm.decisions = ['meeting']
  planForm.regex = stringFromConfig(cfg.regex || cfg.pattern)
  planDialog.value = true
}

async function savePlan() {
  if (!planForm.researchTeamId) {
    ElMessage.error('请选择投研团队')
    return
  }
  if (!planForm.reason.trim()) {
    ElMessage.error('请填写唤醒原因')
    return
  }
  savingPlan.value = true
  try {
    const payload = {
      researchTeamId: planForm.researchTeamId,
      triggerType: planForm.triggerType,
      triggerConfig: buildTriggerConfig(),
      reason: planForm.reason.trim(),
      status: planForm.status,
      nextCheckAt: planForm.nextCheckAt || null
    }
    if (planForm.id) {
      await api.put(`/wake-plans/${planForm.id}`, jsonapiResource('wake-plans', payload, String(planForm.id)))
    } else {
      await api.post('/wake-plans', jsonapiResource('wake-plans', payload))
    }
    ElMessage.success('唤醒计划已保存')
    planDialog.value = false
    await load()
  } catch (error) {
    ElMessage.error(apiErrorText(error, '唤醒计划保存失败'))
  } finally {
    savingPlan.value = false
  }
}

function buildTriggerConfig() {
  const base: Record<string, unknown> = {}
  if (planForm.topic.trim()) base.topic = planForm.topic.trim()
  if (planForm.triggerType === 'indicator') {
    return {
      ...base,
      code: planForm.code.trim(),
      field: planForm.field,
      operator: planForm.operator,
      threshold: String(planForm.threshold ?? ''),
      intervalSeconds: planForm.intervalSeconds
    }
  }
  if (planForm.triggerType === 'event') {
    return {
      ...base,
      keywords: splitList(planForm.keywords),
      symbols: splitList(planForm.relatedSymbols),
      decisions: planForm.decisions,
      regex: planForm.regex.trim() || undefined,
      intervalSeconds: planForm.intervalSeconds
    }
  }
  return base
}

async function firePlan(row: WakePlan) {
  await api.post(`/wake-plans/${row.id}/fire`)
  ElMessage.success(`唤醒计划 #${row.id} 已触发`)
  await load()
}

async function pausePlan(row: WakePlan) {
  await api.post(`/wake-plans/${row.id}/pause`)
  ElMessage.success(`唤醒计划 #${row.id} 已暂停`)
  await load()
}

async function resumePlan(row: WakePlan) {
  await api.post(`/wake-plans/${row.id}/resume`)
  ElMessage.success(`唤醒计划 #${row.id} 已恢复`)
  await load()
}

async function cancelPlan(row: WakePlan) {
  await api.post(`/wake-plans/${row.id}/cancel`)
  ElMessage.success(`唤醒计划 #${row.id} 已取消`)
  await load()
}

async function deletePlan(row: WakePlan) {
  await ElMessageBox.confirm(`确定删除唤醒计划 #${row.id} 吗？`, '删除唤醒计划', {
    type: 'warning'
  })
  await api.delete(`/wake-plans/${row.id}`)
  ElMessage.success(`唤醒计划 #${row.id} 已删除`)
  await load()
}

onMounted(async () => {
  await loadTeams()
  await load()
})

useAutoRefresh({
  enabled: autoRefreshActive,
  intervalMs: 10000,
  refresh: () => load(true)
})

function defaultPlanForm() {
  return {
    id: undefined as number | undefined,
    researchTeamId: undefined as number | undefined,
    triggerType: 'time',
    status: 'active',
    reason: '',
    nextCheckAt: '',
    topic: '',
    code: '',
    field: 'price',
    operator: '>=',
    threshold: 0,
    keywords: '',
    relatedSymbols: '',
    decisions: ['meeting'] as string[],
    regex: '',
    intervalSeconds: 300
  }
}

function normalizeTriggerType(value: string) {
  if (value === 'indicator' || value === 'event' || value === 'time') return value
  if (value === 'market' || value === 'condition') return 'indicator'
  if (value === 'news') return 'event'
  return 'time'
}

function splitList(value: string) {
  return value
    .split(/[,，\n]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function listFromConfig(value: unknown): string[] {
  if (Array.isArray(value)) return value.map((item) => String(item).trim()).filter(Boolean)
  if (value === undefined || value === null || value === '') return []
  return splitList(String(value))
}

function stringFromConfig(value: unknown) {
  if (value === undefined || value === null) return ''
  return String(value)
}

function numberFromConfig(value: unknown, fallback: number) {
  const parsed = Number(value)
  return Number.isFinite(parsed) ? parsed : fallback
}
</script>

<style scoped>
.wake-condition-row {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}

.wake-overdue-alert {
  margin-bottom: 14px;
  border-left: 4px solid #d97706;
}

.wake-overdue-alert__body {
  display: grid;
  gap: 6px;
  line-height: 1.55;
  overflow-wrap: anywhere;
}

.wake-overdue-alert__actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.wake-overdue-panel {
  border: 1px solid #fcd34d;
  border-left: 4px solid #d97706;
  border-radius: 8px;
  background: #fffbeb;
  padding: 14px;
  margin-bottom: 14px;
}

.wake-overdue-list {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  gap: 12px;
  margin-top: 12px;
}

.wake-overdue-card {
  display: grid;
  gap: 10px;
  min-width: 0;
  border: 1px solid #f3d27d;
  border-radius: 8px;
  background: #ffffff;
  padding: 12px;
}

.wake-overdue-card__head {
  display: flex;
  justify-content: space-between;
  gap: 10px;
  align-items: flex-start;
}

.wake-overdue-card__head strong,
.wake-overdue-card__reason,
.wake-overdue-card__meta span {
  min-width: 0;
  overflow-wrap: anywhere;
}

.wake-overdue-card__reason {
  color: #1f2937;
  font-weight: 600;
  line-height: 1.5;
}

.wake-overdue-card__meta {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 6px 10px;
  color: #64748b;
  font-size: 12px;
  line-height: 1.5;
}

.wake-overdue-card__actions {
  margin-top: 0;
}

.wake-status-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  align-items: center;
}

.wake-overdue-text {
  color: #b45309;
  font-weight: 600;
  overflow-wrap: anywhere;
}

:deep(.wake-plan-overdue-row) .el-table__cell {
  background: #fffbeb !important;
}

@media (max-width: 767px) {
  .wake-overdue-card__head {
    flex-direction: column;
  }

  .wake-overdue-card__meta {
    grid-template-columns: 1fr;
  }
}
</style>
