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
    <div class="metric">
      <span>配置阻塞</span>
      <strong>{{ blockedDiagnosticCount }}</strong>
    </div>
  </div>

  <div class="panel">
    <div class="section-head">
      <h2>来源诊断</h2>
      <div class="toolbar compact-toolbar">
        <el-button :loading="running.maintenanceRepair" @click="repairBlockedSources">修复阻塞源</el-button>
        <el-button :loading="running.telegramAccessAudit" @click="auditTelegramAccess">Telegram 权限巡检</el-button>
        <el-button :icon="Refresh" :loading="loading" @click="loadDiagnostics()">刷新</el-button>
      </div>
    </div>
    <div class="table-scroll">
      <el-table v-loading="loading" :data="filteredDiagnostics" empty-text="暂无诊断结果">
        <el-table-column label="来源" min-width="240">
          <template #default="{ row }">
            <div class="source-title-line">
              <strong>{{ row.title || row.sourceRef }}</strong>
              <el-tag size="small" :type="providerTagType(row.provider)" effect="plain">{{ providerLabel(row.provider) }}</el-tag>
            </div>
            <small class="muted source-ref-line">{{ row.sourceRef }}</small>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="120">
          <template #default="{ row }">
            <el-tag :type="diagnosticStatusTagType(row.status)">{{ diagnosticStatusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="路径" min-width="220">
          <template #default="{ row }">
            <div>{{ sourceKindLabel(row.sourceKind) }}</div>
            <small class="muted">{{ proxyRouteLabel(row.proxyRoute) }}</small>
          </template>
        </el-table-column>
        <el-table-column label="推荐动作" min-width="260">
          <template #default="{ row }">
            <div v-if="row.recommendedActions?.length" class="diagnostic-action-list">
              <span v-for="action in row.recommendedActions" :key="action">{{ action }}</span>
            </div>
            <span v-else class="muted">-</span>
          </template>
        </el-table-column>
        <el-table-column label="检查项" min-width="340">
          <template #default="{ row }">
            <div class="diagnostic-check-tags">
              <el-tooltip v-for="check in row.checks || []" :key="`${row.id}:${check.key}`" :content="diagnosticCheckTooltip(check)" placement="top">
                <el-tag size="small" :type="diagnosticCheckTagType(check.status)" effect="plain">{{ check.title }}</el-tag>
              </el-tooltip>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </div>
  </div>

  <div class="panel">
    <div class="section-head">
      <h2>订阅来源</h2>
      <div class="toolbar compact-toolbar">
        <el-button :icon="Refresh" :loading="collecting" @click="collect()">采集全部</el-button>
        <el-button :icon="Refresh" @click="openRSSRotation">RSS 凭据轮换</el-button>
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
            <el-tag v-if="source.provider === 'rss_feed' && source.rssAuthType && source.rssAuthType !== 'none'" size="small" type="warning" effect="plain">{{ rssAuthLabel(source.rssAuthType) }}</el-tag>
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
              <el-tag v-if="row.provider === 'rss_feed' && row.rssAuthType && row.rssAuthType !== 'none'" size="small" type="warning" effect="plain">{{ rssAuthLabel(row.rssAuthType) }}</el-tag>
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
      <template v-if="subscriptionForm.provider === 'rss_feed'">
        <el-form-item label="RSS auth">
          <el-segmented v-model="subscriptionForm.rssAuthType" :options="rssAuthOptions" />
        </el-form-item>
        <el-form-item v-if="subscriptionForm.rssAuthType === 'basic'" label="RSS user">
          <el-input v-model="subscriptionForm.rssUsername" autocomplete="off" />
        </el-form-item>
        <el-form-item v-if="subscriptionForm.rssAuthType !== 'none'" :label="subscriptionForm.rssAuthType === 'bearer' ? 'Bearer token' : 'RSS password'">
          <el-input v-model="subscriptionForm.rssPassword" type="password" show-password autocomplete="new-password" :placeholder="rssPasswordPlaceholder" />
        </el-form-item>
        <el-form-item v-if="subscriptionForm.id && subscriptionForm.rssAuthType !== 'none'" label="Saved secret">
          <el-tag :type="subscriptionForm.hasRssPassword ? 'success' : 'warning'" effect="plain">{{ subscriptionForm.hasRssPassword ? 'configured' : 'missing' }}</el-tag>
        </el-form-item>
      </template>
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

  <el-dialog v-model="rssRotationDialog" title="RSS 凭据轮换" :width="dialogWidth" :fullscreen="isMobile">
    <el-form :model="rssRotationForm" :label-width="isMobile ? 'auto' : '120px'" :label-position="isMobile ? 'top' : 'right'">
      <el-form-item label="认证方式">
        <el-segmented v-model="rssRotationForm.rssAuthType" :options="rssAuthOptions.filter((item) => item.value !== 'none')" />
      </el-form-item>
      <el-form-item v-if="rssRotationForm.rssAuthType === 'basic'" label="RSS user">
        <el-input v-model="rssRotationForm.rssUsername" autocomplete="off" />
      </el-form-item>
      <el-form-item :label="rssRotationForm.rssAuthType === 'bearer' ? 'Bearer token' : 'RSS password'">
        <el-input v-model="rssRotationForm.rssPassword" type="password" show-password autocomplete="new-password" />
      </el-form-item>
      <el-form-item label="范围">
        <el-checkbox v-model="rssRotationForm.onlyBlocked">仅阻塞源</el-checkbox>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="rssRotationDialog = false">取消</el-button>
      <el-button type="primary" :loading="running.rotateRssAuth" @click="rotateRSSAuth">应用轮换</el-button>
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
import { api, apiErrorText, jsonapiResource, unwrapJsonApiCollection, unwrapJsonApiResource, type MessageSubscription, type MessageSubscriptionDiagnostic, type MessageSubscriptionFilter } from '../api'
import { useAutoRefresh } from '../composables/useAutoRefresh'
import { useAsyncAction } from '../composables/useAsyncAction'
import { useResponsive } from '../composables/useResponsive'
import { formatDateTimeUtc8 } from '../utils/datetime'

type SourceProvider = 'telegram_channel' | 'rss_feed'
type RSSAuthType = 'none' | 'basic' | 'bearer'
type DiagnosticCheck = NonNullable<MessageSubscriptionDiagnostic['checks']>[number]

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
const subscriptionForm = reactive({
  id: null as number | null,
  provider: 'telegram_channel' as SourceProvider,
  title: '',
  sourceRef: '',
  filterId: null as number | null,
  teamIds: [] as number[],
  backfillLimit: 20,
  pollIntervalSeconds: 30,
  enabled: true,
  rssAuthType: 'none' as RSSAuthType,
  rssUsername: '',
  rssPassword: '',
  hasRssPassword: false
})
const rssRotationForm = reactive({
  rssAuthType: 'basic' as Exclude<RSSAuthType, 'none'>,
  rssUsername: '',
  rssPassword: '',
  onlyBlocked: false
})
const filterForm = reactive<any>({ id: null, name: '', description: '', promptTemplate: '', providerId: null, model: '', enabled: true, isDefault: false, toolNames: [], skillNames: [] })
const subscriptions = ref<MessageSubscription[]>([])
const diagnostics = ref<MessageSubscriptionDiagnostic[]>([])
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
const rssRotationDialog = ref(false)
const filterDialog = ref(false)

const providerOptions = [
  { label: 'Telegram', value: 'telegram_channel' },
  { label: 'RSS/Atom', value: 'rss_feed' }
]
const rssAuthOptions = [
  { label: 'None', value: 'none' },
  { label: 'Basic', value: 'basic' },
  { label: 'Bearer', value: 'bearer' }
]
const providerFilterOptions = [{ label: '全部', value: 'all' }, ...providerOptions]
const dialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '760px'))
const filterDialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '92vw' : '900px'))
const testDialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '680px'))
const defaultFilterId = computed(() => filters.value.find((filter) => filter.isDefault)?.id)
const enabledFilters = computed(() => filters.value.filter((filter) => filter.enabled))
const enabledSourceCount = computed(() => subscriptions.value.filter((source) => source.enabled).length)
const failedSourceCount = computed(() => subscriptions.value.filter((source) => !!source.lastCollectError).length)
const blockedDiagnosticCount = computed(() => diagnostics.value.filter((item) => item.status === 'blocked').length)
const canStartLogin = computed(() => status.hasAppId && status.hasAppHash && !!loginForm.phone.trim())
const canVerifyLogin = computed(() => !!loginForm.phoneCodeHash && !!loginForm.phone.trim() && !!loginForm.code.trim())
const sourcePlaceholder = computed(() => subscriptionForm.provider === 'rss_feed' ? 'https://example.com/feed.xml' : '频道链接、@用户名或 -100 编号')
const rssPasswordPlaceholder = computed(() => subscriptionForm.hasRssPassword ? 'leave blank to keep saved secret' : '')
const filteredSubscriptions = computed(() => {
  const query = sourceQuery.value.trim().toLowerCase()
  return subscriptions.value.filter((source) => {
    if (sourceProviderFilter.value !== 'all' && source.provider !== sourceProviderFilter.value) return false
    if (!query) return true
    return `${source.title} ${source.sourceRef}`.toLowerCase().includes(query)
  })
})
const filteredDiagnostics = computed(() => {
  const query = sourceQuery.value.trim().toLowerCase()
  return diagnostics.value.filter((item) => {
    if (sourceProviderFilter.value !== 'all' && item.provider !== sourceProviderFilter.value) return false
    if (!query) return true
    return `${item.title} ${item.sourceRef} ${item.sourceKind}`.toLowerCase().includes(query)
  })
})

