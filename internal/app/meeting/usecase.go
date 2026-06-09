package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	"hash/fnv"
	"strconv"
	"strings"
	"time"
)

var ErrNotFound = errors.New("meeting not found")
var ErrInvalidTrustReview = errors.New("invalid meeting trust review")
var ErrInvalidRecapActionReview = errors.New("invalid meeting recap action review")
var ErrRecapActionReviewConfirmationRequired = errors.New("recap action review confirmation required")

type Usecase struct {
	repo       Repository
	service    Service
	settings   Settings
	tx         Transactor
	enqueue    EnqueueMeeting
	enqueueCtx EnqueueMeetingContext
}

type Settings struct {
	MeetingDispatchMode string
}

func NewUsecase(repo Repository, service Service, settings Settings, tx ...Transactor) Usecase {
	u := Usecase{repo: repo, service: service, settings: settings}
	if len(tx) > 0 {
		u.tx = tx[0]
	}
	return u
}

type Repository interface {
	List(ctx context.Context, filter RepositoryListFilter) ([]domainmeeting.Meeting, error)
	Find(ctx context.Context, id uint) (*domainmeeting.Meeting, bool, error)
	ResearchTeamReady(ctx context.Context, teamID uint) (bool, string, error)
	Save(ctx context.Context, meeting *domainmeeting.Meeting) error
	Create(ctx context.Context, meeting *domainmeeting.Meeting) error
	ListEvents(ctx context.Context, meetingID uint, filter RepositoryEventFilter) ([]domainmeeting.Event, error)
	AppendEvent(ctx context.Context, event *domainmeeting.Event) error
	ListReferences(ctx context.Context, meetingID uint) ([]domainmeeting.Reference, error)
	CreateReference(ctx context.Context, ref *domainmeeting.Reference) error
	FindReference(ctx context.Context, id uint) (*domainmeeting.Reference, bool, error)
	DeleteReference(ctx context.Context, ref *domainmeeting.Reference) error
	DeleteGraph(ctx context.Context, meetingID uint) error
	CloneContextAndReferences(ctx context.Context, sourceMeetingID uint, targetMeetingID uint) (map[string]int, error)
}

type Service interface {
	JSON(value any) domainkernel.JSON
	TagsFromJSON(raw []byte) []string
	StartLocalRun(ctx context.Context, meetingID uint) bool
	CancelLocalRun(meetingID uint)
	Recap(ctx context.Context, meeting *domainmeeting.Meeting, requestedBy string) error
	RunOnce(ctx context.Context, meetingID uint) error
	RecoverQueued(ctx context.Context, settings RecoverySettings, limit int, olderThan time.Duration, runner string, dispatch func(*domainmeeting.Meeting) error) (int, error)
}

type Transactor interface {
	WithTx(ctx context.Context, fn func(Repository) error) error
}

type EnqueueMeeting func(meetingID uint) error
type EnqueueMeetingContext func(ctx context.Context, meetingID uint) error

type RecoverySettings struct {
	MeetingStaleAfter       time.Duration
	MeetingAutoRequeueLimit int
}

func (u Usecase) WithMeetingEnqueuer(enqueue EnqueueMeeting) Usecase {
	u.enqueue = enqueue
	return u
}

func (u Usecase) WithMeetingEnqueuerContext(enqueue EnqueueMeetingContext) Usecase {
	u.enqueueCtx = enqueue
	return u
}

type RepositoryListFilter struct {
	ResearchTeamID string
	Status         string
	TriggerSource  string
	Limit          int
	CursorID       uint64
}

type RepositoryEventFilter struct {
	Limit    int
	CursorID uint64
}

type Page struct {
	Limit  int
	Cursor string
}

type ListFilter struct {
	ResearchTeamID string
	Status         string
	TriggerSource  string
	Tag            string
	Page           Page
}

type ListResult struct {
	Rows       []domainmeeting.Meeting
	NextCursor string
}

type EventListResult struct {
	Rows       []domainmeeting.Event
	NextCursor string
}

type StartInput struct {
	ResearchTeamID uint
	Topic          string
	TriggerSource  string
	Context        map[string]any
}

type UpdateInput struct {
	Topic         *string
	TriggerSource *string
	Tags          any
	HasTags       bool
}

type ReferenceInput struct {
	TargetMeetingID uint
	Note            *string
}

