package paper

import (
	"context"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainmarket "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/market"
	domainpaper "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/paper"
	"strings"
	"testing"
	"time"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/connect"
	persistmodel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/model"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestIsTradingTime(t *testing.T) {
	if !IsTradingTime(time.Date(2026, 5, 6, 10, 15, 0, 0, appTZ)) {
		t.Fatal("expected trading time")
	}
	if IsTradingTime(time.Date(2026, 5, 6, 12, 0, 0, 0, appTZ)) {
		t.Fatal("expected lunch break")
	}
}

func TestNextTradingSessionStart(t *testing.T) {
	sameDay := NextTradingSessionStart(time.Date(2026, 5, 6, 12, 0, 0, 0, appTZ))
	if !sameDay.Equal(time.Date(2026, 5, 6, 13, 0, 0, 0, appTZ)) {
		t.Fatalf("same-day session mismatch: %s", sameDay)
	}
	nextDay := NextTradingSessionStart(time.Date(2026, 5, 6, 18, 0, 0, 0, appTZ))
	if !nextDay.Equal(time.Date(2026, 5, 7, 9, 30, 0, 0, appTZ)) {
		t.Fatalf("next-day session mismatch: %s", nextDay)
	}
}

func TestCalculateBuyFees(t *testing.T) {
	cfg := domainpaper.RiskConfig{CommissionRate: decimal.NewFromFloat(0.00005), MinCommission: decimal.NewFromInt(5), StampDutyRate: decimal.NewFromFloat(0.001), TransferFeeRate: decimal.NewFromFloat(0.00001)}
	fees := calculateFees(domainkernel.OrderBuy, decimal.NewFromInt(10000), cfg)
	if !fees["commission"].Equal(decimal.NewFromInt(5)) {
		t.Fatalf("commission mismatch: %s", fees["commission"])
	}
	if !fees["transfer_fee"].Equal(decimal.RequireFromString("0.1000")) {
		t.Fatalf("transfer fee mismatch: %s", fees["transfer_fee"])
	}
	if !fees["stamp_duty"].IsZero() {
		t.Fatalf("stamp duty mismatch: %s", fees["stamp_duty"])
	}
}

func TestFillOrderGeneratesFeesAndUpdatesAccountPosition(t *testing.T) {
	db := newPaperTestDB(t)
	cfg := createRiskConfig(t, db, "default", decimal.NewFromInt(1), decimal.NewFromInt(1), true)
	account := createPaperAccount(t, db, "acct", decimal.NewFromInt(100000), &cfg.ID)
	order := domainpaper.Order{AccountID: account.ID, Code: "600519", Side: domainkernel.OrderBuy, Quantity: 100, SuggestedPrice: decimal.NewFromInt(100)}
	must(t, db.Create(&order).Error)
	must(t, SubmitOrder(db, &order))
	must(t, FillOrder(db, &order, decimal.NewFromInt(100)))

	if order.Status != domainkernel.OrderFilled {
		t.Fatalf("status mismatch: %s", order.Status)
	}
	assertDecimalEqual(t, order.Commission, "5.0000")
	assertDecimalEqual(t, order.TransferFee, "0.1000")
	assertDecimalEqual(t, order.StampDuty, "0")

	var refreshed domainpaper.Account
	must(t, db.First(&refreshed, account.ID).Error)
	metrics := ComputeAccountMetrics(db, refreshed, false)
	assertDecimalEqual(t, metrics["market_value"].(decimal.Decimal), "10000.00")
	assertDecimalEqual(t, refreshed.Cash, "89994.90")
}

