<template>
  <h1 class="page-title">行情与自选</h1>

  <div class="panel">
    <div class="section-head">
      <h2>股票搜索</h2>
      <div class="toolbar compact-toolbar">
        <el-input v-model="symbolQuery" placeholder="输入代码或名称搜索候选" clearable @keyup.enter="loadSymbols" />
        <el-button :loading="symbolLoading" @click="loadSymbols">搜索</el-button>
        <el-button type="primary" :loading="syncingSymbols" @click="syncSymbols">同步股票信息</el-button>
        <el-button @click="openSymbol()">手动新增</el-button>
      </div>
    </div>

    <div v-if="isMobile" class="mobile-card-list">
      <div v-for="symbol in symbols" :key="symbol.code" class="mobile-card">
        <div class="mobile-card__header">
          <div>
            <h3 class="mobile-card__title">{{ symbol.code }} {{ symbol.name }}</h3>
            <div class="muted">{{ symbol.exchange || 'CN' }}</div>
          </div>
          <el-tag :type="symbol.active ? 'success' : 'info'">{{ symbol.active ? '启用' : '停用' }}</el-tag>
        </div>
        <div class="mobile-card__meta">
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">行业</span>
            <span>{{ symbol.industry || '-' }}</span>
          </div>
        </div>
        <div class="mobile-card__actions">
          <el-button type="primary" plain @click="openDetail(symbol)">查看行情</el-button>
          <el-button plain @click="openSymbol(symbol)">编辑</el-button>
          <el-button plain type="danger" @click="deleteSymbol(symbol)">删除</el-button>
        </div>
      </div>
      <el-empty
        v-if="!symbols.length"
        description="输入代码或名称后显示候选，不默认展示全量股票库"
      />
    </div>

    <el-table v-else :data="symbols" empty-text="输入代码或名称后显示候选，不默认展示全量股票库">
      <el-table-column prop="code" label="代码" width="120" />
      <el-table-column prop="name" label="名称" width="180" />
      <el-table-column prop="exchange" label="交易所" width="100" />
      <el-table-column prop="industry" label="行业" />
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="row.active ? 'success' : 'info'">{{ row.active ? '启用' : '停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="210">
        <template #default="{ row }">
          <el-button link type="primary" @click="openDetail(row)">查看行情</el-button>
          <el-button link type="primary" @click="openSymbol(row)">编辑</el-button>
          <el-button link type="danger" @click="deleteSymbol(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="cursor-pagination-footer">
      <span class="muted">已加载 {{ symbols.length }} 条</span>
      <el-button v-if="symbolNextCursor" plain :loading="symbolLoadingMore" @click="loadMoreSymbols">加载更多</el-button>
    </div>
  </div>

  <div class="panel">
    <div class="section-head">
      <h2>自选池</h2>
      <div class="toolbar compact-toolbar">
        <el-select v-model="selectedTeamId" placeholder="投研团队" style="width: 200px" @change="loadWatchlist">
          <el-option v-for="team in teams" :key="team.id" :label="team.name" :value="team.id" />
        </el-select>
        <el-button :loading="watchlistLoading" @click="loadWatchlist">刷新市价</el-button>
        <el-button type="primary" @click="openWatch()">添加自选</el-button>
      </div>
    </div>

    <div v-if="isMobile" class="mobile-card-list">
      <div v-for="item in watchlist" :key="item.id" class="mobile-card">
        <div class="mobile-card__header">
          <div>
            <h3 class="mobile-card__title">{{ item.code }} {{ item.symbolName }}</h3>
            <div class="muted">{{ item.exchange || 'CN' }}</div>
          </div>
          <el-tag :type="item.active ? 'success' : 'info'">{{ item.active ? '启用' : '停用' }}</el-tag>
        </div>
        <div class="mobile-card__meta">
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">当前价</span>
            <span>{{ formatNumber(item.price) }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">涨跌幅</span>
            <span :class="quoteClass(item.changePct)">{{ formatPercent(item.changePct) }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">备注</span>
            <span>{{ item.note || '-' }}</span>
          </div>
        </div>
        <div class="mobile-card__actions">
          <el-button type="primary" plain @click="openDetail(item)">查看行情</el-button>
          <el-button plain @click="openWatch(item)">编辑</el-button>
          <el-button plain type="danger" @click="deleteWatch(item)">删除</el-button>
        </div>
      </div>
      <el-empty v-if="!watchlist.length" description="暂无自选" />
    </div>

    <el-table v-else :data="watchlist" empty-text="暂无自选">
      <el-table-column prop="code" label="代码" width="120" />
      <el-table-column prop="symbolName" label="名称" width="180" />
      <el-table-column prop="exchange" label="交易所" width="100" />
      <el-table-column label="当前价" width="120">
        <template #default="{ row }">{{ formatNumber(row.price) }}</template>
      </el-table-column>
      <el-table-column label="涨跌幅" width="120">
        <template #default="{ row }">
          <span :class="quoteClass(row.changePct)">{{ formatPercent(row.changePct) }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="note" label="备注" />
      <el-table-column label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="row.active ? 'success' : 'info'">{{ row.active ? '启用' : '停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="操作" width="210">
        <template #default="{ row }">
          <el-button link type="primary" @click="openDetail(row)">查看行情</el-button>
          <el-button link type="primary" @click="openWatch(row)">编辑</el-button>
          <el-button link type="danger" @click="deleteWatch(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>

    <div class="cursor-pagination-footer">
      <span class="muted">已加载 {{ watchlist.length }} 条</span>
      <el-button v-if="watchlistNextCursor" plain :loading="watchlistLoadingMore" @click="loadMoreWatchlist">加载更多</el-button>
    </div>
  </div>

  <el-dialog v-model="symbolDialog" title="股票信息" :fullscreen="isMobile" :width="symbolDialogWidth">
    <el-form
      :model="symbolForm"
      :label-width="isMobile ? 'auto' : '90px'"
      :label-position="isMobile ? 'top' : 'right'"
    >
      <el-form-item label="代码"><el-input v-model="symbolForm.code" :disabled="Boolean(editingSymbolCode)" /></el-form-item>
      <el-form-item label="名称"><el-input v-model="symbolForm.name" /></el-form-item>
      <el-form-item label="交易所">
        <el-select v-model="symbolForm.exchange">
          <el-option label="上海" value="SH" />
          <el-option label="深圳" value="SZ" />
          <el-option label="北京" value="BJ" />
          <el-option label="中国" value="CN" />
        </el-select>
      </el-form-item>
      <el-form-item label="行业"><el-input v-model="symbolForm.industry" /></el-form-item>
      <el-form-item label="启用"><el-switch v-model="symbolForm.active" /></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="symbolDialog = false">取消</el-button>
      <el-button type="primary" @click="saveSymbol">保存</el-button>
    </template>
  </el-dialog>

  <el-dialog v-model="watchDialog" title="自选股票" :fullscreen="isMobile" :width="watchDialogWidth">
    <el-form
      :model="watchForm"
      :label-width="isMobile ? 'auto' : '90px'"
      :label-position="isMobile ? 'top' : 'right'"
    >
      <el-form-item label="股票">
        <el-select
          v-model="watchForm.code"
          filterable
          remote
          reserve-keyword
          placeholder="输入代码或名称搜索候选"
          :remote-method="searchCandidates"
          :loading="candidateLoading"
        >
          <el-option
            v-for="symbol in candidates"
            :key="symbol.code"
            :label="`${symbol.code} / ${symbol.name} / ${symbol.exchange}`"
            :value="symbol.code"
          />
        </el-select>
      </el-form-item>
      <el-form-item label="投研团队">
        <el-select v-model="watchForm.researchTeamId" style="width: 100%">
          <el-option v-for="team in teams" :key="team.id" :label="team.name" :value="team.id" />
        </el-select>
      </el-form-item>
      <el-form-item label="备注"><el-input v-model="watchForm.note" type="textarea" :rows="3" /></el-form-item>
      <el-form-item label="启用"><el-switch v-model="watchForm.active" /></el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="watchDialog = false">取消</el-button>
      <el-button type="primary" @click="saveWatch">保存</el-button>
    </template>
  </el-dialog>

  <el-drawer
    v-model="detailDrawer"
    :title="detailTitle"
    :size="drawerSize"
    @opened="loadSeries"
    @closed="disposeChart"
  >
    <div class="quote-strip">
      <div>
        <span>当前价</span>
        <strong>{{ formatNumber(currentQuote?.price) }}</strong>
      </div>
      <div>
        <span>涨跌幅</span>
        <strong :class="quoteClass(currentQuote?.changePct)">{{ formatPercent(currentQuote?.changePct) }}</strong>
      </div>
      <div>
        <span>成交量</span>
        <strong>{{ formatNumber(currentQuote?.volume) }}</strong>
      </div>
      <div>
        <span>更新时间</span>
        <strong>{{ formatDateTimeUtc8(currentQuote?.quoteTime) }}</strong>
      </div>
    </div>

    <el-tabs v-model="seriesRange" @tab-change="loadSeries">
      <el-tab-pane label="分时" name="intraday" />
      <el-tab-pane label="五日" name="five_day" />
      <el-tab-pane label="日线" name="daily" />
      <el-tab-pane label="周线" name="weekly" />
      <el-tab-pane label="月线" name="monthly" />
    </el-tabs>

    <div ref="chartContainer" class="financial-chart" v-loading="seriesLoading">
      <el-empty v-if="!seriesLoading && !seriesRows.length" description="暂无行情序列" />
    </div>

    <div class="table-scroll">
      <el-table :data="seriesRows" :height="isMobile ? 280 : 320" empty-text="暂无数据" min-width="640">
        <el-table-column prop="time" label="时间" width="180" />
        <el-table-column prop="open" label="开" />
        <el-table-column prop="high" label="高" />
        <el-table-column prop="low" label="低" />
        <el-table-column prop="close" label="收" />
        <el-table-column prop="volume" label="量" />
      </el-table>
    </div>
  </el-drawer>
</template>

<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  AreaSeries,
  type BusinessDay,
  CandlestickSeries,
  ColorType,
  createChart,
  HistogramSeries,
  isBusinessDay,
  isUTCTimestamp,
  type IChartApi,
  type ISeriesApi,
  TickMarkType,
  type Time
} from 'lightweight-charts'
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { api, apiErrorText, jsonapiResource, unwrapJsonApiCollection, unwrapJsonApiResource } from '../api'
import { nextCursorFromDocument, useCursorPagination } from '../composables/useCursorPagination'
import { useResponsive } from '../composables/useResponsive'
import { formatDateTimeUtc8, getUtc8DateParts, parseBusinessDate, toUnixSecondsUtc8 } from '../utils/datetime'

const { isMobile, isTablet } = useResponsive()
const teams = ref<any[]>([])
const selectedTeamId = ref<number | null>(null)
const candidates = ref<any[]>([])
const symbolQuery = ref('')
const syncingSymbols = ref(false)
const candidateLoading = ref(false)
const symbolDialog = ref(false)
const watchDialog = ref(false)
const detailDrawer = ref(false)
const seriesLoading = ref(false)
const editingSymbolCode = ref<string | null>(null)
const editingWatchId = ref<number | null>(null)
const selectedSymbol = ref<any | null>(null)
const currentQuote = ref<any | null>(null)
const seriesRange = ref('intraday')
const seriesRows = ref<any[]>([])
const chartContainer = ref<HTMLElement | null>(null)
let chart: IChartApi | null = null
let priceSeries: ISeriesApi<'Area'> | ISeriesApi<'Candlestick'> | null = null
let volumeSeries: ISeriesApi<'Histogram'> | null = null
const chartLocale = 'zh-CN'

const symbolDialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '560px'))
const watchDialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '620px'))
const drawerSize = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '820px'))