function providerCount(provider: SourceProvider) {
  return subscriptions.value.filter((source) => source.provider === provider).length
}

function providerLabel(provider: string) {
  return provider === 'rss_feed' ? 'RSS/Atom' : provider === 'telegram_channel' ? 'Telegram' : provider
}

function rssAuthLabel(value?: string) {
  if (value === 'basic') return 'Basic'
  if (value === 'bearer') return 'Bearer'
  return 'None'
}

function providerTagType(provider: string) {
  return provider === 'rss_feed' ? 'warning' : 'primary'
}

function diagnosticStatusLabel(status?: string) {
  if (status === 'ready') return '就绪'
  if (status === 'warning') return '预警'
  if (status === 'blocked') return '阻塞'
  if (status === 'disabled') return '停用'
  return status || '-'
}

function diagnosticStatusTagType(status?: string) {
  if (status === 'ready') return 'success'
  if (status === 'warning') return 'warning'
  if (status === 'blocked') return 'danger'
  return 'info'
}

function diagnosticCheckTagType(status?: string) {
  if (status === 'ok') return 'success'
  if (status === 'warning') return 'warning'
  if (status === 'blocked') return 'danger'
  return 'info'
}

function diagnosticCheckTooltip(check: DiagnosticCheck) {
  return [check.detail, check.action].filter(Boolean).join(' / ')
}