func TestSellOrderRealizesPNL(t *testing.T) {
	db := newPaperTestDB(t)
	cfg := createRiskConfig(t, db, "default", decimal.NewFromInt(1), decimal.NewFromInt(1), true)
	account := createPaperAccount(t, db, "acct", decimal.NewFromInt(100000), &cfg.ID)

	buyOrder, err := CreateOrderFromSpec(db, map[string]any{"account_id": account.ID, "code": "600519", "side": "buy", "quantity": 100, "suggested_price": 100})
	must(t, err)
	if buyOrder.Status != domainkernel.OrderPending {
		t.Fatalf("buy status mismatch: %s", buyOrder.Status)
	}
	must(t, FillOrder(db, buyOrder, decimal.NewFromInt(100)))
	sellOrder, err := CreateOrderFromSpec(db, map[string]any{"account_id": account.ID, "code": "600519", "side": "sell", "quantity": 100, "suggested_price": 110})
	must(t, err)
	if sellOrder.Status != domainkernel.OrderPending {
		t.Fatalf("sell status mismatch: %s", sellOrder.Status)
	}
	must(t, FillOrder(db, sellOrder, decimal.NewFromInt(110)))
	if sellOrder.Status != domainkernel.OrderFilled {
		t.Fatalf("sell filled status mismatch: %s", sellOrder.Status)
	}

	var refreshed domainpaper.Account
	must(t, db.First(&refreshed, account.ID).Error)
	metrics := ComputeAccountMetrics(db, refreshed, false)
	if !metrics["realized_pnl"].(decimal.Decimal).GreaterThan(decimal.Zero) {
		t.Fatalf("expected positive realized pnl, got %s", metrics["realized_pnl"])
	}
	if !refreshed.Cash.GreaterThan(decimal.NewFromInt(100000)) {
		t.Fatalf("expected cash above initial cash, got %s", refreshed.Cash)
	}
}

func TestDefaultRiskConfigEnablesSHSZMainAndETFLOF(t *testing.T) {
	db := newPaperTestDB(t)
	cfg, accounts, err := EnsureDefaultPaperSetup(db)
	must(t, err)
	if cfg.Name != "默认A股模拟盘风控" {
		t.Fatalf("default risk config name = %q", cfg.Name)
	}
	if len(accounts) != 1 || accounts[0].Name != "默认模拟账户" {
		t.Fatalf("default account mismatch: %+v", accounts)
	}
	if !cfg.AllowSHMain || !cfg.AllowSZMain || !cfg.AllowETFLOF {
		t.Fatalf("expected SH/SZ main and ETF/LOF enabled: %+v", cfg)
	}
	if cfg.AllowBJ || cfg.AllowSTAR || cfg.AllowChiNext {
		t.Fatalf("expected BJ/STAR/ChiNext disabled: %+v", cfg)
	}
	if !cfg.InitialCash.Equal(decimal.NewFromInt(100000)) ||
		!cfg.MaxPositionPct.Equal(decimal.RequireFromString("0.30")) ||
		!cfg.MaxOrderPct.Equal(decimal.RequireFromString("0.20")) ||
		!cfg.CommissionRate.Equal(decimal.RequireFromString("0.0001")) ||
		!cfg.MinCommission.Equal(decimal.RequireFromString("5.00")) ||
		!cfg.StampDutyRate.Equal(decimal.RequireFromString("0.001")) ||
		!cfg.TransferFeeRate.Equal(decimal.RequireFromString("0.00001")) ||
		!cfg.Enabled {
		t.Fatalf("default risk config mismatch: %+v", cfg)
	}
}

func TestDefaultRiskConfigRepairsPythonManagedDefaults(t *testing.T) {
	db := newPaperTestDB(t)
	cfg := domainpaper.RiskConfig{
		Name:            DefaultPaperRiskConfigName,
		InitialCash:     decimal.NewFromInt(100000),
		MaxPositionPct:  decimal.RequireFromString("0.30"),
		MaxOrderPct:     decimal.RequireFromString("0.20"),
		AllowShort:      true,
		AllowMargin:     true,
		AllowBJ:         true,
		AllowSTAR:       true,
		AllowChiNext:    true,
		CommissionRate:  decimal.RequireFromString("0.00005"),
		MinCommission:   decimal.Zero,
		StampDutyRate:   decimal.RequireFromString("0.001"),
		TransferFeeRate: decimal.RequireFromString("0.00001"),
		Enabled:         false,
	}
	must(t, createRiskConfigRecord(db, &cfg))

	repaired, _, err := EnsureDefaultPaperSetup(db)
	must(t, err)
	if repaired.AllowShort || repaired.AllowMargin || repaired.AllowBJ || repaired.AllowSTAR || repaired.AllowChiNext {
		t.Fatalf("expected disabled risk flags repaired: %+v", repaired)
	}
	if !repaired.AllowSHMain || !repaired.AllowSZMain || !repaired.AllowETFLOF || !repaired.Enabled {
		t.Fatalf("expected enabled board flags repaired: %+v", repaired)
	}
	if !repaired.CommissionRate.Equal(decimal.RequireFromString("0.0001")) || !repaired.MinCommission.Equal(decimal.RequireFromString("5.00")) {
		t.Fatalf("expected Python default fee fields repaired: %+v", repaired)
	}
}

