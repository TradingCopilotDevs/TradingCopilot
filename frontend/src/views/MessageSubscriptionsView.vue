<template>
  <h1 class="page-title">消息订阅器</h1>

  <div class="metric-grid message-summary">
    <div class="metric">
      <span>启用来源</span>
      <strong>{{ enabledSourceCount }}</strong>
    </div>
    <div class="metric">
      <span>RSS/Atom</span>
      <strong>{{ providerCount('rss_feed') }}</strong>
    </div>
    <div class="metric">
      <span>Telegram</span>
      <strong>{{ providerCount('telegram_channel') }}</strong>
    </div>
    <div class="metric">
      <span>采集异常</span>
      <strong>{{ failedSourceCount }}</strong>
    </div>
  </div>

  <div class="panel">
    <div class="section-head">
      <h2>订阅来源</h2>
      <div class="toolbar compact-toolbar">
        <el-button :icon="Refresh" :loading="collecting" @click="collect()">采集全部</el-button>
        <el-button type="primary" :icon="Plus" @click="openSubscription()">新增来源</el-button>
      </div>
    </div>

    <div class="toolbar">
      <el-segmented v-model="sourceProviderFilter" :options="providerFilterOptions" />
      <el-input v-model="sourceQuery" clearable placeholder="搜索来源或名称" />
      <el-button :icon="Refresh" :loading="loading" @click="load()">刷新</el-button>
    </div>

    <div v-if="isMobile" v-loading="loading" class="mobile-card-list">
      <div v-for="source in filteredSubscriptions" :key="source.id" class="mobile-card">
        <div class="mobile-card__header">
          <div>
            <h3 class="mobile-card__title">{{ source.title || source.sourceRef }}</h3>
            <div class="muted">{{ source.sourceRef }}</div>
          </div>
          <div class="source-card-tags">
            <el-tag :type="providerTagType(source.provider)" effect="plain">{{ providerLabel(source.provider) }}</el-tag>
            <el-tag :type="source.enabled ? 'success' : 'info'">{{ source.enabled ? '启用' : '停用' }}</el-tag>
          </div>
        </div>
        <div class="mobile-card__meta">
          <div class="mobile-card__meta-row"><span class="mobile-card__meta-label">过滤器</span><span>{{ source.filterName || '-' }}</span></div>
          <div class="mobile-card__meta-row"><span class="mobile-card__meta-label">团队</span><span><el-tag v-for="teamId in source.teamIds || []" :key="teamId" size="small" class="tag-gap">{{ teamName(teamId) }}</el-tag></span></div>
          <div class="mobile-card__meta-row"><span class="mobile-card__meta-label">轮询</span><span>{{ intervalLabel(source.pollIntervalSeconds) }}</span></div>
          <div class="mobile-card__meta-row"><span class="mobile-card__meta-label">下次</span><span>{{ formatDateTimeUtc8(source.nextCollectAt) || '-' }}</span></div>
          <div v-if="source.lastCollectError" class="mobile-card__meta-row"><span class="mobile-card__meta-label">错误</span><span class="source-error">{{ source.lastCollectError }}</span></div>
        </div>
        <div class="mobile-card__actions">
          <el-button plain :icon="Edit" @click="openSubscription(source)">编辑</el-button>
          <el-button plain :loading="running[`testSubscription:${source.id}`]" @click="testSaved(source)">测试</el-button>
          <el-button plain :icon="Refresh" :loading="running[`collect:${source.id}`]" @click="collect(source)">采集</el-button>
          <el-button plain type="primary" :loading="running[`toggleSubscription:${source.id}`]" @click="toggle(source)">{{ source.enabled ? '停用' : '启用' }}</el-button>
          <el-popconfirm title="删除该来源及其消息？" @confirm="deleteSubscription(source)">
            <template #reference><el-button plain type="danger" :icon="Delete">删除</el-button></template>
          </el-popconfirm>
        </div>
      </div>
      <el-empty v-if="!filteredSubscriptions.length" description="暂无订阅来源" />
    </div>

    <div v-else class="table-scroll">
      <el-table v-loading="loading" :data="filteredSubscriptions" empty-text="暂无订阅来源">
        <el-table-column label="来源" min-width="260">
          <template #default="{ row }">
            <div class="source-title-line">
              <strong>{{ row.title || row.sourceRef }}</strong>
              <el-tag size="small" :type="providerTagType(row.provider)" effect="plain">{{ providerLabel(row.provider) }}</el-tag>
            </div>
            <small class="muted source-ref-line">{{ row.sourceRef }}</small>
          </template>
        </el-table-column>
        <el-table-column label="过滤/团队" min-width="240">
          <template #default="{ row }">
            <div>{{ row.filterName || '-' }}</div>
            <div>
              <el-tag v-for="teamId in row.teamIds || []" :key="teamId" size="small" class="tag-gap">{{ teamName(teamId) }}</el-tag>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="轮询" width="120">
          <template #default="{ row }">{{ intervalLabel(row.pollIntervalSeconds) }}</template>
        </el-table-column>
        <el-table-column label="采集状态" min-width="240">
          <template #default="{ row }">
            <div>上次 {{ formatDateTimeUtc8(row.lastCollectedAt) || '-' }}</div>
            <small class="muted">下次 {{ formatDateTimeUtc8(row.nextCollectAt) || '-' }}</small>
            <small v-if="row.lastCollectError" class="source-error">{{ row.lastCollectError }}</small>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }"><el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '启用' : '停用' }}</el-tag></template>
        </el-table-column>
        <el-table-column label="操作" width="300" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" :icon="Edit" @click="openSubscription(row)">编辑</el-button>
            <el-button link type="primary" :loading="running[`testSubscription:${row.id}`]" @click="testSaved(row)">测试</el-button>
            <el-button link type="primary" :loading="running[`collect:${row.id}`]" @click="collect(row)">采集</el-button>
            <el-button link type="primary" :loading="running[`toggleSubscription:${row.id}`]" @click="toggle(row)">{{ row.enabled ? '停用' : '启用' }}</el-button>
            <el-popconfirm title="删除该来源及其消息？" @confirm="deleteSubscription(row)">
              <template #reference><el-button link type="danger" :icon="Delete">删除</el-button></template>
            </el-popconfirm>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>

  <div class="subscription-grid">
    <div class="panel">
      <div class="section-head">
        <h2>消息过滤器</h2>
        <el-button type="primary" :icon="Plus" @click="openFilter()">新增过滤器</el-button>
      </div>
      <el-table v-loading="loading" :data="filters" empty-text="暂无过滤器">
        <el-table-column prop="name" label="名称" min-width="150" />
        <el-table-column label="模型" min-width="180">
          <template #default="{ row }">
            <span>{{ providerName(row.providerId) || '未绑定' }}</span>
            <small class="muted model-line">{{ row.model || '服务商默认模型' }}</small>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="120">
          <template #default="{ row }">
            <el-tag v-if="row.isDefault" type="primary" effect="plain">默认</el-tag>
            <el-tag class="tag-gap" :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '启用' : '停用' }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="170">
          <template #default="{ row }">
            <el-button link type="primary" @click="openFilter(row)">编辑</el-button>
            <el-button link type="primary" :disabled="row.isDefault" :loading="running[`defaultFilter:${row.id}`]" @click="setDefaultFilter(row)">设默认</el-button>
          </template>
        </el-table-column>
      </el-table>
    </div>

    <div class="panel">
      <div class="section-head">
        <h2>Provider 配置</h2>
        <el-button :icon="Refresh" @click="loadStatus">刷新</el-button>
      </div>
      <el-tabs v-model="providerConfigTab">
        <el-tab-pane label="Telegram" name="telegram">
          <div class="credential-status">
            <el-tag :type="status.hasAppId ? 'success' : 'danger'">App ID {{ status.hasAppId ? '已配置' : '未配置' }}</el-tag>
            <el-tag :type="status.hasAppHash ? 'success' : 'danger'">App Hash {{ status.hasAppHash ? '已配置' : '未配置' }}</el-tag>
            <el-tag :type="status.hasSession ? 'success' : 'warning'">会话 {{ status.hasSession ? '已登录' : '未登录' }}</el-tag>
          </div>
          <el-form class="dialog-form" :model="appConfig" :label-width="isMobile ? 'auto' : '100px'" :label-position="isMobile ? 'top' : 'right'">
            <el-form-item label="App ID"><el-input v-model="appConfig.appId" placeholder="123456" /></el-form-item>
            <el-form-item label="App Hash"><el-input v-model="appConfig.appHash" type="password" show-password /></el-form-item>
            <el-button type="primary" :loading="running.saveAppConfig" @click="saveAppConfig">保存配置</el-button>
          </el-form>
          <el-divider />
          <el-form :model="loginForm" :label-width="isMobile ? 'auto' : '100px'" :label-position="isMobile ? 'top' : 'right'">
            <el-form-item label="手机号"><el-input v-model="loginForm.phone" placeholder="+8613800000000" /></el-form-item>
            <el-form-item label="验证码"><el-input v-model="loginForm.code" /></el-form-item>
            <el-form-item label="2FA 密码"><el-input v-model="loginForm.password" type="password" show-password /></el-form-item>
            <div class="toolbar compact-toolbar compact-toolbar-stack">
              <el-button :loading="sendingCode" :disabled="!canStartLogin" @click="startLogin">发送验证码</el-button>
              <el-button type="primary" :loading="verifyingCode" :disabled="!canVerifyLogin" @click="verifyLogin">保存会话</el-button>
            </div>
          </el-form>
        </el-tab-pane>
      </el-tabs>
    </div>
  </div>

  <el-dialog v-model="subscriptionDialog" title="订阅来源" :width="dialogWidth" :fullscreen="isMobile">
    <el-form :model="subscriptionForm" :label-width="isMobile ? 'auto' : '120px'" :label-position="isMobile ? 'top' : 'right'">
      <el-form-item label="Provider">
        <el-segmented v-model="subscriptionForm.provider" :options="providerOptions" />
      </el-form-item>
      <el-form-item label="来源">
        <el-input v-model="subscriptionForm.sourceRef" :placeholder="sourcePlaceholder" />
      </el-form-item>
      <el-form-item label="名称">
        <el-input v-model="subscriptionForm.title" placeholder="可留空，测试或采集后自动补全" />
      </el-form-item>
      <el-form-item label="过滤器">
        <el-select v-model="subscriptionForm.filterId" placeholder="选择过滤器" style="width: 100%">
          <el-option v-for="filter in enabledFilters" :key="filter.id" :label="filter.isDefault ? `${filter.name}（默认）` : filter.name" :value="filter.id" />
        </el-select>
      </el-form-item>
      <el-form-item label="投研团队">
        <el-select v-model="subscriptionForm.teamIds" multiple filterable placeholder="选择团队" style="width: 100%">
          <el-option v-for="team in teams" :key="team.id" :label="team.name" :value="team.id" />
        </el-select>
      </el-form-item>
      <el-form-item label="回填条数"><el-input-number v-model="subscriptionForm.backfillLimit" :min="0" :max="5000" /></el-form-item>
      <el-form-item label="轮询间隔"><el-input-number v-model="subscriptionForm.pollIntervalSeconds" :min="30" :max="86400" /><span class="form-suffix">秒</span></el-form-item>
      <el-form-item label="启用"><el-switch v-model="subscriptionForm.enabled" /></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="subscriptionDialog = false">取消</el-button>
      <el-button :loading="testingDraft" @click="testDraft">测试当前源</el-button>
      <el-button type="primary" :loading="running.saveSubscription" @click="saveSubscription">保存来源</el-button>
    </template>
  </el-dialog>

  <el-dialog v-model="filterDialog" title="消息过滤器" :width="filterDialogWidth" :fullscreen="isMobile">
    <el-form :model="filterForm" :label-width="isMobile ? 'auto' : '120px'" :label-position="isMobile ? 'top' : 'right'">
      <el-form-item label="名称"><el-input v-model="filterForm.name" /></el-form-item>
      <el-form-item label="说明"><el-input v-model="filterForm.description" type="textarea" :rows="2" /></el-form-item>
      <el-form-item label="服务商">
        <el-select v-model="filterForm.providerId" clearable placeholder="选择服务商" style="width: 100%" @change="filterForm.model = ''">
          <el-option v-for="provider in providers" :key="provider.id" :label="provider.name" :value="provider.id" />
        </el-select>
      </el-form-item>
      <el-form-item label="模型">
        <el-select v-if="filterForm.providerId && providerModels[filterForm.providerId]?.length" v-model="filterForm.model" clearable filterable placeholder="留空使用服务商默认模型" style="width: 100%">
          <el-option v-for="model in providerModels[filterForm.providerId]" :key="model.modelId" :label="model.displayName" :value="model.modelId" />
        </el-select>
        <el-input v-else v-model="filterForm.model" placeholder="可手动填写模型 ID" />
      </el-form-item>
      <el-form-item label="能力">
        <el-tag v-for="tool in filterForm.toolNames" :key="tool" size="small" class="tag-gap">{{ tool }}</el-tag>
        <el-tag v-for="skill in filterForm.skillNames" :key="skill" size="small" type="success" class="tag-gap">{{ skill }}</el-tag>
      </el-form-item>
      <el-form-item label="提示词"><el-input v-model="filterForm.promptTemplate" type="textarea" :rows="10" /></el-form-item>
      <el-form-item label="启用"><el-switch v-model="filterForm.enabled" /></el-form-item>
      <el-form-item label="设为默认"><el-switch v-model="filterForm.isDefault" /></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="filterDialog = false">取消</el-button>
      <el-button type="primary" :loading="running.saveFilter" @click="saveFilter">保存过滤器</el-button>
    </template>
  </el-dialog>

  <el-dialog v-model="testVisible" title="订阅源测试结果" :width="testDialogWidth" :fullscreen="isMobile">
    <el-descriptions :column="1" border>
      <el-descriptions-item label="状态">{{ testResult?.status || '-' }}</el-descriptions-item>
      <el-descriptions-item label="来源">{{ testResult?.title }} {{ testResult?.sourceRef }}</el-descriptions-item>
      <el-descriptions-item label="消息编号">{{ testResult?.sourceMessageId || '-' }}</el-descriptions-item>
      <el-descriptions-item label="时间">{{ formatDateTimeUtc8(testResult?.messageTime) || '-' }}</el-descriptions-item>
    </el-descriptions>
    <div class="message-preview">{{ testResult?.text || '没有可展示的最近消息' }}</div>
  </el-dialog>
