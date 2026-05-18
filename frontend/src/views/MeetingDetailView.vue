<template>
  <div class="section-head">
    <h1 class="page-title">会议 #{{ id }}</h1>
    <div class="toolbar compact-toolbar">
      <el-button @click="router.push('/meetings')">返回列表</el-button>
      <el-button v-if="meeting?.status === 'completed'" @click="rerunRecap">重新总结</el-button>
      <el-button v-if="canRestart" type="primary" plain @click="restartMeeting">
        {{ canCancel ? '取消并重新开会' : '重新开会' }}
      </el-button>
      <el-button @click="openReferenceDialog">引用会议</el-button>
      <el-button v-if="canCancel" type="danger" plain @click="cancelMeeting">取消会议</el-button>
    </div>
  </div>

  <div class="panel">
    <div class="meeting-head">
      <div>
        <strong>{{ meeting?.topic }}</strong>
        <p class="muted">{{ triggerSourceLabel(meeting?.triggerSource) }} / {{ formatDateTimeUtc8(meeting?.createdAt) }}</p>
      </div>
      <el-tag :type="statusType">{{ meetingStatusLabel(meeting?.status) }}</el-tag>
    </div>
    <div class="toolbar compact-toolbar">
      <el-tag v-for="tag in meeting?.tags || []" :key="tag" class="tag-gap">{{ tag }}</el-tag>
      <span v-if="!(meeting?.tags || []).length" class="muted">暂无标签</span>
    </div>
    <el-progress :percentage="progressPercent" :status="meeting?.status === 'failed' ? 'exception' : undefined" />
    <div class="meeting-stats">
      <span>当前角色：{{ currentRole || '-' }}</span>
      <span>模型调用：{{ modelCallCount }} 次</span>
      <span>完成角色：{{ completedRoleCount }} 个</span>
      <span>错误：{{ errorCount }} 个</span>
      <span>重总结状态：{{ recapStatusLabel(meeting?.recapStatus) }}</span>
    </div>
    <div v-if="roleStates.length" class="role-progress-line">
      <el-tag v-for="role in roleStates" :key="role.name" :type="roleTagType(role.status)">
        {{ role.name }} / {{ role.statusText }}
      </el-tag>
    </div>
  </div>

  <div class="detail-grid">
    <div class="panel chat-panel">
      <BubbleList :list="chatItems" :auto-scroll="true" :show-back-button="true" :max-height="chatMaxHeight" item-key="id">
        <template #avatar="{ item }">
          <div class="chat-avatar" :class="`chat-avatar-${item.kind}`">{{ item.initial }}</div>
        </template>
        <template #header="{ item }">
          <div class="chat-header">
            <strong>{{ item.title }}</strong>
            <el-tag size="small" :type="item.tagType">{{ item.status }}</el-tag>
          </div>
        </template>
        <template #content="{ item }">
          <div class="markdown-body" v-html="item.html"></div>
        </template>
        <template #footer="{ item }">
          <div class="chat-footer">{{ item.time }}</div>
        </template>
      </BubbleList>
      <div class="cursor-pagination-footer">
        <span class="muted">已加载 {{ events.length }} 条记录</span>
        <el-button v-if="eventNextCursor" plain :loading="eventsLoadingMore" @click="loadOlderEvents">加载更早记录</el-button>
      </div>
      <el-empty v-if="!chatItems.length" description="暂无会议记录" />
    </div>

    <div class="detail-side">
      <div class="panel">
        <div class="section-head">
          <h2>引用链</h2>
          <el-button link type="primary" @click="loadReferences">刷新</el-button>
        </div>
        <div v-if="!referenceChain.length" class="muted">暂无引用</div>
        <div
          v-for="(item, index) in referenceChain"
          :key="`${item.id}-${item.depth}`"
          class="reference-card"
          :class="{
            'reference-card-deleted': item.targetDeleted,
            'reference-card-leaf': isLeafReference(index),
            'reference-card-external': item.referenceType !== 'meeting'
          }"
          :style="{ marginLeft: referenceIndent(item.depth) }"
        >
          <div class="reference-path">
            <span class="reference-depth">第 {{ item.depth + 1 }} 层</span>
            <span class="reference-path-text">{{ referencePathText(item, index) }}</span>
          </div>
          <div class="reference-head">
            <strong>{{ item.targetTopicSnapshot }}</strong>
            <div class="toolbar compact-toolbar">
              <el-tag size="small" type="primary">{{ referenceTypeLabel(item.referenceType) }}</el-tag>
              <el-tag size="small" :type="referenceStatusType(item, index)">
                {{ referenceStatusLabel(item, index) }}
              </el-tag>
            </div>
          </div>
          <div class="muted">{{ item.note || '无备注' }}</div>
          <div v-if="item.targetSummarySnapshot" class="muted reference-summary">{{ item.targetSummarySnapshot }}</div>
          <div class="reference-foot">
            <span class="muted">{{ formatDateTimeUtc8(item.createdAt) }}</span>
            <span class="muted">{{ referenceHint(item, index) }}</span>
          </div>
          <div class="toolbar compact-toolbar">
            <el-button v-if="canOpenReference(item)" link type="primary" @click="router.push(`/meetings/${item.targetMeetingId}`)">
              打开会议
            </el-button>
            <span v-else class="muted">{{ closedReferenceLabel(item, index) }}</span>
          </div>
        </div>
      </div>

      <div class="panel">
        <div class="section-head">
          <h2>唤醒计划</h2>
          <el-button link type="primary" @click="loadWakePlans">刷新</el-button>
        </div>
        <div v-if="!wakePlans.length" class="muted">暂无唤醒计划</div>
        <div v-for="plan in wakePlans" :key="plan.id" class="reference-card">
          <div class="reference-head">
            <strong>#{{ plan.id }} {{ wakeTriggerLabel(plan.triggerType) }}</strong>
            <div class="toolbar compact-toolbar">
              <el-tag size="small">{{ wakeStatusLabel(plan.status) }}</el-tag>
              <el-button link type="danger" @click="deleteWakePlan(plan)">删除</el-button>
            </div>
          </div>
          <div>{{ plan.reason }}</div>
          <div class="muted">下次检查：{{ formatDateTimeUtc8(plan.nextCheckAt) }}</div>
        </div>
      </div>
    </div>
  </div>

  <el-dialog
    v-model="referenceDialogVisible"
    title="引用历史会议"
    :fullscreen="isMobile"
    :width="dialogWidth"
  >
    <el-form
      :model="referenceForm"
      :label-width="isMobile ? 'auto' : '90px'"
      :label-position="isMobile ? 'top' : 'right'"
    >
      <el-form-item label="目标会议">
        <el-select v-model="referenceForm.targetMeetingId" filterable style="width: 100%">
          <el-option
            v-for="item in referenceCandidates"
            :key="item.id"
            :label="`#${item.id} ${item.topic}`"
            :value="item.id"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="备注">
        <el-input v-model="referenceForm.note" type="textarea" :rows="3" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="referenceDialogVisible = false">取消</el-button>
      <el-button type="primary" @click="saveReference">保存</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import DOMPurify from 'dompurify'