function sourceKindLabel(kind?: string) {
  const labels: Record<string, string> = {
    telegram_private_numeric: 'Telegram 私有编号',
    telegram_private_invite: 'Telegram 邀请链接',
    telegram_public_handle: 'Telegram 公开用户名',
    telegram_public_link: 'Telegram 公开链接',
    telegram_peer_ref: 'Telegram Peer',
    rss_public_feed: 'RSS/Atom 公开源',
    rss_private_auth: 'RSS/Atom 私有认证',
    rss_url_credentials: 'RSS/Atom URL 凭据',
    invalid_url: 'URL 无效',
    missing_source_ref: '来源缺失'
  }
  return labels[kind || ''] || kind || '-'
}

function proxyRouteLabel(route?: string) {
  const labels: Record<string, string> = {
    direct: '直连',
    'proxy:telegram': '代理：Telegram',
    'proxy:web': '代理：Web/RSS',
    proxy_configured_but_not_enabled: '代理未路由'
  }
  return labels[route || ''] || route || '-'
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

async function loadDiagnostics() {
  diagnostics.value = unwrapJsonApiCollection<MessageSubscriptionDiagnostic>((await api.get('/message-subscriptions/diagnostics')).data)
}

async function loadTeams() {
  teams.value = unwrapJsonApiCollection((await api.get('/research-teams')).data)
}

async function load(background = false) {
  if (!background) loading.value = true
  try {
    await Promise.all([loadStatus(), loadProviders(), loadFilters(), loadSubscriptions(), loadDiagnostics(), loadTeams()])
  } finally {
    if (!background) loading.value = false
  }
}

async function refreshRuntimeState() {
  await Promise.all([loadStatus(), loadSubscriptions(), loadDiagnostics()])
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
    enabled: row?.enabled ?? true,
    rssAuthType: (row?.rssAuthType as RSSAuthType | undefined) ?? 'none',
    rssUsername: row?.rssUsername ?? '',
    rssPassword: '',
    hasRssPassword: !!row?.hasRssPassword
  })
  subscriptionDialog.value = true
}