</template>

<script setup lang="ts">
import { Delete, Edit, Plus, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { api, apiErrorText, jsonapiResource, unwrapJsonApiCollection, unwrapJsonApiResource, type MessageSubscription, type MessageSubscriptionFilter } from '../api'
import { useAutoRefresh } from '../composables/useAutoRefresh'
import { useAsyncAction } from '../composables/useAsyncAction'
import { useResponsive } from '../composables/useResponsive'
import { formatDateTimeUtc8 } from '../utils/datetime'

type SourceProvider = 'telegram_channel' | 'rss_feed'

interface Status {
  hasAppId: boolean
  hasAppHash: boolean
  hasSession: boolean
}

interface TestResult {
  status: string
  sourceRef: string
  title?: string
  sourceMessageId?: string
  messageTime?: string
  text?: string
}

const { isMobile, isTablet } = useResponsive()
const { running, runAction } = useAsyncAction()
const appConfig = reactive({ appId: '', appHash: '' })
const loginForm = reactive({ phone: '', code: '', password: '', phoneCodeHash: '' })
const status = reactive<Status>({ hasAppId: false, hasAppHash: false, hasSession: false })
const subscriptionForm = reactive({ id: null as number | null, provider: 'telegram_channel' as SourceProvider, title: '', sourceRef: '', filterId: null as number | null, teamIds: [] as number[], backfillLimit: 20, pollIntervalSeconds: 30, enabled: true })
const filterForm = reactive<any>({ id: null, name: '', description: '', promptTemplate: '', providerId: null, model: '', enabled: true, isDefault: false, toolNames: [], skillNames: [] })
const subscriptions = ref<MessageSubscription[]>([])
const filters = ref<MessageSubscriptionFilter[]>([])
const providers = ref<any[]>([])
const teams = ref<any[]>([])
const providerModels = reactive<Record<number, any[]>>({})
const sourceProviderFilter = ref('all')
const sourceQuery = ref('')
const providerConfigTab = ref('telegram')
const sendingCode = ref(false)
const verifyingCode = ref(false)
const testingDraft = ref(false)
const collecting = ref(false)
const loading = ref(false)
const testVisible = ref(false)
const testResult = ref<TestResult | null>(null)
const subscriptionDialog = ref(false)
const filterDialog = ref(false)

const providerOptions = [
  { label: 'Telegram', value: 'telegram_channel' },
  { label: 'RSS/Atom', value: 'rss_feed' }
]
const providerFilterOptions = [{ label: '全部', value: 'all' }, ...providerOptions]
const dialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '760px'))
const filterDialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '92vw' : '900px'))
const testDialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '680px'))
const defaultFilterId = computed(() => filters.value.find((filter) => filter.isDefault)?.id)
const enabledFilters = computed(() => filters.value.filter((filter) => filter.enabled))
const enabledSourceCount = computed(() => subscriptions.value.filter((source) => source.enabled).length)
const failedSourceCount = computed(() => subscriptions.value.filter((source) => !!source.lastCollectError).length)
const canStartLogin = computed(() => status.hasAppId && status.hasAppHash && !!loginForm.phone.trim())
const canVerifyLogin = computed(() => !!loginForm.phoneCodeHash && !!loginForm.phone.trim() && !!loginForm.code.trim())
const sourcePlaceholder = computed(() => subscriptionForm.provider === 'rss_feed' ? 'https://example.com/feed.xml' : '频道链接、@用户名或 -100 编号')
const filteredSubscriptions = computed(() => {
  const query = sourceQuery.value.trim().toLowerCase()
  return subscriptions.value.filter((source) => {
    if (sourceProviderFilter.value !== 'all' && source.provider !== sourceProviderFilter.value) return false
    if (!query) return true
    return `${source.title} ${source.sourceRef}`.toLowerCase().includes(query)
  })
})

