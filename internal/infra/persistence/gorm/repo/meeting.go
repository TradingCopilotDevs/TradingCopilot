package repo

import (
	"context"
	"encoding/json"
	"errors"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	"time"

	appmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/app/meeting"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type MeetingRepository struct {
	db *gorm.DB
}

func NewMeetingRepository(db *gorm.DB) MeetingRepository {
	return MeetingRepository{db: db}
}

func (r MeetingRepository) List(ctx context.Context, filter appmeeting.RepositoryListFilter) ([]domainmeeting.Meeting, error) {
	var rows []persistmodel.Meeting
	q := r.db.WithContext(ctx).Order("created_at desc, id desc")
	if filter.ResearchTeamID != "" {
		q = q.Where("research_team_id = ?", filter.ResearchTeamID)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if filter.TriggerSource != "" {
		q = q.Where("trigger_source = ?", filter.TriggerSource)
	}
	if filter.CursorID > 0 {
		q = q.Where("id < ?", filter.CursorID)
	}
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return meetingsToDomain(rows), nil
}

func (r MeetingRepository) ResearchTeamReady(ctx context.Context, teamID uint) (bool, string, error) {
	var team persistmodel.ResearchTeam
	if err := r.db.WithContext(ctx).First(&team, teamID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, "research team not found", nil
		}
		return false, "", err
	}
	if !team.Active {
		return false, "research team is disabled", nil
	}
	var moderatorCount int64
	if err := r.db.WithContext(ctx).Model(&persistmodel.ResearchTeamRole{}).
		Where("research_team_id = ? AND key = ? AND enabled = ?", teamID, "moderator", true).
		Count(&moderatorCount).Error; err != nil {
		return false, "", err
	}
	if moderatorCount == 0 {
		return false, "research team must have an enabled moderator role", nil
	}
	var participantCount int64
	if err := r.db.WithContext(ctx).Model(&persistmodel.ResearchTeamRole{}).
		Where("research_team_id = ? AND key <> ? AND enabled = ?", teamID, "moderator", true).
		Count(&participantCount).Error; err != nil {
		return false, "", err
	}
	if participantCount == 0 {
		return false, "research team must have at least one enabled participant role", nil
	}
	return true, "", nil
}