type TrustReviewInput struct {
	SentenceID       string
	Sentence         string
	Verdict          string
	CitationIDs      []string
	EvidenceEventIDs []uint
	Comment          string
}

type RecapActionReviewInput struct {
	SuggestionID     string
	SourceEventID    uint
	ActionIndex      *int
	ActionType       string
	Decision         string
	CitationIDs      []string
	EvidenceEventIDs []uint
	Comment          string
	Confirm          bool
}

func (u Usecase) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	page := filter.Page
	if page.Limit <= 0 {
		page.Limit = 100
	}
	var cursorID uint64
	if page.Cursor != "" {
		cursorID, _ = strconv.ParseUint(page.Cursor, 10, 64)
	}
	rows, err := u.repo.List(ctx, RepositoryListFilter{
		Status:         filter.Status,
		TriggerSource:  strings.TrimSpace(filter.TriggerSource),
		ResearchTeamID: strings.TrimSpace(filter.ResearchTeamID),
		Limit:          page.Limit + 1,
		CursorID:       cursorID,
	})
	if err != nil {
		return ListResult{}, err
	}
	if tag := strings.TrimSpace(filter.Tag); tag != "" {
		filtered := make([]domainmeeting.Meeting, 0, len(rows))
		for _, row := range rows {
			for _, existing := range u.service.TagsFromJSON(row.Tags) {
				if existing == tag {
					filtered = append(filtered, row)
					break
				}
			}
		}
		rows = filtered
	}
	nextCursor := ""
	if len(rows) > page.Limit {
		nextCursor = strconv.FormatUint(uint64(rows[page.Limit-1].ID), 10)
		rows = rows[:page.Limit]
	}
	return ListResult{Rows: rows, NextCursor: nextCursor}, nil
}

