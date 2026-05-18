package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/meeting"
	"strconv"
	"strings"
	"time"
)

var ErrNotFound = errors.New("meeting not found")

type Usecase struct {
	repo     Repository
	service  Service
	settings Settings
	tx       Transactor
	enqueue  EnqueueMeeting
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

type RecoverySettings struct {
	MeetingStaleAfter       time.Duration
	MeetingAutoRequeueLimit int
}

func (u Usecase) WithMeetingEnqueuer(enqueue EnqueueMeeting) Usecase {
	u.enqueue = enqueue
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
		if u.enqueue == nil {
			return "", errors.New("meeting enqueuer is not configured")
		}
		if err := u.enqueue(meeting.ID); err != nil {
			return "", err
		}
		runner = "redis"
		if message == "" {
			message = "Meeting job submitted to Redis worker."
		}
	case "auto":
		if u.enqueue != nil && enqueueMeetingWithTimeout(u.enqueue, meeting.ID, 800*time.Millisecond) == nil {
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

func enqueueMeetingWithTimeout(enqueue EnqueueMeeting, meetingID uint, timeout time.Duration) error {
	result := make(chan error, 1)
	go func() { result <- enqueue(meetingID) }()
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

func (u Usecase) cancelMeeting(ctx context.Context, repo Repository, meeting *domainmeeting.Meeting, reason string) error {
	if meeting.Status == domainkernel.MeetingCompleted || meeting.Status == domainkernel.MeetingFailed || meeting.Status == domainkernel.MeetingCancelled {
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
