<template>
  <h1 class="page-title">模拟盘</h1>

  <div class="metrics-grid">
    <div class="metric-card panel">
      <span>账户数</span>
      <strong>{{ overview?.accountCount ?? 0 }}</strong>
    </div>
    <div class="metric-card panel">
      <span>总资产</span>
      <strong>{{ formatMoney(overview?.totalEquity) }}</strong>
    </div>
    <div class="metric-card panel">
      <span>总浮盈亏</span>
      <strong :class="pnlClass(overview?.totalUnrealizedPnl)">{{ formatMoney(overview?.totalUnrealizedPnl) }}</strong>
    </div>
    <div class="metric-card panel">
      <span>总已实现盈亏</span>
      <strong :class="pnlClass(overview?.totalRealizedPnl)">{{ formatMoney(overview?.totalRealizedPnl) }}</strong>
    </div>
    <div class="metric-card panel">
      <span>待执行订单</span>
      <strong>{{ overview?.pendingOrderCount ?? 0 }}</strong>
    </div>
    <div class="metric-card panel">
      <span>持仓数</span>
      <strong>{{ overview?.positionCount ?? 0 }}</strong>
    </div>
    <div class="metric-card panel">
      <span>成交笔数</span>
      <strong>{{ overview?.fillCount ?? 0 }}</strong>
    </div>
    <div class="metric-card panel">
      <span>总收益率</span>
      <strong :class="pnlClass(overview?.totalReturnPct)">{{ formatPercent(overview?.totalReturnPct) }}</strong>
    </div>
    <div class="metric-card panel">
      <span>现金 / 市值</span>
      <strong class="metric-card__compact">{{ formatMoney(overview?.totalCash) }} / {{ formatMoney(overview?.totalMarketValue) }}</strong>
    </div>
    <div class="metric-card panel">
      <span>订单状态</span>
      <strong class="metric-card__compact">
        建议 {{ overview?.suggestedOrderCount ?? 0 }} · 成交 {{ overview?.filledOrderCount ?? 0 }} · 失败 {{ overview?.rejectedOrderCount ?? 0 }} · 撤销 {{ overview?.cancelledOrderCount ?? 0 }}
      </strong>
    </div>
  </div>

  <div class="panel">
    <div class="section-head">
      <h2>账户</h2>
      <div class="toolbar compact-toolbar">
        <el-button @click="loadAll">刷新</el-button>
        <el-button type="primary" @click="openAccountCreate">新建账户</el-button>
        <el-button :disabled="!selectedAccount" @click="openAccountEdit">编辑</el-button>
        <el-button :disabled="!selectedAccount || selectedAccount.active" @click="toggleAccount(true)">启用</el-button>
        <el-button :disabled="!selectedAccount || !selectedAccount.active" @click="toggleAccount(false)">停用</el-button>
        <el-button :disabled="!selectedAccount" type="danger" plain @click="deleteAccount">删除</el-button>
      </div>
    </div>

    <el-alert
      v-if="unconfiguredAccounts.length"
      class="paper-alert"
      type="warning"
      :closable="false"
      show-icon
      :title="`有 ${unconfiguredAccounts.length} 个账户未绑定风控配置，相关订单会被风控拒绝`"
    />

    <div v-if="isMobile" class="mobile-card-list">
      <div
        v-for="account in accounts"
        :key="account.id"
        class="mobile-card"
        :class="{ 'mobile-card--muted': selectedAccountId === account.id }"
        @click="handleAccountChange(account)"
      >
        <div class="mobile-card__header">
          <div>
            <h3 class="mobile-card__title">{{ account.name }}</h3>
            <div class="muted">点击查看详情</div>
          </div>
          <el-tag :type="account.active ? 'success' : 'info'">{{ account.active ? '启用' : '停用' }}</el-tag>
        </div>
        <div class="mobile-card__meta">
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">现金</span>
            <span>{{ formatMoney(account.cash) }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">市值</span>
            <span>{{ formatMoney(account.marketValue) }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">总资产</span>
            <span>{{ formatMoney(account.totalEquity) }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">收益率</span>
            <span :class="pnlClass(account.totalReturnPct)">{{ formatPercent(account.totalReturnPct) }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">风控</span>
            <span :class="{ 'text-warning': !account.riskConfigId }">{{ riskConfigNameForAccount(account) }}</span>
          </div>
        </div>
      </div>
      <el-empty v-if="!accounts.length" description="暂无账户" />
    </div>

    <el-table v-else :data="accounts" highlight-current-row row-key="id" @current-change="handleAccountChange">
      <el-table-column prop="name" label="账户" min-width="180" />
      <el-table-column label="投研团队" min-width="160">
        <template #default="{ row }">
          <el-tag v-if="row.researchTeamId" effect="plain">{{ row.researchTeamName || `#${row.researchTeamId}` }}</el-tag>
          <span v-else class="muted">未绑定</span>
        </template>
      </el-table-column>
      <el-table-column label="风控配置" min-width="180">
        <template #default="{ row }">
          <el-tag v-if="row.riskConfigId" effect="plain">{{ riskConfigNameForAccount(row) }}</el-tag>
          <el-tag v-else type="warning" effect="plain">未绑定</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="active" label="状态" width="90">
        <template #default="{ row }">
          <el-tag :type="row.active ? 'success' : 'info'">{{ row.active ? '启用' : '停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="现金" width="130">
        <template #default="{ row }">{{ formatMoney(row.cash) }}</template>
      </el-table-column>
      <el-table-column label="持仓市值" width="130">
        <template #default="{ row }">{{ formatMoney(row.marketValue) }}</template>
      </el-table-column>
      <el-table-column label="总资产" width="130">
        <template #default="{ row }">{{ formatMoney(row.totalEquity) }}</template>
      </el-table-column>
      <el-table-column label="浮盈亏" width="130">
        <template #default="{ row }">
          <span :class="pnlClass(row.unrealizedPnl)">{{ formatMoney(row.unrealizedPnl) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="已实现盈亏" width="140">
        <template #default="{ row }">
          <span :class="pnlClass(row.realizedPnl)">{{ formatMoney(row.realizedPnl) }}</span>
        </template>
      </el-table-column>
      <el-table-column label="收益率" width="110">
        <template #default="{ row }">
          <span :class="pnlClass(row.totalReturnPct)">{{ formatPercent(row.totalReturnPct) }}</span>
        </template>
      </el-table-column>
      <el-table-column prop="positionCount" label="持仓数" width="90" />
      <el-table-column prop="pendingOrderCount" label="待执行" width="90" />
    </el-table>
  </div>

  <div v-if="selectedAccount" class="detail-grid paper-detail-grid">
    <div class="detail-main">
      <div class="panel">
        <div class="section-head">
          <h2>{{ selectedAccount.name }} 绩效</h2>
          <div class="toolbar compact-toolbar">
            <span class="muted">初始资金 {{ formatMoney(selectedAccount.initialCash) }}</span>
            <span class="muted">最大回撤 {{ formatPercent(performance?.maxDrawdownPct) }}</span>
            <span class="muted">胜率 {{ formatPercent(performance?.winRatePct) }}</span>
          </div>
        </div>
        <div class="metrics-grid account-metrics">
          <div class="metric-card slim">
            <span>现金</span>
            <strong>{{ formatMoney(selectedAccount.cash) }}</strong>
          </div>
          <div class="metric-card slim">
            <span>持仓市值</span>
            <strong>{{ formatMoney(selectedAccount.marketValue) }}</strong>
          </div>
          <div class="metric-card slim">
            <span>总资产</span>
            <strong>{{ formatMoney(selectedAccount.totalEquity) }}</strong>
          </div>
          <div class="metric-card slim">
            <span>累计收益</span>
            <strong :class="pnlClass(selectedAccount.totalReturnPct)">{{ formatPercent(selectedAccount.totalReturnPct) }}</strong>
          </div>
        </div>
        <el-alert
          v-if="!selectedAccount.riskConfigId"
          class="paper-alert"
          type="warning"
          :closable="false"
          show-icon
          title="当前账户未绑定风控配置，订单会被拒绝"
        />
        <div ref="chartContainer" class="financial-chart" />
        <div class="performance-insights">
          <div class="performance-block">
            <div class="performance-block__head">
              <h3>风险预警</h3>
              <el-tag :type="riskSummaryTagType(performance?.riskSummary?.status)" effect="plain">
                {{ riskSummaryLabel(performance?.riskSummary?.status) }}
              </el-tag>
            </div>
            <el-empty v-if="!performance?.riskAlerts?.length" description="暂无风险预警" />
            <div v-else class="risk-alert-list">
              <el-alert
                v-for="alert in performance.riskAlerts"
                :key="`${alert.key}-${alert.code || 'account'}`"
                :type="riskAlertType(alert.severity)"
                :title="alert.title"
                :description="riskAlertDescription(alert)"
                :closable="false"
                show-icon
              />
            </div>
          </div>
          <div class="performance-block">
            <div class="performance-block__head">
              <h3>组合归因</h3>
              <span class="muted">按盈亏贡献排序</span>
            </div>
            <div v-if="isMobile" class="mobile-card-list compact-attribution-list">
              <div v-for="row in performance?.attribution || []" :key="row.code" class="mobile-card">
                <div class="mobile-card__header">
                  <h3 class="mobile-card__title">{{ symbolLabel(row) }}</h3>
                  <el-tag>{{ formatAttributionSource(row.source) }}</el-tag>
                </div>
                <div class="mobile-card__meta">
                  <div class="mobile-card__meta-row">
                    <span class="mobile-card__meta-label">仓位占比</span>
                    <span>{{ formatPercent(row.weightPct) }}</span>
                  </div>
                  <div class="mobile-card__meta-row">
                    <span class="mobile-card__meta-label">总盈亏</span>
                    <span :class="pnlClass(row.totalPnl)">{{ formatMoney(row.totalPnl) }}</span>
                  </div>
                  <div class="mobile-card__meta-row">
                    <span class="mobile-card__meta-label">贡献度</span>
                    <span :class="pnlClass(row.contributionPct)">{{ formatPercent(row.contributionPct) }}</span>
                  </div>
                </div>
              </div>
              <el-empty v-if="!performance?.attribution?.length" description="暂无归因数据" />
            </div>
            <el-table v-else :data="performance?.attribution || []" size="small">
              <el-table-column label="标的" min-width="150">
                <template #default="{ row }">{{ symbolLabel(row) }}</template>
              </el-table-column>
              <el-table-column label="仓位" width="90">
                <template #default="{ row }">{{ formatPercent(row.weightPct) }}</template>
              </el-table-column>
              <el-table-column label="浮盈亏" width="110">
                <template #default="{ row }">
                  <span :class="pnlClass(row.unrealizedPnl)">{{ formatMoney(row.unrealizedPnl) }}</span>
                </template>
              </el-table-column>
              <el-table-column label="已实现" width="110">
                <template #default="{ row }">
                  <span :class="pnlClass(row.realizedPnl)">{{ formatMoney(row.realizedPnl) }}</span>
                </template>
              </el-table-column>
              <el-table-column label="总盈亏" width="110">
                <template #default="{ row }">
                  <span :class="pnlClass(row.totalPnl)">{{ formatMoney(row.totalPnl) }}</span>
                </template>
              </el-table-column>
              <el-table-column label="贡献度" width="100">
                <template #default="{ row }">
                  <span :class="pnlClass(row.contributionPct)">{{ formatPercent(row.contributionPct) }}</span>
                </template>
              </el-table-column>
            </el-table>
          </div>
        </div>
      </div>

      <div class="panel">
        <div class="section-head">
          <h2>历史回测</h2>
          <div class="toolbar compact-toolbar">
            <el-button @click="resetBacktestForm">重置</el-button>
            <el-button type="primary" :loading="backtestLoading" @click="runBacktest">运行</el-button>
          </div>
        </div>

        <el-form class="backtest-form" :model="backtestForm" :label-position="isMobile ? 'top' : 'right'" label-width="92px">
          <el-form-item label="标的">
            <el-select v-model="backtestForm.code" filterable allow-create default-first-option style="width: 100%">
              <el-option v-for="row in positions" :key="row.id" :label="symbolLabel(row)" :value="row.code" />
            </el-select>
          </el-form-item>
          <el-form-item label="区间">
            <el-date-picker v-model="backtestForm.dateRange" type="daterange" start-placeholder="开始" end-placeholder="结束" style="width: 100%" />
          </el-form-item>
          <el-form-item label="初始资金">
            <el-input-number v-model="backtestForm.initialCash" :min="0" :precision="2" :step="10000" style="width: 100%" />
          </el-form-item>
          <el-form-item label="买入阈值">
            <el-input-number v-model="backtestForm.buyThresholdPct" :max="0" :precision="2" :step="0.5" style="width: 100%" />
          </el-form-item>
          <el-form-item label="卖出阈值">
            <el-input-number v-model="backtestForm.sellThresholdPct" :min="0.01" :precision="2" :step="0.5" style="width: 100%" />
          </el-form-item>
          <el-form-item label="单笔比例">
            <el-input-number v-model="backtestForm.orderPct" :min="0.01" :max="1" :precision="4" :step="0.01" style="width: 100%" />
          </el-form-item>
          <el-form-item label="滑点 bps">
            <el-input-number v-model="backtestForm.slippageBps" :min="0" :max="1000" :precision="2" :step="1" style="width: 100%" />
          </el-form-item>
        </el-form>

        <div v-if="backtest" class="backtest-results">
          <div class="metrics-grid backtest-summary-grid">
            <div class="metric-card slim">
              <span>最终权益</span>
              <strong>{{ formatMoney(backtest.summary.finalEquity) }}</strong>
            </div>
            <div class="metric-card slim">
              <span>总收益率</span>
              <strong :class="pnlClass(backtest.summary.totalReturnPct)">{{ formatPercent(backtest.summary.totalReturnPct) }}</strong>
            </div>
            <div class="metric-card slim">
              <span>最大回撤</span>
              <strong>{{ formatPercent(backtest.summary.maxDrawdownPct) }}</strong>
            </div>
            <div class="metric-card slim">
              <span>成交 / 拒绝</span>
              <strong>{{ backtest.summary.tradeCount }} / {{ backtest.summary.rejectedCount }}</strong>
            </div>
          </div>
          <div ref="backtestChartContainer" class="financial-chart backtest-chart" />
          <div v-if="isMobile" class="mobile-card-list compact-attribution-list">
            <div v-for="order in backtest.orders" :key="order.id" class="mobile-card">
              <div class="mobile-card__header">
                <div>
                  <h3 class="mobile-card__title">{{ order.code }} / {{ formatSide(order.side) }}</h3>
                  <div class="muted">{{ formatDateTimeUtc8(order.tradeDate) }}</div>
                </div>
                <el-tag :type="backtestStatusTagType(order.status)" effect="plain">{{ formatBacktestStatus(order.status) }}</el-tag>
              </div>
              <div class="mobile-card__meta">
                <div class="mobile-card__meta-row">
                  <span class="mobile-card__meta-label">数量</span>
                  <span>{{ order.quantity }}</span>
                </div>
                <div class="mobile-card__meta-row">
                  <span class="mobile-card__meta-label">信号</span>
                  <span :class="pnlClass(order.signalPct)">{{ formatPercent(order.signalPct) }}</span>
                </div>
                <div class="mobile-card__meta-row">
                  <span class="mobile-card__meta-label">原因</span>
                  <span>{{ formatBacktestReason(order.reason) }}</span>
                </div>
              </div>
            </div>
            <el-empty v-if="!backtest.orders.length" description="暂无回测订单" />
          </div>
          <el-table v-else :data="backtest.orders" size="small">
            <el-table-column label="日期" width="140">
              <template #default="{ row }">{{ formatDateTimeUtc8(row.tradeDate) }}</template>
            </el-table-column>
            <el-table-column prop="code" label="标的" width="100" />
            <el-table-column label="方向" width="80">
              <template #default="{ row }">
                <el-tag :type="sideTagType(row.side)" effect="plain">{{ formatSide(row.side) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column prop="quantity" label="数量" width="90" />
            <el-table-column label="信号" width="100">
              <template #default="{ row }">
                <span :class="pnlClass(row.signalPct)">{{ formatPercent(row.signalPct) }}</span>
              </template>
            </el-table-column>
            <el-table-column label="成交价" width="110">
              <template #default="{ row }">{{ formatMoney(row.filledPrice, 4) }}</template>
            </el-table-column>
            <el-table-column label="费用" width="100">
              <template #default="{ row }">{{ formatMoney(row.fees) }}</template>
            </el-table-column>
            <el-table-column label="状态" width="100">
              <template #default="{ row }">
                <el-tag :type="backtestStatusTagType(row.status)" effect="plain">{{ formatBacktestStatus(row.status) }}</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="原因" min-width="160">
              <template #default="{ row }">{{ formatBacktestReason(row.reason) }}</template>
            </el-table-column>
          </el-table>
        </div>
        <el-empty v-else description="暂无回测结果" />
      </div>

      <div class="panel">
        <div class="section-head">
          <h2>持仓</h2>
          <div class="toolbar compact-toolbar">
            <el-button @click="loadSelectedAccount">刷新</el-button>
            <el-button type="primary" :disabled="!positions.length" @click="openCorporateActionCreate()">公司行为</el-button>
          </div>
        </div>

        <div v-if="isMobile" class="mobile-card-list">
          <div v-for="row in positions" :key="row.id" class="mobile-card">
            <div class="mobile-card__header">
              <h3 class="mobile-card__title">{{ symbolLabel(row) }}</h3>
              <el-tag>{{ row.quantity }}</el-tag>
            </div>
            <div class="mobile-card__meta">
              <div class="mobile-card__meta-row">
                <span class="mobile-card__meta-label">成本价</span>
                <span>{{ formatMoney(row.avgCost, 4) }}</span>
              </div>
              <div class="mobile-card__meta-row">
                <span class="mobile-card__meta-label">最新价</span>
                <span>{{ formatMoney(row.lastPrice, 4) }}</span>
              </div>
              <div class="mobile-card__meta-row">
                <span class="mobile-card__meta-label">成本</span>
                <span>{{ formatMoney(row.costAmount) }}</span>
              </div>
              <div class="mobile-card__meta-row">
                <span class="mobile-card__meta-label">市值</span>
                <span>{{ formatMoney(row.marketValue) }}</span>
              </div>
              <div class="mobile-card__meta-row">
                <span class="mobile-card__meta-label">浮盈亏</span>
                <span :class="pnlClass(row.unrealizedPnl)">{{ formatMoney(row.unrealizedPnl) }}</span>
              </div>
              <div class="mobile-card__meta-row">
                <span class="mobile-card__meta-label">已实现</span>
                <span :class="pnlClass(row.realizedPnl)">{{ formatMoney(row.realizedPnl) }}</span>
              </div>
            </div>
          </div>
          <el-empty v-if="!positions.length" description="暂无持仓" />
        </div>

        <el-table v-else :data="positions">
          <el-table-column label="标的" min-width="150">
            <template #default="{ row }">{{ symbolLabel(row) }}</template>
          </el-table-column>
          <el-table-column prop="quantity" label="数量" width="90" />
          <el-table-column label="成本价" width="120">
            <template #default="{ row }">{{ formatMoney(row.avgCost, 4) }}</template>
          </el-table-column>
          <el-table-column label="最新价" width="120">
            <template #default="{ row }">{{ formatMoney(row.lastPrice, 4) }}</template>
          </el-table-column>
          <el-table-column label="成本" width="130">
            <template #default="{ row }">{{ formatMoney(row.costAmount) }}</template>
          </el-table-column>
          <el-table-column label="市值" width="130">
            <template #default="{ row }">{{ formatMoney(row.marketValue) }}</template>
          </el-table-column>
          <el-table-column label="浮盈亏" min-width="140">
            <template #default="{ row }">
              <span :class="pnlClass(row.unrealizedPnl)">{{ formatMoney(row.unrealizedPnl) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="已实现盈亏" min-width="140">
            <template #default="{ row }">
              <span :class="pnlClass(row.realizedPnl)">{{ formatMoney(row.realizedPnl) }}</span>
            </template>
          </el-table-column>
        </el-table>
      </div>
    </div>

    <div class="detail-side">
      <div class="panel">
        <div class="section-head">
          <h2>订单</h2>
        </div>

        <div v-if="isMobile" class="mobile-card-list">
          <div v-for="row in orders" :key="row.id" class="mobile-card">
            <div class="mobile-card__header">
              <div>
                <h3 class="mobile-card__title">{{ symbolLabel(row) }} / {{ formatSide(row.side) }}</h3>
                <div class="muted">{{ formatOrderStatus(row.status) }}</div>
              </div>
              <el-tag>{{ row.filledQuantity || 0 }}/{{ row.quantity }}</el-tag>
            </div>
            <div class="mobile-card__meta">
              <div class="mobile-card__meta-row">
                <span class="mobile-card__meta-label">来源会议</span>
                <span>{{ row.meetingId ? `#${row.meetingId}` : '-' }}</span>
              </div>
              <div class="mobile-card__meta-row">
                <span class="mobile-card__meta-label">计划执行</span>
                <span>{{ formatDateTimeUtc8(row.executeAfter) }}</span>
              </div>
              <div class="mobile-card__meta-row">
                <span class="mobile-card__meta-label">说明</span>
                <span>{{ row.executionNote || row.reason || '-' }}</span>
              </div>
              <div class="mobile-card__meta-row">
                <span class="mobile-card__meta-label">复核</span>
                <span>
                  <el-tag :type="approvalRiskTagType(row.approvalRiskLevel)" effect="plain">
                    {{ formatApprovalRiskLevel(row.approvalRiskLevel) }}
                  </el-tag>
                  <span v-if="row.approvalConfirmRequired" class="order-review-cell__hint">需二次确认</span>
                </span>
              </div>
            </div>
            <div class="mobile-card__actions">
              <el-button v-if="row.meetingId" plain @click="router.push(`/meetings/${row.meetingId}`)">查看会议</el-button>
              <el-button v-if="row.status === 'suggested'" plain type="primary" @click="approveOrder(row)">通过</el-button>
              <el-button v-if="row.status === 'suggested'" plain type="danger" @click="rejectOrder(row.id)">拒绝</el-button>
              <el-button v-if="row.status === 'pending'" plain type="primary" @click="openOrderFill(row)">成交</el-button>
              <el-button v-if="row.status === 'pending'" plain type="danger" @click="cancelOrder(row.id)">
                撤单
              </el-button>
              <el-button
                v-else-if="row.status === 'cancelled' || row.status === 'rejected'"
                plain
                type="danger"
                @click="deleteOrder(row.id)"
              >
                删除
              </el-button>
            </div>
          </div>
          <el-empty v-if="!orders.length" description="暂无订单" />
        </div>

        <el-table v-else :data="orders" size="small">
          <el-table-column label="标的" min-width="150">
            <template #default="{ row }">{{ symbolLabel(row) }}</template>
          </el-table-column>
          <el-table-column label="方向" width="80">
            <template #default="{ row }">
              <el-tag :type="sideTagType(row.side)" effect="plain">{{ formatSide(row.side) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="数量" width="120">
            <template #default="{ row }">
              {{ row.filledQuantity || 0 }}/{{ row.quantity }}
              <span v-if="row.remainingQuantity > 0" class="muted">余 {{ row.remainingQuantity }}</span>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tag :type="orderStatusTagType(row.status)" effect="plain">{{ formatOrderStatus(row.status) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="复核" min-width="170">
            <template #default="{ row }">
              <div class="order-review-cell">
                <el-tag :type="approvalRiskTagType(row.approvalRiskLevel)" effect="plain">
                  {{ formatApprovalRiskLevel(row.approvalRiskLevel) }}
                </el-tag>
                <span v-if="row.approvalConfirmRequired" class="order-review-cell__hint">需二次确认</span>
                <span v-if="approvalRiskText(row)" class="muted order-review-cell__reason">{{ approvalRiskText(row) }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="来源会议" width="100">
            <template #default="{ row }">
              <el-button v-if="row.meetingId" link type="primary" @click="router.push(`/meetings/${row.meetingId}`)">#{{ row.meetingId }}</el-button>
              <span v-else>-</span>
            </template>
          </el-table-column>
          <el-table-column label="计划执行" min-width="170">
            <template #default="{ row }">{{ formatDateTimeUtc8(row.executeAfter) }}</template>
          </el-table-column>
          <el-table-column label="说明" min-width="180" show-overflow-tooltip>
            <template #default="{ row }">{{ row.executionNote || row.reason || '-' }}</template>
          </el-table-column>
          <el-table-column label="操作" width="180">
            <template #default="{ row }">
              <template v-if="row.status === 'suggested'">
                <el-button link type="primary" @click="approveOrder(row)">通过</el-button>
                <el-button link type="danger" @click="rejectOrder(row.id)">拒绝</el-button>
              </template>
              <template v-else-if="row.status === 'pending'">
                <el-button link type="primary" @click="openOrderFill(row)">成交</el-button>
                <el-button link type="danger" @click="cancelOrder(row.id)">撤单</el-button>
              </template>
              <el-button v-else-if="row.status === 'cancelled' || row.status === 'rejected'" link type="danger" @click="deleteOrder(row.id)">删除</el-button>
            </template>
          </el-table-column>
        </el-table>

        <div class="cursor-pagination-footer">
          <span class="muted">已加载 {{ orders.length }} 条</span>
          <el-button v-if="orderNextCursor" plain :loading="orderLoadingMore" @click="loadMoreOrders">加载更多</el-button>
        </div>
      </div>

      <div class="panel">
        <div class="section-head">
          <h2>成交</h2>
        </div>

        <div v-if="isMobile" class="mobile-card-list">
          <div v-for="row in fills" :key="row.id" class="mobile-card">
            <div class="mobile-card__header">
              <div>
                <h3 class="mobile-card__title">{{ symbolLabel(row) }} / {{ formatSide(row.side) }}</h3>
                <div class="muted">{{ formatDateTimeUtc8(row.filledAt) }}</div>
              </div>
              <el-tag>{{ row.quantity }}</el-tag>
            </div>
            <div class="mobile-card__meta">
              <div class="mobile-card__meta-row">
                <span class="mobile-card__meta-label">价格</span>
                <span>{{ formatMoney(row.price, 4) }}</span>
              </div>
              <div class="mobile-card__meta-row">
                <span class="mobile-card__meta-label">净额</span>
                <span>{{ formatMoney(row.netAmount) }}</span>
              </div>
              <div class="mobile-card__meta-row">
                <span class="mobile-card__meta-label">已实现</span>
                <span :class="pnlClass(row.realizedPnl)">{{ formatMoney(row.realizedPnl) }}</span>
              </div>
            </div>
          </div>
          <el-empty v-if="!fills.length" description="暂无成交" />
        </div>

        <el-table v-else :data="fills" size="small">
          <el-table-column label="标的" min-width="150">
            <template #default="{ row }">{{ symbolLabel(row) }}</template>
          </el-table-column>
          <el-table-column label="方向" width="80">
            <template #default="{ row }">
              <el-tag :type="sideTagType(row.side)" effect="plain">{{ formatSide(row.side) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column prop="quantity" label="数量" width="70" />
          <el-table-column label="价格" width="110">
            <template #default="{ row }">{{ formatMoney(row.price, 4) }}</template>
          </el-table-column>
          <el-table-column label="净额" width="120">
            <template #default="{ row }">{{ formatMoney(row.netAmount) }}</template>
          </el-table-column>
          <el-table-column label="已实现" width="120">
            <template #default="{ row }">
              <span :class="pnlClass(row.realizedPnl)">{{ formatMoney(row.realizedPnl) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="时间" min-width="170">
            <template #default="{ row }">{{ formatDateTimeUtc8(row.filledAt) }}</template>
          </el-table-column>
        </el-table>

        <div class="cursor-pagination-footer">
          <span class="muted">已加载 {{ fills.length }} 条</span>
          <el-button v-if="fillNextCursor" plain :loading="fillLoadingMore" @click="loadMoreFills">加载更多</el-button>
        </div>
      </div>

      <div class="panel">
        <div class="section-head">
          <h2>公司行为</h2>
          <el-button type="primary" :disabled="!positions.length" @click="openCorporateActionCreate()">新增</el-button>
        </div>

        <div v-if="isMobile" class="mobile-card-list">
          <div v-for="row in corporateActions" :key="row.id" class="mobile-card">
            <div class="mobile-card__header">
              <div>
                <h3 class="mobile-card__title">{{ row.code }}</h3>
                <div class="muted">{{ formatDateTimeUtc8(row.exDate) }}</div>
              </div>
              <el-tag :type="corporateActionTagType(row.actionType)" effect="plain">{{ formatCorporateActionType(row.actionType) }}</el-tag>
            </div>
            <div class="mobile-card__meta">
              <div class="mobile-card__meta-row">
                <span class="mobile-card__meta-label">影响股数</span>
                <span>{{ row.affectedShares }}</span>
              </div>
              <div class="mobile-card__meta-row">
                <span class="mobile-card__meta-label">现金</span>
                <span>{{ formatMoney(row.cashAmount) }}</span>
              </div>
            </div>
          </div>
          <el-empty v-if="!corporateActions.length" description="暂无公司行为" />
        </div>

        <el-table v-else :data="corporateActions" size="small">
          <el-table-column prop="code" label="标的" width="100" />
          <el-table-column label="类型" width="110">
            <template #default="{ row }">
              <el-tag :type="corporateActionTagType(row.actionType)" effect="plain">{{ formatCorporateActionType(row.actionType) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="除权日" min-width="150">
            <template #default="{ row }">{{ formatDateTimeUtc8(row.exDate) }}</template>
          </el-table-column>
          <el-table-column label="影响股数" width="90">
            <template #default="{ row }">{{ row.affectedShares }}</template>
          </el-table-column>
          <el-table-column label="现金" width="110">
            <template #default="{ row }">{{ formatMoney(row.cashAmount) }}</template>
          </el-table-column>
        </el-table>

        <div class="cursor-pagination-footer">
          <span class="muted">已加载 {{ corporateActions.length }} 条</span>
          <el-button v-if="corporateActionNextCursor" plain :loading="corporateActionLoadingMore" @click="loadMoreCorporateActions">加载更多</el-button>
        </div>
      </div>

      <div class="panel">
        <div class="section-head">
          <h2>复盘时间线</h2>
          <span class="muted">{{ replay?.summary?.eventCount ?? 0 }} 个事件</span>
        </div>
        <el-empty v-if="!replay?.events?.length" description="暂无复盘事件" />
        <div v-else class="paper-timeline">
          <div v-for="event in replay.events" :key="event.id" class="paper-timeline__item">
            <div class="paper-timeline__head">
              <el-tag :type="replayEventTagType(event.type)" effect="plain">{{ formatReplayEventType(event.type) }}</el-tag>
              <span class="muted">{{ formatDateTimeUtc8(event.time) }}</span>
            </div>
            <strong>{{ event.code || '-' }}</strong>
            <p>{{ event.summary }}</p>
          </div>
        </div>
      </div>
    </div>
  </div>

  <div class="panel">
    <div class="section-head">
      <h2>风控配置</h2>
      <div class="toolbar compact-toolbar">
        <el-button @click="loadRiskConfigs">刷新</el-button>
        <el-button type="primary" @click="openRiskCreate">新建配置</el-button>
      </div>
    </div>

    <div v-if="isMobile" class="mobile-card-list">
      <div v-for="row in riskConfigs" :key="row.id" class="mobile-card">
        <div class="mobile-card__header">
          <div>
            <h3 class="mobile-card__title">{{ row.name }}</h3>
            <div class="muted">{{ formatBoardScope(row) }}</div>
          </div>
          <el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '启用' : '停用' }}</el-tag>
        </div>
        <div class="mobile-card__meta">
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">初始资金</span>
            <span>{{ formatMoney(row.initialCash) }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">单笔上限</span>
            <span>{{ formatPercent(toPercent(row.maxOrderPct)) }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">单标的上限</span>
            <span>{{ formatPercent(toPercent(row.maxPositionPct)) }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">佣金</span>
            <span>{{ formatCommission(row.commissionRate, row.minCommission) }}</span>
          </div>
          <div class="mobile-card__meta-row">
            <span class="mobile-card__meta-label">绑定账户</span>
            <span>{{ formatRiskConfigAccounts(row) }}</span>
          </div>
        </div>
        <div class="mobile-card__actions">
          <el-button plain @click="openRiskEdit(row)">编辑</el-button>
          <el-button plain type="danger" @click="deleteRiskConfig(row.id)">删除</el-button>
        </div>
      </div>
      <el-empty v-if="!riskConfigs.length" description="暂无风控配置" />
    </div>

    <el-table v-else :data="riskConfigs">
      <el-table-column prop="name" label="名称" min-width="160" />
      <el-table-column label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="row.enabled ? 'success' : 'info'">{{ row.enabled ? '启用' : '停用' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="初始资金" width="130">
        <template #default="{ row }">{{ formatMoney(row.initialCash) }}</template>
      </el-table-column>
      <el-table-column label="单笔上限" width="120">
        <template #default="{ row }">{{ formatPercent(toPercent(row.maxOrderPct)) }}</template>
      </el-table-column>
      <el-table-column label="单标的上限" width="130">
        <template #default="{ row }">{{ formatPercent(toPercent(row.maxPositionPct)) }}</template>
      </el-table-column>
      <el-table-column label="佣金" width="140">
        <template #default="{ row }">{{ formatCommission(row.commissionRate, row.minCommission) }}</template>
      </el-table-column>
      <el-table-column label="允许板块" min-width="260">
        <template #default="{ row }">{{ formatBoardScope(row) }}</template>
      </el-table-column>
      <el-table-column label="绑定账户" min-width="220" show-overflow-tooltip>
        <template #default="{ row }">{{ formatRiskConfigAccounts(row) }}</template>
      </el-table-column>
      <el-table-column label="操作" width="180">
        <template #default="{ row }">
          <el-button link type="primary" @click="openRiskEdit(row)">编辑</el-button>
          <el-button link type="danger" @click="deleteRiskConfig(row.id)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
  </div>

  <el-dialog
    v-model="accountDialogVisible"
    :title="accountDialogMode === 'create' ? '新建账户' : '编辑账户'"
    :fullscreen="isMobile"
    :width="accountDialogWidth"
  >
    <el-form
      :model="accountForm"
      :label-width="isMobile ? 'auto' : '100px'"
      :label-position="isMobile ? 'top' : 'right'"
    >
      <el-form-item label="名称">
        <el-input v-model="accountForm.name" />
      </el-form-item>
      <el-form-item label="初始/当前现金">
        <el-input-number v-model="accountForm.cash" :min="0" :precision="2" :step="1000" style="width: 100%" />
      </el-form-item>
      <el-form-item label="风控配置">
        <el-select v-model="accountForm.riskConfigId" clearable style="width: 100%">
          <el-option v-for="item in riskConfigs" :key="item.id" :label="item.name" :value="item.id" />
        </el-select>
      </el-form-item>
      <el-form-item v-if="accountDialogMode === 'edit'" label="启用">
        <el-switch v-model="accountForm.active" />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="accountDialogVisible = false">取消</el-button>
      <el-button type="primary" @click="saveAccount">保存</el-button>
    </template>
  </el-dialog>

  <el-dialog
    v-model="riskDialogVisible"
    :title="riskDialogMode === 'create' ? '新建风控配置' : '编辑风控配置'"
    :fullscreen="isMobile"
    :width="riskDialogWidth"
  >
    <el-form
      :model="riskForm"
      :label-width="isMobile ? 'auto' : '110px'"
      :label-position="isMobile ? 'top' : 'right'"
    >
      <el-form-item label="名称">
        <el-input v-model="riskForm.name" />
      </el-form-item>
      <el-form-item label="初始资金">
        <el-input-number v-model="riskForm.initialCash" :min="0" :precision="2" :step="1000" style="width: 100%" />
      </el-form-item>
      <el-form-item label="单标的上限">
        <el-input-number v-model="riskForm.maxPositionPct" :min="0.01" :max="1" :precision="4" :step="0.01" style="width: 100%" />
      </el-form-item>
      <el-form-item label="单笔上限">
        <el-input-number v-model="riskForm.maxOrderPct" :min="0.01" :max="1" :precision="4" :step="0.01" style="width: 100%" />
      </el-form-item>
      <el-form-item label="佣金率">
        <el-input-number v-model="riskForm.commissionRate" :min="0" :precision="6" :step="0.00001" style="width: 100%" />
      </el-form-item>
      <el-form-item label="最低佣金">
        <el-input-number v-model="riskForm.minCommission" :min="0" :precision="2" :step="1" style="width: 100%" />
      </el-form-item>
      <el-form-item label="印花税率">
        <el-input-number v-model="riskForm.stampDutyRate" :min="0" :precision="6" :step="0.0001" style="width: 100%" />
      </el-form-item>
      <el-form-item label="过户费率">
        <el-input-number v-model="riskForm.transferFeeRate" :min="0" :precision="6" :step="0.00001" style="width: 100%" />
      </el-form-item>
      <el-form-item label="允许做空">
        <el-switch v-model="riskForm.allowShort" />
      </el-form-item>
      <el-form-item label="允许融资">
        <el-switch v-model="riskForm.allowMargin" />
      </el-form-item>
      <el-form-item label="允许板块">
        <div class="board-switches">
          <el-checkbox v-model="riskForm.allowShMain">沪市主板</el-checkbox>
          <el-checkbox v-model="riskForm.allowSzMain">深市主板</el-checkbox>
          <el-checkbox v-model="riskForm.allowBj">北交所</el-checkbox>
          <el-checkbox v-model="riskForm.allowStar">科创板</el-checkbox>
          <el-checkbox v-model="riskForm.allowChinext">创业板</el-checkbox>
          <el-checkbox v-model="riskForm.allowEtfLof">场内 ETF/LOF</el-checkbox>
        </div>
      </el-form-item>
      <el-form-item label="启用">
        <el-switch v-model="riskForm.enabled" />
      </el-form-item>
      <el-form-item label="绑定账户">
        <el-select
          v-model="riskForm.accountIds"
          multiple
          clearable
          collapse-tags
          collapse-tags-tooltip
          placeholder="可留空，留空时不作用于任何账户"
          style="width: 100%"
        >
          <el-option v-for="account in accounts" :key="account.id" :label="account.name" :value="account.id" />
        </el-select>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="riskDialogVisible = false">取消</el-button>
      <el-button type="primary" @click="saveRiskConfig">保存</el-button>
    </template>
  </el-dialog>

  <el-dialog
    v-model="corporateActionDialogVisible"
    title="新增公司行为"
    :fullscreen="isMobile"
    :width="corporateActionDialogWidth"
  >
    <el-form
      :model="corporateActionForm"
      :label-width="isMobile ? 'auto' : '110px'"
      :label-position="isMobile ? 'top' : 'right'"
    >
      <el-form-item label="标的代码">
        <el-select v-model="corporateActionForm.code" filterable allow-create default-first-option style="width: 100%">
          <el-option v-for="row in positions" :key="row.id" :label="symbolLabel(row)" :value="row.code" />
        </el-select>
      </el-form-item>
      <el-form-item label="类型">
        <el-segmented
          v-model="corporateActionForm.actionType"
          :options="[
            { label: '现金分红', value: 'cash_dividend' },
            { label: '送股', value: 'bonus_share' },
            { label: '拆股', value: 'split' }
          ]"
        />
      </el-form-item>
      <el-form-item label="除权日">
        <el-date-picker v-model="corporateActionForm.exDate" type="datetime" style="width: 100%" />
      </el-form-item>
      <el-form-item v-if="corporateActionForm.actionType === 'cash_dividend'" label="每股现金">
        <el-input-number v-model="corporateActionForm.cashPerShare" :min="0" :precision="4" :step="0.1" style="width: 100%" />
      </el-form-item>
      <el-form-item v-else label="比例">
        <el-input-number v-model="corporateActionForm.shareRatio" :min="0" :precision="4" :step="0.1" style="width: 100%" />
      </el-form-item>
      <el-form-item label="备注">
        <el-input v-model="corporateActionForm.note" type="textarea" :rows="3" maxlength="240" show-word-limit />
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="corporateActionDialogVisible = false">取消</el-button>
      <el-button type="primary" @click="saveCorporateAction">应用</el-button>
    </template>
  </el-dialog>

  <el-dialog
    v-model="orderFillDialogVisible"
    title="记录成交"
    :fullscreen="isMobile"
    :width="orderFillDialogWidth"
  >
    <el-form
      :model="orderFillForm"
      :label-width="isMobile ? 'auto' : '96px'"
      :label-position="isMobile ? 'top' : 'right'"
    >
      <el-form-item label="订单">
        <div class="form-static">{{ orderFillForm.code }} / {{ formatSide(orderFillForm.side) }}</div>
      </el-form-item>
      <el-form-item label="价格">
        <el-input-number v-model="orderFillForm.price" :min="0.0001" :precision="4" :step="0.01" style="width: 100%" />
      </el-form-item>
      <el-form-item label="数量">
        <el-input-number
          v-model="orderFillForm.quantity"
          :min="1"
          :max="orderFillForm.remainingQuantity || undefined"
          :step="100"
          step-strictly
          style="width: 100%"
        />
        <div class="form-hint">剩余 {{ orderFillForm.remainingQuantity }} 股</div>
      </el-form-item>
    </el-form>
    <template #footer>
      <el-button @click="orderFillDialogVisible = false">取消</el-button>
      <el-button type="primary" @click="saveOrderFill">成交</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus'
import { AreaSeries, createChart, type IChartApi, type ISeriesApi, LineStyle, type UTCTimestamp } from 'lightweight-charts'
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import {
  api,
  jsonapiResource,
  type PaperAccount,
  type PaperBacktest,
  type PaperBacktestOrder,
  type PaperCorporateAction,
  type PaperFill,
  type PaperOrder,
  type PaperOverview,
  type PaperPerformance,
  type PaperPosition,
  type PaperReplay,
  type PaperReplayEvent,
  type PaperRiskAlert,
  type RiskConfig,
  unwrapJsonApiCollection,
  unwrapJsonApiResource
} from '../api'
import { useAutoRefresh } from '../composables/useAutoRefresh'
import { nextCursorFromDocument, useCursorPagination } from '../composables/useCursorPagination'
import { useResponsive } from '../composables/useResponsive'
import { useTheme } from '../composables/useTheme'
import { formatDateTimeUtc8, getUtc8DateParts } from '../utils/datetime'
import { chartTheme } from '../utils/theme'

const router = useRouter()
const { isMobile, isTablet } = useResponsive()
const { resolvedTheme } = useTheme()
const overview = ref<PaperOverview | null>(null)
const accounts = ref<PaperAccount[]>([])
const riskConfigs = ref<RiskConfig[]>([])
const selectedAccountId = ref<number | null>(null)
const positions = ref<PaperPosition[]>([])
const performance = ref<PaperPerformance | null>(null)
const accountDialogVisible = ref(false)
const accountDialogMode = ref<'create' | 'edit'>('create')
const riskDialogVisible = ref(false)
const riskDialogMode = ref<'create' | 'edit'>('create')
const corporateActionDialogVisible = ref(false)
const orderFillDialogVisible = ref(false)
const editingRiskId = ref<number | null>(null)
const chartContainer = ref<HTMLElement | null>(null)
const backtestChartContainer = ref<HTMLElement | null>(null)
const selectedAccount = ref<PaperAccount | null>(null)
const replay = ref<PaperReplay | null>(null)
const backtest = ref<PaperBacktest | null>(null)
const backtestLoading = ref(false)

const accountDialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '560px'))
const riskDialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '92vw' : '620px'))
const corporateActionDialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '560px'))
const orderFillDialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '460px'))
const riskConfigById = computed(() => new Map(riskConfigs.value.map((item) => [item.id, item])))
const unconfiguredAccounts = computed(() => accounts.value.filter((item) => !item.riskConfigId))

const accountForm = reactive({
  name: '',
  cash: 1000000,
  riskConfigId: undefined as number | undefined,
  active: true
})

const riskForm = reactive({
  name: '',
  initialCash: 1000000,
  maxPositionPct: 0.3,
  maxOrderPct: 0.2,
  commissionRate: 0.0001,
  minCommission: 5,
  stampDutyRate: 0.001,
  transferFeeRate: 0.00001,
  allowShort: false,
  allowMargin: false,
  allowShMain: true,
  allowSzMain: true,
  allowBj: false,
  allowStar: false,
  allowChinext: false,
  allowEtfLof: true,
  enabled: true,
  accountIds: [] as number[]
})

const corporateActionForm = reactive({
  code: '',
  actionType: 'cash_dividend' as PaperCorporateAction['actionType'],
  exDate: null as Date | null,
  cashPerShare: 0,
  shareRatio: 0.1,
  note: ''
})

const orderFillForm = reactive({
  orderId: 0,
  code: '',
  side: '',
  price: 0,
  quantity: 0,
  remainingQuantity: 0
})

const backtestForm = reactive({
  code: '',
  dateRange: defaultBacktestDateRange() as [Date, Date],
  initialCash: 1000000,
  buyThresholdPct: -3,
  sellThresholdPct: 3,
  orderPct: 0.1,
  slippageBps: 5
})

let chart: IChartApi | null = null
let equitySeries: ISeriesApi<'Area'> | null = null
let backtestChart: IChartApi | null = null
let backtestEquitySeries: ISeriesApi<'Area'> | null = null

const {
  items: orders,
  nextCursor: orderNextCursor,
  loadingMore: orderLoadingMore,
  loadFirstPage: loadFirstOrderPage,
  loadMore: loadMoreOrderPage,
  reset: resetOrders
} = useCursorPagination<PaperOrder>(fetchOrderPage)

const {
  items: fills,
  nextCursor: fillNextCursor,
  loadingMore: fillLoadingMore,
  loadFirstPage: loadFirstFillPage,
  loadMore: loadMoreFillPage,
  reset: resetFills
} = useCursorPagination<PaperFill>(fetchFillPage)

const {
  items: corporateActions,
  nextCursor: corporateActionNextCursor,
  loadingMore: corporateActionLoadingMore,
  loadFirstPage: loadFirstCorporateActionPage,
  loadMore: loadMoreCorporateActionPage,
  reset: resetCorporateActions
} = useCursorPagination<PaperCorporateAction>(fetchCorporateActionPage)

function toNumber(value: string | number | null | undefined) {
  if (value == null) return 0
  return Number(value)
}

function formatMoney(value: string | number | null | undefined, precision = 2) {
  return toNumber(value).toLocaleString('zh-CN', {
    minimumFractionDigits: precision,
    maximumFractionDigits: precision
  })
}

function formatPercent(value: string | number | null | undefined) {
  return `${toNumber(value).toFixed(2)}%`
}

function toPercent(value: string | number | null | undefined) {
  return toNumber(value) * 100
}

function pnlClass(value: string | number | null | undefined) {
  const num = toNumber(value)
  if (num > 0) return 'text-up'
  if (num < 0) return 'text-down'
  return ''
}

function symbolLabel(row: { code: string; symbolName?: string | null }) {
  return row.symbolName ? `${row.code} ${row.symbolName}` : row.code
}

function riskAlertType(severity: PaperRiskAlert['severity']) {
  const map: Record<PaperRiskAlert['severity'], 'error' | 'warning' | 'info'> = {
    critical: 'error',
    warning: 'warning',
    info: 'info'
  }
  return map[severity] || 'info'
}

function riskSummaryTagType(status: string | null | undefined) {
  const map: Record<string, 'success' | 'warning' | 'danger' | 'info'> = {
    ok: 'success',
    watch: 'warning',
    critical: 'danger'
  }
  return map[String(status || '')] || 'info'
}

function riskSummaryLabel(status: string | null | undefined) {
  const map: Record<string, string> = {
    ok: '正常',
    watch: '观察',
    critical: '高风险'
  }
  return map[String(status || '')] || '未计算'
}

function riskAlertDescription(alert: PaperRiskAlert) {
  const subject = alert.code ? `${alert.code} · ` : ''
  const metric = alert.metric == null || toNumber(alert.metric) === 0 ? '' : ` 当前 ${toNumber(alert.metric).toFixed(2)}`
  const threshold = alert.threshold == null || toNumber(alert.threshold) === 0 ? '' : ` / 阈值 ${toNumber(alert.threshold).toFixed(2)}`
  return `${subject}${alert.detail}${metric}${threshold}`
}

function formatAttributionSource(value: string | null | undefined) {
  const map: Record<string, string> = {
    open_position: '持仓',
    closed_realized: '已平仓',
    mixed: '持仓+已实现'
  }
  return map[String(value || '')] || '-'
}

function formatSide(value: string | null | undefined) {
  const map: Record<string, string> = {
    buy: '买入',
    sell: '卖出'
  }
  return map[String(value || '').toLowerCase()] || value || '-'
}

function sideTagType(value: string | null | undefined) {
  return String(value || '').toLowerCase() === 'buy' ? 'danger' : 'success'
}

function formatOrderStatus(value: string | null | undefined) {
  const map: Record<string, string> = {
    suggested: '待审批',
    pending: '待执行',
    filled: '已成交',
    rejected: '已拒绝',
    cancelled: '已撤销'
  }
  return map[String(value || '').toLowerCase()] || value || '-'
}

function formatApprovalRiskLevel(value: string | null | undefined) {
  const map: Record<string, string> = {
    low: '低风险',
    medium: '需复核',
    high: '高风险'
  }
  return map[String(value || 'low').toLowerCase()] || '未计算'
}

function approvalRiskTagType(value: string | null | undefined) {
  const map: Record<string, 'success' | 'warning' | 'danger' | 'info'> = {
    low: 'success',
    medium: 'warning',
    high: 'danger'
  }
  return map[String(value || 'low').toLowerCase()] || 'info'
}

function approvalRiskReasons(row: PaperOrder) {
  return Array.isArray(row.approvalRiskReasons) ? row.approvalRiskReasons.filter(Boolean) : []
}

function approvalRiskText(row: PaperOrder) {
  const reasons = approvalRiskReasons(row)
  if (!reasons.length) return ''
  if (reasons.length === 1) return reasons[0]
  return `${reasons[0]} 等 ${reasons.length} 项`
}

function formatCorporateActionType(value: string | null | undefined) {
  const map: Record<string, string> = {
    cash_dividend: '现金分红',
    bonus_share: '送股',
    split: '拆股'
  }
  return map[String(value || '').toLowerCase()] || value || '-'
}

function corporateActionTagType(value: string | null | undefined) {
  const map: Record<string, 'success' | 'warning' | 'info'> = {
    cash_dividend: 'success',
    bonus_share: 'warning',
    split: 'info'
  }
  return map[String(value || '').toLowerCase()] || 'info'
}

function formatReplayEventType(value: PaperReplayEvent['type'] | string | null | undefined) {
  const map: Record<string, string> = {
    order: '订单',
    fill: '成交',
    corporate_action: '公司行为'
  }
  return map[String(value || '').toLowerCase()] || value || '-'
}

function replayEventTagType(value: PaperReplayEvent['type'] | string | null | undefined) {
  const map: Record<string, 'primary' | 'success' | 'warning' | 'info'> = {
    order: 'primary',
    fill: 'success',
    corporate_action: 'warning'
  }
  return map[String(value || '').toLowerCase()] || 'info'
}

function orderStatusTagType(value: string | null | undefined) {
  const map: Record<string, 'primary' | 'success' | 'warning' | 'info' | 'danger'> = {
    suggested: 'warning',
    pending: 'primary',
    filled: 'success',
    rejected: 'danger',
    cancelled: 'info'
  }
  return map[String(value || '').toLowerCase()] || 'info'
}

function chartLabelUtc8(value: unknown) {
  const timestamp = typeof value === 'number' ? value * 1000 : String(value || '')
  const parts = getUtc8DateParts(timestamp)
  if (!parts) return ''
  return `${parts.month}-${parts.day} ${parts.hour}:${parts.minute}`
}

function formatCommission(rate: string | number, minCommission: string | number) {
  return `万 ${(toNumber(rate) * 10000).toFixed(2)} / 最低 ${formatMoney(minCommission)}`
}

function formatBoardScope(row: RiskConfig) {
  const boards = [
    row.allowShMain ? '沪市主板' : '',
    row.allowSzMain ? '深市主板' : '',
    row.allowBj ? '北交所' : '',
    row.allowStar ? '科创板' : '',
    row.allowChinext ? '创业板' : '',
    row.allowEtfLof ? '场内ETF/LOF' : ''
  ].filter(Boolean)
  return boards.length ? boards.join('、') : '未启用任何交易范围'
}

function riskConfigNameForAccount(account: PaperAccount) {
  if (!account.riskConfigId) return '未绑定'
  return riskConfigById.value.get(account.riskConfigId)?.name || `配置 #${account.riskConfigId}`
}

function formatRiskConfigAccounts(row: RiskConfig) {
  const ids = row.accountIds || []
  if (!ids.length) return '未绑定账户'
  const names = ids.map((id) => accounts.value.find((account) => account.id === id)?.name || `#${id}`)
  return names.join('、')
}

function resetAccountForm() {
  accountForm.name = ''
  accountForm.cash = 1000000
  accountForm.riskConfigId = undefined
  accountForm.active = true
}

function resetRiskForm() {
  riskForm.name = ''
  riskForm.initialCash = 1000000
  riskForm.maxPositionPct = 0.3
  riskForm.maxOrderPct = 0.2
  riskForm.commissionRate = 0.0001
  riskForm.minCommission = 5
  riskForm.stampDutyRate = 0.001
  riskForm.transferFeeRate = 0.00001
  riskForm.allowShort = false
  riskForm.allowMargin = false
  riskForm.allowShMain = true
  riskForm.allowSzMain = true
  riskForm.allowBj = false
  riskForm.allowStar = false
  riskForm.allowChinext = false
  riskForm.allowEtfLof = true
  riskForm.enabled = true
  riskForm.accountIds = []
}

function resetCorporateActionForm() {
  corporateActionForm.code = positions.value[0]?.code || ''
  corporateActionForm.actionType = 'cash_dividend'
  corporateActionForm.exDate = new Date()
  corporateActionForm.cashPerShare = 0
  corporateActionForm.shareRatio = 0.1
  corporateActionForm.note = ''
}

function defaultBacktestDateRange(): [Date, Date] {
  const end = new Date()
  const start = new Date()
  start.setDate(end.getDate() - 180)
  return [start, end]
}

function resetBacktestForm() {
  backtestForm.code = positions.value[0]?.code || ''
  backtestForm.dateRange = defaultBacktestDateRange()
  backtestForm.initialCash = toNumber(selectedAccount.value?.initialCash) || 1000000
  backtestForm.buyThresholdPct = -3
  backtestForm.sellThresholdPct = 3
  backtestForm.orderPct = 0.1
  backtestForm.slippageBps = 5
}

async function loadOverview() {
  overview.value = unwrapJsonApiResource<PaperOverview>((await api.get('/paper/overview')).data)
}

async function loadAccounts() {
  accounts.value = unwrapJsonApiCollection<PaperAccount>((await api.get('/paper/accounts')).data)
  if (selectedAccountId.value) {
    selectedAccount.value = accounts.value.find((item) => item.id === selectedAccountId.value) || null
  }
}

async function loadRiskConfigs() {
  riskConfigs.value = unwrapJsonApiCollection<RiskConfig>((await api.get('/paper/risk-configs')).data)
}

async function fetchOrderPage(cursor?: string) {
  const { data } = await api.get(`/paper/accounts/${selectedAccountId.value}/orders`, {
    params: { 'page[limit]': 100, 'page[cursor]': cursor || undefined }
  })
  return {
    items: unwrapJsonApiCollection<PaperOrder>(data),
    nextCursor: nextCursorFromDocument(data)
  }
}

async function fetchFillPage(cursor?: string) {
  const { data } = await api.get(`/paper/accounts/${selectedAccountId.value}/fills`, {
    params: { 'page[limit]': 100, 'page[cursor]': cursor || undefined }
  })
  return {
    items: unwrapJsonApiCollection<PaperFill>(data),
    nextCursor: nextCursorFromDocument(data)
  }
}

async function fetchCorporateActionPage(cursor?: string) {
  const { data } = await api.get(`/paper/accounts/${selectedAccountId.value}/corporate-actions`, {
    params: { 'page[limit]': 100, 'page[cursor]': cursor || undefined }
  })
  return {
    items: unwrapJsonApiCollection<PaperCorporateAction>(data),
    nextCursor: nextCursorFromDocument(data)
  }
}

async function loadReplay() {
  if (!selectedAccountId.value) {
    replay.value = null
    return
  }
  replay.value = unwrapJsonApiResource<PaperReplay>((await api.get(`/paper/accounts/${selectedAccountId.value}/replay`)).data)
}

async function loadSelectedAccount() {
  if (!selectedAccountId.value) return
  const [positionsResp, performanceResp] = await Promise.all([
    api.get(`/paper/accounts/${selectedAccountId.value}/positions`),
    api.get(`/paper/accounts/${selectedAccountId.value}/performance`),
    loadFirstOrderPage(),
    loadFirstFillPage(),
    loadFirstCorporateActionPage(),
    loadReplay()
  ])
  positions.value = unwrapJsonApiCollection<PaperPosition>(positionsResp.data)
  performance.value = unwrapJsonApiResource<PaperPerformance>(performanceResp.data)
  if (!backtestForm.code && positions.value.length) {
    backtestForm.code = positions.value[0].code
  }
  await nextTick()
  renderChart()
}

async function loadMoreOrders() {
  await loadMoreOrderPage()
}

async function loadMoreFills() {
  await loadMoreFillPage()
}

async function loadMoreCorporateActions() {
  await loadMoreCorporateActionPage()
}

async function loadAll() {
  await Promise.all([loadOverview(), loadAccounts(), loadRiskConfigs()])
  if (!selectedAccountId.value && accounts.value.length) {
    selectedAccountId.value = accounts.value[0].id
    selectedAccount.value = accounts.value[0]
  }
  if (selectedAccountId.value) {
    await loadSelectedAccount()
  }
}

async function refreshPaperState() {
  await Promise.all([
    loadOverview(),
    loadAccounts(),
    selectedAccountId.value ? loadSelectedAccount() : Promise.resolve()
  ])
}

function handleAccountChange(row: PaperAccount | null) {
  selectedAccount.value = row
  selectedAccountId.value = row?.id || null
  backtest.value = null
  disposeBacktestChart()
  if (selectedAccountId.value) {
    backtestForm.code = ''
    backtestForm.initialCash = toNumber(row?.initialCash) || 1000000
    loadSelectedAccount()
    return
  }
  positions.value = []
  resetOrders()
  resetFills()
  resetCorporateActions()
  performance.value = null
  replay.value = null
  disposeChart()
  disposeBacktestChart()
}

function openAccountCreate() {
  accountDialogMode.value = 'create'
  resetAccountForm()
  accountDialogVisible.value = true
}

function openAccountEdit() {
  if (!selectedAccount.value) return
  accountDialogMode.value = 'edit'
  accountForm.name = selectedAccount.value.name
  accountForm.cash = toNumber(selectedAccount.value.cash)
  accountForm.riskConfigId = selectedAccount.value.riskConfigId ?? undefined
  accountForm.active = selectedAccount.value.active
  accountDialogVisible.value = true
}

async function saveAccount() {
  const payload = {
    name: accountForm.name.trim(),
    cash: accountForm.cash,
    riskConfigId: accountForm.riskConfigId ?? null,
    active: accountForm.active
  }
  if (!payload.name) {
    ElMessage.warning('账户名称不能为空')
    return
  }
  if (accountDialogMode.value === 'create') {
    await api.post('/paper/accounts', jsonapiResource('paper-accounts', payload))
  } else if (selectedAccountId.value) {
    await api.put(`/paper/accounts/${selectedAccountId.value}`, jsonapiResource('paper-accounts', payload))
  }
  accountDialogVisible.value = false
  ElMessage.success('账户已保存')
  await loadAll()
}

async function toggleAccount(active: boolean) {
  if (!selectedAccountId.value) return
  await api.post(`/paper/accounts/${selectedAccountId.value}/${active ? 'activate' : 'deactivate'}`, jsonapiResource('paper-accounts', {}))
  ElMessage.success(active ? '账户已启用' : '账户已停用')
  await loadAll()
}

async function deleteAccount() {
  if (!selectedAccountId.value || !selectedAccount.value) return
  await ElMessageBox.confirm(`确定删除账户 ${selectedAccount.value.name} 吗？`, '删除账户', { type: 'warning' })
  await api.delete(`/paper/accounts/${selectedAccountId.value}`)
  ElMessage.success('账户已删除')
  selectedAccountId.value = null
  selectedAccount.value = null
  await loadAll()
}

function openRiskCreate() {
  riskDialogMode.value = 'create'
  editingRiskId.value = null
  resetRiskForm()
  riskDialogVisible.value = true
}

function openRiskEdit(row: RiskConfig) {
  riskDialogMode.value = 'edit'
  editingRiskId.value = row.id
  riskForm.name = row.name
  riskForm.initialCash = toNumber(row.initialCash)
  riskForm.maxPositionPct = toNumber(row.maxPositionPct)
  riskForm.maxOrderPct = toNumber(row.maxOrderPct)
  riskForm.commissionRate = toNumber(row.commissionRate)
  riskForm.minCommission = toNumber(row.minCommission)
  riskForm.stampDutyRate = toNumber(row.stampDutyRate)
  riskForm.transferFeeRate = toNumber(row.transferFeeRate)
  riskForm.allowShort = row.allowShort
  riskForm.allowMargin = row.allowMargin
  riskForm.allowShMain = row.allowShMain
  riskForm.allowSzMain = row.allowSzMain
  riskForm.allowBj = row.allowBj
  riskForm.allowStar = row.allowStar
  riskForm.allowChinext = row.allowChinext
  riskForm.allowEtfLof = row.allowEtfLof
  riskForm.enabled = row.enabled
  riskForm.accountIds = [...(row.accountIds || [])]
  riskDialogVisible.value = true
}

async function saveRiskConfig() {
  const payload = { ...riskForm }
  if (!payload.name.trim()) {
    ElMessage.warning('风控配置名称不能为空')
    return
  }
  if (
    !payload.allowShMain &&
    !payload.allowSzMain &&
    !payload.allowBj &&
    !payload.allowStar &&
    !payload.allowChinext &&
    !payload.allowEtfLof
  ) {
    ElMessage.warning('至少启用一个可交易范围')
    return
  }
  if (riskDialogMode.value === 'create') {
    await api.post('/paper/risk-configs', jsonapiResource('paper-risk-configs', payload))
  } else if (editingRiskId.value) {
    await api.put(`/paper/risk-configs/${editingRiskId.value}`, jsonapiResource('paper-risk-configs', payload))
  }
  riskDialogVisible.value = false
  ElMessage.success('风控配置已保存')
  if (!payload.accountIds.length) {
    ElMessage.warning('该风控配置未绑定账户，暂不会生效')
  }
  await loadRiskConfigs()
  await loadAccounts()
}

async function deleteRiskConfig(id: number) {
  const row = riskConfigs.value.find((item) => item.id === id)
  const boundText = row?.accountIds?.length ? `删除后将解绑账户：${formatRiskConfigAccounts(row)}。` : ''
  await ElMessageBox.confirm(`${boundText}确定删除这个风控配置吗？`, '删除风控配置', { type: 'warning' })
  await api.delete(`/paper/risk-configs/${id}`)
  ElMessage.success('风控配置已删除')
  await loadAll()
}

async function approveOrder(row: PaperOrder) {
  const highRisk = Boolean(row.approvalConfirmRequired)
  const reasons = approvalRiskReasons(row)
  const detail = highRisk && reasons.length
    ? `该订单被标记为高风险：\n${reasons.map((item) => `- ${item}`).join('\n')}\n\n确认已完成复核并继续提交风控检查？`
    : '通过后会提交风控检查，检查通过的订单将进入待执行队列。'
  await ElMessageBox.confirm(detail, highRisk ? '高风险订单复核' : '通过订单审批', {
    type: highRisk ? 'error' : 'warning',
    confirmButtonText: highRisk ? '确认复核并通过' : '通过'
  })
  await api.post(`/paper/orders/${row.id}/approve`, jsonapiResource('paper-order-approvals', { confirmHighRisk: highRisk }))
  ElMessage.success('订单已通过审批')
  await Promise.all([loadOverview(), loadAccounts(), loadSelectedAccount()])
}

async function rejectOrder(orderId: number) {
  const { value } = await ElMessageBox.prompt('请输入拒绝原因，便于后续复盘。', '拒绝订单', {
    inputPlaceholder: '例如：等待更多确认信号',
    confirmButtonText: '拒绝',
    cancelButtonText: '取消',
    inputValue: '人工审批拒绝'
  })
  await api.post(`/paper/orders/${orderId}/reject`, jsonapiResource('paper-order-rejections', { reason: value || '人工审批拒绝' }))
  ElMessage.success('订单已拒绝')
  await Promise.all([loadOverview(), loadAccounts(), loadSelectedAccount()])
}

function openOrderFill(row: PaperOrder) {
  const remainingQuantity = Math.max(0, toNumber(row.remainingQuantity ?? row.quantity))
  if (remainingQuantity <= 0) {
    ElMessage.warning('该订单没有可成交的剩余数量')
    return
  }
  const price = toNumber(row.filledPrice) || toNumber(row.suggestedPrice)
  orderFillForm.orderId = row.id
  orderFillForm.code = row.code
  orderFillForm.side = row.side
  orderFillForm.price = price
  orderFillForm.quantity = remainingQuantity
  orderFillForm.remainingQuantity = remainingQuantity
  orderFillDialogVisible.value = true
}

async function saveOrderFill() {
  if (!orderFillForm.orderId) return
  if (orderFillForm.price <= 0) {
    ElMessage.warning('成交价格必须大于 0')
    return
  }
  if (orderFillForm.quantity <= 0 || orderFillForm.quantity > orderFillForm.remainingQuantity) {
    ElMessage.warning('成交数量必须在剩余数量范围内')
    return
  }
  await ElMessageBox.confirm(
    `确认按 ${formatMoney(orderFillForm.price, 4)} 成交 ${orderFillForm.quantity} 股？`,
    '记录模拟成交',
    { type: 'warning' }
  )
  await api.post(
    `/paper/orders/${orderFillForm.orderId}/fill`,
    jsonapiResource('paper-order-fills', { price: orderFillForm.price, quantity: orderFillForm.quantity })
  )
  orderFillDialogVisible.value = false
  ElMessage.success('成交已记录')
  await Promise.all([loadOverview(), loadAccounts(), loadSelectedAccount()])
}

async function cancelOrder(orderId: number) {
  await api.post(`/paper/orders/${orderId}/cancel`, jsonapiResource('paper-orders', {}))
  ElMessage.success('订单已撤销')
  await Promise.all([loadOverview(), loadAccounts(), loadSelectedAccount()])
}

async function deleteOrder(orderId: number) {
  await ElMessageBox.confirm('确定删除这条订单记录吗？删除后不可恢复。', '删除订单', { type: 'warning' })
  await api.delete(`/paper/orders/${orderId}`)
  ElMessage.success('订单记录已删除')
  await Promise.all([loadOverview(), loadAccounts(), loadSelectedAccount()])
}

function openCorporateActionCreate(row?: PaperPosition) {
  resetCorporateActionForm()
  if (row?.code) {
    corporateActionForm.code = row.code
  }
  corporateActionDialogVisible.value = true
}

async function saveCorporateAction() {
  if (!selectedAccountId.value) return
  const code = corporateActionForm.code.trim().toUpperCase()
  if (!code) {
    ElMessage.warning('标的代码不能为空')
    return
  }
  if (corporateActionForm.actionType === 'cash_dividend' && corporateActionForm.cashPerShare <= 0) {
    ElMessage.warning('现金分红需要填写每股现金')
    return
  }
  if (corporateActionForm.actionType !== 'cash_dividend' && corporateActionForm.shareRatio <= 0) {
    ElMessage.warning('送股/拆股需要填写有效比例')
    return
  }
  if (corporateActionForm.actionType === 'split' && corporateActionForm.shareRatio <= 1) {
    ElMessage.warning('拆股倍数必须大于 1')
    return
  }
  await ElMessageBox.confirm('公司行为会立即调整模拟盘现金或持仓数量，并写入复盘时间线。', '应用公司行为', { type: 'warning' })
  const payload: Record<string, unknown> = {
    code,
    actionType: corporateActionForm.actionType,
    exDate: corporateActionForm.exDate ? corporateActionForm.exDate.toISOString() : undefined,
    note: corporateActionForm.note.trim() || undefined
  }
  if (corporateActionForm.actionType === 'cash_dividend') {
    payload.cashPerShare = corporateActionForm.cashPerShare
  } else {
    payload.shareRatio = corporateActionForm.shareRatio
  }
  await api.post(`/paper/accounts/${selectedAccountId.value}/corporate-actions`, jsonapiResource('paper-corporate-actions', payload))
  corporateActionDialogVisible.value = false
  ElMessage.success('公司行为已应用')
  await Promise.all([loadOverview(), loadAccounts(), loadSelectedAccount()])
}

async function runBacktest() {
  if (!selectedAccountId.value) return
  const code = backtestForm.code.trim().toUpperCase()
  if (!code) {
    ElMessage.warning('回测标的不能为空')
    return
  }
  if (!backtestForm.dateRange?.[0] || !backtestForm.dateRange?.[1]) {
    ElMessage.warning('请选择回测日期范围')
    return
  }
  backtestLoading.value = true
  try {
    const payload = {
      code,
      startDate: backtestForm.dateRange[0].toISOString(),
      endDate: backtestForm.dateRange[1].toISOString(),
      initialCash: backtestForm.initialCash,
      buyThresholdPct: backtestForm.buyThresholdPct,
      sellThresholdPct: backtestForm.sellThresholdPct,
      orderPct: backtestForm.orderPct,
      slippageBps: backtestForm.slippageBps
    }
    const { data } = await api.post(`/paper/accounts/${selectedAccountId.value}/backtests`, jsonapiResource('paper-backtests', payload))
    backtest.value = unwrapJsonApiResource<PaperBacktest>(data)
    await nextTick()
    renderBacktestChart()
    ElMessage.success('回测已完成')
  } finally {
    backtestLoading.value = false
  }
}

function formatBacktestStatus(value: PaperBacktestOrder['status'] | string | null | undefined) {
  const map: Record<string, string> = {
    suggested: '已生成',
    filled: '已成交',
    rejected: '已拒绝'
  }
  return map[String(value || '').toLowerCase()] || value || '-'
}

function backtestStatusTagType(value: PaperBacktestOrder['status'] | string | null | undefined) {
  const map: Record<string, 'success' | 'warning' | 'danger' | 'info'> = {
    suggested: 'info',
    filled: 'success',
    rejected: 'danger'
  }
  return map[String(value || '').toLowerCase()] || 'info'
}

function formatBacktestReason(value: string | null | undefined) {
  const map: Record<string, string> = {
    blocked_by_t_plus_one: 'T+1 限制',
    blocked_by_zero_volume: '停牌/零成交量',
    blocked_by_daily_limit_down: '跌停限制',
    blocked_by_daily_limit_up: '涨停限制',
    blocked_by_cash_or_position_limit: '现金或仓位上限',
    quantity_below_lot: '不足一手',
    quantity_below_lot_after_fees: '计费后不足一手',
    filled: '成交'
  }
  return map[String(value || '')] || value || '-'
}

function renderChart() {
  if (!chartContainer.value || !performance.value) return
  disposeChart()
  const colors = chartTheme()
  chart = createChart(chartContainer.value, {
    autoSize: true,
    layout: {
      background: { color: colors.background },
      textColor: colors.text,
      attributionLogo: false
    },
    grid: {
      vertLines: { color: colors.grid, style: LineStyle.Solid },
      horzLines: { color: colors.grid, style: LineStyle.Solid }
    },
    rightPriceScale: {
      borderColor: colors.border
    },
    localization: {
      timeFormatter: chartLabelUtc8
    },
    timeScale: {
      borderColor: colors.border,
      timeVisible: true,
      secondsVisible: false,
      tickMarkFormatter: chartLabelUtc8
    }
  })
  equitySeries = chart.addSeries(AreaSeries, {
    lineColor: colors.line,
    topColor: colors.lineTop,
    bottomColor: colors.lineBottom
  })
  equitySeries?.setData(
    performance.value.series.map((item) => ({
      time: Math.floor(new Date(item.snapshotTime).getTime() / 1000) as UTCTimestamp,
      value: toNumber(item.totalEquity)
    }))
  )
  chart.timeScale().fitContent()
}

function renderBacktestChart() {
  if (!backtestChartContainer.value || !backtest.value) return
  disposeBacktestChart()
  const colors = chartTheme()
  backtestChart = createChart(backtestChartContainer.value, {
    autoSize: true,
    layout: {
      background: { color: colors.background },
      textColor: colors.text,
      attributionLogo: false
    },
    grid: {
      vertLines: { color: colors.grid, style: LineStyle.Solid },
      horzLines: { color: colors.grid, style: LineStyle.Solid }
    },
    rightPriceScale: {
      borderColor: colors.border
    },
    localization: {
      timeFormatter: chartLabelUtc8
    },
    timeScale: {
      borderColor: colors.border,
      timeVisible: true,
      secondsVisible: false,
      tickMarkFormatter: chartLabelUtc8
    }
  })
  backtestEquitySeries = backtestChart.addSeries(AreaSeries, {
    lineColor: colors.altLine,
    topColor: colors.altLineTop,
    bottomColor: colors.altLineBottom
  })
  backtestEquitySeries?.setData(
    backtest.value.series.map((item) => ({
      time: Math.floor(new Date(item.tradeDate).getTime() / 1000) as UTCTimestamp,
      value: toNumber(item.totalEquity)
    }))
  )
  backtestChart.timeScale().fitContent()
}

function disposeChart() {
  equitySeries = null
  if (chart) {
    chart.remove()
    chart = null
  }
}

function disposeBacktestChart() {
  backtestEquitySeries = null
  if (backtestChart) {
    backtestChart.remove()
    backtestChart = null
  }
}

onMounted(loadAll)

watch(resolvedTheme, () => {
  if (chart && performance.value) renderChart()
  if (backtestChart && backtest.value) renderBacktestChart()
})

useAutoRefresh({
  intervalMs: 10000,
  refresh: refreshPaperState
})
onBeforeUnmount(() => {
  disposeChart()
  disposeBacktestChart()
})
</script>

<style scoped>
.metrics-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
  gap: 16px;
  margin-bottom: 16px;
}

.metric-card {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.metric-card span {
  color: var(--text-muted);
  font-size: 12px;
}

.metric-card strong {
  font-size: 24px;
  font-weight: 700;
  color: var(--text-main);
}

.metric-card__compact {
  font-size: 15px !important;
  line-height: 1.4;
}

.metric-card.slim {
  padding: 10px 12px;
  border: 1px solid var(--border-soft);
  border-radius: 8px;
  background: var(--surface-subtle);
}

.metric-card.slim strong {
  font-size: 18px;
}

.paper-detail-grid {
  grid-template-columns: minmax(0, 1fr) 480px;
}

.detail-main,
.detail-side {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.account-metrics {
  grid-template-columns: repeat(4, minmax(0, 1fr));
  margin-bottom: 12px;
}

.text-up {
  color: var(--quote-up);
}

.text-down {
  color: var(--quote-down);
}

.text-warning {
  color: var(--attention);
}

.paper-alert {
  margin-bottom: 12px;
}

.form-static {
  min-height: 32px;
  display: flex;
  align-items: center;
  color: var(--text-main);
  font-weight: 600;
}

.form-hint {
  width: 100%;
  margin-top: 6px;
  color: var(--text-muted);
  font-size: 12px;
}

.performance-insights {
  display: grid;
  grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.1fr);
  gap: 16px;
  margin-top: 16px;
}

.performance-block {
  min-width: 0;
}

.performance-block__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.performance-block__head h3 {
  margin: 0;
  font-size: 15px;
  font-weight: 700;
}

.risk-alert-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.compact-attribution-list {
  gap: 10px;
}

.backtest-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(220px, 1fr));
  gap: 0 16px;
}

.backtest-results {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.backtest-summary-grid {
  grid-template-columns: repeat(4, minmax(0, 1fr));
  margin-bottom: 0;
}

.backtest-chart {
  min-height: 260px;
}

.board-switches {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 16px;
  width: 100%;
}

.paper-timeline {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.paper-timeline__item {
  border-left: 3px solid var(--border-soft);
  padding: 8px 0 8px 12px;
}

.paper-timeline__head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: 6px;
}

.paper-timeline__item p {
  margin: 6px 0 0;
  color: var(--text-muted);
  line-height: 1.45;
}

.order-review-cell {
  min-width: 0;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
}

.order-review-cell__hint {
  color: var(--danger);
  font-size: 12px;
  font-weight: 600;
}

.order-review-cell__reason {
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 1280px) {
  .metrics-grid,
  .account-metrics,
  .paper-detail-grid,
  .performance-insights,
  .backtest-form,
  .backtest-summary-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 767px) {
  .board-switches {
    grid-template-columns: 1fr;
  }
}
</style>
