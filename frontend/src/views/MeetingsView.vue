<template>
  <h1 class="page-title">虚拟会议</h1>
  <div class="toolbar">
    <el-select v-model="selectedTeamId" placeholder="投研团队" style="width: 220px" @change="load()">
      <el-option v-for="team in teams" :key="team.id" :label="team.name" :value="team.id" />
    </el-select>
    <el-input v-model="topic" placeholder="会议主题" style="max-width: 420px" @keyup.enter="create" />
    <el-select v-model="filters.status" clearable placeholder="状态" style="width: 150px" @change="load()">
      <el-option label="排队中" value="queued" />
      <el-option label="运行中" value="running" />
      <el-option label="已完成" value="completed" />
      <el-option label="失败" value="failed" />
      <el-option label="已取消" value="cancelled" />
    </el-select>
    <el-input v-model="filters.tag" placeholder="标签" style="max-width: 180px" @keyup.enter="load()" />
    <el-button type="primary" :loading="creating" @click="create">创建会议</el-button>
    <el-button :icon="Refresh" :loading="loading" @click="load()">刷新</el-button>
  </div>

  <el-alert
    v-if="historyMode"
    class="history-mode-alert"
    type="info"
    :closable="false"
    show-icon
    title="正在查看历史会议，自动刷新已暂停。"
  >
    <template #default>
      <el-button link type="primary" @click="returnToLatest">回到最新</el-button>
    </template>
  </el-alert>

  <div v-if="isMobile" v-loading="loading" class="mobile-card-list">
    <div v-for="meeting in meetings" :key="meeting.id" class="mobile-card">
      <div class="mobile-card__header">
        <div>
          <h3 class="mobile-card__title">#{{ meeting.id }} {{ meeting.topic }}</h3>
          <div class="muted">{{ formatDateTimeUtc8(meeting.createdAt) }}</div>
        </div>
        <el-tag :type="statusType(meeting.status)">{{ statusLabel(meeting.status) }}</el-tag>
      </div>
      <div class="mobile-card__meta">
        <div class="mobile-card__meta-row">
          <span class="mobile-card__meta-label">团队</span>
          <span>{{ teamName(meeting.researchTeamId) }}</span>
        </div>
        <div class="mobile-card__meta-row">
          <span class="mobile-card__meta-label">来源</span>
          <span>{{ triggerSourceLabel(meeting.triggerSource) }}</span>
        </div>
        <div class="mobile-card__meta-row">
          <span class="mobile-card__meta-label">标签</span>
          <span v-if="meetingTags(meeting).length">
            <el-tag v-for="tag in meetingTags(meeting)" :key="tag" class="tag-gap" size="small">{{ tag }}</el-tag>
          </span>
          <span v-else>-</span>
        </div>
      </div>
      <div class="mobile-card__actions">
        <el-button type="primary" plain @click="$router.push(`/meetings/${meeting.id}`)">记录</el-button>
        <el-button v-if="meeting.status === 'completed'" plain @click="rerunRecap(meeting)">重新总结</el-button>
        <el-button v-if="canCancel(meeting)" plain type="warning" :loading="running[`cancelMeeting:${meeting.id}`]" @click="cancelMeeting(meeting)">取消</el-button>
        <el-popconfirm title="删除该会议和记录？" @confirm="deleteMeeting(meeting)">
          <template #reference><el-button plain type="danger">删除</el-button></template>
        </el-popconfirm>
      </div>
    </div>
    <el-empty v-if="!meetings.length" description="暂无会议" />
  </div>

  <el-table v-else v-loading="loading" :data="meetings" class="panel" empty-text="暂无会议">
    <el-table-column prop="id" label="编号" width="80" />
    <el-table-column label="投研团队" width="180">
      <template #default="{ row }">{{ teamName(row.researchTeamId) }}</template>
    </el-table-column>
    <el-table-column prop="topic" label="主题" min-width="260" />
    <el-table-column label="来源" width="140">
      <template #default="{ row }">{{ triggerSourceLabel(row.triggerSource) }}</template>
    </el-table-column>
    <el-table-column label="标签" min-width="180">
      <template #default="{ row }">
        <template v-if="meetingTags(row).length">
          <el-tag v-for="tag in meetingTags(row)" :key="tag" class="tag-gap" size="small">{{ tag }}</el-tag>
        </template>
        <span v-else>-</span>
      </template>
    </el-table-column>
    <el-table-column label="状态" width="110">
      <template #default="{ row }"><el-tag :type="statusType(row.status)">{{ statusLabel(row.status) }}</el-tag></template>
    </el-table-column>
    <el-table-column label="创建时间" width="190">
      <template #default="{ row }">{{ formatDateTimeUtc8(row.createdAt) }}</template>
    </el-table-column>
    <el-table-column label="操作" width="260" fixed="right">
      <template #default="{ row }">
        <el-button link type="primary" @click="$router.push(`/meetings/${row.id}`)">记录</el-button>
        <el-button v-if="row.status === 'completed'" link type="primary" @click="rerunRecap(row)">重新总结</el-button>
        <el-button v-if="canCancel(row)" link type="warning" :loading="running[`cancelMeeting:${row.id}`]" @click="cancelMeeting(row)">取消</el-button>
        <el-popconfirm title="删除该会议和记录？" @confirm="deleteMeeting(row)">
          <template #reference><el-button link type="danger">删除</el-button></template>
        </el-popconfirm>
      </template>
    </el-table-column>
  </el-table>

  <div class="cursor-pagination-footer">
    <span class="muted">已加载 {{ meetings.length }} 条</span>
    <el-button v-if="nextCursor" plain :loading="loadingMore" @click="loadMore">加载更多</el-button>
  </div>