func TestEnsureDefaultPaperSetupRenamesLegacyDefaultRiskConfig(t *testing.T) {
	db := newPaperTestDB(t)
	legacy := domainpaper.RiskConfig{
		Name:            "default",
		InitialCash:     decimal.NewFromInt(100000),
		MaxPositionPct:  decimal.RequireFromString("0.30"),
		MaxOrderPct:     decimal.RequireFromString("0.20"),
		AllowSHMain:     true,
		AllowSZMain:     true,
		AllowETFLOF:     true,
		CommissionRate:  decimal.RequireFromString("0.0001"),
		MinCommission:   decimal.RequireFromString("5.00"),
		StampDutyRate:   decimal.RequireFromString("0.001"),
		TransferFeeRate: decimal.RequireFromString("0.00001"),
		Enabled:         true,
	}
	must(t, createRiskConfigRecord(db, &legacy))
	account := domainpaper.Account{Name: "legacy-account", InitialCash: legacy.InitialCash, Cash: legacy.InitialCash, RiskConfigID: &legacy.ID, Active: true}
	must(t, db.Create(&account).Error)

	cfg, accounts, err := EnsureDefaultPaperSetup(db)
	must(t, err)
	if cfg.ID != legacy.ID || cfg.Name != DefaultPaperRiskConfigName {
		t.Fatalf("expected legacy default renamed in place, got %+v", cfg)
	}
	if accounts[0].RiskConfigID == nil || *accounts[0].RiskConfigID != cfg.ID {
		t.Fatalf("expected account kept on renamed config, got %+v", accounts[0])
	}
	if countModel(db, &persistmodel.RiskConfig{}, "name = ?", "default") != 0 {
		t.Fatal("expected legacy default name removed")
	}
}

func TestEnsureDefaultPaperSetupRepairsLegacyDefaultAccountName(t *testing.T) {
	db := newPaperTestDB(t)
	cfg := DefaultPaperRiskConfig()
	must(t, createRiskConfigRecord(db, &cfg))
	account := domainpaper.Account{Name: "Default paper account", InitialCash: cfg.InitialCash, Cash: cfg.InitialCash, RiskConfigID: &cfg.ID, Active: true}
	must(t, db.Create(&account).Error)

	_, accounts, err := EnsureDefaultPaperSetup(db)
	must(t, err)
	if len(accounts) != 1 || accounts[0].Name != DefaultPaperAccountName {
		t.Fatalf("expected legacy default account name repaired, got %+v", accounts)
	}
}

func TestEnsureDefaultPaperSetupDoesNotAutoBindUnconfiguredAccount(t *testing.T) {
	db := newPaperTestDB(t)
	cfg := DefaultPaperRiskConfig()
	must(t, createRiskConfigRecord(db, &cfg))
	account := domainpaper.Account{Name: "manual-unconfigured", InitialCash: cfg.InitialCash, Cash: cfg.InitialCash, Active: true}
	must(t, db.Create(&account).Error)

	_, accounts, err := EnsureDefaultPaperSetup(db)
	must(t, err)
	if len(accounts) != 1 || accounts[0].RiskConfigID != nil {
		t.Fatalf("expected existing unconfigured account to remain unbound, got %+v", accounts)
	}
}

