<template>
  <h1 class="page-title">消息库</h1>

  <div class="panel">
    <div class="section-head">
      <h2>已采集消息</h2>
      <div class="toolbar compact-toolbar">
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
        </div>
        <div class="mobile-card__actions">
          <el-button plain :loading="running[`refilter:${message.id}`]" @click="refilterOne(message)">重过滤</el-button>
          <el-popconfirm title="删除这条消息？" @confirm="deleteMessage(message.id)">
            <template #reference><el-button plain type="danger">删除</el-button></template>
          </el-popconfirm>
        </div>
      </div>
      <el-empty v-if="!messages.length" description="暂无消息" />
    </div>

    <el-table v-else v-loading="loading" :data="messages" empty-text="暂无消息">
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
      <el-table-column label="操作" width="180">
        <template #default="{ row }">
          <el-button link type="primary" :loading="running[`refilter:${row.id}`]" @click="refilterOne(row)">重过滤</el-button>
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
import { api, apiErrorText, jsonapiResource, unwrapJsonApiCollection, type IngestedMessage, type MessageSubscription } from '../api'
import { useAutoRefresh } from '../composables/useAutoRefresh'
import { useAsyncAction } from '../composables/useAsyncAction'
import { nextCursorFromDocument, useCursorPagination } from '../composables/useCursorPagination'
import { useResponsive } from '../composables/useResponsive'
import { formatDateTimeUtc8 } from '../utils/datetime'

const subscriptions = ref<MessageSubscription[]>([])
const teams = ref<any[]>([])
const refiltering = ref(false)
const dialogVisible = ref(false)
const historyMode = ref(false)
const { isMobile, isTablet } = useResponsive()
const { running, runAction } = useAsyncAction()
const query = reactive({ q: '', subscriptionId: undefined as number | undefined, researchTeamId: undefined as number | undefined, filterDecision: '' })
const form = reactive({ subscriptionId: undefined as number | undefined, messageTime: '', text: '' })
const dialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '720px'))
const autoRefreshActive = computed(() => !historyMode.value)

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

async function loadSubscriptions() {
  const { data } = await api.get('/message-subscriptions')
  subscriptions.value = unwrapJsonApiCollection(data)
}

async function loadTeams() {
  teams.value = unwrapJsonApiCollection((await api.get('/research-teams')).data)
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
  await Promise.all([loadSubscriptions(), loadTeams()])
  await loadMessages()
})

useAutoRefresh({
  enabled: autoRefreshActive,
  intervalMs: 3000,
  refresh: () => loadMessages(true)
})
</script>