function providerCount(provider: SourceProvider) {
  return subscriptions.value.filter((source) => source.provider === provider).length
}

function providerLabel(provider: string) {
  return provider === 'rss_feed' ? 'RSS/Atom' : provider === 'telegram_channel' ? 'Telegram' : provider
}

function providerTagType(provider: string) {
  return provider === 'rss_feed' ? 'warning' : 'primary'
}

function providerName(providerId?: number | null) {
  return providers.value.find((provider) => provider.id === providerId)?.name
}

function teamName(teamId: number) {
  return teams.value.find((team) => team.id === teamId)?.name || `#${teamId}`
}

function intervalLabel(seconds?: number) {
  if (!seconds) return '-'
  if (seconds < 60) return `${seconds} 秒`
  if (seconds < 3600) return `${Math.round(seconds / 60)} 分钟`
  return `${Math.round(seconds / 3600)} 小时`
}

async function loadStatus() {
  const { data } = await api.get('/message-subscriptions/app-config')
  Object.assign(status, unwrapJsonApiResource(data) || {})
}

async function loadProviders() {
  providers.value = unwrapJsonApiCollection((await api.get('/ai/providers')).data)
  await Promise.all(providers.value.map((provider) => loadProviderModels(provider.id)))
}

async function loadProviderModels(providerId: number) {
  providerModels[providerId] = unwrapJsonApiCollection((await api.get(`/ai/providers/${providerId}/models`)).data)
}