func TestEnsureDefaultPaperSetupMergesDuplicateLegacyDefaultRiskConfig(t *testing.T) {
	db := newPaperTestDB(t)
	canonical := DefaultPaperRiskConfig()
	must(t, createRiskConfigRecord(db, &canonical))
	legacy := DefaultPaperRiskConfig()
	legacy.Name = "default"
	must(t, createRiskConfigRecord(db, &legacy))
	account := domainpaper.Account{Name: "legacy-bound-account", InitialCash: legacy.InitialCash, Cash: legacy.InitialCash, RiskConfigID: &legacy.ID, Active: true}
	must(t, db.Create(&account).Error)

	cfg, accounts, err := EnsureDefaultPaperSetup(db)
	must(t, err)
	if cfg.ID != canonical.ID {
		t.Fatalf("expected canonical config retained, got %+v", cfg)
	}
	if countModel(db, &persistmodel.RiskConfig{}, "id = ?", legacy.ID) != 0 {
		t.Fatal("expected duplicate legacy config deleted")
	}
	if accounts[0].RiskConfigID == nil || *accounts[0].RiskConfigID != canonical.ID {
		t.Fatalf("expected account rebound to canonical config, got %+v", accounts[0])
	}
}

func TestCreateAccountRecordUsesPythonDefaultName(t *testing.T) {
	db := newPaperTestDB(t)
	account, err := CreateAccountRecord(db, map[string]any{"cash": 100000})
	must(t, err)
	if account.Name != "默认模拟账户" {
		t.Fatalf("default account name = %q", account.Name)
	}
}

func TestSubmitOrderAllowsETFLOFWhenEnabled(t *testing.T) {
	db := newPaperTestDB(t)
	cfg, accounts, err := EnsureDefaultPaperSetup(db)
	must(t, err)
	order, err := CreateOrderFromSpec(db, map[string]any{"account_id": accounts[0].ID, "code": "510300", "side": "buy", "quantity": 100, "suggested_price": 4})
	must(t, err)
	if order.Status != domainkernel.OrderPending {
		t.Fatalf("status mismatch: %s reason=%v", order.Status, order.Reason)
	}
	if order.Code != "510300" || !cfg.AllowETFLOF {
		t.Fatalf("ETF/LOF compatibility mismatch: code=%s allow=%v", order.Code, cfg.AllowETFLOF)
	}
}

func TestSubmitOrderSubstitutesETFWhenStockBoardDisabled(t *testing.T) {
	db := newPaperTestDB(t)
	_, accounts, err := EnsureDefaultPaperSetup(db)
	must(t, err)
	db.Create(&domainmarket.RealtimeQuote{Code: "588000", QuoteTime: time.Now(), Price: decimal.RequireFromString("0.9000")})

	order, err := CreateOrderFromSpec(db, map[string]any{"account_id": accounts[0].ID, "code": "688981", "side": "buy", "quantity": 100, "suggested_price": 30})
	must(t, err)
	if order.Status != domainkernel.OrderPending {
		t.Fatalf("status mismatch: %s reason=%v", order.Status, order.Reason)
	}
	if order.Code != "588000" {
		t.Fatalf("expected STAR substitute 588000, got %s", order.Code)
	}
	if order.Reason == nil || !contains(*order.Reason, "substituted with 588000") {
		t.Fatalf("missing substitution reason: %v", order.Reason)
	}
	if !contains(*order.Reason, "科创50ETF") {
		t.Fatalf("substitution reason should use readable ETF name: %v", order.Reason)
	}
}

func TestSubmitOrderRejectsETFLOFWhenScopeDisabled(t *testing.T) {
	db := newPaperTestDB(t)
	cfg := createRiskConfig(t, db, "no-etf", decimal.NewFromInt(1), decimal.NewFromInt(1), false)
	must(t, db.Model(cfg).Update("allow_etf_lof", false).Error)
	account := createPaperAccount(t, db, "acct-etf-disabled", decimal.NewFromInt(100000), &cfg.ID)

	order, err := CreateOrderFromSpec(db, map[string]any{"account_id": account.ID, "code": "510300", "side": "buy", "quantity": 100, "suggested_price": 4})
	must(t, err)
	if order.Status != domainkernel.OrderRejected {
		t.Fatalf("status mismatch: %s", order.Status)
	}
	if order.Reason == nil || !contains(*order.Reason, "Exchange-traded ETF/LOF") {
		t.Fatalf("missing ETF/LOF rejection reason: %v", order.Reason)
	}
}