import { ElMessage, ElMessageBox } from 'element-plus'
import MarkdownIt from 'markdown-it'
import { BubbleList } from 'vue-element-plus-x'
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  api,
  jsonapiResource,
  unwrapJsonApiCollection,
  unwrapJsonApiResource,
  type Meeting,
  type MeetingEvent,
  type MeetingReference,
  type WakePlan
} from '../api'
import { useAutoRefresh } from '../composables/useAutoRefresh'
import { useResponsive } from '../composables/useResponsive'
import { formatDateTimeUtc8 } from '../utils/datetime'

interface ChatItem {
  id: number
  html: string
  initial: string
  kind: string
  placement: 'start'
  shape: 'corner'
  tagType: 'success' | 'danger' | 'warning' | 'info' | 'primary'
  status: string
  time: string
  title: string
  variant: 'filled' | 'outlined' | 'shadow'
}

interface RoleState {
  name: string
  status: string
  statusText: string
}

type ChainItem = MeetingReference & { depth: number }

const route = useRoute()
const router = useRouter()
const { isMobile, isTablet } = useResponsive()
const id = Number(route.params.id)
const meeting = ref<Meeting | null>(null)
const events = ref<MeetingEvent[]>([])
const eventNextCursor = ref('')
const eventsLoadingMore = ref(false)
const wakePlans = ref<WakePlan[]>([])
const referenceChain = ref<ChainItem[]>([])
const referenceCandidates = ref<Meeting[]>([])
const referenceDialogVisible = ref(false)
const referenceForm = reactive({ targetMeetingId: undefined as number | undefined, note: '' })
const markdown = new MarkdownIt({ html: false, linkify: true, breaks: true })

const dialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '620px'))
const chatMaxHeight = computed(() => (isMobile.value ? 'calc(100vh - 300px)' : 'calc(100vh - 360px)'))

const canCancel = computed(() => meeting.value?.status === 'queued' || meeting.value?.status === 'running')
const canRestart = computed(() => Boolean(meeting.value))
const modelCallCount = computed(() => events.value.filter((event) => payloadStatus(event) === 'model_call').length)
const completedRoleCount = computed(() => events.value.filter((event) => payloadStatus(event) === 'role_completed').length)
const errorCount = computed(() => events.value.filter((event) => event.type === 'error').length)
const currentRole = computed(() => {
  if (meeting.value?.status === 'completed') return '已完成'
  if (meeting.value?.status === 'failed') return '失败'
  if (meeting.value?.status === 'cancelled') return '已取消'
  if (meeting.value?.recapStatus === 'running') return '主持人总结中'
  const activeEvent = [...events.value].reverse().find((event) =>
    ['role_started', 'model_call', 'moderator_kickoff', 'moderator_review', 'recap_completed'].includes(payloadStatus(event))
  )
  return payloadText(activeEvent, 'role_name') || activeEvent?.roleKey
})
const progressPercent = computed(() => {
  if (meeting.value?.status === 'completed') return 100

  const roleProgressEvents = events.value.filter((event) =>
    ['role_started', 'model_call', 'role_completed', 'role_error', 'skipped'].includes(payloadStatus(event))
  )
  const totalUnits = roleProgressEvents.reduce((max, event) => {
    const progress = meetingProgress(event)
    return progress && progress.total > max ? progress.total : max
  }, 0)
  const completedRoleUnits = roleProgressEvents.reduce((max, event) => {
    const progress = meetingProgress(event)
    return progress && progress.current > max ? progress.current : max
  }, 0)
  const kickoffDone = events.value.some((event) => payloadStatus(event) === 'moderator_kickoff') ? 1 : 0
  const reviewDone = events.value.filter((event) => payloadStatus(event) === 'moderator_review').length
  const recapDone = events.value.some((event) => payloadStatus(event) === 'recap_completed') ? 1 : 0

  if (totalUnits > 0) {
    const completedUnits = Math.min(totalUnits, completedRoleUnits + kickoffDone + reviewDone + recapDone)
    let percent = Math.round((completedUnits / totalUnits) * 100)
    if (meeting.value?.recapStatus === 'running' && recapDone === 0) {
      percent = Math.max(percent, 95)
    }
    if (meeting.value?.status === 'failed' || meeting.value?.status === 'cancelled') {
      return Math.max(1, Math.min(99, percent))
    }
    return Math.max(kickoffDone > 0 ? 5 : 1, Math.min(99, percent))
  }

  if (meeting.value?.recapStatus === 'running') return 95
  if (events.value.some((event) => payloadStatus(event) === 'moderator_kickoff')) return 5
  if (events.value.some((event) => payloadStatus(event) === 'running')) return 2
  return meeting.value?.status === 'queued' ? 0 : 1
})
const statusType = computed(() => {
  if (meeting.value?.status === 'completed') return 'success'
  if (meeting.value?.status === 'failed') return 'danger'
  if (meeting.value?.status === 'cancelled') return 'info'
  return 'warning'
})
const roleStates = computed<RoleState[]>(() => {
  const byName = new Map<string, RoleState>()
  for (const event of events.value) {
    const roleKey = event.roleKey || payloadText(event, 'role_name')
    if (!roleKey) continue
    const roleName = roleDisplayName(event)
    const status = payloadStatus(event)
    if (status === 'role_started') byName.set(roleKey, { name: roleName, status, statusText: '工作中' })
    if (status === 'model_call') byName.set(roleKey, { name: roleName, status, statusText: '调用模型' })
    if (status === 'role_completed') byName.set(roleKey, { name: roleName, status, statusText: '完成' })
    if (status === 'role_error') byName.set(roleKey, { name: roleName, status, statusText: '失败' })
  }
  return [...byName.values()]
})
const chatItems = computed<ChatItem[]>(() =>
  events.value.map((event) => {
    const kind = eventKind(event)
    return {
      id: event.id,
      html: renderMarkdown(event.content),
      initial: initialFor(event),
      kind,
      placement: 'start',
      shape: 'corner',
      tagType: eventTagType(event),
      status: eventStatusLabel(payloadStatus(event) || event.type),
      time: formatDateTimeUtc8(event.createdAt),
      title: eventTitle(event),
      variant: kind === 'conclusion' ? 'shadow' : kind === 'system' ? 'outlined' : 'filled'
    }
  })
)