func (r MeetingRepository) Find(ctx context.Context, id uint) (*domainmeeting.Meeting, bool, error) {
	var row persistmodel.Meeting
	err := r.db.WithContext(ctx).First(&row, id).Error
	if err == nil {
		out := meetingFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r MeetingRepository) Save(ctx context.Context, meeting *domainmeeting.Meeting) error {
	row := meetingToModel(*meeting)
	if err := r.db.WithContext(ctx).Save(&row).Error; err != nil {
		return err
	}
	*meeting = meetingFromModel(row)
	return nil
}

func (r MeetingRepository) Create(ctx context.Context, meeting *domainmeeting.Meeting) error {
	row := meetingToModel(*meeting)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*meeting = meetingFromModel(row)
	return nil
}

func (r MeetingRepository) ListEvents(ctx context.Context, meetingID uint, filter appmeeting.RepositoryEventFilter) ([]domainmeeting.Event, error) {
	var rows []persistmodel.MeetingEvent
	q := r.db.WithContext(ctx).Where("meeting_id = ?", meetingID)
	if filter.CursorID > 0 {
		q = q.Where("id < ?", filter.CursorID)
	}
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if err := q.Order("id desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return meetingEventsToDomain(rows), nil
}

func (r MeetingRepository) AppendEvent(ctx context.Context, event *domainmeeting.Event) error {
	db := r.db.WithContext(ctx)
	var maxSeq int
	if err := db.Model(&persistmodel.MeetingEvent{}).
		Where("meeting_id = ?", event.MeetingID).
		Select("COALESCE(MAX(sequence), 0)").
		Scan(&maxSeq).Error; err != nil {
		return err
	}
	event.Sequence = maxSeq + 1
	row := meetingEventToModel(*event)
	if err := db.Create(&row).Error; err != nil {
		return err
	}
	*event = meetingEventFromModel(row)
	return nil
}

func (r MeetingRepository) ListReferences(ctx context.Context, meetingID uint) ([]domainmeeting.Reference, error) {
	var rows []persistmodel.MeetingReference
	if err := r.db.WithContext(ctx).Where("source_meeting_id = ?", meetingID).Order("created_at desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return meetingReferencesToDomain(rows), nil
}

func (r MeetingRepository) CreateReference(ctx context.Context, ref *domainmeeting.Reference) error {
	row := meetingReferenceToModel(*ref)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*ref = meetingReferenceFromModel(row)
	return nil
}

func (r MeetingRepository) FindReference(ctx context.Context, id uint) (*domainmeeting.Reference, bool, error) {
	var row persistmodel.MeetingReference
	err := r.db.WithContext(ctx).First(&row, id).Error
	if err == nil {
		out := meetingReferenceFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r MeetingRepository) DeleteReference(ctx context.Context, ref *domainmeeting.Reference) error {
	row := meetingReferenceToModel(*ref)
	return r.db.WithContext(ctx).Delete(&row).Error
}

func (r MeetingRepository) CloneContextAndReferences(ctx context.Context, sourceMeetingID uint, targetMeetingID uint) (map[string]int, error) {
	result := map[string]int{"events": 0, "references": 0}
	var eventRows []persistmodel.MeetingEvent
	if err := r.db.WithContext(ctx).Where("meeting_id = ? AND type = ?", sourceMeetingID, domainkernel.EventSystem).Order("sequence").Find(&eventRows).Error; err != nil {
		return result, err
	}
	events := meetingEventsToDomain(eventRows)
	for _, event := range events {
		var payload map[string]any
		_ = json.Unmarshal(event.Payload, &payload)
		status, _ := payload["status"].(string)
		if status != "telegram_triggered" && status != "telegram_bot_triggered" && status != "meeting_context" {
			continue
		}
		if payload == nil {
			payload = map[string]any{}
		}
		payload["restarted_from_meeting_id"] = sourceMeetingID
		clone := domainmeeting.Event{MeetingID: targetMeetingID, Type: event.Type, RoleKey: event.RoleKey, Content: event.Content, Payload: domainkernel.NewJSON(payload)}
		if err := r.AppendEvent(ctx, &clone); err != nil {
			return result, err
		}
		result["events"]++
	}
	refs, err := r.ListReferences(ctx, sourceMeetingID)
	if err != nil {
		return result, err
	}
	for _, ref := range refs {
		clone := domainmeeting.Reference{
			SourceMeetingID:       targetMeetingID,
			TargetMeetingID:       ref.TargetMeetingID,
			ReferenceType:         ref.ReferenceType,
			Note:                  ref.Note,
			TargetTopicSnapshot:   ref.TargetTopicSnapshot,
			TargetSummarySnapshot: ref.TargetSummarySnapshot,
			TargetDeleted:         ref.TargetDeleted,
			ExternalRef:           ref.ExternalRef,
		}
		if err := r.CreateReference(ctx, &clone); err != nil {
			return result, err
		}
		result["references"]++
	}
	return result, nil
}

func (r MeetingRepository) DeleteGraph(ctx context.Context, meetingID uint) error {
	db := r.db.WithContext(ctx)
	if err := db.Where("meeting_id = ?", meetingID).Delete(&persistmodel.WakePlan{}).Error; err != nil {
		return err
	}
	if err := db.Where("meeting_id = ?", meetingID).Delete(&persistmodel.ToolCallLog{}).Error; err != nil {
		return err
	}
	if err := db.Where("source_meeting_id = ?", meetingID).Delete(&persistmodel.MeetingReference{}).Error; err != nil {
		return err
	}
	if err := db.Model(&persistmodel.MeetingReference{}).Where("target_meeting_id = ?", meetingID).Updates(map[string]any{"target_meeting_id": nil, "target_deleted": true}).Error; err != nil {
		return err
	}
	if err := db.Model(&persistmodel.PaperOrder{}).Where("meeting_id = ?", meetingID).Updates(map[string]any{"meeting_id": nil, "source_meeting_event_id": nil}).Error; err != nil {
		return err
	}
	if err := db.Model(&persistmodel.PredictionWatchlistItem{}).Where("source_meeting_id = ?", meetingID).Updates(map[string]any{"source_meeting_id": nil, "source_meeting_event_id": nil, "source_role_key": nil}).Error; err != nil {
		return err
	}
	if err := db.Where("meeting_id = ?", meetingID).Delete(&persistmodel.MeetingEvent{}).Error; err != nil {
		return err
	}
	return db.Delete(&persistmodel.Meeting{}, meetingID).Error
}

func (r MeetingRepository) AdjustTokenBudget(ctx context.Context, meetingID uint, delta int) error {
	if delta == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Model(&persistmodel.Meeting{}).
		Where("id = ?", meetingID).
		UpdateColumn("token_budget", gorm.Expr("COALESCE(token_budget, 0) + ?", delta)).Error
}

func (r MeetingRepository) DailyTokenUsage(ctx context.Context, at time.Time) (int, error) {
	if at.IsZero() {
		at = time.Now()
	}
	start := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
	var total int
	err := r.db.WithContext(ctx).Model(&persistmodel.Meeting{}).
		Where("created_at >= ?", start).
		Select("COALESCE(SUM(token_budget), 0)").
		Scan(&total).Error
	return total, err
}

func (r MeetingRepository) CompleteRunning(ctx context.Context, meeting *domainmeeting.Meeting, runID string, summary string, conclusion string, completedAt time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&persistmodel.Meeting{}).
		Where("id = ? AND status = ? AND run_id = ?", meeting.ID, domainkernel.MeetingRunning, runID).
		Updates(map[string]any{
			"topic":            meeting.Topic,
			"tags":             meeting.Tags,
			"status":           domainkernel.MeetingCompleted,
			"summary":          summary,
			"conclusion":       conclusion,
			"completed_at":     completedAt,
			"heartbeat_at":     completedAt,
			"run_id":           nil,
			"recap_status":     "completed",
			"recap_updated_at": completedAt,
		})
	if result.Error != nil || result.RowsAffected == 0 {
		return false, result.Error
	}
	meeting.Status = domainkernel.MeetingCompleted
	meeting.Summary = &summary
	meeting.Conclusion = &conclusion
	meeting.CompletedAt = &completedAt
	meeting.HeartbeatAt = &completedAt
	meeting.RunID = nil
	meeting.RecapStatus = strPtr("completed")
	meeting.RecapUpdatedAt = &completedAt
	return true, nil
}

func (r MeetingRepository) HasModeratorRecapEvent(ctx context.Context, meetingID uint) bool {
	var count int64
	r.db.WithContext(ctx).Model(&persistmodel.MeetingEvent{}).
		Where("meeting_id = ? AND type = ? AND role_key = ?", meetingID, domainkernel.EventRoleMessage, "moderator").
		Count(&count)
	return count > 0
}

func (r MeetingRepository) CreateToolCallLog(ctx context.Context, meetingID *uint, roleKey *string, toolName string, arguments domainkernel.JSON, resultPreview domainkernel.JSON) error {
	return r.db.WithContext(ctx).Create(&persistmodel.ToolCallLog{
		MeetingID:     meetingID,
		RoleKey:       roleKey,
		ToolName:      toolName,
		Arguments:     datatypes.JSON(arguments),
		ResultPreview: datatypes.JSON(resultPreview),
	}).Error
}

func (r MeetingRepository) MarkRecapRunning(ctx context.Context, meetingID uint, runID string, at time.Time) error {
	result := r.db.WithContext(ctx).Model(&persistmodel.Meeting{}).
		Where("id = ? AND status = ? AND run_id = ?", meetingID, domainkernel.MeetingRunning, runID).
		Updates(map[string]any{"recap_status": "running", "recap_updated_at": at, "heartbeat_at": at})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return appmeeting.ErrNotFound
	}
	return nil
}

func (r MeetingRepository) TouchActiveRun(ctx context.Context, meetingID uint, runID string, at time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&persistmodel.Meeting{}).
		Where("id = ? AND status = ? AND run_id = ?", meetingID, domainkernel.MeetingRunning, runID).
		Update("heartbeat_at", at)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func (r MeetingRepository) FailActiveRun(ctx context.Context, meetingID uint, runID string, reason string, at time.Time) (bool, error) {
	result := r.db.WithContext(ctx).Model(&persistmodel.Meeting{}).
		Where("id = ? AND status = ? AND run_id = ?", meetingID, domainkernel.MeetingRunning, runID).
		Updates(map[string]any{"status": domainkernel.MeetingFailed, "completed_at": at, "heartbeat_at": at, "run_id": nil, "conclusion": reason})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

func meetingsToDomain(rows []persistmodel.Meeting) []domainmeeting.Meeting {
	out := make([]domainmeeting.Meeting, 0, len(rows))
	for _, row := range rows {
		out = append(out, meetingFromModel(row))
	}
	return out
}

func meetingFromModel(row persistmodel.Meeting) domainmeeting.Meeting {
	return domainmeeting.Meeting{
		ID:               row.ID,
		ResearchTeamID:   row.ResearchTeamID,
		Topic:            row.Topic,
		Status:           row.Status,
		TriggerSource:    row.TriggerSource,
		Summary:          row.Summary,
		Conclusion:       row.Conclusion,
		Tags:             domainkernel.JSON(row.Tags),
		RecapStatus:      row.RecapStatus,
		RecapUpdatedAt:   row.RecapUpdatedAt,
		TokenBudget:      row.TokenBudget,
		RunID:            row.RunID,
		RunAttempt:       row.RunAttempt,
		HeartbeatAt:      row.HeartbeatAt,
		AutoRequeueCount: row.AutoRequeueCount,
		StartedAt:        row.StartedAt,
		CompletedAt:      row.CompletedAt,
		CreatedAt:        row.CreatedAt,
	}
}

func meetingToModel(row domainmeeting.Meeting) persistmodel.Meeting {
	return persistmodel.Meeting{
		ID:               row.ID,
		ResearchTeamID:   row.ResearchTeamID,
		Topic:            row.Topic,
		Status:           row.Status,
		TriggerSource:    row.TriggerSource,
		Summary:          row.Summary,
		Conclusion:       row.Conclusion,
		Tags:             datatypes.JSON(row.Tags),
		RecapStatus:      row.RecapStatus,
		RecapUpdatedAt:   row.RecapUpdatedAt,
		TokenBudget:      row.TokenBudget,
		RunID:            row.RunID,
		RunAttempt:       row.RunAttempt,
		HeartbeatAt:      row.HeartbeatAt,
		AutoRequeueCount: row.AutoRequeueCount,
		StartedAt:        row.StartedAt,
		CompletedAt:      row.CompletedAt,
		CreatedAt:        row.CreatedAt,
	}
}

func meetingEventsToDomain(rows []persistmodel.MeetingEvent) []domainmeeting.Event {
	out := make([]domainmeeting.Event, 0, len(rows))
	for _, row := range rows {
		out = append(out, meetingEventFromModel(row))
	}
	return out
}

func meetingEventFromModel(row persistmodel.MeetingEvent) domainmeeting.Event {
	return domainmeeting.Event{
		ID:        row.ID,
		MeetingID: row.MeetingID,
		Sequence:  row.Sequence,
		Type:      row.Type,
		RoleKey:   row.RoleKey,
		Content:   row.Content,
		Payload:   domainkernel.JSON(row.Payload),
		CreatedAt: row.CreatedAt,
	}
}

func meetingEventToModel(row domainmeeting.Event) persistmodel.MeetingEvent {
	return persistmodel.MeetingEvent{
		ID:        row.ID,
		MeetingID: row.MeetingID,
		Sequence:  row.Sequence,
		Type:      row.Type,
		RoleKey:   row.RoleKey,
		Content:   row.Content,
		Payload:   datatypes.JSON(row.Payload),
		CreatedAt: row.CreatedAt,
	}
}

func meetingReferencesToDomain(rows []persistmodel.MeetingReference) []domainmeeting.Reference {
	out := make([]domainmeeting.Reference, 0, len(rows))
	for _, row := range rows {
		out = append(out, meetingReferenceFromModel(row))
	}
	return out
}

func meetingReferenceFromModel(row persistmodel.MeetingReference) domainmeeting.Reference {
	return domainmeeting.Reference{
		ID:                    row.ID,
		SourceMeetingID:       row.SourceMeetingID,
		TargetMeetingID:       row.TargetMeetingID,
		ReferenceType:         row.ReferenceType,
		Note:                  row.Note,
		TargetTopicSnapshot:   row.TargetTopicSnapshot,
		TargetSummarySnapshot: row.TargetSummarySnapshot,
		TargetDeleted:         row.TargetDeleted,
		ExternalRef:           row.ExternalRef,
		CreatedAt:             row.CreatedAt,
	}
}

func meetingReferenceToModel(row domainmeeting.Reference) persistmodel.MeetingReference {
	return persistmodel.MeetingReference{
		ID:                    row.ID,
		SourceMeetingID:       row.SourceMeetingID,
		TargetMeetingID:       row.TargetMeetingID,
		ReferenceType:         row.ReferenceType,
		Note:                  row.Note,
		TargetTopicSnapshot:   row.TargetTopicSnapshot,
		TargetSummarySnapshot: row.TargetSummarySnapshot,
		TargetDeleted:         row.TargetDeleted,
		ExternalRef:           row.ExternalRef,
		CreatedAt:             row.CreatedAt,
	}
}

func strPtr(v string) *string { return &v }