func (u Usecase) Start(ctx context.Context, input StartInput) (*domainmeeting.Meeting, error) {
	if input.ResearchTeamID == 0 {
		return nil, errors.New("research team is required")
	}
	ready, reason, err := u.repo.ResearchTeamReady(ctx, input.ResearchTeamID)
	if err != nil {
		return nil, err
	}
	if !ready {
		return nil, errors.New(reason)
	}
	trigger := input.TriggerSource
	if trigger == "" {
		trigger = "manual"
	}
	var meeting *domainmeeting.Meeting
	if err := u.withTx(ctx, func(repo Repository) error {
		meeting = &domainmeeting.Meeting{ResearchTeamID: input.ResearchTeamID, Topic: input.Topic, TriggerSource: trigger, Status: domainkernel.MeetingQueued, Tags: u.service.JSON(nil)}
		if err := repo.Create(ctx, meeting); err != nil {
			return err
		}
		if err := repo.AppendEvent(ctx, &domainmeeting.Event{MeetingID: meeting.ID, Type: domainkernel.EventSystem, Content: "Meeting submitted for execution.", Payload: u.service.JSON(map[string]any{"status": "queued"})}); err != nil {
			return err
		}
		if input.Context != nil {
			return repo.AppendEvent(ctx, &domainmeeting.Event{MeetingID: meeting.ID, Type: domainkernel.EventSystem, Content: "Meeting input context attached.", Payload: u.service.JSON(map[string]any{"status": "meeting_context", "context": input.Context})})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return meeting, nil
}

func (u Usecase) DispatchRun(ctx context.Context, meeting *domainmeeting.Meeting, queuedStatus string, queuedMessage string, queuedPayload map[string]any) (string, error) {
	if queuedStatus == "" {
		queuedStatus = "queued"
	}
	mode := strings.ToLower(strings.TrimSpace(u.settings.MeetingDispatchMode))
	if mode == "" {
		mode = "auto"
	}
	runner := "local"
	message := queuedMessage
	switch mode {
	case "redis":
		if u.enqueue == nil && u.enqueueCtx == nil {
			return "", errors.New("meeting enqueuer is not configured")
		}
		if err := u.enqueueMeeting(context.WithoutCancel(ctx), meeting.ID); err != nil {
			return "", err
		}
		runner = "redis"
		if message == "" {
			message = "Meeting job submitted to Redis worker."
		}
	case "auto":
		enqueueCtx := context.WithoutCancel(ctx)
		if (u.enqueue != nil || u.enqueueCtx != nil) && enqueueMeetingWithContextTimeout(enqueueCtx, u.enqueueMeeting, meeting.ID, 800*time.Millisecond) == nil {
			runner = "redis"
			if message == "" {
				message = "Meeting job submitted to Redis worker."
			}
		} else {
			u.service.StartLocalRun(ctx, meeting.ID)
			if message == "" {
				message = "Redis worker unavailable; switched to local background execution."
			}
		}
	default:
		u.service.StartLocalRun(ctx, meeting.ID)
		if message == "" {
			message = "Meeting queued for local background execution."
		}
	}
	payload := map[string]any{"status": queuedStatus, "runner": runner}
	for key, value := range queuedPayload {
		payload[key] = value
	}
	_ = u.withTx(ctx, func(repo Repository) error {
		return repo.AppendEvent(ctx, &domainmeeting.Event{MeetingID: meeting.ID, Type: domainkernel.EventSystem, Content: message, Payload: u.service.JSON(payload)})
	})
	return runner, nil
}

func (u Usecase) enqueueMeeting(ctx context.Context, meetingID uint) error {
	if u.enqueueCtx != nil {
		return u.enqueueCtx(ctx, meetingID)
	}
	if u.enqueue != nil {
		return u.enqueue(meetingID)
	}
	return errors.New("meeting enqueuer is not configured")
}

func enqueueMeetingWithContextTimeout(ctx context.Context, enqueue EnqueueMeetingContext, meetingID uint, timeout time.Duration) error {
	result := make(chan error, 1)
	go func() { result <- enqueue(ctx, meetingID) }()
	select {
	case err := <-result:
		return err
	case <-time.After(timeout):
		return errors.New("redis enqueue timed out")
	}
}

func (u Usecase) Get(ctx context.Context, id uint) (*domainmeeting.Meeting, bool, error) {
	return u.repo.Find(ctx, id)
}

func (u Usecase) Update(ctx context.Context, id uint, input UpdateInput) (*domainmeeting.Meeting, bool, error) {
	row, found, err := u.Get(ctx, id)
	if err != nil || !found {
		return nil, found, err
	}
	if err := u.withTx(ctx, func(repo Repository) error {
		if input.Topic != nil {
			row.Topic = *input.Topic
		}
		if input.TriggerSource != nil {
			row.TriggerSource = *input.TriggerSource
		}
		if input.HasTags {
			row.Tags = u.service.JSON(input.Tags)
		}
		return repo.Save(ctx, row)
	}); err != nil {
		return nil, true, err
	}
	return row, true, nil
}

func (u Usecase) Recap(ctx context.Context, id uint, requestedBy string) (*domainmeeting.Meeting, bool, error) {
	row, found, err := u.Get(ctx, id)
	if err != nil || !found {
		return nil, found, err
	}
	if err := u.service.Recap(ctx, row, requestedBy); err != nil {
		return nil, true, err
	}
	return row, true, nil
}

func (u Usecase) RunOnce(ctx context.Context, id uint) error {
	return u.service.RunOnce(ctx, id)
}

func (u Usecase) RecoverQueued(ctx context.Context, settings RecoverySettings, limit int, olderThan time.Duration, runner string, dispatch func(*domainmeeting.Meeting) error) (int, error) {
	return u.service.RecoverQueued(ctx, settings, limit, olderThan, runner, dispatch)
}

func (u Usecase) Delete(ctx context.Context, id uint) (bool, error) {
	row, found, err := u.Get(ctx, id)
	if err != nil || !found {
		return found, err
	}
	if row.Status == domainkernel.MeetingQueued || row.Status == domainkernel.MeetingRunning {
		u.service.CancelLocalRun(row.ID)
	}
	if err := u.withTx(ctx, func(repo Repository) error {
		if row.Status == domainkernel.MeetingQueued || row.Status == domainkernel.MeetingRunning {
			_ = u.cancelMeeting(ctx, repo, row, "Meeting deleted by user.")
		}
		return repo.DeleteGraph(ctx, row.ID)
	}); err != nil {
		return true, err
	}
	return true, nil
}

func (u Usecase) Cancel(ctx context.Context, id uint) (*domainmeeting.Meeting, bool, error) {
	row, found, err := u.Get(ctx, id)
	if err != nil || !found {
		return nil, found, err
	}
	u.service.CancelLocalRun(row.ID)
	if err := u.withTx(ctx, func(repo Repository) error {
		_ = u.cancelMeeting(ctx, repo, row, "Meeting cancelled by user.")
		if refreshed, ok, refreshErr := repo.Find(ctx, row.ID); refreshErr == nil && ok {
			row = refreshed
		} else if refreshErr != nil {
			return refreshErr
		}
		return nil
	}); err != nil {
		return nil, true, err
	}
	return row, true, nil
}

func (u Usecase) Restart(ctx context.Context, id uint) (*domainmeeting.Meeting, bool, error) {
	old, found, err := u.Get(ctx, id)
	if err != nil || !found {
		return nil, found, err
	}
	if old.Status == domainkernel.MeetingQueued || old.Status == domainkernel.MeetingRunning {
		u.service.CancelLocalRun(old.ID)
	}
	var meeting *domainmeeting.Meeting
	if err := u.withTx(ctx, func(repo Repository) error {
		if old.Status == domainkernel.MeetingQueued || old.Status == domainkernel.MeetingRunning {
			_ = u.cancelMeeting(ctx, repo, old, "Meeting cancelled before restart.")
		}
		meeting = &domainmeeting.Meeting{ResearchTeamID: old.ResearchTeamID, Topic: old.Topic, TriggerSource: old.TriggerSource, Status: domainkernel.MeetingQueued, Tags: u.service.JSON(nil)}
		if err := repo.Create(ctx, meeting); err != nil {
			return err
		}
		if err := repo.AppendEvent(ctx, &domainmeeting.Event{MeetingID: meeting.ID, Type: domainkernel.EventSystem, Content: "Meeting submitted for execution.", Payload: u.service.JSON(map[string]any{"status": "queued"})}); err != nil {
			return err
		}
		meeting.Tags = old.Tags
		if err := repo.Save(ctx, meeting); err != nil {
			return err
		}
		if err := repo.AppendEvent(ctx, &domainmeeting.Event{MeetingID: meeting.ID, Type: domainkernel.EventSystem, Content: fmt.Sprintf("Meeting restarted from #%d.", old.ID), Payload: u.service.JSON(map[string]any{"status": "restarted", "source_meeting_id": old.ID})}); err != nil {
			return err
		}
		_, cloneErr := repo.CloneContextAndReferences(ctx, old.ID, meeting.ID)
		return cloneErr
	}); err != nil {
		return nil, true, err
	}
	return meeting, true, nil
}

func (u Usecase) ListEvents(ctx context.Context, meetingID uint, page Page) (EventListResult, error) {
	if page.Limit <= 0 {
		page.Limit = 100
	}
	var cursorID uint64
	if page.Cursor != "" {
		cursorID, _ = strconv.ParseUint(page.Cursor, 10, 64)
	}
	rows, err := u.repo.ListEvents(ctx, meetingID, RepositoryEventFilter{Limit: page.Limit + 1, CursorID: cursorID})
	if err != nil {
		return EventListResult{}, err
	}
	nextCursor := ""
	if len(rows) > page.Limit {
		nextCursor = strconv.FormatUint(uint64(rows[page.Limit-1].ID), 10)
		rows = rows[:page.Limit]
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	return EventListResult{Rows: rows, NextCursor: nextCursor}, nil
}

func (u Usecase) ListReferences(ctx context.Context, meetingID uint) ([]domainmeeting.Reference, error) {
	return u.repo.ListReferences(ctx, meetingID)
}

func (u Usecase) CreateReference(ctx context.Context, sourceID uint, input ReferenceInput) (*domainmeeting.Reference, error) {
	var ref domainmeeting.Reference
	if err := u.withTx(ctx, func(repo Repository) error {
		source, found, err := repo.Find(ctx, sourceID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		if sourceID == input.TargetMeetingID {
			return errors.New("meeting cannot reference itself")
		}
		target, found, err := repo.Find(ctx, input.TargetMeetingID)
		if err != nil {
			return err
		}
		if !found {
			return errors.New("target meeting not found")
		}
		ref = domainmeeting.Reference{SourceMeetingID: source.ID, TargetMeetingID: &input.TargetMeetingID, ReferenceType: "meeting", Note: input.Note, TargetTopicSnapshot: target.Topic, TargetSummarySnapshot: target.Summary}
		return repo.CreateReference(ctx, &ref)
	}); err != nil {
		return nil, err
	}
	return &ref, nil
}

func (u Usecase) DeleteReference(ctx context.Context, meetingID uint, referenceID uint) (bool, error) {
	deleted := false
	if err := u.withTx(ctx, func(repo Repository) error {
		ref, found, err := repo.FindReference(ctx, referenceID)
		if err != nil {
			return err
		}
		if !found || ref.SourceMeetingID != meetingID {
			return nil
		}
		deleted = true
		return repo.DeleteReference(ctx, ref)
	}); err != nil {
		return false, err
	}
	return deleted, nil
}

func (u Usecase) ReviewTrustSentence(ctx context.Context, meetingID uint, input TrustReviewInput, reviewer string) (*domainmeeting.Event, bool, error) {
	input = normalizeTrustReviewInput(input)
	if err := validateTrustReviewInput(input); err != nil {
		return nil, true, err
	}
	reviewer = strings.TrimSpace(reviewer)
	if reviewer == "" {
		reviewer = "unknown"
	}
	reviewedAt := time.Now().UTC()
	var event *domainmeeting.Event
	if err := u.withTx(ctx, func(repo Repository) error {
		if _, found, err := repo.Find(ctx, meetingID); err != nil {
			return err
		} else if !found {
			return ErrNotFound
		}
		event = &domainmeeting.Event{
			MeetingID: meetingID,
			Type:      domainkernel.EventSystem,
			Content:   fmt.Sprintf("Trust review recorded for conclusion sentence: %s.", input.Verdict),
			Payload: u.service.JSON(map[string]any{
				"status":             "trust_review",
				"sentence_id":        input.SentenceID,
				"sentence":           input.Sentence,
				"verdict":            input.Verdict,
				"citation_ids":       input.CitationIDs,
				"evidence_event_ids": input.EvidenceEventIDs,
				"comment":            input.Comment,
				"reviewer":           reviewer,
				"reviewed_at":        reviewedAt,
			}),
		}
		return repo.AppendEvent(ctx, event)
	}); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, false, nil
		}
		return nil, true, err
	}
	return event, true, nil
}

func (u Usecase) ReviewRecapAction(ctx context.Context, meetingID uint, input RecapActionReviewInput, reviewer string) (*domainmeeting.Event, bool, error) {
	input = normalizeRecapActionReviewInput(input)
	if err := validateRecapActionReviewInput(input); err != nil {
		return nil, true, err
	}
	if input.Decision == "approved" && !input.Confirm {
		return nil, true, ErrRecapActionReviewConfirmationRequired
	}
	reviewer = strings.TrimSpace(reviewer)
	if reviewer == "" {
		reviewer = "unknown"
	}
	reviewedAt := time.Now().UTC()
	var event *domainmeeting.Event
	if err := u.withTx(ctx, func(repo Repository) error {
		if _, found, err := repo.Find(ctx, meetingID); err != nil {
			return err
		} else if !found {
			return ErrNotFound
		}
		events, err := repo.ListEvents(ctx, meetingID, RepositoryEventFilter{Limit: 500})
		if err != nil {
			return err
		}
		match, found := findRecapActionReviewSuggestion(events, input)
		if !found {
			return fmt.Errorf("%w: suggestion not found", ErrInvalidRecapActionReview)
		}
		event = &domainmeeting.Event{
			MeetingID: meetingID,
			Type:      domainkernel.EventSystem,
			RoleKey:   match.event.RoleKey,
			Content:   fmt.Sprintf("Recap action review recorded: %s.", input.Decision),
			Payload: u.service.JSON(map[string]any{
				"status":                "recap_action_review",
				"suggestion_id":         input.SuggestionID,
				"source_event_id":       match.event.ID,
				"source_sequence":       match.event.Sequence,
				"action_index":          match.actionIndex,
				"action_type":           match.actionType,
				"decision":              input.Decision,
				"execution_disposition": recapActionReviewExecutionDisposition(input.Decision),
				"citation_ids":          input.CitationIDs,
				"evidence_event_ids":    input.EvidenceEventIDs,
				"comment":               input.Comment,
				"reviewer":              reviewer,
				"reviewed_at":           reviewedAt,
				"action_spec":           match.spec,
				"original_status":       match.status,
				"original_policy":       match.policy,
				"original_disposition":  match.disposition,
				"original_reason":       match.reason,
			}),
		}
		return repo.AppendEvent(ctx, event)
	}); err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, false, nil
		}
		return nil, true, err
	}
	return event, true, nil
}

func normalizeTrustReviewInput(input TrustReviewInput) TrustReviewInput {
	input.Sentence = collapseMeetingWhitespace(input.Sentence)
	input.SentenceID = strings.TrimSpace(input.SentenceID)
	input.Verdict = strings.ToLower(strings.TrimSpace(input.Verdict))
	input.Comment = strings.TrimSpace(input.Comment)
	input.CitationIDs = uniqueNonEmptyTrustReviewStrings(input.CitationIDs, 20)
	input.EvidenceEventIDs = uniqueNonZeroTrustReviewIDs(input.EvidenceEventIDs, 20)
	if input.SentenceID == "" && input.Sentence != "" {
		input.SentenceID = "sentence:" + trustReviewSentenceHash(input.Sentence)
	}
	return input
}

func normalizeRecapActionReviewInput(input RecapActionReviewInput) RecapActionReviewInput {
	input.SuggestionID = strings.TrimSpace(input.SuggestionID)
	input.ActionType = strings.ToLower(strings.TrimSpace(input.ActionType))
	input.Decision = strings.ToLower(strings.TrimSpace(input.Decision))
	input.Comment = strings.TrimSpace(input.Comment)
	input.CitationIDs = uniqueNonEmptyTrustReviewStrings(input.CitationIDs, 20)
	input.EvidenceEventIDs = uniqueNonZeroTrustReviewIDs(input.EvidenceEventIDs, 20)
	if sourceEventID, actionIndex, ok := parseRecapActionSuggestionID(input.SuggestionID); ok {
		if input.SourceEventID == 0 {
			input.SourceEventID = sourceEventID
		}
		if input.ActionIndex == nil {
			input.ActionIndex = &actionIndex
		}
	}
	if input.SuggestionID == "" && input.SourceEventID > 0 && input.ActionIndex != nil {
		input.SuggestionID = recapActionSuggestionID(input.SourceEventID, *input.ActionIndex)
	}
	return input
}

func validateTrustReviewInput(input TrustReviewInput) error {
	if input.Sentence == "" {
		return fmt.Errorf("%w: sentence is required", ErrInvalidTrustReview)
	}
	switch input.Verdict {
	case "confirmed", "needs_evidence", "rejected", "superseded":
		return nil
	default:
		return fmt.Errorf("%w: verdict must be confirmed, needs_evidence, rejected, or superseded", ErrInvalidTrustReview)
	}
}

func validateRecapActionReviewInput(input RecapActionReviewInput) error {
	if input.SourceEventID == 0 {
		return fmt.Errorf("%w: sourceEventId or suggestionId is required", ErrInvalidRecapActionReview)
	}
	if input.ActionIndex == nil || *input.ActionIndex < 0 {
		return fmt.Errorf("%w: actionIndex is required", ErrInvalidRecapActionReview)
	}
	switch input.Decision {
	case "approved", "needs_evidence", "rejected", "superseded":
		return nil
	default:
		return fmt.Errorf("%w: decision must be approved, needs_evidence, rejected, or superseded", ErrInvalidRecapActionReview)
	}
}

func trustReviewSentenceHash(sentence string) string {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte(strings.ToLower(collapseMeetingWhitespace(sentence))))
	return strconv.FormatUint(hash.Sum64(), 36)
}

func collapseMeetingWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func uniqueNonEmptyTrustReviewStrings(values []string, limit int) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func uniqueNonZeroTrustReviewIDs(values []uint, limit int) []uint {
	seen := map[uint]struct{}{}
	out := []uint{}
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

type recapActionReviewSuggestion struct {
	event       domainmeeting.Event
	actionIndex int
	actionType  string
	spec        map[string]any
	status      string
	policy      string
	disposition string
	reason      string
}

func findRecapActionReviewSuggestion(events []domainmeeting.Event, input RecapActionReviewInput) (recapActionReviewSuggestion, bool) {
	for _, event := range events {
		if input.SourceEventID > 0 && event.ID != input.SourceEventID {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal(event.Payload, &payload); err != nil || payload == nil {
			continue
		}
		status := strings.TrimSpace(fmt.Sprint(payload["status"]))
		if status != "recap_actions_blocked" && status != "recap_actions_review_required" {
			continue
		}
		items := recapActionReviewAnyList(payload["suggested_actions"])
		for i, item := range items {
			raw := recapActionReviewMap(item)
			actionIndex := i
			if rawIndex, ok := recapActionReviewUint(raw["index"]); ok {
				actionIndex = int(rawIndex)
			}
			if input.ActionIndex != nil && actionIndex != *input.ActionIndex {
				continue
			}
			actionType := strings.ToLower(strings.TrimSpace(firstNonEmptyReviewString(raw["action_type"], raw["actionType"])))
			if input.ActionType != "" && actionType != input.ActionType {
				continue
			}
			return recapActionReviewSuggestion{
				event:       event,
				actionIndex: actionIndex,
				actionType:  actionType,
				spec:        recapActionReviewMap(raw["spec"]),
				status:      status,
				policy:      strings.TrimSpace(fmt.Sprint(payload["policy"])),
				disposition: firstNonEmptyReviewString(raw["disposition"], payload["disposition"]),
				reason:      firstNonEmptyReviewString(raw["reason"], payload["reason"]),
			}, true
		}
	}
	return recapActionReviewSuggestion{}, false
}

func parseRecapActionSuggestionID(id string) (uint, int, bool) {
	left, right, ok := strings.Cut(strings.TrimSpace(id), ":recap-action:")
	if !ok {
		return 0, 0, false
	}
	eventID, err := strconv.ParseUint(strings.TrimSpace(left), 10, 64)
	if err != nil || eventID == 0 {
		return 0, 0, false
	}
	ordinal, err := strconv.ParseInt(strings.TrimSpace(right), 10, 64)
	if err != nil || ordinal <= 0 {
		return 0, 0, false
	}
	return uint(eventID), int(ordinal - 1), true
}

func recapActionSuggestionID(sourceEventID uint, actionIndex int) string {
	return strconv.FormatUint(uint64(sourceEventID), 10) + ":recap-action:" + strconv.Itoa(actionIndex+1)
}

func recapActionReviewExecutionDisposition(decision string) string {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "approved":
		return "manual_execution_required"
	case "needs_evidence":
		return "needs_evidence"
	case "rejected":
		return "rejected"
	case "superseded":
		return "superseded"
	default:
		return "reviewed"
	}
}

func recapActionReviewAnyList(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func recapActionReviewMap(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	default:
		return map[string]any{}
	}
}

func recapActionReviewUint(value any) (uint, bool) {
	switch typed := value.(type) {
	case uint:
		return typed, true
	case uint64:
		return uint(typed), true
	case int:
		if typed >= 0 {
			return uint(typed), true
		}
	case int64:
		if typed >= 0 {
			return uint(typed), true
		}
	case float64:
		if typed >= 0 {
			return uint(typed), true
		}
	case json.Number:
		parsed, err := typed.Int64()
		if err == nil && parsed >= 0 {
			return uint(parsed), true
		}
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		if err == nil {
			return uint(parsed), true
		}
	}
	return 0, false
}

func firstNonEmptyReviewString(values ...any) string {
	for _, value := range values {
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

func (u Usecase) cancelMeeting(ctx context.Context, repo Repository, meeting *domainmeeting.Meeting, reason string) error {
	if IsTerminalStatus(meeting.Status) {
		return nil
	}
	now := time.Now()
	meeting.Status = domainkernel.MeetingCancelled
	meeting.CompletedAt = &now
	meeting.HeartbeatAt = &now
	meeting.RunID = nil
	meeting.Conclusion = &reason
	if err := repo.Save(ctx, meeting); err != nil {
		return err
	}
	return repo.AppendEvent(ctx, &domainmeeting.Event{
		MeetingID: meeting.ID,
		Type:      domainkernel.EventSystem,
		Content:   reason,
		Payload:   u.service.JSON(map[string]any{"status": "cancelled"}),
	})
}

func (u Usecase) withTx(ctx context.Context, fn func(Repository) error) error {
	if u.tx != nil {
		return u.tx.WithTx(ctx, fn)
	}
	return errors.New("meeting unit of work is not configured")
}

func TagsFromJSON(raw []byte) []string {
	return domainTagsFromJSON(raw)
}

func domainTagsFromJSON(raw []byte) []string {
	var values []string
	if len(raw) == 0 {
		return values
	}
	_ = json.Unmarshal(raw, &values)
	return values
}
