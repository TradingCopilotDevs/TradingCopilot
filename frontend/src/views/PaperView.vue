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
      </div>

      <div class="panel">
        <div class="section-head">
          <h2>持仓</h2>
          <el-button @click="loadSelectedAccount">刷新</el-button>
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
              <el-tag>{{ row.quantity }}</el-tag>
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
            </div>
            <div class="mobile-card__actions">
              <el-button v-if="row.meetingId" plain @click="router.push(`/meetings/${row.meetingId}`)">查看会议</el-button>
              <el-button v-if="row.status === 'pending' || row.status === 'suggested'" plain type="danger" @click="cancelOrder(row.id)">
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
          <el-table-column prop="quantity" label="数量" width="70" />
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tag :type="orderStatusTagType(row.status)" effect="plain">{{ formatOrderStatus(row.status) }}</el-tag>
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
          <el-table-column label="操作" width="90">
            <template #default="{ row }">
              <el-button v-if="row.status === 'pending' || row.status === 'suggested'" link type="danger" @click="cancelOrder(row.id)">撤单</el-button>
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
</template>

<script setup lang="ts">
import { ElMessage, ElMessageBox } from 'element-plus'
import { AreaSeries, createChart, type IChartApi, type ISeriesApi, LineStyle, type UTCTimestamp } from 'lightweight-charts'
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  api,
  jsonapiResource,
  type PaperAccount,
  type PaperFill,
  type PaperOrder,
  type PaperOverview,
  type PaperPerformance,
  type PaperPosition,
  type RiskConfig,
  unwrapJsonApiCollection,
  unwrapJsonApiResource
} from '../api'
import { useAutoRefresh } from '../composables/useAutoRefresh'
import { nextCursorFromDocument, useCursorPagination } from '../composables/useCursorPagination'
import { useResponsive } from '../composables/useResponsive'
import { formatDateTimeUtc8, getUtc8DateParts } from '../utils/datetime'

const router = useRouter()
const { isMobile, isTablet } = useResponsive()
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
const editingRiskId = ref<number | null>(null)
const chartContainer = ref<HTMLElement | null>(null)
const selectedAccount = ref<PaperAccount | null>(null)

const accountDialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '90vw' : '560px'))
const riskDialogWidth = computed(() => (isMobile.value ? '100%' : isTablet.value ? '92vw' : '620px'))
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

let chart: IChartApi | null = null
let equitySeries: ISeriesApi<'Area'> | null = null

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
    suggested: '建议',
    pending: '待执行',
    filled: '已成交',
    rejected: '已拒绝',
    cancelled: '已撤销'
  }
  return map[String(value || '').toLowerCase()] || value || '-'
}

function orderStatusTagType(value: string | null | undefined) {
  const map: Record<string, 'primary' | 'success' | 'warning' | 'info' | 'danger'> = {
    suggested: 'info',
    pending: 'warning',
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

async function loadSelectedAccount() {
  if (!selectedAccountId.value) return
  const [positionsResp, performanceResp] = await Promise.all([
    api.get(`/paper/accounts/${selectedAccountId.value}/positions`),
    api.get(`/paper/accounts/${selectedAccountId.value}/performance`),
    loadFirstOrderPage(),
    loadFirstFillPage()
  ])
  positions.value = unwrapJsonApiCollection<PaperPosition>(positionsResp.data)
  performance.value = unwrapJsonApiResource<PaperPerformance>(performanceResp.data)
  await nextTick()
  renderChart()
}

async function loadMoreOrders() {
  await loadMoreOrderPage()
}

async function loadMoreFills() {
  await loadMoreFillPage()
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
  if (selectedAccountId.value) {
    loadSelectedAccount()
    return
  }
  positions.value = []
  resetOrders()
  resetFills()
  performance.value = null
  disposeChart()
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

function renderChart() {
  if (!chartContainer.value || !performance.value) return
  disposeChart()
  chart = createChart(chartContainer.value, {
    autoSize: true,
    layout: {
      background: { color: '#ffffff' },
      textColor: '#334155',
      attributionLogo: false
    },
    grid: {
      vertLines: { color: '#eef2f7', style: LineStyle.Solid },
      horzLines: { color: '#eef2f7', style: LineStyle.Solid }
    },
    rightPriceScale: {
      borderColor: '#dbe3ee'
    },
    localization: {
      timeFormatter: chartLabelUtc8
    },
    timeScale: {
      borderColor: '#dbe3ee',
      timeVisible: true,
      secondsVisible: false,
      tickMarkFormatter: chartLabelUtc8
    }
  })
  equitySeries = chart.addSeries(AreaSeries, {
    lineColor: '#2563eb',
    topColor: 'rgba(37, 99, 235, 0.25)',
    bottomColor: 'rgba(37, 99, 235, 0.03)'
  })
  equitySeries?.setData(
    performance.value.series.map((item) => ({
      time: Math.floor(new Date(item.snapshotTime).getTime() / 1000) as UTCTimestamp,
      value: toNumber(item.totalEquity)
    }))
  )
  chart.timeScale().fitContent()
}

function disposeChart() {
  equitySeries = null
  if (chart) {
    chart.remove()
    chart = null
  }
}

onMounted(loadAll)
useAutoRefresh({
  intervalMs: 10000,
  refresh: refreshPaperState
})
onBeforeUnmount(disposeChart)
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
  color: #64748b;
  font-size: 12px;
}

.metric-card strong {
  font-size: 24px;
  font-weight: 700;
  color: #0f172a;
}

.metric-card__compact {
  font-size: 15px !important;
  line-height: 1.4;
}

.metric-card.slim {
  padding: 10px 12px;
  border: 1px solid #dbe3ee;
  border-radius: 8px;
  background: #fbfdff;
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
  color: #dc2626;
}

.text-down {
  color: #16a34a;
}

.text-warning {
  color: #d97706;
}

.paper-alert {
  margin-bottom: 12px;
}

.board-switches {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 8px 16px;
  width: 100%;
}

@media (max-width: 1280px) {
  .metrics-grid,
  .account-metrics,
  .paper-detail-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 767px) {
  .board-switches {
    grid-template-columns: 1fr;
  }
}
</style>
