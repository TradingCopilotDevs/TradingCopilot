package paper

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainpaper "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/paper"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/marketdata/ashare"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func EnsureDefaultPaperSetup(db *gorm.DB) (*domainpaper.RiskConfig, []domainpaper.Account, error) {
	cfg, err := firstRiskConfigByName(db, DefaultPaperRiskConfigName)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		legacy, legacyErr := firstLegacyPaperRiskConfig(db)
		if legacyErr == nil {
			cfg = *legacy
			cfg.Name = DefaultPaperRiskConfigName
			if err := saveRiskConfigRecord(db, &cfg); err != nil {
				return nil, nil, err
			}
		} else if errors.Is(legacyErr, gorm.ErrRecordNotFound) {
			cfg = DefaultPaperRiskConfig()
			if err := createRiskConfigRecord(db, &cfg); err != nil {
				return nil, nil, err
			}
		} else {
			return nil, nil, legacyErr
		}
	} else if err != nil {
		return nil, nil, err
	}
	if err := mergeLegacyPaperRiskConfigs(db, cfg.ID); err != nil {
		return nil, nil, err
	}
	changed := false
	if !cfg.Enabled {
		cfg.Enabled = true
		changed = true
	}
	if cfg.AllowShort || cfg.AllowMargin || !cfg.AllowSHMain || !cfg.AllowSZMain || !cfg.AllowETFLOF || cfg.AllowBJ || cfg.AllowSTAR || cfg.AllowChiNext {
		cfg.AllowShort, cfg.AllowMargin = false, false
		cfg.AllowSHMain, cfg.AllowSZMain, cfg.AllowETFLOF = true, true, true
		cfg.AllowBJ, cfg.AllowSTAR, cfg.AllowChiNext = false, false, false
		changed = true
	}
	if !cfg.CommissionRate.Equal(decimal.RequireFromString("0.0001")) {
		cfg.CommissionRate = decimal.RequireFromString("0.0001")
		changed = true
	}
	if !cfg.MinCommission.Equal(decimal.RequireFromString("5.00")) {
		cfg.MinCommission = decimal.RequireFromString("5.00")
		changed = true
	}
	if changed {
		if err := saveRiskConfigRecord(db, &cfg); err != nil {
			return nil, nil, err
		}
	}
	var accountRows []persistmodel.PaperAccount
	if err := db.Order("id").Find(&accountRows).Error; err != nil {
		return nil, nil, err
	}
	accounts := paperAccountsModelToDomain(accountRows)
	if len(accounts) == 0 {
		account := domainpaper.Account{Name: DefaultPaperAccountName, InitialCash: cfg.InitialCash, Cash: cfg.InitialCash, RiskConfigID: &cfg.ID, Active: true}
		if err := gormrepo.NewPaperRepository(db).SaveAccount(dbContext(db), &account); err != nil {
			return nil, nil, err
		}
		accounts = []domainpaper.Account{account}
	} else {
		for i := range accounts {
			accountChanged := false
			if isLegacyPaperAccountName(accounts[i].Name) && accounts[i].RiskConfigID != nil && *accounts[i].RiskConfigID == cfg.ID {
				accounts[i].Name = DefaultPaperAccountName
				accountChanged = true
			}
			if accountChanged {
				if err := savePaperAccountRecord(db, &accounts[i]); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	return &cfg, accounts, nil
}

func isLegacyPaperAccountName(name string) bool {
	for _, legacy := range legacyPaperAccountNames {
		if name == legacy {
			return true
		}
	}
	return false
}

func firstLegacyPaperRiskConfig(db *gorm.DB) (*domainpaper.RiskConfig, error) {
	var row persistmodel.RiskConfig
	err := db.Where("name IN ?", legacyPaperRiskConfigNames).Order("id").First(&row).Error
	cfg := riskConfigModelToDomain(row)
	return &cfg, err
}

func mergeLegacyPaperRiskConfigs(db *gorm.DB, canonicalID uint) error {
	var legacy []persistmodel.RiskConfig
	if err := db.Where("name IN ? AND id <> ?", legacyPaperRiskConfigNames, canonicalID).Find(&legacy).Error; err != nil {
		return err
	}
	repo := gormrepo.NewPaperRepository(db)
	for _, item := range legacy {
		if err := repo.ReassignRiskConfigAccounts(dbContext(db), item.ID, canonicalID); err != nil {
			return err
		}
		if err := repo.DeleteRiskConfig(dbContext(db), item.ID); err != nil {
			return err
		}
	}
	return nil
}

func CreateOrderFromSpec(db *gorm.DB, spec map[string]any) (*domainpaper.Order, error) {
	account, err := resolveAccount(db, spec["account_id"])
	if err != nil {
		return nil, err
	}
	side := domainkernel.OrderSide(strings.ToLower(stringFromPayload(spec["side"])))
	if side == "" {
		side = domainkernel.OrderBuy
	}
	code, err := ashare.EnsureCode(stringFromPayload(spec["code"]))
	if err != nil {
		return nil, err
	}
	reason := optionalString(spec["reason"])
	price := decimalFromPayload(spec["suggested_price"])
	targetCode, reason := resolveTargetCode(db, account, code, side, reason)
	if targetCode != code && price.IsZero() {
		price = latestQuotePrice(db, targetCode)
	}
	if price.IsZero() {
		price = latestQuotePrice(db, targetCode)
	}
	quantity := intFromPayload(spec["quantity"])
	positionPct := inferPositionPct(spec)
	minPct, maxPct := positionPctBounds(spec)
	if quantity <= 0 && side == domainkernel.OrderBuy && !positionPct.IsZero() {
		quantity, err = resolveQuantityFromPct(db, account, targetCode, price, positionPct)
		if err != nil {
			order := rejectedOrder(account.ID, targetCode, side, reason, err.Error())
			order.SuggestedPrice = price
			_ = savePaperOrderRecord(db, order)
			return order, nil
		}
	} else if quantity <= 0 && side == domainkernel.OrderSell {
		order := rejectedOrder(account.ID, targetCode, side, reason, "sell orders require positive explicit quantity")
		order.SuggestedPrice = price
		_ = savePaperOrderRecord(db, order)
		return order, nil
	}
	if side == domainkernel.OrderBuy && !price.IsZero() {
		maxQuantity, minQuantity, notes := maxBuyQuantityForConstraints(db, account, targetCode, price, minPct, maxPct)
		if maxQuantity > 0 && quantity > maxQuantity {
			old := quantity
			quantity = maxQuantity
			reason = appendReason(reason, fmt.Sprintf("auto-resized from %d to %d shares", old, quantity))
		}
		if quantity <= 0 && minQuantity > 0 {
			quantity = minQuantity
			reason = appendReason(reason, "auto-raised to 100 shares to satisfy the A-share minimum lot")
		}
		for _, note := range notes {
			reason = appendReason(reason, note)
		}
	}
	order := domainpaper.Order{
		AccountID:            account.ID,
		Code:                 targetCode,
		Side:                 side,
		Quantity:             quantity,
		Status:               domainkernel.OrderSuggested,
		SuggestedPrice:       price,
		Reason:               reason,
		MeetingID:            optionalUintPtr(spec["meeting_id"]),
		SourceMeetingEventID: optionalUintPtr(spec["source_meeting_event_id"]),
		ExecuteAfter:         optionalTimePtr(spec["execute_after"]),
		ExpireAt:             optionalTimePtr(spec["expire_at"]),
	}
	if err := savePaperOrderRecord(db, &order); err != nil {
		return nil, err
	}
	if err := SubmitOrder(db, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

func SubmitOrder(db *gorm.DB, order *domainpaper.Order) error {
	code, err := ashare.EnsureCode(order.Code)
	if err != nil {
		order.Status = domainkernel.OrderRejected
		order.Reason = strPtr("only China-listed 6-digit stocks, ETFs, and LOFs are supported")
		return savePaperOrderRecord(db, order)
	}
	order.Code = code
	order.Reason = cleanOrderReason(order.Reason)
	var accountRow persistmodel.PaperAccount
	if err := db.First(&accountRow, order.AccountID).Error; err != nil {
		order.Status = domainkernel.OrderRejected
		order.Reason = strPtr("paper account not found")
		return savePaperOrderRecord(db, order)
	}
	account := paperAccountModelToDomain(accountRow)
	if !account.Active {
		order.Status = domainkernel.OrderRejected
		order.Reason = strPtr("paper account is inactive")
		return savePaperOrderRecord(db, order)
	}
	cfg, _ := RiskEnabled(db, account)
	if cfg == nil {
		order.Status = domainkernel.OrderRejected
		order.Reason = strPtr("risk config disabled")
		return savePaperOrderRecord(db, order)
	}
	if _, err := ashare.ValidateAgainstRiskConfig(*cfg, order.Code); err != nil {
		order.Status = domainkernel.OrderRejected
		order.Reason = strPtr(err.Error())
		return savePaperOrderRecord(db, order)
	}
	if order.Quantity <= 0 {
		order.Status = domainkernel.OrderRejected
		order.Reason = strPtr("paper order quantity must be positive")
		return savePaperOrderRecord(db, order)
	}
	if order.SuggestedPrice.IsZero() {
		order.SuggestedPrice = latestQuotePrice(db, order.Code)
	}
	notional := q2(order.SuggestedPrice.Mul(decimal.NewFromInt(int64(order.Quantity))))
	metrics := ComputeAccountMetrics(db, account, false)
	if !order.SuggestedPrice.IsZero() && !cfg.MaxOrderPct.IsZero() {
		maxOrderValue := q2(metrics["total_equity"].(decimal.Decimal).Mul(cfg.MaxOrderPct))
		if notional.GreaterThan(maxOrderValue) {
			order.Status = domainkernel.OrderRejected
			order.Reason = strPtr("order value exceeds max_order_pct limit " + cfg.MaxOrderPct.String())
			return savePaperOrderRecord(db, order)
		}
	}
	if order.Side == domainkernel.OrderBuy {
		currentValue := currentPositionValue(db, account.ID, order.Code, order.SuggestedPrice)
		maxPositionValue := q2(metrics["total_equity"].(decimal.Decimal).Mul(cfg.MaxPositionPct))
		if !cfg.MaxPositionPct.IsZero() && currentValue.Add(notional).GreaterThan(maxPositionValue) {
			order.Status = domainkernel.OrderRejected
			order.Reason = strPtr("post-trade position exceeds max_position_pct limit " + cfg.MaxPositionPct.String())
			return savePaperOrderRecord(db, order)
		}
		if account.Cash.LessThan(notional) {
			order.Status = domainkernel.OrderRejected
			order.Reason = strPtr("insufficient cash for order estimate")
			return savePaperOrderRecord(db, order)
		}
	} else {
		var pos persistmodel.PaperPosition
		if err := db.Where("account_id = ? AND code = ?", account.ID, order.Code).First(&pos).Error; err != nil || pos.Quantity < order.Quantity {
			order.Status = domainkernel.OrderRejected
			order.Reason = strPtr("insufficient position")
			return savePaperOrderRecord(db, order)
		}
	}
	now := paperNow()
	order.Status = domainkernel.OrderPending
	order.SubmittedAt = &now
	if order.ExecuteAfter == nil {
		next := now
		if !IsTradingTime(now) {
			next = NextTradingSessionStart(now)
		}
		order.ExecuteAfter = &next
	}
	if IsTradingTime(*order.ExecuteAfter) {
		order.ExecutionNote = strPtr("Submitted during market hours.")
	} else {
		order.ExecutionNote = strPtr("Submitted after market close; scheduled for " + order.ExecuteAfter.Format(time.RFC3339) + ".")
	}
	return savePaperOrderRecord(db, order)
}

func ReconcileUnfinishedOrders(db *gorm.DB) (map[string][]uint, error) {
	_, _, _ = EnsureDefaultPaperSetup(db)
	result := map[string][]uint{"repaired": {}, "cancelled": {}}
	var orderRows []persistmodel.PaperOrder
	if err := db.Where("status IN ?", []domainkernel.OrderStatus{domainkernel.OrderSuggested, domainkernel.OrderPending}).Order("id").Find(&orderRows).Error; err != nil {
		return result, err
	}
	orders := paperOrdersModelToDomain(orderRows)
	for i := range orders {
		order := &orders[i]
		if _, err := ashare.EnsureCode(order.Code); err != nil {
			order.Status = domainkernel.OrderCancelled
			order.ExecutionNote = strPtr("Cancelled because the code is not a valid A-share symbol.")
			order.Reason = cleanOrderReason(order.Reason)
			_ = savePaperOrderRecord(db, order)
			result["cancelled"] = append(result["cancelled"], order.ID)
			continue
		}
		var accountRow persistmodel.PaperAccount
		cfgOk := false
		if db.First(&accountRow, order.AccountID).Error == nil {
			account := paperAccountModelToDomain(accountRow)
			if cfg, _ := RiskEnabled(db, account); cfg != nil {
				cfgOk = true
				if _, err := ashare.ValidateAgainstRiskConfig(*cfg, order.Code); err != nil {
					order.Status = domainkernel.OrderCancelled
					order.ExecutionNote = strPtr("Cancelled by risk configuration: " + err.Error())
					order.Reason = cleanOrderReason(order.Reason)
					_ = savePaperOrderRecord(db, order)
					result["cancelled"] = append(result["cancelled"], order.ID)
					continue
				}
			}
		}
		if order.Status == domainkernel.OrderSuggested || !cfgOk {
			_ = SubmitOrder(db, order)
			if order.Status == domainkernel.OrderPending {
				result["repaired"] = append(result["repaired"], order.ID)
			}
		}
	}
	return result, nil
}

func CancelOrder(db *gorm.DB, order *domainpaper.Order) error {
	if order.Status != domainkernel.OrderPending && order.Status != domainkernel.OrderSuggested {
		return errors.New("only pending or suggested orders can be cancelled")
	}
	order.Status = domainkernel.OrderCancelled
	order.ExecutionNote = strPtr("Cancelled by user.")
	return savePaperOrderRecord(db, order)
}

func DeleteOrderRecord(db *gorm.DB, order *domainpaper.Order) error {
	if order.Status != domainkernel.OrderCancelled && order.Status != domainkernel.OrderRejected {
		return errors.New("only cancelled or rejected orders can be deleted")
	}
	return gormrepo.NewPaperRepository(db).DeleteOrder(dbContext(db), order.ID)
}

func FillOrder(db *gorm.DB, order *domainpaper.Order, price decimal.Decimal) error {
	repo := gormrepo.NewPaperRepository(db)
	ctx := dbContext(db)
	account, found, err := repo.FindAccount(ctx, order.AccountID)
	if err != nil || !found {
		order.Status = domainkernel.OrderRejected
		order.Reason = strPtr("paper account not found")
		return repo.SaveOrder(ctx, order)
	}
	var cfg *domainpaper.RiskConfig
	if account.RiskConfigID != nil {
		if foundCfg, ok, err := repo.FindRiskConfig(ctx, *account.RiskConfigID); err == nil && ok && foundCfg.Enabled {
			cfg = foundCfg
		} else if err != nil {
			return err
		}
	}
	if cfg == nil {
		order.Status = domainkernel.OrderRejected
		order.Reason = strPtr("risk config disabled")
		return repo.SaveOrder(ctx, order)
	}
	if _, err := ashare.ValidateAgainstRiskConfig(*cfg, order.Code); err != nil {
		order.Status = domainkernel.OrderRejected
		order.Reason = strPtr(err.Error())
		return repo.SaveOrder(ctx, order)
	}
	price = q4(price)
	gross := q4(price.Mul(decimal.NewFromInt(int64(order.Quantity))))
	fees := calculateFees(order.Side, gross, *cfg)
	totalFees := fees["commission"].Add(fees["transfer_fee"]).Add(fees["stamp_duty"])
	pos, found, err := repo.FindPosition(ctx, order.AccountID, order.Code)
	if err != nil {
		return err
	}
	if !found {
		pos = &domainpaper.Position{AccountID: order.AccountID, Code: order.Code}
		if err := repo.CreatePosition(ctx, pos); err != nil {
			return err
		}
	}
	realized := decimal.Zero
	if order.Side == domainkernel.OrderBuy {
		needed := q2(gross.Add(totalFees))
		if account.Cash.LessThan(needed) {
			order.Status = domainkernel.OrderRejected
			order.Reason = strPtr("insufficient cash")
			return repo.SaveOrder(ctx, order)
		}
		account.Cash = q2(account.Cash.Sub(needed))
		pos.CostAmount = q4(pos.CostAmount.Add(gross).Add(totalFees))
		pos.Quantity += order.Quantity
		pos.AvgCost = q4(pos.CostAmount.Div(decimal.NewFromInt(int64(pos.Quantity))))
		order.NetAmount = q4(gross.Add(totalFees))
	} else {
		if pos.Quantity < order.Quantity {
			order.Status = domainkernel.OrderRejected
			order.Reason = strPtr("insufficient position")
			return repo.SaveOrder(ctx, order)
		}
		costReleased := q4(pos.AvgCost.Mul(decimal.NewFromInt(int64(order.Quantity))))
		realized = q4(gross.Sub(totalFees).Sub(costReleased))
		pos.Quantity -= order.Quantity
		pos.CostAmount = q4(pos.CostAmount.Sub(costReleased))
		pos.RealizedPNL = q4(pos.RealizedPNL.Add(realized))
		if pos.Quantity > 0 {
			pos.AvgCost = q4(pos.CostAmount.Div(decimal.NewFromInt(int64(pos.Quantity))))
		} else {
			pos.AvgCost = decimal.Zero
			pos.CostAmount = decimal.Zero
		}
		account.Cash = q2(account.Cash.Add(gross).Sub(totalFees))
		order.NetAmount = q4(gross.Sub(totalFees))
	}
	now := paperNow()
	pos.LastPrice = price
	pos.MarketValue = q4(price.Mul(decimal.NewFromInt(int64(pos.Quantity))))
	pos.UnrealizedPNL = q4(pos.MarketValue.Sub(pos.CostAmount))
	pos.UpdatedAt = now
	account.UpdatedAt = now
	order.Status = domainkernel.OrderFilled
	order.FilledPrice = price
	order.FilledAt = &now
	order.ExecutionNote = strPtr("Filled manually.")
	order.Commission = fees["commission"]
	order.TransferFee = fees["transfer_fee"]
	order.StampDuty = fees["stamp_duty"]
	order.NetAmount = q4(order.NetAmount)
	if err := repo.SaveAccount(ctx, account); err != nil {
		return err
	}
	if pos.Quantity == 0 {
		if err := repo.DeletePosition(ctx, pos.ID); err != nil {
			return err
		}
	} else if err := repo.SavePosition(ctx, pos); err != nil {
		return err
	}
	if err := repo.SaveOrder(ctx, order); err != nil {
		return err
	}
	return repo.CreateFill(ctx, &domainpaper.Fill{
		OrderID:     order.ID,
		AccountID:   order.AccountID,
		Code:        order.Code,
		Side:        order.Side,
		Quantity:    order.Quantity,
		Price:       price,
		GrossAmount: gross,
		Commission:  fees["commission"],
		StampDuty:   fees["stamp_duty"],
		TransferFee: fees["transfer_fee"],
		NetAmount:   order.NetAmount,
		RealizedPNL: realized,
		FilledAt:    now,
	})
}

func DuePendingOrders(db *gorm.DB, limit int) ([]domainpaper.Order, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []persistmodel.PaperOrder
	err := db.Where("status = ? AND execute_after IS NOT NULL AND execute_after <= ?", domainkernel.OrderPending, paperNow()).Order("execute_after, id").Limit(limit).Find(&rows).Error
	return paperOrdersModelToDomain(rows), err
}

func ExecuteDueOrders(db *gorm.DB, limit int) ([]domainpaper.Order, error) {
	now := paperNow()
	if !IsTradingTime(now) {
		return nil, nil
	}
	orders, err := DuePendingOrders(db, limit)
	if err != nil {
		return nil, err
	}
	for i := range orders {
		price := orders[i].SuggestedPrice
		if price.IsZero() {
			price = latestQuotePrice(db, orders[i].Code)
		}
		if price.IsZero() {
			orders[i].ExecutionNote = strPtr("No executable price is available.")
			_ = savePaperOrderRecord(db, &orders[i])
			continue
		}
		_ = FillOrder(db, &orders[i], price)
	}
	return orders, nil
}

func RunPaperMaintenanceOnce(db *gorm.DB) map[string]any {
	executed, _ := ExecuteDueOrders(db, 50)
	sampled := 0
	now := paperNow()
	isTradingTime := IsTradingTime(now)
	if isTradingTime {
		sampled = int(countModel(db, &persistmodel.PaperAccount{}, "active = ?", true))
		var accountRows []persistmodel.PaperAccount
		db.Where("active = ?", true).Find(&accountRows)
		accounts := paperAccountsModelToDomain(accountRows)
		for _, account := range accounts {
			CaptureAccountSnapshot(db, account)
		}
	}
	return map[string]any{"executed_orders": len(executed), "sampled_accounts": sampled, "is_trading_time": isTradingTime}
}

func RunPaperMaintenanceLoop(ctx context.Context, db *gorm.DB, interval time.Duration) {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	for {
		RunPaperMaintenanceOnce(db)
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