func TestSubmitOrderSizesQuantityFromPositionPct(t *testing.T) {
	db := newPaperTestDB(t)
	_, accounts, err := EnsureDefaultPaperSetup(db)
	must(t, err)
	db.Create(&domainmarket.RealtimeQuote{Code: "600519", QuoteTime: time.Now(), Price: decimal.NewFromInt(10)})

	order, err := CreateOrderFromSpec(db, map[string]any{"account_id": accounts[0].ID, "code": "600519", "side": "buy", "position_pct": 0.05})
	must(t, err)
	if order.Status != domainkernel.OrderPending || order.Quantity != 500 {
		t.Fatalf("expected pending 500 shares, got status=%s quantity=%d reason=%v", order.Status, order.Quantity, order.Reason)
	}
}

func TestSubmitOrderNormalizesPercentStylePositionPct(t *testing.T) {
	db := newPaperTestDB(t)
	_, accounts, err := EnsureDefaultPaperSetup(db)
	must(t, err)
	db.Create(&domainmarket.RealtimeQuote{Code: "600519", QuoteTime: time.Now(), Price: decimal.NewFromInt(10)})

	order, err := CreateOrderFromSpec(db, map[string]any{"account_id": accounts[0].ID, "code": "600519", "side": "buy", "position_pct": 5})
	must(t, err)
	if order.Status != domainkernel.OrderPending || order.Quantity != 500 {
		t.Fatalf("expected pending 500 shares, got status=%s quantity=%d reason=%v", order.Status, order.Quantity, order.Reason)
	}
}

func TestSubmitOrderIgnoresZeroQuantityAndFallsBackToPositionPct(t *testing.T) {
	db := newPaperTestDB(t)
	_, accounts, err := EnsureDefaultPaperSetup(db)
	must(t, err)

	order, err := CreateOrderFromSpec(db, map[string]any{
		"account_id":      accounts[0].ID,
		"code":            "601899",
		"side":            "buy",
		"quantity":        0,
		"position_pct":    0.02,
		"suggested_price": 34.8,
		"reason":          "pilot position",
	})
	must(t, err)
	if order.Status != domainkernel.OrderPending || order.Quantity != 100 {
		t.Fatalf("expected pending 100 shares, got status=%s quantity=%d reason=%s", order.Status, order.Quantity, reasonValue(order.Reason))
	}
	if order.Reason == nil || !contains(*order.Reason, "auto-raised to 100 shares") {
		t.Fatalf("missing minimum-lot note: %v", order.Reason)
	}
}

func TestSubmitOrderResizesOversizedQuantityFromMeetingGuidance(t *testing.T) {
	db := newPaperTestDB(t)
	_, accounts, err := EnsureDefaultPaperSetup(db)
	must(t, err)

	order, err := CreateOrderFromSpec(db, map[string]any{
		"account_id":         accounts[0].ID,
		"code":               "601899",
		"side":               "buy",
		"quantity":           14000,
		"meeting_summary":    "pilot watchlist candidate",
		"meeting_conclusion": "strictly execute 5-8% position sizing limit",
		"reason":             "pilot position",
		"suggested_price":    33.30,
	})
	must(t, err)
	if order.Status != domainkernel.OrderPending || order.Quantity != 200 {
		t.Fatalf("expected pending 200 shares, got status=%s quantity=%d reason=%v", order.Status, order.Quantity, order.Reason)
	}
	if order.Reason == nil || !contains(*order.Reason, "auto-resized from 14000 to 200 shares") || !contains(*order.Reason, "meeting sizing range 5.00%-8.00%") {
		t.Fatalf("missing resize notes: %v", order.Reason)
	}
}

