package wake

import (
	"fmt"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainwake "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/wake"
	"strings"

	"github.com/shopspring/decimal"
)

func ValidatePlan(plan *domainwake.Plan) error {
	if plan == nil {
		return fmt.Errorf("wake plan is required")
	}
	switch plan.TriggerType {
	case domainkernel.WakeTime:
		return nil
	case domainkernel.WakeIndicator:
		return validateIndicatorPlan(plan)
	case domainkernel.WakeEvent:
		return validateEventPlan(plan)
	default:
		return fmt.Errorf("unsupported wake trigger type %s", plan.TriggerType)
	}
}

func validateIndicatorPlan(plan *domainwake.Plan) error {
	cfg := wakeConfig(plan)
	conditions := indicatorConditionsFromConfig(cfg)
	if len(conditions) == 0 {
		conditions = []map[string]any{cfg}
	}
	for i, condition := range conditions {
		code := strings.TrimSpace(firstNonEmptyString(stringFromConfig(condition["code"]), stringFromConfig(condition["symbol"]), stringFromConfig(condition["ticker"])))
		thresholdText := firstNonEmptyString(stringFromConfig(condition["threshold"]), stringFromConfig(condition["target"]), stringFromConfig(condition["targetPrice"]), stringFromConfig(condition["target_price"]), stringFromConfig(condition["value"]), stringFromConfig(condition["targetValue"]), stringFromConfig(condition["target_value"]))
		if code == "" {
			return fmt.Errorf("indicator wake condition %d requires code, symbol, or ticker", i+1)
		}
		if strings.TrimSpace(thresholdText) == "" {
			return fmt.Errorf("indicator wake condition %d requires a numeric threshold", i+1)
		}
		if _, err := decimal.NewFromString(thresholdText); err != nil {
			return fmt.Errorf("indicator wake condition %d has invalid numeric threshold %q", i+1, thresholdText)
		}
	}
	return nil
}

func validateEventPlan(plan *domainwake.Plan) error {
	cfg := wakeConfig(plan)
	if len(stringsFromConfig(firstNonNil(cfg["keywords"], cfg["keyword"], cfg["contains"], cfg["text_contains"]))) > 0 {
		return nil
	}
	if strings.TrimSpace(firstNonEmptyString(stringFromConfig(cfg["regex"]), stringFromConfig(cfg["pattern"]))) != "" {
		return nil
	}
	if len(stringsFromConfig(firstNonNil(cfg["relatedSymbols"], cfg["related_symbols"], cfg["symbols"], cfg["codes"]))) > 0 {
		return nil
	}
	if len(stringsFromConfig(firstNonNil(cfg["decisions"], cfg["decision"], cfg["filterDecision"], cfg["filter_decision"]))) > 0 {
		return nil
	}
	if len(stringsFromConfig(firstNonNil(cfg["channelIds"], cfg["channelID"], cfg["channel_ids"], cfg["channels"], cfg["channel_id"]))) > 0 {
		return nil
	}
	return fmt.Errorf("event wake requires keywords, regex, symbols, decisions, or channel filters")
}
