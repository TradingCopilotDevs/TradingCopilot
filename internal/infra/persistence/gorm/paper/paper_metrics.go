package paper

import (
	"sort"
	"time"

	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainpaper "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/paper"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func CaptureAccountSnapshot(db *gorm.DB, account domainpaper.Account) error {
	metrics := ComputeAccountMetrics(db, account, false)
	snap := domainpaper.EquitySnapshot{AccountID: account.ID, SnapshotTime: paperNow(), Cash: account.Cash, MarketValue: metrics["market_value"].(decimal.Decimal), TotalEquity: metrics["total_equity"].(decimal.Decimal), UnrealizedPNL: metrics["unrealized_pnl"].(decimal.Decimal), RealizedPNL: metrics["realized_pnl"].(decimal.Decimal)}
	return gormrepo.NewPaperRepository(db).CreateEquitySnapshot(dbContext(db), &snap)
}

func ComputeAccountMetrics(db *gorm.DB, account domainpaper.Account, refresh bool) map[string]any {
	var positionRows []persistmodel.PaperPosition
	db.Where("account_id = ?", account.ID).Find(&positionRows)
	positions := paperPositionsModelToDomain(positionRows)
	marketValue := decimal.Zero
	unrealized := decimal.Zero
	positionCount := 0
	for _, pos := range positions {
		if pos.Quantity > 0 {
			positionCount++
		}
		price := pos.LastPrice
		if refresh {
			if latest := latestQuotePrice(db, pos.Code); !latest.IsZero() {
				price = latest
			}
		}
		mv := q2(price.Mul(decimal.NewFromInt(int64(pos.Quantity))))
		marketValue = marketValue.Add(mv)
		unrealized = unrealized.Add(q2(mv.Sub(pos.CostAmount)))
	}
	var fillRows []persistmodel.PaperFill
	db.Where("account_id = ?", account.ID).Find(&fillRows)
	fills := paperFillsModelToDomain(fillRows)
	realized := decimal.Zero
	for _, fill := range fills {
		realized = realized.Add(fill.RealizedPNL)
	}
	totalEquity := q2(account.Cash.Add(marketValue))
	totalReturn := decimal.Zero
	if !account.InitialCash.IsZero() {
		totalReturn = q4(totalEquity.Sub(account.InitialCash).Div(account.InitialCash).Mul(decimal.NewFromInt(100)))
	}
	return map[string]any{"market_value": q2(marketValue), "total_equity": totalEquity, "unrealized_pnl": q2(unrealized), "realized_pnl": q2(realized), "total_return_pct": totalReturn, "position_count": positionCount, "pending_order_count": countModel(db, &persistmodel.PaperOrder{}, "account_id = ? AND status IN ?", account.ID, []domainkernel.OrderStatus{domainkernel.OrderPending, domainkernel.OrderSuggested})}
}

func PaperOverview(db *gorm.DB) (map[string]any, error) {
	var accountRows []persistmodel.PaperAccount
	if err := db.Find(&accountRows).Error; err != nil {
		return nil, err
	}
	accounts := paperAccountsModelToDomain(accountRows)
	totalCash := decimal.Zero
	totalMarketValue := decimal.Zero
	totalUnrealized := decimal.Zero
	totalRealized := decimal.Zero
	totalInitial := decimal.Zero
	active := 0
	for _, account := range accounts {
		metrics := ComputeAccountMetrics(db, account, false)
		totalCash = totalCash.Add(account.Cash)
		totalInitial = totalInitial.Add(account.InitialCash)
		totalMarketValue = totalMarketValue.Add(metrics["market_value"].(decimal.Decimal))
		totalUnrealized = totalUnrealized.Add(metrics["unrealized_pnl"].(decimal.Decimal))
		totalRealized = totalRealized.Add(metrics["realized_pnl"].(decimal.Decimal))
		if account.Active {
			active++
		}
	}
	totalEquity := q2(totalCash.Add(totalMarketValue))
	totalReturn := decimal.Zero
	if !totalInitial.IsZero() {
		totalReturn = q4(totalEquity.Sub(totalInitial).Div(totalInitial).Mul(decimal.NewFromInt(100)))
	}
	return map[string]any{
		"account_count": len(accounts), "active_account_count": active, "total_cash": q2(totalCash), "total_market_value": q2(totalMarketValue), "total_equity": totalEquity,
		"total_unrealized_pnl": q2(totalUnrealized), "total_realized_pnl": q2(totalRealized), "total_return_pct": totalReturn,
		"position_count": countModel(db, &persistmodel.PaperPosition{}, "quantity > ?", 0), "fill_count": countModel(db, &persistmodel.PaperFill{}, ""),
		"suggested_order_count": countModel(db, &persistmodel.PaperOrder{}, "status = ?", domainkernel.OrderSuggested), "pending_order_count": countModel(db, &persistmodel.PaperOrder{}, "status = ?", domainkernel.OrderPending),
		"filled_order_count": countModel(db, &persistmodel.PaperOrder{}, "status = ?", domainkernel.OrderFilled), "rejected_order_count": countModel(db, &persistmodel.PaperOrder{}, "status = ?", domainkernel.OrderRejected), "cancelled_order_count": countModel(db, &persistmodel.PaperOrder{}, "status = ?", domainkernel.OrderCancelled),
	}, nil
}

func PaperPerformance(db *gorm.DB, account domainpaper.Account) map[string]any {
	_ = CaptureAccountSnapshot(db, account)
	var seriesRows []persistmodel.PaperEquitySnapshot
	db.Where("account_id = ?", account.ID).Order("snapshot_time").Find(&seriesRows)
	series := paperEquitySnapshotsModelToDomain(seriesRows)
	metrics := ComputeAccountMetrics(db, account, false)
	peak := decimal.Zero
	maxDrawdown := decimal.Zero
	for _, point := range series {
		if point.TotalEquity.GreaterThan(peak) {
			peak = point.TotalEquity
			continue
		}
		if !peak.IsZero() {
			dd := peak.Sub(point.TotalEquity).Div(peak).Mul(decimal.NewFromInt(100))
			if dd.GreaterThan(maxDrawdown) {
				maxDrawdown = dd
			}
		}
	}
	fills := countModel(db, &persistmodel.PaperFill{}, "account_id = ?", account.ID)
	wins := countModel(db, &persistmodel.PaperFill{}, "account_id = ? AND realized_pnl > ?", account.ID, 0)
	winRate := decimal.Zero
	if fills > 0 {
		winRate = decimal.NewFromInt(wins).Div(decimal.NewFromInt(fills)).Mul(decimal.NewFromInt(100)).Round(4)
	}
	var positionRows []persistmodel.PaperPosition
	db.Where("account_id = ?", account.ID).Order("code").Find(&positionRows)
	positions := paperPositionsModelToDomain(positionRows)
	var fillRows []persistmodel.PaperFill
	db.Where("account_id = ?", account.ID).Find(&fillRows)
	fillItems := paperFillsModelToDomain(fillRows)
	attribution := paperPortfolioAttribution(db, account, positions, fillItems, metrics)
	riskAlerts := paperRiskAlerts(db, account, positions, metrics, maxDrawdown.Round(4))
	return map[string]any{"account_id": account.ID, "initial_cash": account.InitialCash, "latest_equity": metrics["total_equity"], "latest_cash": account.Cash, "latest_market_value": metrics["market_value"], "unrealized_pnl": metrics["unrealized_pnl"], "realized_pnl": metrics["realized_pnl"], "total_return_pct": metrics["total_return_pct"], "max_drawdown_pct": maxDrawdown.Round(4), "win_rate_pct": winRate, "fills_count": fills, "series": series, "attribution": attribution, "risk_alerts": riskAlerts, "risk_summary": paperRiskSummary(riskAlerts)}
}

func paperPortfolioAttribution(db *gorm.DB, account domainpaper.Account, positions []domainpaper.Position, fills []domainpaper.Fill, metrics map[string]any) []map[string]any {
	type row struct {
		Code        string
		Quantity    int
		CostAmount  decimal.Decimal
		MarketValue decimal.Decimal
		Unrealized  decimal.Decimal
		Realized    decimal.Decimal
		UpdatedAt   time.Time
		Source      string
	}
	rows := map[string]*row{}
	for _, position := range positions {
		item := rows[position.Code]
		if item == nil {
			item = &row{Code: position.Code}
			rows[position.Code] = item
		}
		item.Quantity = position.Quantity
		item.CostAmount = position.CostAmount
		item.MarketValue = position.MarketValue
		item.Unrealized = position.UnrealizedPNL
		item.UpdatedAt = position.UpdatedAt
		item.Source = "open_position"
	}
	for _, fill := range fills {
		if fill.RealizedPNL.IsZero() {
			continue
		}
		item := rows[fill.Code]
		if item == nil {
			item = &row{Code: fill.Code, Source: "closed_realized"}
			rows[fill.Code] = item
		} else if item.Source == "open_position" {
			item.Source = "mixed"
		}
		item.Realized = item.Realized.Add(fill.RealizedPNL)
	}
	totalEquity := decimalFromMetric(metrics, "total_equity")
	totalPNL := decimalFromMetric(metrics, "unrealized_pnl").Add(decimalFromMetric(metrics, "realized_pnl"))
	items := make([]row, 0, len(rows))
	for _, item := range rows {
		items = append(items, *item)
	}
	sort.Slice(items, func(i, j int) bool {
		left := items[i].Unrealized.Add(items[i].Realized).Abs()
		right := items[j].Unrealized.Add(items[j].Realized).Abs()
		if !left.Equal(right) {
			return left.GreaterThan(right)
		}
		return items[i].MarketValue.GreaterThan(items[j].MarketValue)
	})
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		total := q4(item.Unrealized.Add(item.Realized))
		weightPct := decimal.Zero
		if !totalEquity.IsZero() {
			weightPct = q4(item.MarketValue.Div(totalEquity).Mul(decimal.NewFromInt(100)))
		}
		returnPct := decimal.Zero
		if !item.CostAmount.IsZero() {
			returnPct = q4(total.Div(item.CostAmount).Mul(decimal.NewFromInt(100)))
		}
		contributionPct := decimal.Zero
		if !totalPNL.IsZero() {
			contributionPct = q4(total.Div(totalPNL).Mul(decimal.NewFromInt(100)))
		}
		out = append(out, map[string]any{
			"code":             item.Code,
			"symbol_name":      symbolName(db, item.Code),
			"quantity":         item.Quantity,
			"cost_amount":      q2(item.CostAmount),
			"market_value":     q2(item.MarketValue),
			"weight_pct":       weightPct,
			"unrealized_pnl":   q2(item.Unrealized),
			"realized_pnl":     q2(item.Realized),
			"total_pnl":        q2(total),
			"return_pct":       returnPct,
			"contribution_pct": contributionPct,
			"updated_at":       item.UpdatedAt,
			"source":           item.Source,
		})
	}
	return out
}