func TestReconcileUnfinishedOrdersDoesNotAutoBindMissingRiskConfig(t *testing.T) {
	db := newPaperTestDB(t)
	_, accounts, err := EnsureDefaultPaperSetup(db)
	must(t, err)
	account := accounts[0]
	account.RiskConfigID = nil
	must(t, db.Save(&account).Error)
	reason := "old; risk config disabled"
	orderA := domainpaper.Order{AccountID: account.ID, Code: "601899", Side: domainkernel.OrderBuy, Quantity: 100, SuggestedPrice: decimal.NewFromInt(30), Status: domainkernel.OrderSuggested, Reason: &reason}
	orderUS := domainpaper.Order{AccountID: account.ID, Code: "AVGO", Side: domainkernel.OrderBuy, Quantity: 10, SuggestedPrice: decimal.NewFromInt(200), Status: domainkernel.OrderSuggested, Reason: &reason}
	must(t, db.Create(&orderA).Error)
	must(t, db.Create(&orderUS).Error)

	result, err := ReconcileUnfinishedOrders(db)
	must(t, err)
	must(t, db.First(&orderA, orderA.ID).Error)
	must(t, db.First(&orderUS, orderUS.ID).Error)
	must(t, db.First(&account, account.ID).Error)

	if containsUint(result["repaired"], orderA.ID) || orderA.Status != domainkernel.OrderRejected {
		t.Fatalf("expected A-share order rejected without auto-binding risk config, result=%v order=%+v", result, orderA)
	}
	if orderA.Reason == nil || !contains(*orderA.Reason, "risk config disabled") {
		t.Fatalf("expected disabled-risk reason retained, got %v", orderA.Reason)
	}
	if account.RiskConfigID != nil {
		t.Fatalf("expected account risk config to remain empty, got %v", account.RiskConfigID)
	}
	if !containsUint(result["cancelled"], orderUS.ID) || orderUS.Status != domainkernel.OrderCancelled {
		t.Fatalf("expected unsupported order cancelled, result=%v order=%+v", result, orderUS)
	}
}

func TestReconcileCancelsPendingOrdersForDisabledBoards(t *testing.T) {
	db := newPaperTestDB(t)
	cfg := createRiskConfig(t, db, "star-enabled", decimal.NewFromInt(1), decimal.NewFromInt(1), true)
	cfg.AllowSTAR = true
	must(t, saveRiskConfigRecord(db, cfg))
	account := createPaperAccount(t, db, "acct", decimal.NewFromInt(100000), &cfg.ID)
	order := domainpaper.Order{AccountID: account.ID, Code: "688981", Side: domainkernel.OrderBuy, Quantity: 100, SuggestedPrice: decimal.NewFromInt(30), Status: domainkernel.OrderPending}
	must(t, db.Create(&order).Error)

	cfg.AllowSTAR = false
	must(t, saveRiskConfigRecord(db, cfg))
	result, err := ReconcileUnfinishedOrders(db)
	must(t, err)
	must(t, db.First(&order, order.ID).Error)
	if !containsUint(result["cancelled"], order.ID) || order.Status != domainkernel.OrderCancelled {
		t.Fatalf("expected disabled-board order cancelled, result=%v order=%+v", result, order)
	}
	if order.ExecutionNote == nil || !contains(*order.ExecutionNote, "STAR Market") {
		t.Fatalf("missing STAR cancellation note: %v", order.ExecutionNote)
	}
}

func TestDeleteOrderRecordOnlyAllowsCancelledOrRejected(t *testing.T) {
	db := newPaperTestDB(t)
	_, accounts, err := EnsureDefaultPaperSetup(db)
	must(t, err)
	order, err := CreateOrderFromSpec(db, map[string]any{"account_id": accounts[0].ID, "code": "600519", "side": "sell", "quantity": 100, "suggested_price": 30})
	must(t, err)
	if order.Status != domainkernel.OrderRejected {
		t.Fatalf("expected rejected sell without position, got %s", order.Status)
	}
	orderID := order.ID
	must(t, DeleteOrderRecord(db, order))
	var count int64
	db.Model(&domainpaper.Order{}).Where("id = ?", orderID).Count(&count)
	if count != 0 {
		t.Fatalf("expected deleted order %d", orderID)
	}
}

