package wake

import (
	"context"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/market"
	domainwake "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/wake"
	"strings"
	"testing"
)

func TestFireRequiresConfiguredUnitOfWork(t *testing.T) {
	u := NewUsecase(nil, fakeWakeService{})
	_, _, err := u.Fire(context.Background(), 1)
	if err == nil || !strings.Contains(err.Error(), "unit of work") {
		t.Fatalf("Fire error = %v, want unit of work error", err)
	}
}

func TestValidatePlanRejectsInvalidIndicatorConfig(t *testing.T) {
	plan := domainwake.Plan{TriggerType: domainkernel.WakeIndicator, TriggerConfig: domainkernel.NewJSON(map[string]any{"topic": "missing condition"})}
	err := ValidatePlan(&plan)
	if err == nil || !strings.Contains(err.Error(), "requires code") {
		t.Fatalf("ValidatePlan error = %v, want missing code error", err)
	}
}

func TestValidatePlanRejectsInvalidEventConfig(t *testing.T) {
	plan := domainwake.Plan{TriggerType: domainkernel.WakeEvent, TriggerConfig: domainkernel.NewJSON(map[string]any{"topic": "missing filters"})}
	err := ValidatePlan(&plan)
	if err == nil || !strings.Contains(err.Error(), "event wake requires") {
		t.Fatalf("ValidatePlan error = %v, want missing event filters error", err)
	}
}

func TestValidatePlanAcceptsExecutableWakePlans(t *testing.T) {
	for _, plan := range []domainwake.Plan{
		{TriggerType: domainkernel.WakeTime, TriggerConfig: domainkernel.NewJSON(map[string]any{})},
		{TriggerType: domainkernel.WakeIndicator, TriggerConfig: domainkernel.NewJSON(map[string]any{"code": "600519", "threshold": "100"})},
		{TriggerType: domainkernel.WakeEvent, TriggerConfig: domainkernel.NewJSON(map[string]any{"keywords": []string{"earnings"}})},
	} {
		if err := ValidatePlan(&plan); err != nil {
			t.Fatalf("ValidatePlan(%s) returned %v", plan.TriggerType, err)
		}
	}
}

type fakeWakeService struct{}

func (fakeWakeService) JSON(any) domainkernel.JSON                { return nil }
func (fakeWakeService) NormalizeCode(code string) (string, error) { return code, nil }
func (fakeWakeService) RefreshRealtimeQuote(context.Context, string) (*domainmarket.RealtimeQuote, error) {
	return nil, nil
}