async function loadMeeting() {
  meeting.value = unwrapJsonApiResource((await api.get(`/meetings/${id}`)).data)
}

async function fetchEventPage(cursor?: string) {
  const { data } = await api.get(`/meetings/${id}/events`, {
    params: { 'page[limit]': 100, 'page[cursor]': cursor || undefined }
  })
  return {
    rows: unwrapJsonApiCollection<MeetingEvent>(data),
    nextCursor: String(data?.meta?.nextCursor || '')
  }
}

async function loadEvents(background = false) {
  const page = await fetchEventPage()
  eventNextCursor.value = page.nextCursor
  events.value = background ? dedupeEvents(events.value.concat(page.rows)) : dedupeEvents(page.rows)
}

async function loadOlderEvents() {
  if (!eventNextCursor.value || eventsLoadingMore.value) return
  eventsLoadingMore.value = true
  try {
    const page = await fetchEventPage(eventNextCursor.value)
    eventNextCursor.value = page.nextCursor
    events.value = dedupeEvents(page.rows.concat(events.value))
  } finally {
    eventsLoadingMore.value = false
  }
}

async function loadWakePlans() {
  wakePlans.value = unwrapJsonApiCollection((await api.get('/wake-plans', { params: { meetingId: id } })).data)
}

async function deleteWakePlan(plan: WakePlan) {
  await ElMessageBox.confirm(`确定删除唤醒计划 #${plan.id} 吗？`, '删除唤醒计划', { type: 'warning' })
  await api.delete(`/wake-plans/${plan.id}`)
  ElMessage.success('唤醒计划已删除')
  await loadWakePlans()
}

async function buildReferenceChain(meetingId: number, depth = 0, seen = new Set<number>()) {
  if (seen.has(meetingId) || depth > 3) return []
  seen.add(meetingId)
  const { data } = await api.get(`/meetings/${meetingId}/references`)
  const references = unwrapJsonApiCollection<MeetingReference>(data)
  const items: ChainItem[] = []
  for (const item of references) {
    items.push({ ...item, depth })
    if (item.targetMeetingId && !item.targetDeleted) {
      items.push(...(await buildReferenceChain(item.targetMeetingId, depth + 1, seen)))
    }
  }
  return items
}

async function loadReferences() {
  referenceChain.value = await buildReferenceChain(id)
}

async function loadInitial() {
  await Promise.all([loadMeeting(), loadEvents()])
  await Promise.all([loadWakePlans(), loadReferences()])
}

async function cancelMeeting() {
  meeting.value = unwrapJsonApiResource((await api.post(`/meetings/${id}/cancel`)).data)
  ElMessage.success('会议已取消')
}

async function restartMeeting() {
  const { data } = await api.post(`/meetings/${id}/restart`)
  const restarted = unwrapJsonApiResource<Meeting>(data)
  if (!restarted) return
  ElMessage.success(`已重新开会，新的会议编号：#${restarted.id}`)
  await router.push(`/meetings/${restarted.id}`)
}

async function rerunRecap() {
  await api.post(`/meetings/${id}/recap`, jsonapiResource('meeting-recaps', { requestedBy: 'ui' }))
  ElMessage.success('已触发主持人重新总结')
  await loadMeeting()
}