func paperRiskAlerts(db *gorm.DB, account domainpaper.Account, positions []domainpaper.Position, metrics map[string]any, maxDrawdown decimal.Decimal) []map[string]any {
	alerts := []map[string]any{}
	totalEquity := decimalFromMetric(metrics, "total_equity")
	pendingOrders := int64FromMetric(metrics, "pending_order_count")
	cfg, _ := RiskEnabled(db, account)
	if cfg == nil {
		alerts = append(alerts, paperRiskAlert("missing_risk_config", "critical", "账户未启用风控配置", "新订单会被拒绝，需先绑定并启用风控配置。", "", decimal.Zero, decimal.Zero))
	} else if !cfg.MaxPositionPct.IsZero() && !totalEquity.IsZero() {
		limitPct := cfg.MaxPositionPct.Mul(decimal.NewFromInt(100))
		warnPct := limitPct.Mul(decimal.RequireFromString("0.90"))
		for _, position := range positions {
			if position.Quantity <= 0 || position.MarketValue.IsZero() {
				continue
			}
			weightPct := q4(position.MarketValue.Div(totalEquity).Mul(decimal.NewFromInt(100)))
			switch {
			case weightPct.GreaterThan(limitPct):
				alerts = append(alerts, paperRiskAlert("position_concentration", "critical", "单标的仓位超过风控上限", "该持仓市值占比已超过风险配置的单标的上限。", position.Code, weightPct, limitPct))
			case weightPct.GreaterThanOrEqual(warnPct):
				alerts = append(alerts, paperRiskAlert("position_concentration_near_limit", "warning", "单标的仓位接近上限", "该持仓市值占比已接近风险配置的单标的上限。", position.Code, weightPct, limitPct))
			}
		}
	}
	if !totalEquity.IsZero() {
		cashPct := q4(account.Cash.Div(totalEquity).Mul(decimal.NewFromInt(100)))
		if account.Cash.IsNegative() {
			alerts = append(alerts, paperRiskAlert("negative_cash", "critical", "现金余额为负", "账户现金已低于零，需要复核成交与费用。", "", account.Cash, decimal.Zero))
		} else if cashPct.LessThan(decimal.NewFromInt(5)) {
			alerts = append(alerts, paperRiskAlert("low_cash_reserve", "warning", "现金缓冲偏低", "现金占总资产比例低于 5%，后续买入空间有限。", "", cashPct, decimal.NewFromInt(5)))
		}
	}
	if maxDrawdown.GreaterThanOrEqual(decimal.NewFromInt(20)) {
		alerts = append(alerts, paperRiskAlert("drawdown", "critical", "最大回撤过高", "账户权益曲线最大回撤已达到高风险区间。", "", maxDrawdown, decimal.NewFromInt(20)))
	} else if maxDrawdown.GreaterThanOrEqual(decimal.NewFromInt(10)) {
		alerts = append(alerts, paperRiskAlert("drawdown", "warning", "最大回撤偏高", "账户权益曲线最大回撤已进入观察区间。", "", maxDrawdown, decimal.NewFromInt(10)))
	}
	if pendingOrders >= 10 {
		alerts = append(alerts, paperRiskAlert("pending_order_backlog", "warning", "待处理订单积压", "待审批或待执行订单较多，建议先处理队列。", "", decimal.NewFromInt(pendingOrders), decimal.NewFromInt(10)))
	} else if pendingOrders > 0 {
		alerts = append(alerts, paperRiskAlert("pending_orders", "info", "存在待处理订单", "账户仍有待审批或待执行订单。", "", decimal.NewFromInt(pendingOrders), decimal.Zero))
	}
	now := paperNow()
	for _, position := range positions {
		if position.CostAmount.GreaterThan(decimal.Zero) {
			returnPct := q4(position.UnrealizedPNL.Div(position.CostAmount).Mul(decimal.NewFromInt(100)))
			if returnPct.LessThanOrEqual(decimal.NewFromInt(-20)) {
				alerts = append(alerts, paperRiskAlert("position_loss", "critical", "持仓浮亏过大", "该持仓浮亏比例已达到高风险区间。", position.Code, returnPct, decimal.NewFromInt(-20)))
			} else if returnPct.LessThanOrEqual(decimal.NewFromInt(-10)) {
				alerts = append(alerts, paperRiskAlert("position_loss", "warning", "持仓浮亏偏高", "该持仓浮亏比例已进入观察区间。", position.Code, returnPct, decimal.NewFromInt(-10)))
			}
		}
		if !position.UpdatedAt.IsZero() && now.Sub(position.UpdatedAt) > 72*time.Hour {
			hours := decimal.NewFromFloat(now.Sub(position.UpdatedAt).Hours()).Round(2)
			alerts = append(alerts, paperRiskAlert("stale_position_price", "warning", "持仓价格可能过期", "该持仓超过 72 小时未更新估值。", position.Code, hours, decimal.NewFromInt(72)))
		}
	}
	sort.SliceStable(alerts, func(i, j int) bool {
		return paperRiskSeverityRank(fmtString(alerts[i]["severity"])) > paperRiskSeverityRank(fmtString(alerts[j]["severity"]))
	})
	return alerts
}