const symbolForm = reactive<any>({
  code: '',
  name: '',
  exchange: 'CN',
  industry: '',
  active: true
})

const watchForm = reactive<any>({
  researchTeamId: null,
  code: '',
  note: '',
  active: true
})

const detailTitle = computed(() => {
  if (!selectedSymbol.value) return '行情详情'
  return `${selectedSymbol.value.code} ${selectedSymbol.value.name || selectedSymbol.value.symbolName || ''}`
})

const {
  items: symbols,
  nextCursor: symbolNextCursor,
  loading: symbolLoading,
  loadingMore: symbolLoadingMore,
  loadFirstPage: loadFirstSymbolPage,
  loadMore: loadMoreSymbolsPage,
  reset: resetSymbols
} = useCursorPagination<any>(fetchSymbolPage)

const {
  items: watchlist,
  nextCursor: watchlistNextCursor,
  loading: watchlistLoading,
  loadingMore: watchlistLoadingMore,
  loadFirstPage: loadFirstWatchlistPage,
  loadMore: loadMoreWatchlistPage,
  reset: resetWatchlist
} = useCursorPagination<any>(fetchWatchlistPage)

function inferExchangeForCode(code: string) {
  if (code.startsWith('5') || code.startsWith('6')) return 'SH'
  if (code.startsWith('15') || code.startsWith('16') || code.startsWith('0') || code.startsWith('3')) return 'SZ'
  if (code.startsWith('4') || code.startsWith('8') || code.startsWith('92')) return 'BJ'
  return 'CN'
}