async function openReferenceDialog() {
  referenceCandidates.value = unwrapJsonApiCollection<Meeting>((await api.get('/meetings', { params: { 'page[limit]': 200 } })).data).filter(
    (item: Meeting) => item.id !== id
  )
  referenceDialogVisible.value = true
}

async function saveReference() {
  if (!referenceForm.targetMeetingId) {
    ElMessage.warning('请选择目标会议')
    return
  }
  await api.post(
    `/meetings/${id}/references`,
    jsonapiResource('meeting-references', {
      targetMeetingId: referenceForm.targetMeetingId,
      note: referenceForm.note || null
    })
  )
  referenceDialogVisible.value = false
  referenceForm.targetMeetingId = undefined
  referenceForm.note = ''
  ElMessage.success('引用已保存')
  await loadReferences()
}

function payloadStatus(event?: MeetingEvent) {
  return String(event?.payload?.status || '')
}

function dedupeEvents(rows: MeetingEvent[]) {
  const byId = new Map<number, MeetingEvent>()
  for (const row of rows) {
    byId.set(row.id, row)
  }
  return [...byId.values()].sort((left, right) => left.sequence - right.sequence || left.id - right.id)
}

function meetingStatusLabel(value: string | null | undefined) {
  const map: Record<string, string> = {
    queued: '排队中',
    running: '运行中',
    completed: '已完成',
    failed: '失败',
    cancelled: '已取消'
  }
  return map[String(value || '')] || (value ? String(value) : '加载中')
}

