<template>
  <h1 class="page-title">唤醒计划</h1>
  <div class="toolbar">
    <el-select v-model="filters.researchTeamId" clearable placeholder="投研团队" style="width: 200px">
      <el-option v-for="team in teams" :key="team.id" :label="team.name" :value="team.id" />
    </el-select>
    <el-select v-model="filters.status" clearable placeholder="状态" style="width: 180px">
      <el-option label="生效中" value="active" />
      <el-option label="已暂停" value="paused" />
      <el-option label="已触发" value="fired" />
      <el-option label="已取消" value="cancelled" />
    </el-select>
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

  <div v-if="isMobile" v-loading="loading" class="mobile-card-list">
    <div v-for="plan in plans" :key="plan.id" class="mobile-card">
      <div class="mobile-card__header">
        <div>
            <h3 class="mobile-card__title">#{{ plan.id }} {{ wakeTriggerLabel(plan.triggerType) }}</h3>
          <div class="muted">来源会议：{{ plan.meetingId || '-' }}</div>
          <div class="muted">投研团队：{{ teamName(plan.researchTeamId) }}</div>
        </div>
        <el-tag :type="wakeStatusType(plan.status)">{{ wakeStatusLabel(plan.status) }}</el-tag>
      </div>
      <div class="mobile-card__meta">
        <div class="mobile-card__meta-row">
          <span class="mobile-card__meta-label">原因</span>
          <span>{{ plan.reason }}</span>
        </div>
        <div class="mobile-card__meta-row">
          <span class="mobile-card__meta-label">下次检查</span>
          <span>{{ formatDateTimeUtc8(plan.nextCheckAt) }}</span>
        </div>
        <div class="mobile-card__meta-row">
          <span class="mobile-card__meta-label">最后执行</span>
          <span>{{ formatDateTimeUtc8(plan.lastRunAt) }}</span>
        </div>
      </div>
      <div class="mobile-card__actions">
        <el-button plain @click="openPlan(plan)">编辑</el-button>
        <el-button plain type="primary" @click="firePlan(plan)">立即触发</el-button>
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
    <el-empty v-if="!plans.length" description="暂无唤醒计划" />
  </div>

  <el-table v-else v-loading="loading" :data="plans" class="panel">
    <el-table-column prop="id" label="编号" width="80" />
    <el-table-column label="投研团队" min-width="160">
      <template #default="{ row }">{{ teamName(row.researchTeamId) }}</template>
    </el-table-column>
    <el-table-column prop="meetingId" label="来源会议" width="100" />
    <el-table-column label="类型" width="120">
      <template #default="{ row }">{{ wakeTriggerLabel(row.triggerType) }}</template>
    </el-table-column>
    <el-table-column prop="reason" label="原因" min-width="260" />
    <el-table-column label="状态" width="120">
      <template #default="{ row }"><el-tag :type="wakeStatusType(row.status)">{{ wakeStatusLabel(row.status) }}</el-tag></template>
    </el-table-column>
    <el-table-column label="下次检查" width="220">
      <template #default="{ row }">{{ formatDateTimeUtc8(row.nextCheckAt) }}</template>
    </el-table-column>
    <el-table-column label="最后执行" width="220">
      <template #default="{ row }">{{ formatDateTimeUtc8(row.lastRunAt) }}</template>
    </el-table-column>
    <el-table-column label="操作" width="360">
      <template #default="{ row }">
        <el-button link type="primary" @click="openPlan(row)">编辑</el-button>
        <el-button link type="primary" @click="firePlan(row)">立即触发</el-button>
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
    <span class="muted">已加载 {{ plans.length }} 条</span>
    <el-button v-if="nextCursor" plain :loading="loadingMore" @click="loadMore">加载更多</el-button>
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
import { api, apiErrorText, jsonapiResource, unwrapJsonApiCollection, type ResearchTeam, type WakePlan } from '../api'
import { useAutoRefresh } from '../composables/useAutoRefresh'
import { nextCursorFromDocument, useCursorPagination } from '../composables/useCursorPagination'
import { useResponsive } from '../composables/useResponsive'
import { formatDateTimeUtc8 } from '../utils/datetime'

const { isMobile } = useResponsive()
const teams = ref<ResearchTeam[]>([])
const filters = reactive({ researchTeamId: undefined as number | undefined, status: '', meetingId: '' })
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

async function loadTeams() {
  teams.value = unwrapJsonApiCollection((await api.get('/research-teams')).data)
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
  ElMessage.success('唤醒计划已触发')
  await load()
}

async function pausePlan(row: WakePlan) {
  await api.post(`/wake-plans/${row.id}/pause`)
  ElMessage.success('唤醒计划已暂停')
  await load()
}

async function resumePlan(row: WakePlan) {
  await api.post(`/wake-plans/${row.id}/resume`)
  ElMessage.success('唤醒计划已恢复')
  await load()
}

async function cancelPlan(row: WakePlan) {
  await api.post(`/wake-plans/${row.id}/cancel`)
  ElMessage.success('唤醒计划已取消')
  await load()
}

async function deletePlan(row: WakePlan) {
  await ElMessageBox.confirm(`确定删除唤醒计划 #${row.id} 吗？`, '删除唤醒计划', {
    type: 'warning'
  })
  await api.delete(`/wake-plans/${row.id}`)
  ElMessage.success('唤醒计划已删除')
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
</style>
