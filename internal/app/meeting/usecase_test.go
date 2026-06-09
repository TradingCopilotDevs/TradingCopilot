package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
)

func TestRuntimeEntrypointsDelegateThroughMeetingUsecase(t *testing.T) {
	ctx := context.Background()
	service := &fakeRuntimeService{}
	usecase := NewUsecase(nil, service, Settings{})

	if err := usecase.RunOnce(ctx, 42); err != nil {
		t.Fatal(err)
	}
	if service.runOnceID != 42 {
		t.Fatalf("RunOnce delegated id = %d, want 42", service.runOnceID)
	}

	recovered, err := usecase.RecoverQueued(ctx, RecoverySettings{MeetingStaleAfter: time.Minute, MeetingAutoRequeueLimit: 3}, 7, time.Second, "redis", nil)
	if err != nil {
		t.Fatal(err)
	}
	if recovered != 2 {
		t.Fatalf("RecoverQueued returned %d, want 2", recovered)
	}
	if service.recoveryRunner != "redis" || service.recoveryLimit != 7 {
		t.Fatalf("RecoverQueued delegated runner=%q limit=%d", service.recoveryRunner, service.recoveryLimit)
	}
}

func TestReviewTrustSentenceAppendsStructuredEvent(t *testing.T) {
	ctx := context.Background()
	repo := &fakeMeetingRepository{
		meetings: map[uint]domainmeeting.Meeting{
			7: {ID: 7, Topic: "fixture"},
		},
	}
	usecase := NewUsecase(repo, &fakeRuntimeService{}, Settings{}, fakeMeetingTransactor{repo: repo})

	event, found, err := usecase.ReviewTrustSentence(ctx, 7, TrustReviewInput{
		Sentence:         "Risk must be reviewed.",
		Verdict:          "CONFIRMED",
		CitationIDs:      []string{"12:1", "12:1", " "},
		EvidenceEventIDs: []uint{11, 0, 11},
		Comment:          "  checked against quote  ",
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !found || event == nil {
		t.Fatalf("ReviewTrustSentence found=%v event=%#v, want event", found, event)
	}
	if event.MeetingID != 7 || event.Type != domainkernel.EventSystem || event.Sequence != 1 {
		t.Fatalf("unexpected review event: %+v", event)
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "trust_review" || payload["verdict"] != "confirmed" || payload["reviewer"] != "admin" {
		t.Fatalf("review payload mismatch: %+v", payload)
	}
	if payload["sentence_id"] == "" || payload["sentence"] != "Risk must be reviewed." || payload["comment"] != "checked against quote" {
		t.Fatalf("review payload missing sentence metadata: %+v", payload)
	}
	citations := payload["citation_ids"].([]any)
	evidenceIDs := payload["evidence_event_ids"].([]any)
	if len(citations) != 1 || citations[0] != "12:1" || len(evidenceIDs) != 1 || evidenceIDs[0] != float64(11) {
		t.Fatalf("review references were not normalized: %+v", payload)
	}
}

func TestReviewRecapActionRequiresConfirmationAndAppendsStructuredEvent(t *testing.T) {
	ctx := context.Background()
	repo := &fakeMeetingRepository{
		meetings: map[uint]domainmeeting.Meeting{
			7: {ID: 7, Topic: "fixture"},
		},
		events: []domainmeeting.Event{
			{
				ID:        20,
				MeetingID: 7,
				Sequence:  1,
				Type:      domainkernel.EventSystem,
				Payload: domainkernel.NewJSON(map[string]any{
					"status":      "recap_actions_review_required",
					"policy":      "weak_reference_review",
					"disposition": "manual_review_required",
					"reason":      "weak role-only citation",
					"suggested_actions": []map[string]any{
						{
							"action_type": "watchlist",
							"index":       0,
							"disposition": "manual_review_required",
							"reason":      "weak role-only citation",
							"spec":        map[string]any{"code": "600519", "active": true},
						},
					},
				}),
			},
		},
	}
	usecase := NewUsecase(repo, &fakeRuntimeService{}, Settings{}, fakeMeetingTransactor{repo: repo})

	_, _, err := usecase.ReviewRecapAction(ctx, 7, RecapActionReviewInput{
		SuggestionID: "20:recap-action:1",
		Decision:     "approved",
	}, "admin")
	if !errors.Is(err, ErrRecapActionReviewConfirmationRequired) {
		t.Fatalf("ReviewRecapAction err = %v, want confirmation required", err)
	}

	event, found, err := usecase.ReviewRecapAction(ctx, 7, RecapActionReviewInput{
		SuggestionID:     "20:recap-action:1",
		ActionType:       "watchlist",
		Decision:         "APPROVED",
		CitationIDs:      []string{"market.symbols", "market.symbols", " "},
		EvidenceEventIDs: []uint{11, 0, 11},
		Comment:          "  reviewed source quality  ",
		Confirm:          true,
	}, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if !found || event == nil {
		t.Fatalf("ReviewRecapAction found=%v event=%#v, want event", found, event)
	}
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "recap_action_review" || payload["decision"] != "approved" || payload["reviewer"] != "admin" {
		t.Fatalf("recap action review payload mismatch: %+v", payload)
	}
	if payload["suggestion_id"] != "20:recap-action:1" || payload["source_event_id"] != float64(20) || payload["action_index"] != float64(0) || payload["action_type"] != "watchlist" {
		t.Fatalf("recap action review source mismatch: %+v", payload)
	}
	if payload["execution_disposition"] != "manual_execution_required" || payload["comment"] != "reviewed source quality" {
		t.Fatalf("recap action review disposition/comment mismatch: %+v", payload)
	}
	spec := payload["action_spec"].(map[string]any)
	if spec["code"] != "600519" || spec["active"] != true {
		t.Fatalf("recap action review spec mismatch: %+v", payload)
	}
	citations := payload["citation_ids"].([]any)
	evidenceIDs := payload["evidence_event_ids"].([]any)
	if len(citations) != 1 || citations[0] != "market.symbols" || len(evidenceIDs) != 1 || evidenceIDs[0] != float64(11) {
		t.Fatalf("recap action review references were not normalized: %+v", payload)
	}
}

type fakeRuntimeService struct {
	runOnceID      uint
	recoveryRunner string
	recoveryLimit  int
}

func (s *fakeRuntimeService) JSON(value any) domainkernel.JSON { return domainkernel.NewJSON(value) }
func (s *fakeRuntimeService) TagsFromJSON([]byte) []string {
	return nil
}
func (s *fakeRuntimeService) StartLocalRun(context.Context, uint) bool { return true }
func (s *fakeRuntimeService) CancelLocalRun(uint)                      {}
func (s *fakeRuntimeService) Recap(context.Context, *domainmeeting.Meeting, string) error {
	return nil
}
func (s *fakeRuntimeService) RunOnce(_ context.Context, meetingID uint) error {
	s.runOnceID = meetingID
	return nil
}
func (s *fakeRuntimeService) RecoverQueued(_ context.Context, _ RecoverySettings, limit int, _ time.Duration, runner string, _ func(*domainmeeting.Meeting) error) (int, error) {
	s.recoveryRunner = runner
	s.recoveryLimit = limit
	return 2, nil
}

type fakeMeetingTransactor struct {
	repo *fakeMeetingRepository
}

func (tx fakeMeetingTransactor) WithTx(ctx context.Context, fn func(Repository) error) error {
	return fn(tx.repo)
}

type fakeMeetingRepository struct {
	meetings map[uint]domainmeeting.Meeting
	events   []domainmeeting.Event
	nextID   uint
}

func (r *fakeMeetingRepository) List(context.Context, RepositoryListFilter) ([]domainmeeting.Meeting, error) {
	out := make([]domainmeeting.Meeting, 0, len(r.meetings))
	for _, meeting := range r.meetings {
		out = append(out, meeting)
	}
	return out, nil
}

func (r *fakeMeetingRepository) Find(_ context.Context, id uint) (*domainmeeting.Meeting, bool, error) {
	meeting, ok := r.meetings[id]
	if !ok {
		return nil, false, nil
	}
	return &meeting, true, nil
}

func (r *fakeMeetingRepository) ResearchTeamReady(context.Context, uint) (bool, string, error) {
	return true, "", nil
}

func (r *fakeMeetingRepository) Save(_ context.Context, meeting *domainmeeting.Meeting) error {
	if r.meetings == nil {
		r.meetings = map[uint]domainmeeting.Meeting{}
	}
	r.meetings[meeting.ID] = *meeting
	return nil
}

func (r *fakeMeetingRepository) Create(_ context.Context, meeting *domainmeeting.Meeting) error {
	if r.meetings == nil {
		r.meetings = map[uint]domainmeeting.Meeting{}
	}
	if meeting.ID == 0 {
		meeting.ID = uint(len(r.meetings) + 1)
	}
	r.meetings[meeting.ID] = *meeting
	return nil
}

func (r *fakeMeetingRepository) ListEvents(_ context.Context, meetingID uint, _ RepositoryEventFilter) ([]domainmeeting.Event, error) {
	out := []domainmeeting.Event{}
	for _, event := range r.events {
		if event.MeetingID == meetingID {
			out = append(out, event)
		}
	}
	return out, nil
}

func (r *fakeMeetingRepository) AppendEvent(_ context.Context, event *domainmeeting.Event) error {
	r.nextID++
	event.ID = r.nextID
	event.Sequence = len(r.events) + 1
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}
	r.events = append(r.events, *event)
	return nil
}

func (r *fakeMeetingRepository) ListReferences(context.Context, uint) ([]domainmeeting.Reference, error) {
	return nil, nil
}

func (r *fakeMeetingRepository) CreateReference(context.Context, *domainmeeting.Reference) error {
	return nil
}

func (r *fakeMeetingRepository) FindReference(context.Context, uint) (*domainmeeting.Reference, bool, error) {
	return nil, false, nil
}

func (r *fakeMeetingRepository) DeleteReference(context.Context, *domainmeeting.Reference) error {
	return nil
}

func (r *fakeMeetingRepository) DeleteGraph(context.Context, uint) error {
	return nil
}

func (r *fakeMeetingRepository) CloneContextAndReferences(context.Context, uint, uint) (map[string]int, error) {
	return map[string]int{}, nil
}