async function loadFilters() {
  filters.value = unwrapJsonApiCollection<MessageSubscriptionFilter>((await api.get('/message-subscription-filters')).data)
}

async function loadSubscriptions() {
  subscriptions.value = unwrapJsonApiCollection<MessageSubscription>((await api.get('/message-subscriptions')).data)
}

async function loadTeams() {
  teams.value = unwrapJsonApiCollection((await api.get('/research-teams')).data)
}

async function load(background = false) {
  if (!background) loading.value = true
  try {
    await Promise.all([loadStatus(), loadProviders(), loadFilters(), loadSubscriptions(), loadTeams()])
  } finally {
    if (!background) loading.value = false
  }
}

async function refreshRuntimeState() {
  await Promise.all([loadStatus(), loadSubscriptions()])
}

function openSubscription(row?: MessageSubscription) {
  const provider = (row?.provider as SourceProvider | undefined) ?? 'telegram_channel'
  Object.assign(subscriptionForm, {
    id: row?.id ?? null,
    provider,
    title: row?.title ?? '',
    sourceRef: row?.sourceRef ?? '',
    filterId: row?.filterId ?? defaultFilterId.value ?? enabledFilters.value[0]?.id ?? null,
    teamIds: [...(row?.teamIds ?? [])],
    backfillLimit: row?.backfillLimit ?? 20,
    pollIntervalSeconds: row?.pollIntervalSeconds ?? (provider === 'rss_feed' ? 900 : 30),
    enabled: row?.enabled ?? true
  })
  subscriptionDialog.value = true
}

