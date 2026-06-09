package wake

import (
	"context"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/market"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domaintelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/telegram"
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

func TestFireRejectsInactivePlan(t *testing.T) {
	repo := &fakeWakeRepo{
		plan: domainwake.Plan{
			ID:             1,
			ResearchTeamID: 1,
			TriggerType:    domainkernel.WakeTime,
			TriggerConfig:  domainkernel.NewJSON(map[string]any{}),
			Status:         domainkernel.WakePaused,
		},
	}
	u := NewUsecase(repo, fakeWakeService{}, fakeWakeTx{repo: repo})

	_, found, err := u.Fire(context.Background(), 1)
	if !found {
		t.Fatal("Fire found = false, want true")
	}
	if err == nil || !strings.Contains(err.Error(), "only active wake plans can be fired") {
		t.Fatalf("Fire error = %v, want inactive plan error", err)
	}
	if repo.saved.ID != 0 {
		t.Fatalf("inactive plan should not be saved: %+v", repo.saved)
	}
}

type fakeWakeService struct{}

func (fakeWakeService) JSON(any) domainkernel.JSON                { return nil }
func (fakeWakeService) NormalizeCode(code string) (string, error) { return code, nil }
func (fakeWakeService) RefreshRealtimeQuote(context.Context, string) (*domainmarket.RealtimeQuote, error) {
	return nil, nil
}

type fakeWakeTx struct {
	repo *fakeWakeRepo
}

func (tx fakeWakeTx) WithTx(ctx context.Context, fn func(Repository) error) error {
	return fn(tx.repo)
}

type fakeWakeRepo struct {
	plan  domainwake.Plan
	saved domainwake.Plan
}

func (repo *fakeWakeRepo) List(context.Context, RepositoryListFilter) ([]domainwake.Plan, error) {
	return nil, nil
}

func (repo *fakeWakeRepo) ListDue(context.Context, int) ([]domainwake.Plan, error) {
	return nil, nil
}

func (repo *fakeWakeRepo) Create(context.Context, *domainwake.Plan) error {
	return nil
}

func (repo *fakeWakeRepo) Find(_ context.Context, id uint) (*domainwake.Plan, bool, error) {
	if repo.plan.ID != id {
		return nil, false, nil
	}
	plan := repo.plan
	return &plan, true, nil
}

func (repo *fakeWakeRepo) Save(_ context.Context, plan *domainwake.Plan) error {
	repo.saved = *plan
	return nil
}

func (repo *fakeWakeRepo) Delete(context.Context, uint) error {
	return nil
}

func (repo *fakeWakeRepo) FindMeetingSnapshot(context.Context, uint) (*domainmeeting.Meeting, bool, error) {
	return nil, false, nil
}

func (repo *fakeWakeRepo) CreateMeeting(_ context.Context, meeting *domainmeeting.Meeting) error {
	meeting.ID = 1
	return nil
}

func (repo *fakeWakeRepo) AppendMeetingEvent(context.Context, *domainmeeting.Event) error {
	return nil
}

func (repo *fakeWakeRepo) CreateMeetingReference(context.Context, *domainmeeting.Reference) error {
	return nil
}

func (repo *fakeWakeRepo) ResearchTeamReady(context.Context, uint) (bool, string, error) {
	return true, "", nil
}

func (repo *fakeWakeRepo) LatestRealtimeQuote(context.Context, string) (*domainmarket.RealtimeQuote, bool, error) {
	return nil, false, nil
}

func (repo *fakeWakeRepo) LatestDailyBar(context.Context, string) (*domainmarket.DailyBar, bool, error) {
	return nil, false, nil
}

func (repo *fakeWakeRepo) RecentTelegramMessages(context.Context, MessageFilter) ([]domaintelegram.Message, error) {
	return nil, nil
}
