<template>
  <h1 class="page-title">预测市场</h1>

  <div class="panel">
    <div class="section-head">
      <h2>Polymarket 市场发现</h2>
      <div class="toolbar compact-toolbar">
        <el-input v-model="query" placeholder="Polymarket URL、slug 或事件关键词" clearable @keyup.enter="searchMarkets" />
        <el-button :loading="loading" @click="searchMarkets">搜索</el-button>
        <el-button type="primary" :loading="syncing" @click="syncMarkets">同步活跃市场</el-button>
      </div>
    </div>

    <el-alert v-if="searchNotice" class="inline-alert" type="info" :closable="false" show-icon :title="searchNotice" />
    <el-alert v-if="searchWarning" class="inline-alert" type="warning" :closable="false" show-icon :title="searchWarning" />
    <el-alert v-if="searchError" class="inline-alert" type="error" :closable="false" show-icon :title="searchError" />
    <el-alert v-if="linkedMeetingNotice" class="inline-alert" type="success" :closable="false" show-icon :title="linkedMeetingNotice" />

    <el-table :data="markets" :empty-text="marketEmptyText">
      <el-table-column label="市场问题" min-width="320" show-overflow-tooltip>
        <template #default="{ row }">
          <div class="market-title-line">
            <el-tag v-if="isLinkedMarket(row)" size="small" type="success" effect="plain">会议证据</el-tag>
            <span>{{ row.question || `#${row.id}` }}</span>
          </div>
          <div v-if="isLinkedMarket(row) && sourceMeetingId" class="muted">来自会议 #{{ sourceMeetingId }} 的结论证据</div>
        </template>
      </el-table-column>
      <el-table-column label="身份" min-width="180" show-overflow-tooltip>
        <template #default="{ row }">
          <div>{{ row.externalMarketId || row.conditionId || '-' }}</div>
          <div v-if="eventLabel(row)" class="muted">{{ eventLabel(row) }}</div>
        </template>
      </el-table-column>
      <el-table-column label="价格" width="160">
        <template #default="{ row }">
          <span>{{ formatPrice(row.lastTradePrice) }}</span>
          <span class="muted"> / {{ formatSpread(row.spread) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="流动性" width="130">
        <template #default="{ row }">{{ formatNumber(row.liquidity) }}</template>
      </el-table-column>
      <el-table-column label="状态" width="150">
        <template #default="{ row }">
          <el-tag :type="row.active && !row.closed ? 'success' : 'info'">{{ row.active && !row.closed ? '活跃' : '关闭' }}</el-tag>
          <el-tag v-if="row.restricted" type="warning" effect="plain">受限</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="Token" width="120">
        <template #default="{ row }">
          <el-tag :type="tokenCount(row) > 0 ? 'success' : 'warning'" effect="plain">{{ tokenStatus(row) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="slug" label="Slug" min-width="180" show-overflow-tooltip />
      <el-table-column label="操作" width="150" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" :loading="watchingId === row.id" @click="watchMarket(row)">关注</el-button>
          <el-button v-if="sourceMeetingId && isLinkedMarket(row)" link type="primary" @click="openMeeting(sourceMeetingId)">
            回会议
          </el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>

  <div class="panel">
    <div class="section-head">
      <h2>预测市场关注</h2>
      <div class="toolbar compact-toolbar">
        <el-select v-model="selectedWatchTeamId" placeholder="预测市场团队" style="width: 240px" @change="loadWatchlist">
          <el-option v-for="team in predictionTeams" :key="team.id" :label="team.name" :value="team.id" />
        </el-select>
        <el-button :loading="watchlistLoading" @click="loadWatchlist">刷新</el-button>
      </div>
    </div>

    <el-table :data="watchlist" empty-text="暂无关注市场">
      <el-table-column label="市场问题" min-width="320" show-overflow-tooltip>
        <template #default="{ row }">
          <div>{{ row.market?.question || `#${row.marketId}` }}</div>
          <div v-if="eventLabel(row.market)" class="muted">{{ eventLabel(row.market) }}</div>
        </template>
      </el-table-column>
      <el-table-column label="价格" width="150">
        <template #default="{ row }">{{ formatPrice(row.market?.lastTradePrice) }}</template>
      </el-table-column>
      <el-table-column label="状态" width="120">
        <template #default="{ row }">
          <el-tag :type="row.active ? 'success' : 'info'">{{ row.active ? '关注中' : '已停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="note" label="备注" min-width="220" show-overflow-tooltip />
      <el-table-column label="来源" min-width="180">
        <template #default="{ row }">
          <div v-if="row.sourceMeetingId" class="source-link">
            <el-button link type="primary" @click="openMeeting(row.sourceMeetingId)">
              会议 #{{ row.sourceMeetingId }}
            </el-button>
            <span v-if="row.sourceMeetingEventId" class="muted">事件 #{{ row.sourceMeetingEventId }}</span>
          </div>
          <span v-else class="muted">手工关注或无来源</span>
        </template>
      </el-table-column>
    </el-table>
  </div>

  <div class="panel">
    <div class="section-head">
      <h2>新闻匹配治理</h2>
      <div class="toolbar compact-toolbar">
        <el-select v-model="matchStatus" placeholder="状态" clearable style="width: 180px" @change="loadMatches">
          <el-option label="自动关联" value="linked" />
          <el-option label="人工确认" value="review_required" />
          <el-option label="已确认" value="confirmed" />
          <el-option label="已驳回" value="rejected" />
          <el-option label="候选" value="candidate" />
        </el-select>
        <el-button :loading="matchesLoading" @click="loadMatches">刷新</el-button>
      </div>
    </div>

    <el-table :data="matches" empty-text="暂无预测市场匹配样本">
      <el-table-column label="候选市场" min-width="300" show-overflow-tooltip>
        <template #default="{ row }">
          <div>{{ row.market?.question || `#${row.marketId}` }}</div>
          <div v-if="eventLabel(row.market)" class="muted">{{ eventLabel(row.market) }}</div>
        </template>
      </el-table-column>
      <el-table-column label="分数" width="110">
        <template #default="{ row }">{{ formatScore(row.score) }}</template>
      </el-table-column>
      <el-table-column label="状态" width="130">
        <template #default="{ row }">
          <el-tag :type="statusTag(row.status)">{{ statusLabel(row.status) }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="newsSnippet" label="新闻片段" min-width="280" show-overflow-tooltip />
      <el-table-column label="操作" width="210">
        <template #default="{ row }">
          <el-button link type="primary" @click="reviewMatch(row, 'confirmed')">确认</el-button>
          <el-button link type="danger" @click="reviewMatch(row, 'rejected')">驳回</el-button>
          <el-button v-if="row.marketId" link type="primary" :loading="watchingId === row.marketId" @click="watchMarketById(row.marketId, row.market?.question)">关注</el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>
</template>

<script setup lang="ts">
import { ElMessage } from 'element-plus'
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { api, apiErrorText, jsonapiResource, unwrapJsonApiCollection } from '../api'
import type { ApiResourceAttributes } from '../api'

type PredictionMarket = ApiResourceAttributes<'PredictionMarketResource'> & { id: number }
type PredictionMatch = ApiResourceAttributes<'PredictionMarketMatchResource'> & { id: number }
type PredictionWatchlistItem = ApiResourceAttributes<'PredictionWatchlistResource'> & { id: number }
type PredictionMarketIdentity = Pick<PredictionMarket, 'eventId' | 'externalEventId' | 'eventSlug' | 'eventTitle'>

type ResearchTeam = {
  id: number
  name?: string
  assetClass?: string
  active?: boolean
}

const query = ref('')
const markets = ref<PredictionMarket[]>([])
const searchMeta = ref<Record<string, unknown>>({})
const searchError = ref('')
const loading = ref(false)
const syncing = ref(false)
const teams = ref<ResearchTeam[]>([])
const selectedWatchTeamId = ref<number | undefined>()
const matches = ref<PredictionMatch[]>([])
const matchesLoading = ref(false)
const matchStatus = ref('review_required')
const watchlist = ref<PredictionWatchlistItem[]>([])
const watchlistLoading = ref(false)
const watchingId = ref<number | undefined>()
const route = useRoute()
const router = useRouter()
const linkedMarketId = computed(() => positiveNumber(route.query.marketId))
const sourceMeetingId = computed(() => positiveNumber(route.query.sourceMeetingId))
const predictionTeams = computed(() => teams.value.filter((team) => team.active !== false && (team.assetClass === 'prediction_market' || team.assetClass === 'mixed')))
const searchNotice = computed(() => {
  const original = String(searchMeta.value.query || '').trim()
  const normalized = String(searchMeta.value.normalizedQuery || '').trim()
  if (original && normalized && original !== normalized) return `已将 Polymarket URL/分享片段归一化为 slug：${normalized}`
  return ''
})
const searchWarning = computed(() => {
  const warning = String(searchMeta.value.providerWarning || '').trim()
  if (!warning) return ''
  const normalized = String(searchMeta.value.normalizedQuery || '').trim()
  const suffix = normalized ? `；本地缓存查询：${normalized}` : ''
  return `外部 Polymarket 搜索暂不可用，当前结果来自本地缓存${suffix}。原因：${warning}`
})
const linkedMeetingNotice = computed(() => sourceMeetingId.value && linkedMarketId.value ? `已打开会议 #${sourceMeetingId.value} 关联的预测市场 #${linkedMarketId.value}` : '')
const marketEmptyText = computed(() => {
  if (searchError.value) return '搜索失败，查看上方错误信息'
  if (query.value.trim()) return '没有匹配的预测市场；如果上方提示外部搜索不可用，说明本地缓存也没有命中'
  return '输入 Polymarket URL、slug 或关键词后搜索'
})

async function loadTeams() {
  try {
    const { data } = await api.get('/research-teams')
    teams.value = unwrapJsonApiCollection<ResearchTeam>(data)
    if (!selectedWatchTeamId.value || !predictionTeams.value.some((team) => team.id === selectedWatchTeamId.value)) {
      selectedWatchTeamId.value = predictionTeams.value[0]?.id
      if (!selectedWatchTeamId.value) watchlist.value = []
    }
  } catch (error) {
    ElMessage.error(apiErrorText(error, '预测市场团队加载失败'))
  }
}

async function searchMarkets() {
  loading.value = true
  searchError.value = ''
  try {
    const { data } = await api.get('/prediction-markets/search', { params: { q: query.value, 'page[limit]': 50 } })
    searchMeta.value = (data as any)?.meta || {}
    markets.value = unwrapJsonApiCollection<PredictionMarket>(data)
    await ensureLinkedMarketLoaded()
    if (searchMeta.value.providerWarning && markets.value.length > 0) {
      ElMessage.warning('已显示本地缓存，外部搜索暂不可用')
    }
  } catch (error) {
    searchMeta.value = { query: query.value, normalizedQuery: normalizePolymarketQuery(query.value) }
    markets.value = []
    searchError.value = predictionSearchErrorText(error)
    ElMessage.error(searchError.value)
    await ensureLinkedMarketLoaded()
  } finally {
    loading.value = false
  }
}

async function ensureLinkedMarketLoaded() {
  const marketId = linkedMarketId.value
  if (!marketId || markets.value.some((row) => row.id === marketId)) return
  try {
    const { data } = await api.get(`/prediction-markets/${marketId}`)
    const row = unwrapJsonApiCollection<PredictionMarket>({ data: [data?.data] })[0]
    if (row) markets.value = [row, ...markets.value]
  } catch {
    // The route may point to a deleted/local-only market; the watchlist still shows whatever the API can load.
  }
}

async function syncMarkets() {
  syncing.value = true
  try {
    await api.post('/prediction-markets/sync', null, { params: { limit: 100 } })
    ElMessage.success('预测市场同步完成')
    await searchMarkets()
  } catch (error) {
    ElMessage.error(apiErrorText(error, '预测市场同步失败'))
  } finally {
    syncing.value = false
  }
}

async function loadMatches() {
  matchesLoading.value = true
  try {
    const { data } = await api.get('/prediction-matches', { params: { status: matchStatus.value || undefined, 'page[limit]': 100 } })
    matches.value = unwrapJsonApiCollection<PredictionMatch>(data)
  } catch (error) {
    ElMessage.error(apiErrorText(error, '匹配样本加载失败'))
  } finally {
    matchesLoading.value = false
  }
}

async function reviewMatch(row: PredictionMatch, status: 'confirmed' | 'rejected') {
  try {
    await api.post(`/prediction-matches/${row.id}/review`, jsonapiResource('prediction-market-matches', { status, reason: 'manual review from console' }, String(row.id)))
    ElMessage.success(status === 'confirmed' ? '已确认匹配' : '已驳回匹配')
    await loadMatches()
  } catch (error) {
    ElMessage.error(apiErrorText(error, '匹配复核失败'))
  }
}

async function loadWatchlist() {
  if (!selectedWatchTeamId.value) {
    watchlist.value = []
    return
  }
  watchlistLoading.value = true
  try {
    const { data } = await api.get('/prediction-watchlist', { params: { researchTeamId: selectedWatchTeamId.value, limit: 100 } })
    watchlist.value = unwrapJsonApiCollection<PredictionWatchlistItem>(data)
  } catch (error) {
    ElMessage.error(apiErrorText(error, '预测市场关注加载失败'))
  } finally {
    watchlistLoading.value = false
  }
}

async function watchMarket(row: PredictionMarket) {
  if (!row.id) return
  await watchMarketById(row.id, row.question)
}

async function watchMarketById(marketId: number, question?: string) {
  if (!selectedWatchTeamId.value) {
    ElMessage.warning('请先创建或选择预测市场投研团队')
    return
  }
  watchingId.value = marketId
  try {
    await api.post('/prediction-watchlist', jsonapiResource('prediction-watchlist-items', {
      researchTeamId: selectedWatchTeamId.value,
      marketId,
      active: true,
      note: question ? `关注：${question}` : '手动关注'
    }))
    ElMessage.success('已加入预测市场关注')
    await loadWatchlist()
  } catch (error) {
    ElMessage.error(apiErrorText(error, '预测市场关注失败'))
  } finally {
    watchingId.value = undefined
  }
}

function numberValue(value: unknown) {
  const n = Number(value)
  return Number.isFinite(n) ? n : undefined
}

function formatPrice(value: unknown) {
  const n = numberValue(value)
  return n === undefined || n === 0 ? '-' : n.toFixed(3)
}

function formatSpread(value: unknown) {
  const n = numberValue(value)
  return n === undefined || n === 0 ? 'spread -' : `spread ${n.toFixed(3)}`
}

function formatNumber(value: unknown) {
  const n = numberValue(value)
  return n === undefined ? '-' : Intl.NumberFormat('zh-CN', { maximumFractionDigits: 0 }).format(n)
}

function formatScore(value: unknown) {
  const n = numberValue(value)
  return n === undefined ? '-' : n.toFixed(2)
}

function tokenCount(row: PredictionMarket) {
  return Array.isArray(row.clobTokenIds) ? row.clobTokenIds.filter(Boolean).length : 0
}

function tokenStatus(row: PredictionMarket) {
  const count = tokenCount(row)
  return count > 0 ? `CLOB ${count}` : '缺 token'
}

function eventLabel(row?: Partial<PredictionMarketIdentity>) {
  if (!row) return ''
  const title = String(row.eventTitle || '').trim()
  const slug = String(row.eventSlug || '').trim()
  const external = String(row.externalEventId || '').trim()
  if (title && slug) return `${title} · ${slug}`
  if (title) return title
  if (slug) return slug
  if (external) return `event ${external}`
  return row.eventId ? `event #${row.eventId}` : ''
}

function statusLabel(value?: string) {
  return ({ linked: '自动关联', review_required: '人工确认', confirmed: '已确认', rejected: '已驳回', candidate: '候选' } as Record<string, string>)[value || ''] || value || '-'
}

function statusTag(value?: string) {
  if (value === 'linked' || value === 'confirmed') return 'success'
  if (value === 'review_required') return 'warning'
  if (value === 'rejected') return 'danger'
  return 'info'
}

function positiveNumber(value: unknown) {
  const raw = Array.isArray(value) ? value[0] : value
  const n = Number(raw)
  return Number.isFinite(n) && n > 0 ? n : undefined
}

function isLinkedMarket(row: PredictionMarket) {
  return Boolean(linkedMarketId.value && row.id === linkedMarketId.value)
}

function openMeeting(meetingId?: number) {
  if (!meetingId) return
  void router.push(`/meetings/${meetingId}`)
}

function predictionSearchErrorText(error: unknown) {
  const text = apiErrorText(error, '预测市场搜索失败')
  const normalized = String(searchMeta.value.normalizedQuery || '').trim()
  const queryText = query.value.trim()
  const hint = normalized && normalized !== queryText ? `已尝试使用归一化 slug「${normalized}」。` : ''
  return `${text}。${hint}外部 Polymarket 搜索失败且本地缓存没有命中时，页面无法展示实时结果；请先同步活跃市场或稍后重试。`
}

function normalizePolymarketQuery(value: string) {
  const trimmed = value.trim()
  if (!trimmed) return ''
  const fromUrl = polymarketSlugFromUrl(trimmed)
  if (fromUrl) return fromUrl
  const plain = trimmed.split('#')[0]?.split('?')[0]?.trim() || ''
  if (/^[A-Za-z0-9_-]+(?:-[A-Za-z0-9_-]+)+$/.test(plain)) return plain.toLowerCase()
  return trimmed
}

function polymarketSlugFromUrl(value: string) {
  const candidates = [value]
  if (/^(?:www\.)?polymarket\.com(?:[/?#]|$)/i.test(value)) {
    candidates.push(`https://${value}`)
  }
  for (const candidate of candidates) {
    try {
      const url = new URL(candidate)
      if (!/(^|\.)polymarket\.com$/i.test(url.hostname)) continue
      const parts = url.pathname.split('/').filter(Boolean)
      for (let i = 0; i + 1 < parts.length; i += 1) {
        if (!['event', 'events', 'market', 'markets'].includes(parts[i].toLowerCase())) continue
        return decodeURIComponent(parts[i + 1]).trim().toLowerCase()
      }
    } catch {
      // Keep trying the protocol-prefixed candidate.
    }
  }
  return ''
}

onMounted(() => {
  void (async () => {
    await loadTeams()
    if (linkedMarketId.value) {
      await ensureLinkedMarketLoaded()
    }
    await Promise.all([loadMatches(), loadWatchlist()])
  })()
})
</script>

<style scoped>
.inline-alert {
  margin-bottom: 12px;
}

.market-title-line,
.source-link {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.market-title-line span,
.source-link span {
  min-width: 0;
  overflow-wrap: anywhere;
}
</style>
