package meeting

import (
	"context"
	"errors"
	"fmt"

	domainai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/ai"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/meeting"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	"gorm.io/gorm"
)

func runManagedMeetingFlow(ctx context.Context, db *gorm.DB, meeting *domainmeeting.Meeting, roles []domainai.AgentRole, runID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	settings := config.Load()
	maxRounds := meetingMaxRounds(db, settings)
	moderator := findRole(roles, "moderator")
	if moderator == nil {
		return errors.New("moderator role is missing")
	}
	moderatorModel := modelForRole(*moderator)
	if !roleProviderReady(*moderator) || moderatorModel == "" {
		return errors.New("moderator role is missing provider, model, or API key")
	}
	participants := make([]domainai.AgentRole, 0, len(roles))
	for _, role := range roles {
		if role.Key != "moderator" {
			participants = append(participants, role)
		}
	}
	if len(participants) == 0 {
		return errors.New("no participant roles are enabled for the meeting")
	}

	validRoleKeys := map[string]struct{}{}
	for _, role := range roles {
		validRoleKeys[role.Key] = struct{}{}
	}
	priorDiscussion := []map[string]any{}
	pendingQuestions := []map[string]string{}
	turnCounter := 0
	estimatedTotalTurns := maxRounds*max(len(participants), 1) + maxRounds + 1

	kickoff, err := runModeratorPlan(ctx, db, *meeting, *moderator, moderatorModel, roles, priorDiscussion, pendingQuestions, 1, true)
	if err != nil {
		return err
	}
	if err := ensureActiveMeetingRun(db, meeting.ID, runID); err != nil {
		return err
	}
	_, _ = AppendEvent(db, meeting.ID, domainkernel.EventRoleMessage, &moderator.Key, kickoff.Content, map[string]any{
		"status": "moderator_kickoff", "role_name": moderator.Name, "round": 1,
		"questions": kickoff.Questions, "focus_roles": kickoff.FocusRoles, "raw_json": kickoff.Raw,
	})
	priorDiscussion = append(priorDiscussion, map[string]any{"round": 1, "role_key": moderator.Key, "content": kickoff.Content})
	pendingQuestions = kickoff.Questions

	for roundNumber := 1; roundNumber <= maxRounds; roundNumber++ {
		if err := ensureActiveMeetingRun(db, meeting.ID, runID); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if isMeetingCancelled(db, meeting.ID) {
			return errMeetingRunSuperseded
		}
		_, _ = AppendEvent(db, meeting.ID, domainkernel.EventSystem, nil, fmt.Sprintf("Discussion round %d started.", roundNumber), map[string]any{
			"status": "discussion_round_started", "progress": map[string]any{"current": roundNumber, "total": maxRounds},
		})

		targetRoles := participants
		if roundNumber > 1 {
			targetRoles = focusedParticipantRoles(participants, pendingQuestions, nil)
			if len(targetRoles) == 0 {
				break
			}
		}

		nextQuestions := []map[string]string{}
		for _, role := range targetRoles {
			if err := ensureActiveMeetingRun(db, meeting.ID, runID); err != nil {
				return err
			}
			if isMeetingCancelled(db, meeting.ID) {
				return errMeetingRunSuperseded
			}
			turnCounter++
			stage := "initial_analysis"
			if roundNumber > 1 {
				stage = "follow_up"
			}
			result, err := runManagedRoleTurn(
				ctx,
				db,
				*meeting,
				role,
				stage,
				roundNumber,
				priorDiscussion,
				questionsForRole(pendingQuestions, role.Key),
				map[string]int{"current": turnCounter, "total": estimatedTotalTurns},
				validRoleKeys,
			)
			if err != nil {
				content := fmt.Sprintf("%s model call failed: %v", role.Name, err)
				_, _ = AppendEvent(db, meeting.ID, domainkernel.EventError, &role.Key, content, map[string]any{
					"status": "role_error", "model_called": false, "role_name": role.Name, "round": roundNumber, "stage": stage,
				})
				if err := ensureActiveMeetingRun(db, meeting.ID, runID); err != nil {
					return err
				}
				priorDiscussion = append(priorDiscussion, map[string]any{"round": roundNumber, "role_key": role.Key, "content": content})
				continue
			}
			if err := ensureActiveMeetingRun(db, meeting.ID, runID); err != nil {
				return err
			}
			priorDiscussion = append(priorDiscussion, map[string]any{
				"round": roundNumber, "role_key": role.Key, "content": result.Content, "questions": result.Questions,
			})
			nextQuestions = append(nextQuestions, result.Questions...)
		}

		if roundNumber >= maxRounds {
			break
		}
		review, err := runModeratorPlan(ctx, db, *meeting, *moderator, moderatorModel, roles, priorDiscussion, nextQuestions, roundNumber+1, false)
		if err != nil {
			return err
		}
		if err := ensureActiveMeetingRun(db, meeting.ID, runID); err != nil {
			return err
		}
		_, _ = AppendEvent(db, meeting.ID, domainkernel.EventRoleMessage, &moderator.Key, review.Content, map[string]any{
			"status": "moderator_review", "role_name": moderator.Name, "round": roundNumber + 1,
			"questions": review.Questions, "focus_roles": review.FocusRoles, "continue_discussion": review.ContinueDiscussion, "raw_json": review.Raw,
		})
		priorDiscussion = append(priorDiscussion, map[string]any{"round": roundNumber + 1, "role_key": moderator.Key, "content": review.Content})
		pendingQuestions = review.Questions
		if review.ContinueDiscussion && len(pendingQuestions) == 0 && len(review.FocusRoles) > 0 {
			for _, roleKey := range review.FocusRoles {
				pendingQuestions = append(pendingQuestions, map[string]string{
					"target": roleKey, "question": "Continue addressing the moderator's unresolved question with evidence and a clear conclusion.",
				})
			}
		}
		if !review.ContinueDiscussion || len(pendingQuestions) == 0 {
			break
		}
	}

	if err := ensureActiveMeetingRun(db, meeting.ID, runID); err != nil {
		return err
	}
	if err := finalizeManagedMeeting(ctx, db, meeting, *moderator, moderatorModel, settings, runID); err != nil {
		if errors.Is(err, errMeetingRunSuperseded) || errors.Is(err, context.Canceled) {
			return err
		}
		return activeMeetingFailure{reason: "Moderator recap failed: " + err.Error()}
	}
	return nil
}
