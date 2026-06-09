package meeting

import (
	"context"
	"strings"

	appmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/app/meeting"
	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	"gorm.io/gorm"
)

func runManagedRoleTurn(ctx context.Context, db *gorm.DB, meeting domainmeeting.Meeting, role domainai.AgentRole, stage string, roundNumber int, priorDiscussion []map[string]any, assignedQuestions []map[string]string, progress map[string]int, validRoleKeys map[string]struct{}) (roleTurnResult, error) {
	if err := ctx.Err(); err != nil {
		return roleTurnResult{}, err
	}
	modelName := modelForRole(role)
	_, _ = AppendEvent(db, meeting.ID, domainkernel.EventSystem, &role.Key, role.Name+" started working.", map[string]any{
		"status": "role_started", "role_name": role.Name, "progress": progress, "provider": providerName(role), "model": modelName,
		"tools": stringsFromJSON(role.ToolNames), "skills": stringsFromJSON(role.SkillNames), "round": roundNumber, "stage": stage,
	})
	if !roleProviderReady(role) || modelName == "" {
		content := role.Name + " skipped model call because provider, model, or API key is missing. Configure the role in AI settings before running this meeting."
		_, _ = AppendEvent(db, meeting.ID, domainkernel.EventRoleMessage, &role.Key, content, map[string]any{
			"status": "skipped", "model_called": false, "role_name": role.Name, "progress": progress, "round": roundNumber, "stage": stage,
		})
		return roleTurnResult{Content: content, Raw: content, Confidence: "medium"}, nil
	}

	roleContext, relatedSymbols := buildManagedRoleContext(db, meeting, role, stage, roundNumber, priorDiscussion, assignedQuestions)
	systemPrompt := managedRoleSystemPrompt(role, stage)
	toolContextBlocks := []string{}
	var finalData map[string]any
	var finalPromptSnapshot map[string]any
	finalRaw := ""
	for toolIteration := 0; toolIteration < 3; toolIteration++ {
		userPrompt := roleContext
		if len(toolContextBlocks) > 0 {
			userPrompt += "\n\nTool results already obtained:\n" + strings.Join(toolContextBlocks, "\n\n")
		}
		messages := []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		}
		promptSnapshot := rolePromptSnapshot(role, modelName, messages, tokenLabel(role.Key, "analysis"), managedMeetingPromptVersion)
		finalPromptSnapshot = promptSnapshot
		_, _ = AppendEvent(db, meeting.ID, domainkernel.EventToolCall, &role.Key, role.Name+" is calling model "+modelName+".", map[string]any{
			"status": "model_call", "model_called": true, "model": modelName, "role_name": role.Name, "progress": progress,
			"round": roundNumber, "stage": stage, "tool_iteration": toolIteration, "provider_id": providerIDForRole(role),
			"provider_name": providerName(role), "prompt_version": managedMeetingPromptVersion, "prompt_snapshot": promptSnapshot,
		})
		data, raw, err := callRoleModelJSON(ctx, db, meeting.ID, role, modelName, messages, "Your previous response was not valid JSON. Return one JSON object only.", tokenLabel(role.Key, "analysis"))
		if err != nil {
			fallbackMessages := []map[string]string{
				{"role": "system", "content": systemPrompt},
				{"role": "user", "content": userPrompt},
			}
			finalPromptSnapshot = rolePromptSnapshot(role, modelName, fallbackMessages, tokenLabel(role.Key, "analysis_fallback"), managedMeetingPromptVersion)
			rawText, chatErr := chatRole(ctx, db, meeting.ID, role, modelName, fallbackMessages, tokenLabel(role.Key, "analysis_fallback"))
			if chatErr != nil {
				return roleTurnResult{}, err
			}
			data = map[string]any{"type": "analysis", "content": rawText}
			raw = rawText
		}
		finalData = data
		finalRaw = raw
		if strings.TrimSpace(stringFromAny(data["type"])) != "tool_request" {
			break
		}
		toolCalls := objectList(data["tool_calls"])
		if len(toolCalls) == 0 {
			break
		}
		toolResults := runRequestedToolCalls(db, meeting, role, toolCalls, relatedSymbols)
		toolContextBlocks = append(toolContextBlocks, compactJSON(toolResults, 2200))
	}
	if finalData == nil {
		finalData = map[string]any{"type": "analysis", "content": finalRaw}
	}
	result := appmeeting.BuildRoleTurnResult(role.Name, finalRaw, finalData, validRoleKeys)
	_, _ = AppendEvent(db, meeting.ID, domainkernel.EventRoleMessage, &role.Key, result.Content, map[string]any{
		"status": "role_completed", "model_called": true, "role_name": role.Name, "progress": progress, "round": roundNumber, "stage": stage,
		"questions": result.Questions, "mentions": result.Mentions, "citations": result.Citations, "facts": result.Facts, "assumptions": result.Assumptions, "inferences": result.Inferences, "evidence_gaps": result.EvidenceGaps, "confidence": result.Confidence, "raw_json": result.RawJSON,
		"provider_id": providerIDForRole(role), "provider_name": providerName(role), "model": modelName, "prompt_version": managedMeetingPromptVersion, "prompt_snapshot": finalPromptSnapshot,
	})
	return result, nil
}