function buildManualCandidate(query: string) {
  const normalized = query.trim()
  if (
    !/^(5\d{5}|15\d{4}|16\d{4}|688\d{3}|(600|601|603|605)\d{3}|(300|301)\d{3}|(000|001|002|003)\d{3}|(4\d{5}|8\d{5}|92\d{4}))$/.test(normalized)
  ) {
    return null
  }
  const isEtfLof = normalized.startsWith('5') || normalized.startsWith('15') || normalized.startsWith('16')
  return {
    code: normalized,
    name: isEtfLof ? '手工录入 ETF/LOF' : '手工录入标的',
    exchange: inferExchangeForCode(normalized)
  }
}

async function fetchSymbolPage(cursor?: string) {
  const { data } = await api.get('/market/symbols', {
    params: { q: symbolQuery.value, 'page[limit]': 100, 'page[cursor]': cursor || undefined }
  })
  return {
    items: unwrapJsonApiCollection<any>(data),
    nextCursor: nextCursorFromDocument(data)
  }
}

async function loadSymbols() {
  if (!symbolQuery.value.trim()) {
    resetSymbols()
    return
  }
  await loadFirstSymbolPage()
}

async function loadMoreSymbols() {
  await loadMoreSymbolsPage()
}

async function fetchWatchlistPage(cursor?: string) {
  const { data } = await api.get('/market/watchlist', {
    params: { researchTeamId: selectedTeamId.value, 'page[limit]': 100, 'page[cursor]': cursor || undefined }
  })
  return {
    items: unwrapJsonApiCollection<any>(data),
    nextCursor: nextCursorFromDocument(data)
  }
}