func paperRiskAlert(key string, severity string, title string, detail string, code string, metric decimal.Decimal, threshold decimal.Decimal) map[string]any {
	var codeValue any
	if code != "" {
		codeValue = code
	}
	return map[string]any{
		"key":       key,
		"severity":  severity,
		"title":     title,
		"detail":    detail,
		"code":      codeValue,
		"metric":    q4(metric),
		"threshold": q4(threshold),
	}
}

func paperRiskSummary(alerts []map[string]any) map[string]any {
	summary := map[string]any{"status": "ok", "max_severity": "ok", "alert_count": len(alerts), "critical_count": 0, "warning_count": 0, "info_count": 0}
	maxSeverity := "ok"
	for _, alert := range alerts {
		severity := fmtString(alert["severity"])
		switch severity {
		case "critical":
			summary["critical_count"] = summary["critical_count"].(int) + 1
		case "warning":
			summary["warning_count"] = summary["warning_count"].(int) + 1
		case "info":
			summary["info_count"] = summary["info_count"].(int) + 1
		}
		if paperRiskSeverityRank(severity) > paperRiskSeverityRank(maxSeverity) {
			maxSeverity = severity
		}
	}
	status := "ok"
	if paperRiskSeverityRank(maxSeverity) >= paperRiskSeverityRank("critical") {
		status = "critical"
	} else if paperRiskSeverityRank(maxSeverity) >= paperRiskSeverityRank("warning") {
		status = "watch"
	}
	summary["status"] = status
	summary["max_severity"] = maxSeverity
	return summary
}

