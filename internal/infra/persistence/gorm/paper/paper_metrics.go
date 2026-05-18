package paper

import (
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainpaper "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/paper"
	persistmodel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/model"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
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
	return map[string]any{"account_id": account.ID, "initial_cash": account.InitialCash, "latest_equity": metrics["total_equity"], "latest_cash": account.Cash, "latest_market_value": metrics["market_value"], "unrealized_pnl": metrics["unrealized_pnl"], "realized_pnl": metrics["realized_pnl"], "total_return_pct": metrics["total_return_pct"], "max_drawdown_pct": maxDrawdown.Round(4), "win_rate_pct": winRate, "fills_count": fills, "series": series}
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
	for _, row := range rows {
		item := map[string]any{"id": row.ID, "account_id": row.AccountID, "meeting_id": row.MeetingID, "code": row.Code, "symbol_name": symbolName(db, row.Code), "side": row.Side, "quantity": row.Quantity, "status": row.Status, "suggested_price": row.SuggestedPrice, "filled_price": row.FilledPrice, "reason": row.Reason, "submitted_at": row.SubmittedAt, "execute_after": row.ExecuteAfter, "expire_at": row.ExpireAt, "source_meeting_event_id": row.SourceMeetingEventID, "execution_note": row.ExecutionNote, "commission": row.Commission, "stamp_duty": row.StampDuty, "transfer_fee": row.TransferFee, "net_amount": row.NetAmount, "created_at": row.CreatedAt, "filled_at": row.FilledAt}
		out = append(out, item)
	}
	return out
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