function openRSSRotation() {
  Object.assign(rssRotationForm, { rssAuthType: 'basic', rssUsername: '', rssPassword: '', onlyBlocked: false })
  rssRotationDialog.value = true
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
    await loadDiagnostics()
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
    await Promise.all([loadStatus(), loadDiagnostics()])
    ElMessage.success('会话已保存')
  } catch (error) {
    ElMessage.error(apiErrorText(error))
  } finally {
    verifyingCode.value = false
  }
}

function maintenanceMessage(result: any) {
  const skipped = Array.isArray(result?.skipped) ? result.skipped.length : 0
  const audit = result?.checked ? `，巡检 ${result.checked}，通过 ${result?.passed ?? 0}，失败 ${result?.failed ?? 0}` : ''
  return `匹配 ${result?.matched ?? 0}${audit}，更新 ${result?.updated ?? 0}，入队 ${result?.queued ?? 0}${skipped ? `，跳过 ${skipped}` : ''}`
}

async function runSubscriptionMaintenance(payload: Record<string, any>) {
  const { data } = await api.post('/message-subscriptions/maintenance', jsonapiResource('message-subscription-maintenance-results', payload, 'current'))
  return unwrapJsonApiResource<any>(data)
}

async function repairBlockedSources() {
  await runAction('maintenanceRepair', async () => {
    const result = await runSubscriptionMaintenance({ action: 'repair_defaults', onlyBlocked: true })
    await Promise.all([loadSubscriptions(), loadDiagnostics()])
    ElMessage.success(maintenanceMessage(result))
  })
}

async function auditTelegramAccess() {
  await runAction('telegramAccessAudit', async () => {
    const result = await runSubscriptionMaintenance({ action: 'audit_telegram_access', provider: 'telegram_channel' })
    await Promise.all([loadSubscriptions(), loadDiagnostics()])
    ElMessage.success(maintenanceMessage(result))
  })
}

async function rotateRSSAuth() {
  if (rssRotationForm.rssAuthType === 'basic' && !rssRotationForm.rssUsername.trim()) {
    ElMessage.warning('RSS Basic auth requires a username')
    return
  }
  if (!rssRotationForm.rssPassword.trim()) {
    ElMessage.warning('RSS auth requires a password or token')
    return
  }
  await runAction('rotateRssAuth', async () => {
    const result = await runSubscriptionMaintenance({
      action: 'rotate_rss_auth',
      provider: 'rss_feed',
      onlyBlocked: rssRotationForm.onlyBlocked,
      rssAuthType: rssRotationForm.rssAuthType,
      rssUsername: rssRotationForm.rssAuthType === 'basic' ? rssRotationForm.rssUsername : '',
      rssPassword: rssRotationForm.rssPassword
    })
    rssRotationDialog.value = false
    await Promise.all([loadSubscriptions(), loadDiagnostics()])
    ElMessage.success(maintenanceMessage(result))
  })
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
  if (subscriptionForm.provider === 'rss_feed' && subscriptionForm.rssAuthType === 'basic' && !subscriptionForm.rssUsername.trim()) {
    ElMessage.warning('RSS Basic auth requires a username')
    return
  }
  if (subscriptionForm.provider === 'rss_feed' && subscriptionForm.rssAuthType !== 'none' && !subscriptionForm.rssPassword.trim() && !subscriptionForm.hasRssPassword) {
    ElMessage.warning('RSS auth requires a password or token')
    return
  }
  await runAction('saveSubscription', async () => {
    const payload: Record<string, any> = {
      provider: subscriptionForm.provider,
      title: subscriptionForm.title,
      sourceRef: subscriptionForm.sourceRef,
      filterId: subscriptionForm.filterId,
      teamIds: subscriptionForm.teamIds,
      backfillLimit: subscriptionForm.backfillLimit,
      pollIntervalSeconds: subscriptionForm.pollIntervalSeconds,
      enabled: subscriptionForm.enabled
    }
    if (subscriptionForm.provider === 'rss_feed') {
      payload.rssAuthType = subscriptionForm.rssAuthType
      payload.rssUsername = subscriptionForm.rssAuthType === 'basic' ? subscriptionForm.rssUsername : ''
      if (subscriptionForm.rssAuthType !== 'none' && subscriptionForm.rssPassword.trim()) {
        payload.rssPassword = subscriptionForm.rssPassword
      }
    }
    if (subscriptionForm.id) {
      await api.put(`/message-subscriptions/${subscriptionForm.id}`, jsonapiResource('message-subscriptions', payload, String(subscriptionForm.id)))
    } else {
      await api.post('/message-subscriptions', jsonapiResource('message-subscriptions', payload))
    }
    subscriptionDialog.value = false
    await Promise.all([loadSubscriptions(), loadDiagnostics()])
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
    await Promise.all([loadFilters(), loadSubscriptions(), loadDiagnostics()])
  }, { success: '过滤器已保存' })
}

