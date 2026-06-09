package paper

import (
	"fmt"
	"strings"
	"time"

	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainpaper "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/paper"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/marketdata/ashare"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

var (
	defaultAutoSlippageRate            = decimal.RequireFromString("0.0005")
	defaultAutoVolumeParticipationRate = decimal.RequireFromString("0.10")
	defaultAutoPriceImpactMaxRate      = decimal.RequireFromString("0.0015")
	oneHundred                         = decimal.NewFromInt(100)
)

type executionCheck struct {
	Blocked bool
	Note    string
}

type priceBand struct {
	PreviousClose decimal.Decimal
	Lower         decimal.Decimal
	Upper         decimal.Decimal
	Rate          decimal.Decimal
}

type priceImpact struct {
	Rate          decimal.Decimal
	Participation decimal.Decimal
	QuoteVolume   int
}

func validateFillExecution(db *gorm.DB, order domainpaper.Order, price decimal.Decimal, now time.Time) executionCheck {
	if order.Side == domainkernel.OrderSell {
		if available := sellableQuantityTPlusOne(db, order.AccountID, order.Code, now); available < order.Quantity {
			return executionCheck{Blocked: true, Note: fmt.Sprintf("Blocked by A-share T+1 rule: sellable quantity is %d shares.", available)}
		}
	}
	if quote, ok := latestRealtimeQuote(db, order.Code); ok && isSameTradingDay(quote.QuoteTime, now) && quote.Volume.IsZero() {
		return executionCheck{Blocked: true, Note: "Blocked because latest realtime quote has zero volume; treated as suspended or non-tradable."}
	}
	if band, ok := latestLimitBand(db, order.Code, now); ok {
		if price.GreaterThan(band.Upper) || price.LessThan(band.Lower) {
			return executionCheck{Blocked: true, Note: fmt.Sprintf("Blocked by daily price limit %.2f%%: executable range is %s-%s.", band.Rate.Mul(oneHundred).InexactFloat64(), band.Lower.StringFixed(4), band.Upper.StringFixed(4))}
		}
	}
	return executionCheck{}
}

func autoExecutionPrice(db *gorm.DB, order domainpaper.Order, base decimal.Decimal, now time.Time) (decimal.Decimal, string) {
	if base.IsZero() {
		return base, ""
	}
	price := base
	rate := defaultAutoSlippageRate
	impact, hasImpact := autoPriceImpact(db, order, now)
	adverseRate := rate.Add(impact.Rate)
	if order.Side == domainkernel.OrderBuy {
		price = price.Mul(decimal.NewFromInt(1).Add(adverseRate))
	} else {
		price = price.Mul(decimal.NewFromInt(1).Sub(adverseRate))
	}
	price = q4(price)
	notes := []string{}
	if !price.Equal(base) {
		notes = append(notes, fmt.Sprintf("Applied %.2f bps simulated slippage.", rate.Mul(decimal.NewFromInt(10000)).InexactFloat64()))
	}
	if hasImpact && impact.Rate.IsPositive() {
		notes = append(notes, fmt.Sprintf("Applied %.2f bps simulated price impact at %.2f%% realtime-volume participation (%d/%d shares).", impact.Rate.Mul(decimal.NewFromInt(10000)).InexactFloat64(), impact.Participation.Mul(oneHundred).InexactFloat64(), order.Quantity, impact.QuoteVolume))
	}
	if band, ok := latestLimitBand(db, order.Code, now); ok {
		capped := price
		if capped.GreaterThan(band.Upper) {
			capped = band.Upper
		}
		if capped.LessThan(band.Lower) {
			capped = band.Lower
		}
		if !capped.Equal(price) {
			notes = append(notes, fmt.Sprintf("Capped fill price to daily limit band %s-%s.", band.Lower.StringFixed(4), band.Upper.StringFixed(4)))
			price = capped
		}
	}
	if hasImpact {
		notes = append(notes, fmt.Sprintf("Auto match report: queue_fill=%d, quote_volume=%d, participation=%.2f%%, adverse_rate=%.2f bps, final_price=%s.", order.Quantity, impact.QuoteVolume, impact.Participation.Mul(oneHundred).InexactFloat64(), adverseRate.Mul(decimal.NewFromInt(10000)).InexactFloat64(), price.StringFixed(4)))
	}
	return price, strings.Join(notes, " ")
}

func autoPriceImpact(db *gorm.DB, order domainpaper.Order, now time.Time) (priceImpact, bool) {
	if order.Quantity <= 0 {
		return priceImpact{}, false
	}
	quote, ok := latestRealtimeQuote(db, order.Code)
	if !ok || !isSameTradingDay(quote.QuoteTime, now) || !quote.Volume.IsPositive() {
		return priceImpact{}, false
	}
	quoteVolume := int(quote.Volume.IntPart())
	if quoteVolume <= 0 {
		return priceImpact{}, false
	}
	participation := decimal.NewFromInt(int64(order.Quantity)).Div(quote.Volume)
	capped := participation
	if capped.GreaterThan(defaultAutoVolumeParticipationRate) {
		capped = defaultAutoVolumeParticipationRate
	}
	rate := defaultAutoPriceImpactMaxRate.Mul(capped.Div(defaultAutoVolumeParticipationRate))
	return priceImpact{Rate: rate, Participation: participation, QuoteVolume: quoteVolume}, true
}

func autoExecutionQuantity(db *gorm.DB, order domainpaper.Order, now time.Time, volumeBudgets map[string]int) (int, string) {
	progress := paperOrderFillProgress(db, []domainpaper.Order{order})[order.ID]
	remaining := progress.RemainingQuantity
	if remaining <= 0 {
		return 0, "Order already has no remaining quantity."
	}
	if volumeBudgets == nil {
		return remaining, ""
	}
	quote, ok := latestRealtimeQuote(db, order.Code)
	if !ok || !isSameTradingDay(quote.QuoteTime, now) || !quote.Volume.IsPositive() {
		return remaining, ""
	}
	if _, ok := volumeBudgets[order.Code]; !ok {
		volumeBudgets[order.Code] = int(q4(quote.Volume.Mul(defaultAutoVolumeParticipationRate)).IntPart())
	}
	available := volumeBudgets[order.Code]
	if available <= 0 {
		return 0, fmt.Sprintf("Waiting for more market volume: realtime volume participation budget is exhausted for %s.", order.Code)
	}
	if available < remaining {
		return available, fmt.Sprintf("Capped by realtime volume participation %.2f%%: filled %d of %d remaining shares.", defaultAutoVolumeParticipationRate.Mul(oneHundred).InexactFloat64(), available, remaining)
	}
	return remaining, ""
}

func sellableQuantityTPlusOne(db *gorm.DB, accountID uint, code string, now time.Time) int {
	var pos persistmodel.PaperPosition
	if err := db.Where("account_id = ? AND code = ?", accountID, code).First(&pos).Error; err != nil || pos.Quantity <= 0 {
		return 0
	}
	dayStart, dayEnd := tradingDayBounds(now)
	var sameDayBuys int64
	db.Model(&persistmodel.PaperFill{}).
		Where("account_id = ? AND code = ? AND side = ? AND filled_at >= ? AND filled_at < ?", accountID, code, domainkernel.OrderBuy, dayStart, dayEnd).
		Select("COALESCE(SUM(quantity), 0)").
		Scan(&sameDayBuys)
	available := pos.Quantity - int(sameDayBuys)
	if available < 0 {
		return 0
	}
	return available
}

func latestLimitBand(db *gorm.DB, code string, now time.Time) (priceBand, bool) {
	var bar persistmodel.DailyBar
	dayStart, _ := tradingDayBounds(now)
	if err := db.Where("code = ? AND trade_date < ? AND close > ?", code, dayStart, 0).Order("trade_date desc").First(&bar).Error; err != nil {
		return priceBand{}, false
	}
	rate := limitRateForCode(db, code)
	upper := bar.Close.Mul(decimal.NewFromInt(1).Add(rate)).Round(2)
	lower := bar.Close.Mul(decimal.NewFromInt(1).Sub(rate)).Round(2)
	return priceBand{PreviousClose: bar.Close, Lower: lower, Upper: upper, Rate: rate}, true
}

func limitRateForCode(db *gorm.DB, code string) decimal.Decimal {
	name := ""
	var symbol persistmodel.MarketSymbol
	if err := db.First(&symbol, "code = ?", code).Error; err == nil {
		name = strings.ToUpper(strings.TrimSpace(symbol.Name))
	}
	if strings.HasPrefix(name, "ST") || strings.Contains(name, "*ST") {
		return decimal.RequireFromString("0.05")
	}
	if listing := ashare.Detect(code); listing != nil {
		switch listing.Board {
		case ashare.BoardSTAR, ashare.BoardChiNext:
			return decimal.RequireFromString("0.20")
		case ashare.BoardBJ:
			return decimal.RequireFromString("0.30")
		}
	}
	return decimal.RequireFromString("0.10")
}

func latestRealtimeQuote(db *gorm.DB, code string) (persistmodel.RealtimeQuote, bool) {
	var quote persistmodel.RealtimeQuote
	if err := db.Where("code = ?", code).Order("quote_time desc").First(&quote).Error; err == nil {
		return quote, true
	}
	return persistmodel.RealtimeQuote{}, false
}

func tradingDayBounds(moment time.Time) (time.Time, time.Time) {
	t := moment.In(appTZ)
	start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, appTZ)
	return start, start.AddDate(0, 0, 1)
}

func isSameTradingDay(a time.Time, b time.Time) bool {
	aStart, _ := tradingDayBounds(a)
	bStart, _ := tradingDayBounds(b)
	return aStart.Equal(bStart)
}
