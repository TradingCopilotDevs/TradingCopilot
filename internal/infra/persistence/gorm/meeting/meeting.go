package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	appmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/app/meeting"

	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	"strings"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/ai/client"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"gorm.io/gorm"
)

func AppendEvent(db *gorm.DB, meetingID uint, eventType domainkernel.MeetingEventType, roleKey *string, content string, payload map[string]any) (*domainmeeting.Event, error) {
	event := domainmeeting.Event{MeetingID: meetingID, Type: eventType, RoleKey: roleKey, Content: content, Payload: JSON(payload)}
	if err := gormrepo.NewMeetingRepository(db).AppendEvent(dbContext(db), &event); err != nil {
		return nil, err
	}
	return &event, nil
}

func RunMeetingOnce(db *gorm.DB, meetingID uint) error {
	return RunMeetingOnceWithContext(dbContext(db), db, meetingID)
}

func RunMeetingOnceWithContext(ctx context.Context, db *gorm.DB, meetingID uint) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var meetingRow persistmodel.Meeting
	if err := db.First(&meetingRow, meetingID).Error; err != nil {
		return err
	}
	meeting := meetingFromModel(meetingRow)
	if appmeeting.IsTerminalStatus(meeting.Status) {
		return nil
	}
	startPlan := appmeeting.NewRunStartPlan(meetingID, appmeeting.DefaultRunnerName, time.Now())
	runID := startPlan.RunID
	appmeeting.ApplyRunStart(&meeting, startPlan)
	if err := saveMeeting(db, &meeting); err != nil {
		return err
	}
	_, _ = AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, appmeeting.RunStartedEventContent, appmeeting.RunStartedEventPayload(startPlan))
	if err := ensureActiveMeetingRun(db, meeting.ID, runID); err != nil {
		return nil
	}

	var roleRows []persistmodel.ResearchTeamRole
	db.Preload("Provider.APIKeySecret").Where("research_team_id = ? AND enabled = ?", meeting.ResearchTeamID, true).Order("sort_order, id").Find(&roleRows)
	roles := researchTeamRolesFromModel(roleRows)
	if len(roles) == 0 && !hasModeratorRecapEvent(db, meeting.ID) {
		_ = failActiveMeetingRun(db, meeting.ID, runID, "No enabled AI roles are available for the meeting.")
		return nil
	}
	if shouldUseManagedMeetingRunner(roles) {
		if err := runManagedMeetingFlow(ctx, db, &meeting, roles, runID); err != nil {
			if errors.Is(err, errMeetingRunSuperseded) || errors.Is(err, context.Canceled) {
				return nil
			}
			var activeFailure activeMeetingFailure
			reason := "Meeting run crashed after start: " + err.Error()
			if errors.As(err, &activeFailure) {
				reason = activeFailure.reason
			}
			_ = failActiveMeetingRun(db, meeting.ID, runID, reason)
			return nil
		}
		return nil
	}
	for _, role := range roles {
		if err := ctx.Err(); err != nil {
			return err
		}
		if db.Select("status").First(&meetingRow, meeting.ID).Error == nil {
			meeting.Status = meetingRow.Status
		}
		if meeting.Status == domainkernel.MeetingCancelled {
			_, _ = AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, "Meeting runner stopped after cancellation.", map[string]any{"status": "cancelled"})
			return nil
		}
		key := role.Key
		content, payload := runRoleAnalysisWithToolsWithContext(ctx, db, meeting, role)
		if err := ensureActiveMeetingRun(db, meeting.ID, runID); err != nil {
			return nil
		}
		_, _ = AppendEvent(db, meeting.ID, domainkernel.EventRoleMessage, &key, content, payload)
	}
	if db.Select("status").First(&meetingRow, meeting.ID).Error == nil {
		meeting.Status = meetingRow.Status
	}
	if meeting.Status == domainkernel.MeetingCancelled {
		_, _ = AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, "Meeting runner stopped before completion because it was cancelled.", map[string]any{"status": "cancelled"})
		return nil
	}
	if err := ensureActiveMeetingRun(db, meeting.ID, runID); err != nil {
		return nil
	}
	defaults := appmeeting.LegacyCompletionDefaults()
	summary := defaults.Summary
	conclusion := defaults.Conclusion
	if recap, applied, err := TryApplyModeratorRecapFromEvents(db, &meeting); err == nil && applied {
		if value := strings.TrimSpace(stringFromAny(recap["summary"])); value != "" {
			summary = value
		}
		if value := strings.TrimSpace(stringFromAny(recap["conclusion"])); value != "" {
			conclusion = value
		}
		if value := strings.TrimSpace(stringFromAny(recap["topic"])); value != "" {
			meeting.Topic = value
		}
		if tags := anyList(recap["tags"]); len(tags) > 0 {
			meeting.Tags = JSON(sanitizeStringList(tags, 12))
		}
	} else if err != nil {
		_, _ = AppendEvent(db, meeting.ID, domainkernel.EventError, nil, "Moderator recap action application failed: "+err.Error(), map[string]any{"status": "recap_action_error"})
	}
	done := time.Now()
	refreshMeetingTokenBudget(db, &meeting)
	completed, err := gormrepo.NewMeetingRepository(db).CompleteRunning(dbContext(db), &meeting, runID, summary, conclusion, done)
	if err != nil || !completed {
		return nil
	}
	_, _ = AppendEvent(db, meeting.ID, domainkernel.EventConclusion, nil, conclusion, map[string]any{"status": "completed"})
	settings := config.Load()
	NotifyMeetingFinished(db, &meeting, settings, security.New(settings))
	return nil
}