async function setDefaultFilter(row: MessageSubscriptionFilter) {
  if (row.isDefault) return
  await runAction(`defaultFilter:${row.id}`, async () => {
    await api.put(`/message-subscription-filters/${row.id}`, jsonapiResource('message-subscription-filters', { isDefault: true }, String(row.id)))
    await Promise.all([loadFilters(), loadDiagnostics()])
  }, { success: '默认过滤器已更新' })
}

async function toggle(row: MessageSubscription) {
  await runAction(`toggleSubscription:${row.id}`, async () => {
    await api.put(`/message-subscriptions/${row.id}`, jsonapiResource('message-subscriptions', { enabled: !row.enabled }, String(row.id)))
    await Promise.all([loadSubscriptions(), loadDiagnostics()])
  }, { success: row.enabled ? '订阅来源已停用' : '订阅来源已启用' })
}

async function deleteSubscription(row: MessageSubscription) {
  await runAction(`deleteSubscription:${row.id}`, async () => {
    await api.delete(`/message-subscriptions/${row.id}`)
    await Promise.all([loadSubscriptions(), loadDiagnostics()])
  }, { success: '订阅来源已删除' })
}

async function testDraft() {
  if (!subscriptionForm.sourceRef.trim()) {
    ElMessage.warning('请先填写订阅来源')
    return
  }
  if (subscriptionForm.provider === 'rss_feed' && subscriptionForm.rssAuthType === 'basic' && !subscriptionForm.rssUsername.trim()) {
    ElMessage.warning('RSS Basic auth requires a username')
    return
  }
  if (subscriptionForm.provider === 'rss_feed' && subscriptionForm.rssAuthType !== 'none' && !subscriptionForm.rssPassword.trim()) {
    ElMessage.warning('RSS draft test requires a password or token')
    return
  }
  testingDraft.value = true
  try {
    const payload: Record<string, any> = { provider: subscriptionForm.provider, sourceRef: subscriptionForm.sourceRef }
    if (subscriptionForm.provider === 'rss_feed') {
      payload.rssAuthType = subscriptionForm.rssAuthType
      payload.rssUsername = subscriptionForm.rssAuthType === 'basic' ? subscriptionForm.rssUsername : ''
      if (subscriptionForm.rssAuthType !== 'none') payload.rssPassword = subscriptionForm.rssPassword
    }
    const { data } = await api.post('/message-subscriptions/test', jsonapiResource('message-subscription-tests', payload))
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
      await Promise.all([loadSubscriptions(), loadDiagnostics()])
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
    if (provider !== 'rss_feed') {
      subscriptionForm.rssAuthType = 'none'
      subscriptionForm.rssUsername = ''
      subscriptionForm.rssPassword = ''
      subscriptionForm.hasRssPassword = false
    }
  }
)

watch(
  () => subscriptionForm.rssAuthType,
  (authType) => {
    if (authType === 'none') {
      subscriptionForm.rssUsername = ''
      subscriptionForm.rssPassword = ''
    }
    if (authType === 'bearer') {
      subscriptionForm.rssUsername = ''
    }
  }
)

watch(
  () => rssRotationForm.rssAuthType,
  (authType) => {
    if (authType === 'bearer') {
      rssRotationForm.rssUsername = ''
    }
  }
)
</script>