function openFilter(row?: MessageSubscriptionFilter) {
  Object.assign(filterForm, {
    id: row?.id ?? null,
    name: row?.name ?? '',
    description: row?.description ?? '',
    promptTemplate: row?.promptTemplate ?? '',
    providerId: row?.providerId ?? null,
    model: row?.model ?? '',
    enabled: row?.enabled ?? true,
    isDefault: row?.isDefault ?? false,
    toolNames: [...(row?.toolNames ?? [])],
    skillNames: [...(row?.skillNames ?? [])]
  })
  filterDialog.value = true
}

async function saveAppConfig() {
  if (!appConfig.appId || !appConfig.appHash) {
    ElMessage.warning('App ID 和 App Hash 都必须填写')
    return
  }
  await runAction('saveAppConfig', async () => {
    const { data } = await api.post('/message-subscriptions/app-config', jsonapiResource('message-subscription-app-configs', appConfig))
    Object.assign(status, unwrapJsonApiResource(data) || {})
    appConfig.appId = ''
    appConfig.appHash = ''
  }, { success: '配置已保存' })
}

async function startLogin() {
  sendingCode.value = true
  Object.assign(loginForm, { code: '', password: '', phoneCodeHash: '' })
  try {
    const { data } = await api.post('/message-subscriptions/telegram/login/start', jsonapiResource('message-subscription-login-sessions', { phone: loginForm.phone }))
    loginForm.phoneCodeHash = unwrapJsonApiResource<any>(data)?.phoneCodeHash || ''
    ElMessage.success('验证码已发送')
  } catch (error) {
    ElMessage.error(apiErrorText(error))
  } finally {
    sendingCode.value = false
  }
}