func paperRiskSeverityRank(severity string) int {
	switch severity {
	case "critical":
		return 3
	case "warning":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

func decimalFromMetric(metrics map[string]any, key string) decimal.Decimal {
	switch value := metrics[key].(type) {
	case decimal.Decimal:
		return value
	case int64:
		return decimal.NewFromInt(value)
	case int:
		return decimal.NewFromInt(int64(value))
	case float64:
		return decimal.NewFromFloat(value)
	default:
		return decimal.Zero
	}
}

func int64FromMetric(metrics map[string]any, key string) int64 {
	switch value := metrics[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case decimal.Decimal:
		return value.IntPart()
	default:
		return 0
	}
}

func fmtString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func PaperPositionsPublic(db *gorm.DB, rows []domainpaper.Position) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := map[string]any{"id": row.ID, "account_id": row.AccountID, "code": row.Code, "symbol_name": symbolName(db, row.Code), "quantity": row.Quantity, "avg_cost": row.AvgCost, "cost_amount": row.CostAmount, "last_price": row.LastPrice, "market_value": row.MarketValue, "unrealized_pnl": row.UnrealizedPNL, "realized_pnl": row.RealizedPNL, "updated_at": row.UpdatedAt}
		out = append(out, item)
	}
	return out
}

func PaperOrdersPublic(db *gorm.DB, rows []domainpaper.Order) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	progressByOrderID := paperOrderFillProgress(db, rows)
	for _, row := range rows {
		progress := progressByOrderID[row.ID]
		item := map[string]any{"id": row.ID, "account_id": row.AccountID, "meeting_id": row.MeetingID, "code": row.Code, "symbol_name": symbolName(db, row.Code), "side": row.Side, "quantity": row.Quantity, "status": row.Status, "suggested_price": row.SuggestedPrice, "filled_price": row.FilledPrice, "reason": row.Reason, "submitted_at": row.SubmittedAt, "execute_after": row.ExecuteAfter, "expire_at": row.ExpireAt, "source_meeting_event_id": row.SourceMeetingEventID, "execution_note": row.ExecutionNote, "commission": row.Commission, "stamp_duty": row.StampDuty, "transfer_fee": row.TransferFee, "net_amount": row.NetAmount, "created_at": row.CreatedAt, "filled_at": row.FilledAt}
		item["filled_quantity"] = progress.FilledQuantity
		item["remaining_quantity"] = progress.RemainingQuantity
		item["partial_fill_count"] = progress.PartialFillCount
		item["approval_required"] = row.Status == domainkernel.OrderSuggested
		item["approval_status"] = paperOrderApprovalStatus(row.Status)
		out = append(out, item)
	}
	return out
}