async function loadWatchlist() {
  if (!selectedTeamId.value) {
    resetWatchlist()
    return
  }
  await loadFirstWatchlistPage()
}

async function loadMoreWatchlist() {
  await loadMoreWatchlistPage()
}

async function loadTeams() {
  teams.value = unwrapJsonApiCollection((await api.get('/research-teams')).data)
  if (!selectedTeamId.value && teams.value.length) selectedTeamId.value = teams.value[0].id
}

async function syncSymbols() {
  syncingSymbols.value = true
  try {
    const { data } = await api.post('/market/symbols/sync')
    const result = unwrapJsonApiResource(data)
    ElMessage.success(`已同步 ${result?.synced ?? 0} 条股票信息`)
    await loadSymbols()
  } finally {
    syncingSymbols.value = false
  }
}

function openSymbol(symbol?: any) {
  editingSymbolCode.value = symbol?.code ?? null
  Object.assign(symbolForm, {
    code: symbol?.code ?? '',
    name: symbol?.name ?? '',
    exchange: symbol?.exchange ?? 'CN',
    industry: symbol?.industry ?? '',
    active: symbol?.active ?? true
  })
  symbolDialog.value = true
}

async function saveSymbol() {
  if (editingSymbolCode.value) {
    await api.put(`/market/symbols/${editingSymbolCode.value}`, jsonapiResource('market-symbols', symbolForm, editingSymbolCode.value))
  } else {
    await api.post('/market/symbols', jsonapiResource('market-symbols', symbolForm))
  }
  symbolDialog.value = false
  ElMessage.success('股票信息已保存')
  await loadSymbols()
}

