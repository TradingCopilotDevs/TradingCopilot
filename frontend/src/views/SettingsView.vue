<template>
  <h1 class="page-title">系统设置</h1>

  <el-alert
    class="panel"
    type="warning"
    :closable="false"
    title="APP_SECRET_KEY、JWT_SECRET_KEY、DATABASE_URL、REDIS_URL 仍然只应通过启动级 .env 文件管理。下方可编辑的在线运行参数在保存时会同步写回当前生效的 .env 文件，便于下次重启继续生效。"
  />

  <div class="panel">
    <h2>启动级 .env 配置</h2>
    <div v-if="isMobile" class="mobile-card-list">
      <div v-for="row in runtimeEnv" :key="row.key" class="mobile-card">
        <div class="mobile-card__header">
          <h3 class="mobile-card__title">{{ row.key }}</h3>
          <el-tag :type="row.editableInUi ? 'success' : 'info'">{{ row.editableInUi ? '支持' : '仅 .env' }}</el-tag>
        </div>
        <div class="mobile-card__meta">
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">当前值</span>
            <span>{{ row.value }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">说明</span>
            <span>{{ row.description }}</span>
          </div>
        </div>
      </div>
      <el-empty v-if="!runtimeEnv.length" description="暂无配置" />
    </div>
    <el-table v-else :data="runtimeEnv" empty-text="暂无配置">
      <el-table-column prop="key" label="变量" width="280" />
      <el-table-column label="当前值" width="240">
        <template #default="{ row }">
          <el-tag v-if="row.sensitive" type="info">{{ row.value }}</el-tag>
          <span v-else>{{ row.value }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="description" label="说明" />
      <el-table-column label="在线覆盖" width="120">
        <template #default="{ row }">
          <el-tag :type="row.editableInUi ? 'success' : 'info'">
            {{ row.editableInUi ? '支持' : '仅 .env' }}
          </el-tag>
        </template>
      </el-table-column>
    </el-table>
  </div>

  <div class="panel">
    <h2>在线运行参数</h2>
    <el-form :model="appForm" :label-width="isMobile ? 'auto' : '220px'" :label-position="isMobile ? 'top' : 'right'">
      <el-form-item label="公开前端地址">
        <el-input v-model="appForm.PUBLIC_BASE_URL" placeholder="http://localhost:8000" />
      </el-form-item>

      <el-divider content-position="left">市场数据源</el-divider>

      <el-form-item label="第 1 类：历史分析数据源">
        <el-select v-model="appForm.DEFAULT_MARKET_PROVIDER">
          <el-option label="adata-go" value="adata" />
          <el-option label="Tushare" value="tushare" />
        </el-select>
        <div class="form-tip">用于股票信息同步、历史数据补全和 AI 分析师读取历史数据。</div>
      </el-form-item>

      <el-form-item label="第 2 类：实时主数据源">
        <el-select v-model="appForm.MARKET_REALTIME_PROVIDER">
          <el-option label="adata-go" value="adata" />
        </el-select>
        <div class="form-tip">用于页面行情、实时价格和 K 线优先取数。</div>
      </el-form-item>

      <el-form-item label="ETF/LOF 实时兼容源">
        <el-select v-model="appForm.MARKET_REALTIME_COMPAT_PROVIDER">
          <el-option label="腾讯" value="tencent" />
          <el-option label="新浪" value="sina" />
          <el-option label="禁用" value="disabled" />
        </el-select>
        <div class="form-tip">仅在 adata-go 对 ETF/LOF 实时报价缺失时使用，默认腾讯。</div>
      </el-form-item>

      <el-form-item label="实时行情缓存秒数">
        <el-input-number v-model="appForm.MARKET_REALTIME_CACHE_TTL_SECONDS" :min="0" :max="60" />
        <div class="form-tip">0 表示不缓存。值越小越实时，值越大越省上游请求。</div>
      </el-form-item>

      <el-divider content-position="left">会议与工具</el-divider>

      <el-form-item label="会议最大轮次">
        <el-input-number v-model="appForm.MEETING_MAX_ROUNDS" :min="1" :max="30" />
      </el-form-item>
      <el-form-item label="每日令牌预算">
        <el-input-number v-model="appForm.MEETING_DAILY_TOKEN_BUDGET" :min="-1" :step="10000" />
        <div class="form-tip">-1 means unlimited; positive integers enforce a daily cap.</div>
      </el-form-item>
      <el-form-item label="工具结果上限">
        <el-input-number v-model="appForm.TOOL_RESULT_LIMIT" :min="10" :max="2000" />
      </el-form-item>
      <el-form-item label="数据库语句超时毫秒">
        <el-input-number v-model="appForm.SQL_STATEMENT_TIMEOUT_MS" :min="500" :step="500" />
      </el-form-item>

      <el-divider content-position="left">日志</el-divider>

      <el-alert
        type="info"
        :closable="false"
        title="日志配置保存后写入 .env，下次重启后生效。当前进程仍使用启动时读取的日志配置。"
        class="settings-inline-alert"
      />
      <el-form-item label="日志目录">
        <el-input v-model="appForm.LOG_DIR" placeholder="./logs" />
      </el-form-item>
      <el-form-item label="日志级别">
        <el-select v-model="appForm.LOG_LEVEL">
          <el-option label="debug" value="debug" />
          <el-option label="info" value="info" />
          <el-option label="warn" value="warn" />
          <el-option label="error" value="error" />
        </el-select>
      </el-form-item>
      <el-form-item label="轮转模式">
        <el-radio-group v-model="appForm.LOG_ROTATION_MODE">
          <el-radio-button label="size">按大小</el-radio-button>
          <el-radio-button label="time">按时间</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <el-form-item label="单文件大小 MB">
        <el-input-number v-model="appForm.LOG_ROTATION_SIZE_MB" :min="1" :max="1024" />
        <div class="form-tip">仅按大小轮转时使用，默认 5MB。</div>
      </el-form-item>
      <el-form-item label="日志目录总上限 MB">
        <el-input-number v-model="appForm.LOG_ROTATION_TOTAL_SIZE_MB" :min="1" :max="10240" />
        <div class="form-tip">仅按大小轮转时使用，超过后删除最旧的非活跃日志文件。</div>
      </el-form-item>
      <el-form-item label="保留天数">
        <el-input-number v-model="appForm.LOG_ROTATION_MAX_AGE_DAYS" :min="1" :max="365" />
        <div class="form-tip">仅按时间轮转时使用，默认每日切分并保留 7 天。</div>
      </el-form-item>

      <el-button type="primary" @click="saveAppSettings">保存并同步 .env 文件</el-button>
    </el-form>
  </div>

  <div class="panel">
    <h2>出站代理</h2>
    <el-form :model="proxyForm" :label-width="isMobile ? 'auto' : '220px'" :label-position="isMobile ? 'top' : 'right'">
      <el-form-item label="代理地址">
        <el-input v-model="proxyForm.proxyUrl" placeholder="http://127.0.0.1:7890 或 socks5h://127.0.0.1:1080" show-password />
        <div class="form-tip">支持 http、https、socks5、socks5h。保存后新请求立即生效，Telegram 用户监听会自动重连。</div>
      </el-form-item>
      <el-form-item label="启用模块">
        <el-checkbox v-model="proxyForm.enabledAi">AI</el-checkbox>
        <el-checkbox v-model="proxyForm.enabledTelegram">Telegram</el-checkbox>
        <el-checkbox v-model="proxyForm.enabledMarket">Market</el-checkbox>
        <el-checkbox v-model="proxyForm.enabledWeb">Web Search</el-checkbox>
      </el-form-item>
      <el-form-item label="绕过代理">
        <el-input v-model="proxyNoProxyText" placeholder="localhost,127.0.0.1,10.0.0.0/8,.internal" />
        <div class="form-tip">逗号分隔，默认会包含 localhost 和 127.0.0.1。</div>
      </el-form-item>
      <el-form-item>
        <el-button type="primary" @click="saveProxySettings">保存并测试代理</el-button>
      </el-form-item>
    </el-form>
    <div v-if="proxyTestResults.length" class="proxy-test-grid">
      <div v-for="item in proxyTestResults" :key="item.module" class="proxy-test-card">
        <strong>{{ item.module }}</strong>
        <el-tag :type="proxyStatusType(item.status)" effect="plain">{{ item.status }}</el-tag>
        <p>{{ item.detail }}</p>
      </div>
    </div>
  </div>

  <div class="panel">
    <h2>业务凭据</h2>
    <div class="secret-grid">
      <div v-for="definition in secretDefinitions" :key="secretKey(definition)" class="secret-card">
        <strong>{{ definition.title }}</strong>
        <p>{{ definition.purpose }}</p>
        <small>{{ definition.requiredFor }}</small>
        <el-input
          v-model="secretValues[secretKey(definition)]"
          class="secret-input"
          type="password"
          show-password
          :placeholder="definition.placeholder"
        />
        <el-button type="primary" plain @click="saveSecret(definition)">保存</el-button>
      </div>
    </div>
  </div>

  <div class="panel">
    <h2>已保存凭据</h2>
    <div v-if="isMobile" class="mobile-card-list">
      <div v-for="item in secrets" :key="`${item.kind}-${item.name}`" class="mobile-card">
        <div class="mobile-card__header">
          <h3 class="mobile-card__title">{{ item.name }}</h3>
          <el-tag type="info">{{ item.kind }}</el-tag>
        </div>
        <div class="mobile-card__meta">
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">明文</span>
            <span>不回显</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">更新时间</span>
            <span>{{ item.updatedAt }}</span>
          </div>
        </div>
      </div>
      <el-empty v-if="!secrets.length" description="暂无凭据" />
    </div>
    <el-table v-else :data="secrets" empty-text="暂无凭据">
      <el-table-column prop="kind" label="类型" width="160" />
      <el-table-column prop="name" label="名称" width="220" />
      <el-table-column label="明文" width="120">
        <template #default>
          <el-tag type="info">不回显</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="updatedAt" label="更新时间" />
    </el-table>
  </div>
</template>

<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { onMounted, reactive, ref } from 'vue'
import { api, jsonapiResource, unwrapJsonApiCollection, unwrapJsonApiResource, type ProxySettings } from '../api'
import { useResponsive } from '../composables/useResponsive'

interface RuntimeEnvItem {
  key: string
  value: unknown
  description: string
  sensitive: boolean
  editableInUi: boolean
  restartRequired: boolean
}

interface AppSettingRecord {
  key: string
  value: { value?: string | number | boolean | null }
}

interface AppFormState {
  PUBLIC_BASE_URL: string
  DEFAULT_MARKET_PROVIDER: string
  MARKET_REALTIME_PROVIDER: string
  MARKET_REALTIME_COMPAT_PROVIDER: 'tencent' | 'sina' | 'disabled'
  MARKET_REALTIME_CACHE_TTL_SECONDS: number
  MEETING_MAX_ROUNDS: number
  MEETING_DAILY_TOKEN_BUDGET: number
  TOOL_RESULT_LIMIT: number
  SQL_STATEMENT_TIMEOUT_MS: number
  LOG_DIR: string
  LOG_LEVEL: string
  LOG_ROTATION_MODE: 'size' | 'time'
  LOG_ROTATION_SIZE_MB: number
  LOG_ROTATION_TOTAL_SIZE_MB: number
  LOG_ROTATION_MAX_AGE_DAYS: number
}

interface ProxyTestItem {
  module: string
  status: string
  detail: string
}

const { isMobile } = useResponsive()
const runtimeEnv = ref<RuntimeEnvItem[]>([])
const secretDefinitions = ref<any[]>([])
const secrets = ref<any[]>([])
const savedAppSettings = ref<AppSettingRecord[]>([])
const secretValues = reactive<Record<string, string>>({})
const proxyNoProxyText = ref('localhost,127.0.0.1')
const proxyTestResults = ref<ProxyTestItem[]>([])

const appForm = reactive<AppFormState>({
  PUBLIC_BASE_URL: '',
  DEFAULT_MARKET_PROVIDER: 'adata',
  MARKET_REALTIME_PROVIDER: 'adata',
  MARKET_REALTIME_COMPAT_PROVIDER: 'tencent',
  MARKET_REALTIME_CACHE_TTL_SECONDS: 5,
  MEETING_MAX_ROUNDS: 6,
  MEETING_DAILY_TOKEN_BUDGET: -1,
  TOOL_RESULT_LIMIT: 200,
  SQL_STATEMENT_TIMEOUT_MS: 5000,
  LOG_DIR: './logs',
  LOG_LEVEL: 'info',
  LOG_ROTATION_MODE: 'size',
  LOG_ROTATION_SIZE_MB: 5,
  LOG_ROTATION_TOTAL_SIZE_MB: 100,
  LOG_ROTATION_MAX_AGE_DAYS: 7
})

const proxyForm = reactive<ProxySettings>({
  proxyUrl: '',
  enabledAi: false,
  enabledTelegram: false,
  enabledMarket: false,
  enabledWeb: false,
  noProxy: ['localhost', '127.0.0.1']
})

function secretKey(definition: any) {
  return `${definition.kind}:${definition.name}`
}

function valueFromSetting(key: string) {
  const setting = savedAppSettings.value.find((item) => item.key === key)
  return setting?.value?.value
}

function castEnvValue(row: RuntimeEnvItem): string | number {
  const numeric = Number(row.value)
  const text = String(row.value ?? '')
  return Number.isFinite(numeric) && text.trim() !== '' ? numeric : text
}

function normalizeCompatProvider(value: unknown): AppFormState['MARKET_REALTIME_COMPAT_PROVIDER'] {
  const text = String(value ?? '').trim()
  return text === 'sina' || text === 'disabled' ? text : 'tencent'
}

async function load() {
  const [envResp, defsResp, secretsResp, appResp, proxyResp] = await Promise.all([
    api.get('/settings/runtime-env'),
    api.get('/settings/secret-definitions'),
    api.get('/settings/secrets'),
    api.get('/settings/app-settings'),
    api.get('/settings/proxy')
  ])

  runtimeEnv.value = unwrapJsonApiCollection<RuntimeEnvItem>(envResp.data)
  secretDefinitions.value = unwrapJsonApiCollection<any>(defsResp.data)
  secrets.value = unwrapJsonApiCollection<any>(secretsResp.data)
  savedAppSettings.value = unwrapJsonApiCollection<AppSettingRecord>(appResp.data)
  Object.assign(proxyForm, unwrapJsonApiResource<ProxySettings>(proxyResp.data))
  proxyNoProxyText.value = (proxyForm.noProxy || ['localhost', '127.0.0.1']).join(',')

  for (const row of runtimeEnv.value) {
    if (!row.editableInUi || !(row.key in appForm)) {
      continue
    }
    const override = valueFromSetting(row.key)
    const value = override ?? castEnvValue(row)
    switch (row.key) {
      case 'PUBLIC_BASE_URL':
        appForm.PUBLIC_BASE_URL = String(value)
        break
      case 'DEFAULT_MARKET_PROVIDER':
        appForm.DEFAULT_MARKET_PROVIDER = String(value)
        break
      case 'MARKET_REALTIME_PROVIDER':
        appForm.MARKET_REALTIME_PROVIDER = String(value)
        break
      case 'MARKET_REALTIME_COMPAT_PROVIDER':
        appForm.MARKET_REALTIME_COMPAT_PROVIDER = normalizeCompatProvider(value)
        break
      case 'MARKET_REALTIME_CACHE_TTL_SECONDS':
        appForm.MARKET_REALTIME_CACHE_TTL_SECONDS = Number(value)
        break
      case 'MEETING_MAX_ROUNDS':
        appForm.MEETING_MAX_ROUNDS = Number(value)
        break
      case 'MEETING_DAILY_TOKEN_BUDGET':
        appForm.MEETING_DAILY_TOKEN_BUDGET = Number(value)
        break
      case 'TOOL_RESULT_LIMIT':
        appForm.TOOL_RESULT_LIMIT = Number(value)
        break
      case 'SQL_STATEMENT_TIMEOUT_MS':
        appForm.SQL_STATEMENT_TIMEOUT_MS = Number(value)
        break
      case 'LOG_DIR':
        appForm.LOG_DIR = String(value)
        break
      case 'LOG_LEVEL':
        appForm.LOG_LEVEL = String(value)
        break
      case 'LOG_ROTATION_MODE':
        appForm.LOG_ROTATION_MODE = String(value) === 'time' ? 'time' : 'size'
        break
      case 'LOG_ROTATION_SIZE_MB':
        appForm.LOG_ROTATION_SIZE_MB = Number(value)
        break
      case 'LOG_ROTATION_TOTAL_SIZE_MB':
        appForm.LOG_ROTATION_TOTAL_SIZE_MB = Number(value)
        break
      case 'LOG_ROTATION_MAX_AGE_DAYS':
        appForm.LOG_ROTATION_MAX_AGE_DAYS = Number(value)
        break
    }
  }
}

async function saveSecret(definition: any) {
  const key = secretKey(definition)
  const value = secretValues[key]
  if (!value) {
    ElMessage.warning('请输入凭据内容')
    return
  }
  await api.post('/settings/secrets', jsonapiResource('secrets', {
    kind: definition.kind,
    name: definition.name,
    value
  }))
  secretValues[key] = ''
  ElMessage.success('已加密保存')
  await load()
}

async function saveAppSettings() {
  const payloads: Record<string, string | number> = {
    PUBLIC_BASE_URL: appForm.PUBLIC_BASE_URL,
    DEFAULT_MARKET_PROVIDER: appForm.DEFAULT_MARKET_PROVIDER,
    MARKET_REALTIME_PROVIDER: appForm.MARKET_REALTIME_PROVIDER,
    MARKET_REALTIME_COMPAT_PROVIDER: appForm.MARKET_REALTIME_COMPAT_PROVIDER,
    MARKET_REALTIME_CACHE_TTL_SECONDS: appForm.MARKET_REALTIME_CACHE_TTL_SECONDS,
    MEETING_MAX_ROUNDS: appForm.MEETING_MAX_ROUNDS,
    MEETING_DAILY_TOKEN_BUDGET: appForm.MEETING_DAILY_TOKEN_BUDGET,
    TOOL_RESULT_LIMIT: appForm.TOOL_RESULT_LIMIT,
    SQL_STATEMENT_TIMEOUT_MS: appForm.SQL_STATEMENT_TIMEOUT_MS,
    LOG_DIR: appForm.LOG_DIR,
    LOG_LEVEL: appForm.LOG_LEVEL,
    LOG_ROTATION_MODE: appForm.LOG_ROTATION_MODE,
    LOG_ROTATION_SIZE_MB: appForm.LOG_ROTATION_SIZE_MB,
    LOG_ROTATION_TOTAL_SIZE_MB: appForm.LOG_ROTATION_TOTAL_SIZE_MB,
    LOG_ROTATION_MAX_AGE_DAYS: appForm.LOG_ROTATION_MAX_AGE_DAYS
  }

  await Promise.all(
    Object.entries(payloads).map(([key, value]) =>
      api.put(`/settings/app-settings/${key}`, jsonapiResource('app-settings', {
        key,
        value: { value },
        description: 'frontend runtime override'
      }, key))
    )
  )

  ElMessage.success('在线配置已保存')
  await load()
}

function parseNoProxyText(value: string): string[] {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

async function saveProxySettings() {
  const saved = await api.put('/settings/proxy', jsonapiResource('proxy-settings', {
    ...proxyForm,
    noProxy: parseNoProxyText(proxyNoProxyText.value)
  }, 'current'))
  Object.assign(proxyForm, unwrapJsonApiResource<ProxySettings>(saved.data))
  proxyNoProxyText.value = (proxyForm.noProxy || []).join(',')
  const test = await api.post('/settings/proxy/test')
  const testResult = unwrapJsonApiResource<{ results: Record<string, unknown> }>(test.data)
  proxyTestResults.value = Object.entries(testResult?.results || {}).map(([module, value]) => ({
    module,
    status: String((value as any).status || '-'),
    detail: String((value as any).detail || '')
  }))
  ElMessage.success('代理配置已保存并测试')
}

function proxyStatusType(value: string) {
  const map: Record<string, 'success' | 'warning' | 'danger' | 'info'> = {
    ok: 'success',
    skipped: 'info',
    warning: 'warning',
    error: 'danger'
  }
  return map[value] || 'info'
}

onMounted(load)
</script>

<style scoped>
.form-tip {
  color: var(--el-text-color-secondary);
  font-size: 12px;
  line-height: 1.5;
  margin-top: 6px;
}

.settings-inline-alert {
  margin-bottom: 16px;
}

.proxy-test-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
  gap: 12px;
  margin-top: 16px;
}

.proxy-test-card {
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  padding: 12px;
  display: grid;
  gap: 8px;
}

.proxy-test-card p {
  margin: 0;
  color: var(--el-text-color-secondary);
  line-height: 1.5;
}
</style>
