package paper

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"

	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainpaper "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/paper"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/marketdata/ashare"
	persistmodel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/model"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

func calculateFees(side domainkernel.OrderSide, gross decimal.Decimal, cfg domainpaper.RiskConfig) map[string]decimal.Decimal {
	commissionRate := cfg.CommissionRate
	if commissionRate.IsZero() {
		commissionRate = decimal.NewFromFloat(0.00005)
	}
	minCommission := cfg.MinCommission
	if minCommission.IsZero() {
		minCommission = decimal.NewFromInt(5)
	}
	transferRate := cfg.TransferFeeRate
	if transferRate.IsZero() {
		transferRate = decimal.NewFromFloat(0.00001)
	}
	stampRate := cfg.StampDutyRate
	if stampRate.IsZero() {
		stampRate = decimal.NewFromFloat(0.001)
	}
	commission := gross.Mul(commissionRate)
	if commission.LessThan(minCommission) {
		commission = minCommission
	}
	stamp := decimal.Zero
	if side == domainkernel.OrderSell {
		stamp = gross.Mul(stampRate)
	}
	return map[string]decimal.Decimal{"commission": commission.Round(4), "stamp_duty": stamp.Round(4), "transfer_fee": gross.Mul(transferRate).Round(4)}
}

func resolveAccount(db *gorm.DB, value any) (domainpaper.Account, error) {
	if id, ok := uintFromPayload(value); ok {
		var row persistmodel.PaperAccount
		err := db.First(&row, id).Error
		return paperAccountModelToDomain(row), err
	}
	var row persistmodel.PaperAccount
	err := db.Where("active = ?", true).Order("id").First(&row).Error
	return paperAccountModelToDomain(row), err
}

func resolveTargetCode(db *gorm.DB, account domainpaper.Account, code string, side domainkernel.OrderSide, reason *string) (string, *string) {
	cfg, _ := RiskEnabled(db, account)
	if cfg == nil {
		return code, reason
	}
	if _, err := ashare.ValidateAgainstRiskConfig(*cfg, code); err == nil {
		return code, reason
	}
	listing := ashare.Detect(code)
	if listing == nil || side != domainkernel.OrderBuy || listing.Board == ashare.BoardETFLOF || !cfg.AllowETFLOF {
		return code, reason
	}
	sub, ok := boardSubstitutes[listing.Board]
	if !ok {
		return code, reason
	}
	if _, err := ashare.ValidateAgainstRiskConfig(*cfg, sub.Code); err != nil {
		return code, reason
	}
	note := fmt.Sprintf("risk config does not allow %s for %s; substituted with %s %s", ashare.BoardLabel(listing.Board), code, sub.Code, sub.Name)
	return sub.Code, appendReason(reason, note)
}

func resolveQuantityFromPct(db *gorm.DB, account domainpaper.Account, code string, price decimal.Decimal, pct decimal.Decimal) (int, error) {
	if price.IsZero() {
		return 0, errors.New("cannot size order because no suggested_price or realtime quote is available")
	}
	metrics := ComputeAccountMetrics(db, account, false)
	totalEquity := metrics["total_equity"].(decimal.Decimal)
	if totalEquity.LessThanOrEqual(decimal.Zero) {
		return 0, errors.New("cannot size order because total equity is zero")
	}
	target := totalEquity.Mul(pct)
	lots := target.Div(price.Mul(decimal.NewFromInt(100))).Floor().IntPart()
	qty := int(lots * 100)
	if qty <= 0 {
		return 0, nil
	}
	return qty, nil
}

func maxBuyQuantityForConstraints(db *gorm.DB, account domainpaper.Account, code string, price decimal.Decimal, minPct decimal.Decimal, maxPct decimal.Decimal) (int, int, []string) {
	cfg, _ := RiskEnabled(db, account)
	if cfg == nil || price.IsZero() {
		return 0, 0, nil
	}
	metrics := ComputeAccountMetrics(db, account, false)
	totalEquity := metrics["total_equity"].(decimal.Decimal)
	limits := []decimal.Decimal{account.Cash}
	if !cfg.MaxOrderPct.IsZero() {
		limits = append(limits, totalEquity.Mul(cfg.MaxOrderPct))
	}
	if !cfg.MaxPositionPct.IsZero() {
		remaining := totalEquity.Mul(cfg.MaxPositionPct).Sub(currentPositionValue(db, account.ID, code, price))
		if remaining.LessThan(decimal.Zero) {
			remaining = decimal.Zero
		}
		limits = append(limits, remaining)
	}
	hardAllowed := minPositive(limits)
	notes := []string{}
	if !maxPct.IsZero() {
		limits = append(limits, totalEquity.Mul(maxPct))
		if !minPct.IsZero() && !minPct.Equal(maxPct) {
			notes = append(notes, fmt.Sprintf("meeting sizing range %.2f%%-%.2f%%", minPct.Mul(decimal.NewFromInt(100)).InexactFloat64(), maxPct.Mul(decimal.NewFromInt(100)).InexactFloat64()))
		} else {
			notes = append(notes, fmt.Sprintf("meeting sizing target %.2f%%", maxPct.Mul(decimal.NewFromInt(100)).InexactFloat64()))
		}
	}
	allowed := minPositive(limits)
	lotValue := price.Mul(decimal.NewFromInt(100))
	maxLots := allowed.Div(lotValue).Floor().IntPart()
	hardLots := hardAllowed.Div(lotValue).Floor().IntPart()
	minQty := 0
	if !minPct.IsZero() {
		minNotional := totalEquity.Mul(minPct)
		minLots := int(math.Ceil(minNotional.Div(lotValue).InexactFloat64()))
		if int64(minLots) <= maxLots || (maxLots == 0 && minLots == 1 && hardLots >= 1) {
			minQty = minLots * 100
		}
	}
	return int(maxLots * 100), minQty, notes
}