func paperOrderApprovalStatus(status domainkernel.OrderStatus) string {
	switch status {
	case domainkernel.OrderSuggested:
		return "waiting"
	case domainkernel.OrderPending, domainkernel.OrderFilled:
		return "approved"
	case domainkernel.OrderRejected:
		return "rejected"
	case domainkernel.OrderCancelled, domainkernel.OrderExpired:
		return "closed"
	default:
		return "unknown"
	}
}

func PaperFillsPublic(db *gorm.DB, rows []domainpaper.Fill) []map[string]any {
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		item := map[string]any{"id": row.ID, "order_id": row.OrderID, "account_id": row.AccountID, "code": row.Code, "symbol_name": symbolName(db, row.Code), "side": row.Side, "quantity": row.Quantity, "price": row.Price, "gross_amount": row.GrossAmount, "commission": row.Commission, "stamp_duty": row.StampDuty, "transfer_fee": row.TransferFee, "net_amount": row.NetAmount, "realized_pnl": row.RealizedPNL, "filled_at": row.FilledAt}
		out = append(out, item)
	}
	return out
}

func symbolName(db *gorm.DB, code string) *string {
	var symbol persistmodel.MarketSymbol
	if err := db.First(&symbol, "code = ?", code).Error; err == nil && symbol.Name != "" {
		return &symbol.Name
	}
	return nil
}