async function deleteSymbol(symbol: any) {
  await ElMessageBox.confirm(`确认删除 ${symbol.code} ${symbol.name}？`, '删除股票信息', { type: 'warning' })
  await api.delete(`/market/symbols/${symbol.code}`)
  ElMessage.success('已删除')
  await loadSymbols()
}

async function searchCandidates(query: string) {
  if (!query.trim()) {
    candidates.value = []
    return
  }
  candidateLoading.value = true
  try {
    const { data } = await api.get('/market/symbols', { params: { q: query, 'page[limit]': 50 } })
    const nextCandidates = unwrapJsonApiCollection<any>(data)
    const manualCandidate = buildManualCandidate(query)
    if (manualCandidate && !nextCandidates.some((item) => item.code === manualCandidate.code)) {
      nextCandidates.unshift(manualCandidate)
    }
    candidates.value = nextCandidates
  } finally {
    candidateLoading.value = false
  }
}

function openWatch(item?: any) {
  editingWatchId.value = item?.id ?? null
  Object.assign(watchForm, {
    researchTeamId: item?.researchTeamId ?? selectedTeamId.value,
    code: item?.code ?? '',
    note: item?.note ?? '',
    active: item?.active ?? true
  })
  candidates.value = item?.code
    ? [{ code: item.code, name: item.symbolName || item.code, exchange: item.exchange || 'CN' }]
    : []
  watchDialog.value = true
}

async function saveWatch() {
  if (!watchForm.researchTeamId) {
    ElMessage.warning('请选择投研团队')
    return
  }
  if (editingWatchId.value) {
    await api.put(`/market/watchlist/${editingWatchId.value}`, jsonapiResource('market-watchlist-items', watchForm, String(editingWatchId.value)))
  } else {
    await api.post('/market/watchlist', jsonapiResource('market-watchlist-items', watchForm))
  }
  watchDialog.value = false
  ElMessage.success('自选已保存')
  await loadWatchlist()
}

async function deleteWatch(item: any) {
  await ElMessageBox.confirm(`确认从自选删除 ${item.code}？`, '删除自选', { type: 'warning' })
  await api.delete(`/market/watchlist/${item.id}`)
  ElMessage.success('已删除')
  await loadWatchlist()
}

async function openDetail(row: any) {
  selectedSymbol.value = { ...row, name: row.name || row.symbolName }
  currentQuote.value = row.price ? row : null
  seriesRange.value = 'intraday'
  detailDrawer.value = true
  try {
    currentQuote.value = unwrapJsonApiResource((await api.get(`/market/quotes/${row.code}`)).data)
  } catch (error: unknown) {
    ElMessage.warning(apiErrorText(error, '当前行情获取失败'))
  }
}