async function verifyLogin() {
  verifyingCode.value = true
  try {
    await api.post('/message-subscriptions/telegram/login/verify', jsonapiResource('message-subscription-login-sessions', {
      phone: loginForm.phone,
      code: loginForm.code,
      password: loginForm.password || null,
      phoneCodeHash: loginForm.phoneCodeHash
    }))
    Object.assign(loginForm, { code: '', password: '', phoneCodeHash: '' })
    await loadStatus()
    ElMessage.success('会话已保存')
  } catch (error) {
    ElMessage.error(apiErrorText(error))
  } finally {
    verifyingCode.value = false
  }
}

async function saveSubscription() {
  if (!subscriptionForm.sourceRef.trim()) {
    ElMessage.warning('请填写订阅来源')
    return
  }
  if (!subscriptionForm.filterId) {
    ElMessage.warning('请选择过滤器')
    return
  }
  if (subscriptionForm.enabled && subscriptionForm.teamIds.length === 0) {
    ElMessage.warning('启用的来源至少绑定一个投研团队')
    return
  }
  await runAction('saveSubscription', async () => {
    const payload = { ...subscriptionForm }
    if (subscriptionForm.id) {
      await api.put(`/message-subscriptions/${subscriptionForm.id}`, jsonapiResource('message-subscriptions', payload, String(subscriptionForm.id)))
    } else {
      await api.post('/message-subscriptions', jsonapiResource('message-subscriptions', payload))
    }
    subscriptionDialog.value = false
    await loadSubscriptions()
  }, { success: '订阅来源已保存，采集任务已启动' })
}