func TestPaperPublicRowsIncludeSymbolNameAndOverviewCounts(t *testing.T) {
	db := newPaperTestDB(t)
	must(t, db.Create(&domainmarket.Symbol{Code: "600519", Name: "Kweichow Moutai", Exchange: "SH", Active: true}).Error)
	cfg, accounts, err := EnsureDefaultPaperSetup(db)
	must(t, err)
	order, err := CreateOrderFromSpec(db, map[string]any{"account_id": accounts[0].ID, "code": "600519", "side": "buy", "quantity": 100, "suggested_price": 100})
	must(t, err)
	must(t, FillOrder(db, order, decimal.NewFromInt(100)))

	positions := PaperPositionsPublic(db, listPositions(t, db, accounts[0].ID))
	orders := PaperOrdersPublic(db, listOrders(t, db, accounts[0].ID))
	fills := PaperFillsPublic(db, listFills(t, db, accounts[0].ID))
	overview, err := PaperOverview(db)
	must(t, err)

	if !cfg.Enabled {
		t.Fatal("expected default config enabled")
	}
	if positions[0]["symbol_name"].(*string) == nil || *positions[0]["symbol_name"].(*string) != "Kweichow Moutai" {
		t.Fatalf("position symbol_name mismatch: %v", positions[0]["symbol_name"])
	}
	if orders[0]["symbol_name"].(*string) == nil || *orders[0]["symbol_name"].(*string) != "Kweichow Moutai" {
		t.Fatalf("order symbol_name mismatch: %v", orders[0]["symbol_name"])
	}
	if fills[0]["symbol_name"].(*string) == nil || *fills[0]["symbol_name"].(*string) != "Kweichow Moutai" {
		t.Fatalf("fill symbol_name mismatch: %v", fills[0]["symbol_name"])
	}
	if overview["position_count"].(int64) != 1 || overview["fill_count"].(int64) != 1 || overview["filled_order_count"].(int64) != 1 {
		t.Fatalf("overview counts mismatch: %+v", overview)
	}
}

func TestRunPaperMaintenanceLoopExecutesDueOrdersAndSnapshots(t *testing.T) {
	db := newPaperTestDB(t)
	fixedNow := time.Date(2026, 5, 6, 10, 0, 0, 0, appTZ)
	oldNow := paperNow
	paperNow = func() time.Time { return fixedNow }
	defer func() { paperNow = oldNow }()
	cfg := createRiskConfig(t, db, "loop", decimal.NewFromInt(1), decimal.NewFromInt(1), true)
	account := createPaperAccount(t, db, "loop-acct", decimal.NewFromInt(100000), &cfg.ID)
	order := domainpaper.Order{AccountID: account.ID, Code: "600519", Side: domainkernel.OrderBuy, Quantity: 100, Status: domainkernel.OrderPending, SuggestedPrice: decimal.NewFromInt(100), ExecuteAfter: &fixedNow}
	must(t, db.Create(&order).Error)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		RunPaperMaintenanceLoop(ctx, db, time.Hour)
		close(done)
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		var refreshed domainpaper.Order
		must(t, db.First(&refreshed, order.ID).Error)
		if refreshed.Status == domainkernel.OrderFilled {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("paper maintenance loop did not stop after cancellation")
	}
	must(t, db.First(&order, order.ID).Error)
	if order.Status != domainkernel.OrderFilled {
		t.Fatalf("expected loop to fill due order, got %+v", order)
	}
	var snapshots int64
	db.Model(&domainpaper.EquitySnapshot{}).Where("account_id = ?", account.ID).Count(&snapshots)
	if snapshots == 0 {
		t.Fatal("expected trading-time maintenance to capture account snapshot")
	}
}