func runRoleAnalysis(db *gorm.DB, meeting domainmeeting.Meeting, role domainai.AgentRole) (string, map[string]any) {
	if role.ProviderID == nil {
		return fmt.Sprintf("%s skipped; AI Provider is not configured.", role.Name), map[string]any{"status": "role_skipped", "reason": "provider_not_configured"}
	}
	var providerRow persistmodel.AiProvider
	if err := db.Preload("APIKeySecret").First(&providerRow, *role.ProviderID).Error; err != nil {
		return fmt.Sprintf("%s skipped; provider is unavailable or missing API key.", role.Name), map[string]any{"status": "role_skipped", "reason": "provider_not_ready"}
	}
	provider := aiProviderFromModel(providerRow)
	if !provider.Enabled || provider.APIKeySecret == nil {
		return fmt.Sprintf("%s skipped; provider is unavailable or missing API key.", role.Name), map[string]any{"status": "role_skipped", "reason": "provider_not_ready"}
	}
	settings := config.Load()
	client := ai.Client{Provider: provider, Security: security.New(settings), Settings: settings, HTTPClient: runtimeproxy.NewHTTPClient(db, settings, runtimeproxy.ModuleAI, settings.AIChatTimeout)}
	model := ""
	if role.Model != nil {
		model = *role.Model
	}
	messages := []map[string]string{
		{"role": "system", "content": role.PromptTemplate},
		{"role": "user", "content": "Meeting topic: " + meeting.Topic + "\nProvide a concise, verifiable response with risk boundaries for your role."},
	}
	content, err := client.Chat(messages, model)
	if err != nil {
		return fmt.Sprintf("%s AI provider call failed: %v", role.Name, err), map[string]any{"status": "role_error", "error": err.Error()}
	}
	roleForSnapshot := role
	roleForSnapshot.Provider = &provider
	modelName := firstNonEmptyString(model, provider.DefaultModel)
	return content, map[string]any{
		"status": "role_completed", "provider_id": provider.ID, "provider_name": provider.Name, "model": modelName,
		"prompt_version": legacyMeetingPromptVersion, "prompt_snapshot": rolePromptSnapshot(roleForSnapshot, modelName, messages, tokenLabel(role.Key, "legacy"), legacyMeetingPromptVersion),
	}
}

func hasModeratorRecapEvent(db *gorm.DB, meetingID uint) bool {
	return gormrepo.NewMeetingRepository(db).HasModeratorRecapEvent(dbContext(db), meetingID)
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func CancelMeeting(db *gorm.DB, meeting *domainmeeting.Meeting, reason string) error {
	if appmeeting.IsTerminalStatus(meeting.Status) {
		return nil
	}
	now := time.Now()
	meeting.Status = domainkernel.MeetingCancelled
	meeting.CompletedAt = &now
	meeting.HeartbeatAt = &now
	meeting.RunID = nil
	meeting.Conclusion = &reason
	if err := saveMeeting(db, meeting); err != nil {
		return err
	}
	_, err := AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, reason, map[string]any{"status": "cancelled"})
	return err
}

func RecapMeeting(db *gorm.DB, meeting *domainmeeting.Meeting, requestedBy string) error {
	var eventRows []persistmodel.MeetingEvent
	if err := db.Where("meeting_id = ?", meeting.ID).Order("sequence").Find(&eventRows).Error; err != nil {
		return err
	}
	events := meetingEventsFromModel(eventRows)
	roleTurns := 0
	conclusion := ""
	for _, event := range events {
		if event.Type == domainkernel.EventRoleMessage {
			roleTurns++
		}
		if event.Type == domainkernel.EventConclusion && strings.TrimSpace(event.Content) != "" {
			conclusion = event.Content
		}
	}
	if conclusion == "" && meeting.Conclusion != nil {
		conclusion = *meeting.Conclusion
	}
	if conclusion == "" {
		conclusion = "No conclusion has been generated yet."
	}
	summary := fmt.Sprintf("Recap generated from %d events and %d role turns.", len(events), roleTurns)
	now := time.Now()
	meeting.Summary = &summary
	meeting.Conclusion = &conclusion
	meeting.RecapStatus = strPtr("completed")
	meeting.RecapUpdatedAt = &now
	if err := saveMeeting(db, meeting); err != nil {
		return err
	}
	payload := map[string]any{"status": "recap_completed", "event_count": len(events), "role_turn_count": roleTurns}
	if requestedBy != "" {
		payload["requested_by"] = requestedBy
	}
	_, err := AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, "Meeting recap regenerated.", payload)
	return err
}

func CloneMeetingContextAndReferences(db *gorm.DB, sourceMeetingID uint, targetMeetingID uint) (map[string]int, error) {
	return gormrepo.NewMeetingRepository(db).CloneContextAndReferences(dbContext(db), sourceMeetingID, targetMeetingID)
}

func DeleteMeetingGraph(db *gorm.DB, meetingID uint) error {
	return gormrepo.NewMeetingRepository(db).DeleteGraph(dbContext(db), meetingID)
}

func TagsFromJSON(raw []byte) []string {
	var tags []string
	_ = json.Unmarshal(raw, &tags)
	if tags == nil {
		return []string{}
	}
	return tags
}

func strPtr(v string) *string { return &v }