async function loadSeries() {
  if (!selectedSymbol.value) return
  seriesLoading.value = true
  try {
    const { data } = await api.get(`/market/series/${selectedSymbol.value.code}`, {
      params: { range: seriesRange.value }
    })
    seriesRows.value = unwrapJsonApiCollection(data)
    await nextTick()
    renderChart()
  } catch (error: unknown) {
    seriesRows.value = []
    renderChart()
    ElMessage.warning(apiErrorText(error, '行情序列获取失败'))
  } finally {
    seriesLoading.value = false
  }
}

function renderChart() {
  if (!chartContainer.value) return
  disposeChart()
  if (!seriesRows.value.length) return

  chart = createChart(chartContainer.value, {
    autoSize: true,
    layout: {
      background: { type: ColorType.Solid, color: '#ffffff' },
      textColor: '#334155',
      attributionLogo: false
    },
    grid: {
      vertLines: { color: '#eef2f7' },
      horzLines: { color: '#eef2f7' }
    },
    rightPriceScale: {
      borderColor: '#dbe3ee',
      scaleMargins: {
        top: 0.08,
        bottom: seriesRange.value === 'intraday' || seriesRange.value === 'five_day' ? 0.08 : 0.26
      }
    },
    timeScale: {
      borderColor: '#dbe3ee',
      timeVisible: seriesRange.value === 'intraday' || seriesRange.value === 'five_day',
      secondsVisible: false,
      tickMarkFormatter: formatTickMark
    },
    localization: {
      locale: chartLocale,
      dateFormat: 'yyyy-MM-dd',
      priceFormatter: (price: number) => price.toFixed(2),
      timeFormatter: formatCrosshairTime
    }
  })

  if (seriesRange.value === 'intraday' || seriesRange.value === 'five_day') {
    priceSeries = chart.addSeries(AreaSeries, {
      lineColor: '#2563eb',
      topColor: 'rgba(37, 99, 235, 0.22)',
      bottomColor: 'rgba(37, 99, 235, 0.02)',
      lineWidth: 2
    })
    priceSeries.setData(toLineData())
  } else {
    priceSeries = chart.addSeries(CandlestickSeries, {
      upColor: '#dc2626',
      downColor: '#059669',
      borderUpColor: '#dc2626',
      borderDownColor: '#059669',
      wickUpColor: '#dc2626',
      wickDownColor: '#059669'
    })
    priceSeries.setData(toCandleData())
    volumeSeries = chart.addSeries(HistogramSeries, {
      priceFormat: { type: 'volume' },
      priceScaleId: 'volume',
      color: 'rgba(100, 116, 139, 0.35)'
    })
    volumeSeries.priceScale().applyOptions({
      scaleMargins: {
        top: 0.78,
        bottom: 0
      }
    })
    volumeSeries.setData(toVolumeData())
  }

  applyInitialVisibleRange()
}

function disposeChart() {
  if (chart) {
    chart.remove()
    chart = null
    priceSeries = null
    volumeSeries = null
  }
}

function toLineData() {
  const deduped = new Map<string, { time: Time; value: number }>()
  for (const row of seriesRows.value) {
    const value = Number(row.close)
    const time = parseChartTime(row.time)
    if (!time || !Number.isFinite(value)) continue
    deduped.set(String(row.time), { time, value })
  }
  return Array.from(deduped.values())
}

function toCandleData() {
  const deduped = new Map<
    string,
    { time: Time; open: number; high: number; low: number; close: number }
  >()
  for (const row of seriesRows.value) {
    const item = {
      time: parseChartTime(row.time),
      open: Number(row.open),
      high: Number(row.high),
      low: Number(row.low),
      close: Number(row.close)
    }
    if (
      !item.time ||
      !Number.isFinite(item.open) ||
      !Number.isFinite(item.high) ||
      !Number.isFinite(item.low) ||
      !Number.isFinite(item.close)
    ) {
      continue
    }
    deduped.set(String(row.time), item)
  }
  return Array.from(deduped.values())
}