function recapStatusLabel(value: string | null | undefined) {
  const map: Record<string, string> = {
    queued: '排队中',
    running: '总结中',
    completed: '已完成',
    failed: '失败',
    cancelled: '已取消'
  }
  return map[String(value || '')] || '-'
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

function eventStatusLabel(value: string | null | undefined) {
  const map: Record<string, string> = {
    running: '运行中',
    model_call: '调用模型',
    role_started: '角色开始',
    role_completed: '角色完成',
    role_error: '角色失败',
    skipped: '已跳过',
    moderator_kickoff: '主持人开场',
    moderator_review: '主持人复核',
    recap_completed: '总结完成',
    wake_plan_created: '已创建唤醒计划',
    error: '错误',
    conclusion: '结论',
    tool_call: '调用工具',
    tool_result: '工具结果',
    role_message: '角色发言'
  }
  return map[String(value || '')] || value || '-'
}

function payloadText(event: MeetingEvent | undefined, key: string) {
  const value = event?.payload?.[key]
  return typeof value === 'string' ? value : ''
}

function meetingProgress(event?: MeetingEvent) {
  const progress = (event?.payload as { progress?: { current?: unknown; total?: unknown } } | undefined)?.progress
  const current = Number(progress?.current)
  const total = Number(progress?.total)
  if (!Number.isFinite(current) || !Number.isFinite(total) || total <= 0 || current < 0) return null
  return { current, total }
}

function renderMarkdown(content: string) {
  return DOMPurify.sanitize(markdown.render(content || ''))
}

function eventKind(event: MeetingEvent) {
  if (event.type === 'error') return 'error'
  if (event.type === 'conclusion') return 'conclusion'
  if (event.type === 'tool_call' || event.type === 'tool_result') return 'tool'
  if (event.type === 'role_message') return 'role'
  return 'system'
}

function eventTitle(event: MeetingEvent) {
  if (event.type === 'conclusion') return '会议结论'
  if (event.type === 'error') return roleDisplayName(event) || '错误'
  if (event.type === 'tool_call') return roleDisplayName(event) || '工具调用'
  if (event.type === 'tool_result') return roleDisplayName(event) || '工具结果'
  if (event.type === 'role_message') return roleDisplayName(event) || '角色发言'
  return '系统'
}

function roleDisplayName(event: MeetingEvent) {
  const directName = payloadText(event, 'role_name')
  if (directName) return directName
  if (!event.roleKey) return ''
  const started = [...events.value].reverse().find((item) => item.roleKey === event.roleKey && payloadText(item, 'role_name'))
  return payloadText(started, 'role_name') || event.roleKey
}

function initialFor(event: MeetingEvent) {
  const title = eventTitle(event)
  if (event.type === 'conclusion') return '结'
  if (event.type === 'error') return '!'
  if (event.type === 'tool_call' || event.type === 'tool_result') return '工'
  return title.slice(0, 1).toUpperCase()
}

function eventTagType(event: MeetingEvent) {
  if (event.type === 'error' || payloadStatus(event) === 'role_error') return 'danger'
  if (payloadStatus(event) === 'role_completed' || event.type === 'conclusion') return 'success'
  if (event.type === 'tool_call' || event.type === 'tool_result') return 'primary'
  return 'warning'
}

function roleTagType(status: string) {
  if (status === 'role_completed') return 'success'
  if (status === 'role_error') return 'danger'
  if (status === 'model_call') return 'primary'
  return 'warning'
}

function referenceTypeLabel(type: string) {
  if (type === 'telegram_message') return '采集消息'
  if (type === 'meeting') return '历史会议'
  return type || '引用'
}

function referenceStatusType(item: ChainItem, index: number) {
  if (item.targetDeleted) return 'info'
  if (item.referenceType !== 'meeting') return 'warning'
  if (isLeafReference(index)) return 'success'
  return 'primary'
}

function referenceStatusLabel(item: ChainItem, index: number) {
  if (item.targetDeleted) return '目标已删除'
  if (item.referenceType !== 'meeting') return '外部快照'
  if (isLeafReference(index)) return '链路终点'
  return '继续展开'
}

function canOpenReference(item: ChainItem) {
  return Boolean(!item.targetDeleted && item.targetMeetingId)
}

function isLeafReference(index: number) {
  const current = referenceChain.value[index]
  const next = referenceChain.value[index + 1]
  return !next || next.depth <= current.depth
}

function referencePathText(item: ChainItem, index: number) {
  if (item.targetDeleted) return '引用对象已删除，链路在此保留快照'
  if (item.referenceType === 'telegram_message') return '来自采集消息触发'
  return isLeafReference(index) ? '这是当前可追溯到的链路终点' : '继续引用下游会议'
}

function referenceHint(item: ChainItem, index: number) {
  if (item.targetDeleted) return '目标已删除，不能继续展开'
  if (item.referenceType !== 'meeting') return '保留外部消息快照'
  return isLeafReference(index) ? '没有更深层引用' : '可以继续查看下游会议'
}

function closedReferenceLabel(item: ChainItem, index: number) {
  if (item.targetDeleted) return '已删除节点'
  if (item.referenceType !== 'meeting') return '外部引用'
  if (isLeafReference(index)) return '链路终点'
  return '不可打开'
}

function referenceIndent(depth: number) {
  const unit = isMobile.value ? 8 : 12
  const max = isMobile.value ? 24 : 48
  return `${Math.min(depth * unit, max)}px`
}

onMounted(async () => {
  await loadInitial()
})

useAutoRefresh({
  intervalMs: 2000,
  refresh: async () => {
    await Promise.all([loadMeeting(), loadEvents(true)])
  }
})

useAutoRefresh({
  intervalMs: 10000,
  refresh: async () => {
    await Promise.all([loadWakePlans(), loadReferences()])
  }
})
</script>

<style scoped>
.reference-card-deleted {
  border-style: dashed;
  background: #f8fafc;
}

.reference-card-leaf {
  box-shadow: inset 0 0 0 1px rgba(34, 197, 94, 0.08);
}

.reference-card-external {
  border-color: #cbd5e1;
}

.reference-path {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.reference-depth {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-width: 36px;
  height: 22px;
  padding: 0 8px;
  border-radius: 999px;
  background: #eef2ff;
  color: #3730a3;
  font-size: 12px;
  font-weight: 600;
}

.reference-path-text {
  color: #64748b;
  font-size: 12px;
  line-height: 1.5;
}

.reference-summary {
  margin-top: 6px;
  white-space: pre-wrap;
  word-break: break-word;
}

.reference-foot {
  display: flex;
  flex-wrap: wrap;
  justify-content: space-between;
  gap: 8px;
  margin-top: 8px;
}
</style>