async function saveFilter() {
  if (!filterForm.name.trim() || !filterForm.promptTemplate.trim()) {
    ElMessage.warning('名称和提示词都必须填写')
    return
  }
  const payload = {
    name: filterForm.name,
    description: filterForm.description,
    promptTemplate: filterForm.promptTemplate,
    providerId: filterForm.providerId || null,
    model: filterForm.model || null,
    enabled: filterForm.enabled,
    isDefault: filterForm.isDefault
  }
  await runAction('saveFilter', async () => {
    if (filterForm.id) {
      await api.put(`/message-subscription-filters/${filterForm.id}`, jsonapiResource('message-subscription-filters', payload, String(filterForm.id)))
    } else {
      await api.post('/message-subscription-filters', jsonapiResource('message-subscription-filters', payload))
    }
    filterDialog.value = false
    await Promise.all([loadFilters(), loadSubscriptions()])
  }, { success: '过滤器已保存' })
}

async function setDefaultFilter(row: MessageSubscriptionFilter) {
  if (row.isDefault) return
  await runAction(`defaultFilter:${row.id}`, async () => {
    await api.put(`/message-subscription-filters/${row.id}`, jsonapiResource('message-subscription-filters', { isDefault: true }, String(row.id)))
    await loadFilters()
  }, { success: '默认过滤器已更新' })
}

async function toggle(row: MessageSubscription) {
  await runAction(`toggleSubscription:${row.id}`, async () => {
    await api.put(`/message-subscriptions/${row.id}`, jsonapiResource('message-subscriptions', { enabled: !row.enabled }, String(row.id)))
    await loadSubscriptions()
  }, { success: row.enabled ? '订阅来源已停用' : '订阅来源已启用' })
}

async function deleteSubscription(row: MessageSubscription) {
  await runAction(`deleteSubscription:${row.id}`, async () => {
    await api.delete(`/message-subscriptions/${row.id}`)
    await loadSubscriptions()
  }, { success: '订阅来源已删除' })
}

async function testDraft() {
  if (!subscriptionForm.sourceRef.trim()) {
    ElMessage.warning('请先填写订阅来源')
    return
  }
  testingDraft.value = true
  try {
    const { data } = await api.post('/message-subscriptions/test', jsonapiResource('message-subscription-tests', { provider: subscriptionForm.provider, sourceRef: subscriptionForm.sourceRef }))
    testResult.value = unwrapJsonApiResource(data)
    if (!subscriptionForm.title && testResult.value?.title) subscriptionForm.title = testResult.value.title
    testVisible.value = true
  } catch (error) {
    ElMessage.error(apiErrorText(error))
  } finally {
    testingDraft.value = false
  }
}

async function testSaved(row: MessageSubscription) {
  await runAction(`testSubscription:${row.id}`, async () => {
    const { data } = await api.post(`/message-subscriptions/${row.id}/test`)
    testResult.value = unwrapJsonApiResource(data)
    testVisible.value = true
  })
}

async function collect(row?: MessageSubscription) {
  const key = row ? `collect:${row.id}` : 'collectAll'
  if (!row) collecting.value = true
  try {
    await runAction(key, async () => {
      await api.post('/message-subscriptions/collect', jsonapiResource('message-subscription-collect-results', { limit: 200, subscriptionId: row?.id }))
      await loadSubscriptions()
    }, { success: '采集任务已启动' })
  } finally {
    if (!row) collecting.value = false
  }
}

onMounted(load)

useAutoRefresh({
  intervalMs: 10000,
  refresh: refreshRuntimeState
})

watch(
  () => loginForm.phone,
  () => {
    Object.assign(loginForm, { code: '', password: '', phoneCodeHash: '' })
  }
)

watch(
  () => subscriptionForm.provider,
  (provider) => {
    if (subscriptionDialog.value && subscriptionForm.id === null) {
      subscriptionForm.pollIntervalSeconds = provider === 'rss_feed' ? 900 : 30
    }
  }
)
</script>