func countModel(db *gorm.DB, model any, cond string, args ...any) int64 {
	var count int64
	q := db.Model(model)
	if cond != "" {
		q = q.Where(cond, args...)
	}
	q.Count(&count)
	return count
}

type orderFillProgress struct {
	FilledQuantity    int
	RemainingQuantity int
	PartialFillCount  int
	FilledGross       decimal.Decimal
}

func paperOrderFillProgress(db *gorm.DB, rows []domainpaper.Order) map[uint]orderFillProgress {
	out := make(map[uint]orderFillProgress, len(rows))
	orderQuantity := make(map[uint]int, len(rows))
	ids := make([]uint, 0, len(rows))
	for _, row := range rows {
		if row.ID == 0 {
			continue
		}
		ids = append(ids, row.ID)
		orderQuantity[row.ID] = row.Quantity
		out[row.ID] = orderFillProgress{RemainingQuantity: maxInt(row.Quantity, 0)}
	}
	if len(ids) == 0 {
		return out
	}
	var aggregates []struct {
		OrderID          uint
		FilledQuantity   int
		PartialFillCount int
		FilledGross      decimal.Decimal
	}
	if err := db.Model(&persistmodel.PaperFill{}).
		Select("order_id, COALESCE(SUM(quantity), 0) AS filled_quantity, COUNT(*) AS partial_fill_count, COALESCE(SUM(gross_amount), 0) AS filled_gross").
		Where("order_id IN ?", ids).
		Group("order_id").
		Scan(&aggregates).Error; err != nil {
		return out
	}
	for _, aggregate := range aggregates {
		quantity := orderQuantity[aggregate.OrderID]
		remaining := quantity - aggregate.FilledQuantity
		if remaining < 0 {
			remaining = 0
		}
		out[aggregate.OrderID] = orderFillProgress{
			FilledQuantity:    aggregate.FilledQuantity,
			RemainingQuantity: remaining,
			PartialFillCount:  aggregate.PartialFillCount,
			FilledGross:       q4(aggregate.FilledGross),
		}
	}
	return out
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