func TestPaperPerformanceComputesDrawdownAndWinRateFromSnapshots(t *testing.T) {
	db := newPaperTestDB(t)
	cfg := createRiskConfig(t, db, "perf", decimal.NewFromInt(1), decimal.NewFromInt(1), true)
	account := createPaperAccount(t, db, "perf-acct", decimal.NewFromInt(100000), &cfg.ID)
	base := time.Date(2026, 5, 6, 10, 0, 0, 0, appTZ)
	must(t, db.Create(&domainpaper.EquitySnapshot{AccountID: account.ID, SnapshotTime: base, Cash: decimal.NewFromInt(100000), TotalEquity: decimal.NewFromInt(100000)}).Error)
	must(t, db.Create(&domainpaper.EquitySnapshot{AccountID: account.ID, SnapshotTime: base.Add(time.Minute), Cash: decimal.NewFromInt(90000), TotalEquity: decimal.NewFromInt(120000)}).Error)
	must(t, db.Create(&domainpaper.EquitySnapshot{AccountID: account.ID, SnapshotTime: base.Add(2 * time.Minute), Cash: decimal.NewFromInt(80000), TotalEquity: decimal.NewFromInt(90000)}).Error)
	must(t, db.Create(&domainpaper.Fill{AccountID: account.ID, OrderID: 1, Code: "600519", Side: domainkernel.OrderSell, Quantity: 100, Price: decimal.NewFromInt(100), RealizedPNL: decimal.NewFromInt(100), FilledAt: base}).Error)
	must(t, db.Create(&domainpaper.Fill{AccountID: account.ID, OrderID: 2, Code: "600519", Side: domainkernel.OrderSell, Quantity: 100, Price: decimal.NewFromInt(100), RealizedPNL: decimal.NewFromInt(-50), FilledAt: base}).Error)

	perf := PaperPerformance(db, *account)
	assertDecimalEqual(t, perf["max_drawdown_pct"].(decimal.Decimal), "25.0000")
	assertDecimalEqual(t, perf["win_rate_pct"].(decimal.Decimal), "50.0000")
	if perf["fills_count"].(int64) != 2 {
		t.Fatalf("fills_count mismatch: %+v", perf)
	}
}

func newPaperTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	must(t, err)
	must(t, database.AutoMigrate(db))
	sqlDB, err := db.DB()
	must(t, err)
	sqlDB.SetMaxOpenConns(1)
	return db
}

func createRiskConfig(t *testing.T, db *gorm.DB, name string, maxPositionPct decimal.Decimal, maxOrderPct decimal.Decimal, allowETFLOF bool) *domainpaper.RiskConfig {
	t.Helper()
	cfg := &domainpaper.RiskConfig{
		Name:            name,
		InitialCash:     decimal.NewFromInt(100000),
		MaxPositionPct:  maxPositionPct,
		MaxOrderPct:     maxOrderPct,
		AllowSHMain:     true,
		AllowSZMain:     true,
		AllowBJ:         false,
		AllowSTAR:       false,
		AllowChiNext:    false,
		AllowETFLOF:     allowETFLOF,
		CommissionRate:  decimal.RequireFromString("0.00005"),
		MinCommission:   decimal.NewFromInt(5),
		StampDutyRate:   decimal.RequireFromString("0.001"),
		TransferFeeRate: decimal.RequireFromString("0.00001"),
		Enabled:         true,
	}
	must(t, createRiskConfigRecord(db, cfg))
	return cfg
}

func createPaperAccount(t *testing.T, db *gorm.DB, name string, cash decimal.Decimal, riskConfigID *uint) *domainpaper.Account {
	t.Helper()
	account := &domainpaper.Account{Name: name, InitialCash: cash, Cash: cash, RiskConfigID: riskConfigID, Active: true}
	must(t, db.Create(account).Error)
	return account
}

func listPositions(t *testing.T, db *gorm.DB, accountID uint) []domainpaper.Position {
	t.Helper()
	var rows []domainpaper.Position
	must(t, db.Where("account_id = ?", accountID).Order("id").Find(&rows).Error)
	return rows
}

func listOrders(t *testing.T, db *gorm.DB, accountID uint) []domainpaper.Order {
	t.Helper()
	var rows []domainpaper.Order
	must(t, db.Where("account_id = ?", accountID).Order("id").Find(&rows).Error)
	return rows
}

func listFills(t *testing.T, db *gorm.DB, accountID uint) []domainpaper.Fill {
	t.Helper()
	var rows []domainpaper.Fill
	must(t, db.Where("account_id = ?", accountID).Order("id").Find(&rows).Error)
	return rows
}

func assertDecimalEqual(t *testing.T, actual decimal.Decimal, expected string) {
	t.Helper()
	want := decimal.RequireFromString(expected)
	if !actual.Equal(want) {
		t.Fatalf("decimal mismatch: got %s want %s", actual, want)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func contains(value string, needle string) bool {
	return strings.Contains(value, needle)
}

func containsUint(values []uint, needle uint) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}

func reasonValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