</template>

<script setup lang="ts">
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { api, jsonapiResource, unwrapJsonApiCollection } from '../api'
import { useAutoRefresh } from '../composables/useAutoRefresh'
import { useAsyncAction } from '../composables/useAsyncAction'
import { nextCursorFromDocument, useCursorPagination } from '../composables/useCursorPagination'
import { useResponsive } from '../composables/useResponsive'
import { formatDateTimeUtc8 } from '../utils/datetime'

const router = useRouter()
const { isMobile } = useResponsive()
const { running, runAction } = useAsyncAction()
const topic = ref('')
const creating = ref(false)
const teams = ref<any[]>([])
const selectedTeamId = ref<number | null>(null)
const filters = reactive({ status: '', tag: '' })
const historyMode = ref(false)
const autoRefreshActive = computed(() => !historyMode.value)

const {
  items: meetings,
  nextCursor,
  loading,
  loadingMore,
  loadFirstPage,
  refreshFirstPage,
  loadMore: loadMorePage
} = useCursorPagination<any>(fetchMeetingPage)

function teamName(id?: number | null) {
  return teams.value.find((team) => team.id === id)?.name || (id ? `#${id}` : '-')
}

function canCancel(row: any) {
  return row.status === 'queued' || row.status === 'running'
}

function meetingTags(row: any) {
  return Array.isArray(row?.tags) ? row.tags.filter((tag: unknown): tag is string => typeof tag === 'string' && tag.trim().length > 0) : []
}

function statusType(status: string) {
  if (status === 'completed') return 'success'
  if (status === 'failed') return 'danger'
  if (status === 'cancelled') return 'info'
  return 'warning'
}

function statusLabel(status: string) {
  return ({ queued: '排队中', running: '运行中', completed: '已完成', failed: '失败', cancelled: '已取消' } as Record<string, string>)[status] || status
}

function triggerSourceLabel(value: string) {
  return ({ manual: '手动', message_subscription: '消息订阅', wake_plan: '唤醒计划' } as Record<string, string>)[value] || value || '-'
}

async function loadTeams() {
  teams.value = unwrapJsonApiCollection((await api.get('/research-teams')).data)
  if (!selectedTeamId.value && teams.value.length) selectedTeamId.value = teams.value[0].id
}

async function fetchMeetingPage(cursor?: string) {
  const { data } = await api.get('/meetings', {
    params: {
      researchTeamId: selectedTeamId.value || undefined,
      status: filters.status || undefined,
      tag: filters.tag || undefined,
      'page[limit]': 100,
      'page[cursor]': cursor || undefined
    }
  })
  return {
    items: unwrapJsonApiCollection<any>(data),
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

async function create() {
  if (!topic.value.trim() || !selectedTeamId.value) {
    ElMessage.warning('请选择投研团队并填写会议主题')
    return
  }
  creating.value = true
  try {
    const { data } = await api.post('/meetings', jsonapiResource('meetings', { researchTeamId: selectedTeamId.value, topic: topic.value, triggerSource: 'manual' }))
    topic.value = ''
    router.push(`/meetings/${data.data.id}`)
  } finally {
    creating.value = false
  }
}

async function rerunRecap(row: any) {
  await api.post(`/meetings/${row.id}/recap`, jsonapiResource('meeting-recaps', { force: true }))
  router.push(`/meetings/${row.id}`)
}

async function cancelMeeting(row: any) {
  try {
    await ElMessageBox.confirm(`确定取消会议 #${row.id} 吗？`, '取消会议', { type: 'warning', confirmButtonText: '取消会议', cancelButtonText: '返回' })
  } catch {
    return
  }
  await runAction(`cancelMeeting:${row.id}`, async () => {
    await api.post(`/meetings/${row.id}/cancel`)
    await load()
  }, { success: '会议已取消' })
}

async function deleteMeeting(row: any) {
  await runAction(`deleteMeeting:${row.id}`, async () => {
    await api.delete(`/meetings/${row.id}`)
    await load()
  }, { success: '会议已删除' })
}

onMounted(async () => {
  await loadTeams()
  await load()
})

useAutoRefresh({
  enabled: autoRefreshActive,
  intervalMs: 3000,
  refresh: () => load(true)
})
</script>