func currentPositionValue(db *gorm.DB, accountID uint, code string, price decimal.Decimal) decimal.Decimal {
	var pos persistmodel.PaperPosition
	if err := db.Where("account_id = ? AND code = ?", accountID, code).First(&pos).Error; err != nil || pos.Quantity <= 0 {
		return decimal.Zero
	}
	return q2(price.Mul(decimal.NewFromInt(int64(pos.Quantity))))
}

func latestQuotePrice(db *gorm.DB, code string) decimal.Decimal {
	var quote persistmodel.RealtimeQuote
	if err := db.Where("code = ?", code).Order("quote_time desc").First(&quote).Error; err == nil {
		return quote.Price
	}
	return decimal.Zero
}

func inferPositionPct(spec map[string]any) decimal.Decimal {
	if pct := normalizePositionPct(firstNonNil(spec["position_pct"], spec["allocation_pct"])); !pct.IsZero() {
		return pct
	}
	low, high := positionPctBounds(spec)
	if low.IsZero() && high.IsZero() {
		return decimal.Zero
	}
	if !low.IsZero() && !high.IsZero() && !low.Equal(high) {
		if prefersPilot(spec["reason"], spec["meeting_summary"], spec["meeting_conclusion"]) {
			return low
		}
		return high
	}
	if !high.IsZero() {
		return high
	}
	return low
}

func positionPctBounds(spec map[string]any) (decimal.Decimal, decimal.Decimal) {
	if pct := normalizePositionPct(firstNonNil(spec["position_pct"], spec["allocation_pct"])); !pct.IsZero() {
		return pct, pct
	}
	text := strings.Join([]string{stringFromPayload(spec["reason"]), stringFromPayload(spec["meeting_summary"]), stringFromPayload(spec["meeting_conclusion"])}, " ")
	text = strings.ReplaceAll(strings.ReplaceAll(text, "~", "-"), "to", "-")
	rangeRe := regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:%|pct|percent)?\s*(?:-|to)\s*(\d+(?:\.\d+)?)\s*(?:%|pct|percent)`)
	if match := rangeRe.FindStringSubmatch(text); len(match) == 3 {
		low := normalizePositionPct(match[1])
		high := normalizePositionPct(match[2])
		if low.GreaterThan(high) {
			low, high = high, low
		}
		return low, high
	}
	singleRe := regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:%|pct|percent)`)
	if match := singleRe.FindStringSubmatch(text); len(match) == 2 {
		pct := normalizePositionPct(match[1])
		return pct, pct
	}
	return decimal.Zero, decimal.Zero
}

func normalizePositionPct(value any) decimal.Decimal {
	if value == nil || fmt.Sprint(value) == "" {
		return decimal.Zero
	}
	pct := decimalFromPayload(value)
	if pct.LessThanOrEqual(decimal.Zero) {
		return decimal.Zero
	}
	if pct.GreaterThan(decimal.NewFromInt(1)) {
		if pct.LessThanOrEqual(decimal.NewFromInt(100)) {
			pct = pct.Div(decimal.NewFromInt(100))
		} else {
			return decimal.Zero
		}
	}
	return pct
}

func prefersPilot(values ...any) bool {
	text := strings.ToLower(strings.Join(func() []string {
		out := make([]string, 0, len(values))
		for _, value := range values {
			out = append(out, stringFromPayload(value))
		}
		return out
	}(), " "))
	for _, keyword := range []string{"pilot", "small position", "starter", "probe"} {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func rejectedOrder(accountID uint, code string, side domainkernel.OrderSide, reason *string, extra string) *domainpaper.Order {
	return &domainpaper.Order{AccountID: accountID, Code: code, Side: side, Status: domainkernel.OrderRejected, Reason: appendReason(reason, extra)}
}

func cleanOrderReason(reason *string) *string {
	if reason == nil {
		return nil
	}
	cleaned := strings.Trim(strings.ReplaceAll(*reason, "; risk config disabled", ""), " ;")
	if cleaned == "" {
		return nil
	}
	return &cleaned
}

func appendReason(reason *string, extra string) *string {
	base := ""
	if reason != nil {
		base = strings.TrimSpace(*reason)
	}
	if base == "" {
		return &extra
	}
	joined := base + "; " + extra
	return &joined
}

func minPositive(values []decimal.Decimal) decimal.Decimal {
	min := decimal.Zero
	for _, value := range values {
		if value.LessThanOrEqual(decimal.Zero) {
			continue
		}
		if min.IsZero() || value.LessThan(min) {
			min = value
		}
	}
	return min
}
