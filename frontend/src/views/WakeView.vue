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
    <el-table-column label="操作" width="320">
      <template #default="{ row }">
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
</template>

<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, onMounted, reactive, ref } from 'vue'
import { api, unwrapJsonApiCollection, type ResearchTeam, type WakePlan } from '../api'
import { useAutoRefresh } from '../composables/useAutoRefresh'
import { nextCursorFromDocument, useCursorPagination } from '../composables/useCursorPagination'
import { useResponsive } from '../composables/useResponsive'
import { formatDateTimeUtc8 } from '../utils/datetime'

const { isMobile } = useResponsive()
const teams = ref<ResearchTeam[]>([])
const filters = reactive({ researchTeamId: undefined as number | undefined, status: '', meetingId: '' })
const historyMode = ref(false)
const autoRefreshActive = computed(() => !historyMode.value)

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
</script>