function toVolumeData() {
  const deduped = new Map<string, { time: Time; value: number; color: string }>()
  for (const row of seriesRows.value) {
    const value = Number(row.volume)
    const time = parseChartTime(row.time)
    if (!time || !Number.isFinite(value)) continue
    const open = Number(row.open)
    const close = Number(row.close)
    const rising = Number.isFinite(open) && Number.isFinite(close) ? close >= open : true
    deduped.set(String(row.time), {
      time,
      value,
      color: rising ? 'rgba(220, 38, 38, 0.28)' : 'rgba(5, 150, 105, 0.28)'
    })
  }
  return Array.from(deduped.values())
}

function applyInitialVisibleRange() {
  if (!chart) return
  const rowCount = seriesRows.value.length
  if (rowCount === 0) return
  if (seriesRange.value === 'intraday' || seriesRange.value === 'five_day') {
    chart.timeScale().fitContent()
    return
  }
  const visibleBars = seriesRange.value === 'monthly' ? 90 : seriesRange.value === 'weekly' ? 120 : 160
  const from = Math.max(rowCount - visibleBars, 0)
  chart.timeScale().setVisibleLogicalRange({
    from,
    to: rowCount - 1
  })
}

function parseChartTime(value: string): Time {
  if (seriesRange.value === 'intraday' || seriesRange.value === 'five_day') {
    const timestamp = toUnixSecondsUtc8(value)
    return (timestamp ?? 0) as Time
  }
  return (parseBusinessDay(value) ?? value.slice(0, 10)) as Time
}

function parseBusinessDay(value: string): BusinessDay | null {
  const parts = parseBusinessDate(value)
  if (!parts) return null
  return parts
}

function formatCrosshairTime(time: Time): string {
  if (isUTCTimestamp(time)) {
    const parts = getUtc8DateParts(Number(time) * 1000)
    if (!parts) return ''
    return `${parts.year}-${parts.month}-${parts.day} ${parts.hour}:${parts.minute}`
  }
  const businessDay = toBusinessDay(time)
  if (!businessDay) return ''
  return `${businessDay.year}-${pad2(businessDay.month)}-${pad2(businessDay.day)}`
}

function formatTickMark(time: Time, tickMarkType: TickMarkType): string {
  if (isUTCTimestamp(time)) {
    const parts = getUtc8DateParts(Number(time) * 1000)
    if (!parts) return ''
    if (
      tickMarkType === TickMarkType.DayOfMonth ||
      tickMarkType === TickMarkType.Month ||
      tickMarkType === TickMarkType.Year
    ) {
      return `${parts.month}-${parts.day}`
    }
    return `${parts.hour}:${parts.minute}`
  }
  const businessDay = toBusinessDay(time)
  if (!businessDay) return ''
  if (tickMarkType === TickMarkType.Year) return String(businessDay.year)
  return `${pad2(businessDay.month)}-${pad2(businessDay.day)}`
}

function toBusinessDay(time: Time): BusinessDay | null {
  if (isBusinessDay(time)) return time
  if (typeof time === 'string') return parseBusinessDay(time)
  return null
}

function pad2(value: number) {
  return String(value).padStart(2, '0')
}

function formatNumber(value: unknown) {
  if (value === null || value === undefined || value === '') return '-'
  const numeric = Number(value)
  return Number.isFinite(numeric) ? numeric.toLocaleString() : '-'
}

function formatPercent(value: unknown) {
  if (value === null || value === undefined || value === '') return '-'
  const numeric = Number(value)
  return Number.isFinite(numeric) ? `${numeric.toFixed(2)}%` : '-'
}

function quoteClass(value: unknown) {
  const numeric = Number(value)
  if (!Number.isFinite(numeric)) return ''
  return numeric > 0 ? 'quote-up' : numeric < 0 ? 'quote-down' : ''
}

onMounted(async () => {
  await loadTeams()
  await loadWatchlist()
})
onBeforeUnmount(disposeChart)
</script>
