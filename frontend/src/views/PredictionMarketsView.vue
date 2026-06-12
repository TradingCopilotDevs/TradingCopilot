<template>
  <h1 class="page-title">预测市场</h1>

  <div class="panel">
    <div class="section-head">
      <h2>Polymarket 市场发现</h2>
      <div class="toolbar compact-toolbar">
        <el-input v-model="query" placeholder="搜索事件、问题或 slug" clearable @keyup.enter="searchMarkets" />
        <el-button :loading="loading" @click="searchMarkets">搜索</el-button>
        <el-button type="primary" :loading="syncing" @click="syncMarkets">同步活跃市场</el-button>
      </div>
    </div>

    <el-table :data="markets" empty-text="输入关键词后搜索 Polymarket 市场">
      <el-table-column prop="question" label="市场问题" min-width="320" show-overflow-tooltip />
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
      <el-table-column prop="slug" label="Slug" min-width="180" show-overflow-tooltip />
      <el-table-column label="操作" width="100" fixed="right">
        <template #default="{ row }">
          <el-button link type="primary" :loading="watchingId === row.id" @click="watchMarket(row)">关注</el-button>
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
        <template #default="{ row }">{{ row.market?.question || `#${row.marketId}` }}</template>
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
        <template #default="{ row }">{{ row.market?.question || `#${row.marketId}` }}</template>
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
import { api, apiErrorText, jsonapiResource, unwrapJsonApiCollection } from '../api'

type PredictionMarket = {
  id: number
  question?: string
  slug?: string
  active?: boolean
  closed?: boolean
  restricted?: boolean
  lastTradePrice?: unknown
  spread?: unknown
  liquidity?: unknown
}

type PredictionMatch = {
  id: number
  marketId?: number
  market?: PredictionMarket
  score?: unknown
  status?: string
  newsSnippet?: string
}

type PredictionWatchlistItem = {
  id: number
  marketId?: number
  market?: PredictionMarket
  note?: string
  active?: boolean
}

type ResearchTeam = {
  id: number
  name?: string
  assetClass?: string
  active?: boolean
}

const query = ref('')
const markets = ref<PredictionMarket[]>([])
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
const predictionTeams = computed(() => teams.value.filter((team) => team.active !== false && (team.assetClass === 'prediction_market' || team.assetClass === 'mixed')))

async function loadTeams() {
  try {
    const { data } = await api.get('/research-teams')
    teams.value = unwrapJsonApiCollection<ResearchTeam>(data)
    if (!selectedWatchTeamId.value) {
      selectedWatchTeamId.value = predictionTeams.value[0]?.id
    }
  } catch (error) {
    ElMessage.error(apiErrorText(error, '预测市场团队加载失败'))
  }
}

async function searchMarkets() {
  loading.value = true
  try {
    const { data } = await api.get('/prediction-markets/search', { params: { q: query.value, 'page[limit]': 50 } })
    markets.value = unwrapJsonApiCollection<PredictionMarket>(data)
  } catch (error) {
    ElMessage.error(apiErrorText(error, '预测市场搜索失败'))
  } finally {
    loading.value = false
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

function statusLabel(value?: string) {
  return ({ linked: '自动关联', review_required: '人工确认', confirmed: '已确认', rejected: '已驳回', candidate: '候选' } as Record<string, string>)[value || ''] || value || '-'
}

function statusTag(value?: string) {
  if (value === 'linked' || value === 'confirmed') return 'success'
  if (value === 'review_required') return 'warning'
  if (value === 'rejected') return 'danger'
  return 'info'
}

onMounted(() => {
  void (async () => {
    await loadTeams()
    await Promise.all([loadMatches(), loadWatchlist()])
  })()
})
</script>
