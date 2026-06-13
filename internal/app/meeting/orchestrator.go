package meeting

import (
	"fmt"
	"strings"
	"time"

	appwake "github.com/TradingCopilotDevs/TradingCopilot/internal/app/wake"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainwake "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/wake"
)

const (
	DefaultRunnerName      = "go"
	RunStartedEventContent = "Virtual research meeting started."
)

type RunStartPlan struct {
	Runner    string
	RunID     string
	StartedAt time.Time
}

type CompletionDefaults struct {
	Summary    string
	Conclusion string
}

type RoleTurnResult struct {
	Content      string
	Raw          string
	Questions    []map[string]string
	Mentions     []string
	Citations    []string
	Facts        []string
	Assumptions  []string
	Inferences   []string
	EvidenceGaps []string
	Confidence   string
	RawJSON      any
}

type ModeratorPlan struct {
	Content            string
	Raw                string
	ContinueDiscussion bool
	FocusRoles         []string
	Questions          []map[string]string
}

func NewRunStartPlan(meetingID uint, runner string, now time.Time) RunStartPlan {
	runner = strings.TrimSpace(runner)
	if runner == "" {
		runner = DefaultRunnerName
	}
	return RunStartPlan{
		Runner:    runner,
		RunID:     fmt.Sprintf("%s-%d-%d", runner, meetingID, now.UnixNano()),
		StartedAt: now,
	}
}

func ApplyRunStart(meeting *domainmeeting.Meeting, plan RunStartPlan) {
	if meeting == nil {
		return
	}
	meeting.Status = domainkernel.MeetingRunning
	meeting.StartedAt = &plan.StartedAt
	meeting.HeartbeatAt = &plan.StartedAt
	meeting.RunID = &plan.RunID
	meeting.RunAttempt++
}

func RunStartedEventPayload(plan RunStartPlan) map[string]any {
	return map[string]any{"status": "running", "runner": plan.Runner, "run_id": plan.RunID}
}

func IsTerminalStatus(status domainkernel.MeetingStatus) bool {
	return status == domainkernel.MeetingCancelled || status == domainkernel.MeetingCompleted || status == domainkernel.MeetingFailed
}

func LegacyCompletionDefaults() CompletionDefaults {
	return CompletionDefaults{
		Summary:    "Go migration runner completed the configured multi-role research flow.",
		Conclusion: "No investment advice. Review each role event, assumptions, risks, and source quality before taking any action.",
	}
}

func BuildRoleTurnResult(roleName string, raw string, data map[string]any, validRoleKeys map[string]struct{}) RoleTurnResult {
	if data == nil {
		data = map[string]any{}
	}
	content := strings.TrimSpace(stringFromAny(data["content"]))
	if content == "" {
		content = strings.TrimSpace(roleName) + " had no additional material contribution in this turn."
	}
	confidence := NormalizeRoleConfidence(stringFromAny(data["confidence"]))
	var rawJSON any
	if strings.HasPrefix(strings.TrimSpace(raw), "{") {
		rawJSON = raw
	}
	return RoleTurnResult{
		Content:      content,
		Raw:          raw,
		Questions:    sanitizeQuestions(data["questions"], validRoleKeys, ""),
		Mentions:     sanitizeMentions(data["mentions"], validRoleKeys),
		Citations:    sanitizeCitations(data["citations"]),
		Facts:        sanitizeClaimList(data["facts"], 12),
		Assumptions:  sanitizeClaimList(data["assumptions"], 12),
		Inferences:   sanitizeClaimList(data["inferences"], 12),
		EvidenceGaps: sanitizeClaimList(firstNonEmptyAny(data["evidence_gaps"], data["evidenceGaps"], data["gaps"]), 12),
		Confidence:   confidence,
		RawJSON:      rawJSON,
	}
}

func NormalizeRoleConfidence(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "low", "medium", "high":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "medium"
	}
}

func FallbackModeratorPlanFromText(raw string, focusRoleKeys []string, kickoff bool) (ModeratorPlan, bool) {
	content := strings.TrimSpace(raw)
	if !LooksLikeSubstantiveMarkdown(content) {
		return ModeratorPlan{}, false
	}
	plan := ModeratorPlan{Content: content, Raw: raw, ContinueDiscussion: kickoff}
	if kickoff {
		plan.ContinueDiscussion = true
		for _, key := range focusRoleKeys {
			key = strings.TrimSpace(strings.TrimPrefix(key, "@"))
			if key == "" {
				continue
			}
			plan.Questions = append(plan.Questions, map[string]string{"target": key, "question": "Provide the most important judgment and evidence from your responsibility area."})
		}
	}
	return plan, true
}

func LooksLikeSubstantiveMarkdown(text string) bool {
	text = strings.TrimSpace(text)
	if len([]rune(text)) < 80 {
		return false
	}
	if strings.HasPrefix(text, "#") || strings.Contains(text, "\n#") || strings.Contains(text, "\n- ") || strings.Contains(text, "\n* ") {
		return true
	}
	return strings.Count(text, "\n") >= 2
}

func ValidateModeratorRecapActions(recap map[string]any) error {
	if err := validateModeratorRecapActionEvidence(recap); err != nil {
		return err
	}
	for index, raw := range objectList(recap["wake_plans"]) {
		triggerType := domainkernel.WakeTriggerType(strings.ToLower(firstNonEmptyString(stringFromAny(raw["trigger_type"]), string(domainkernel.WakeTime))))
		triggerConfig := raw["trigger_config"]
		if triggerConfig == nil {
			triggerConfig = map[string]any{}
		}
		plan := domainwake.Plan{
			TriggerType:   triggerType,
			TriggerConfig: domainkernel.NewJSON(triggerConfig),
			Status:        domainkernel.WakeActive,
		}
		if err := appwake.ValidatePlan(&plan); err != nil {
			return fmt.Errorf("wake_plans[%d]: %w", index, err)
		}
	}
	return nil
}

func ModeratorRecapValidationRetryInstruction(err error) string {
	return "Your previous response was valid JSON but contained invalid executable actions: " + err.Error() + `. Return one corrected JSON object only.
If you keep watchlist_actions, prediction_watchlist_actions, wake_plans, or orders, include facts or inferences plus citations that tie the action to transcript, referenced meetings, or tool results.
If evidence is missing, move the idea to assumptions/evidence_gaps and remove the executable action.
For indicator wake_plans, trigger_config must include code, symbol, or ticker, plus a numeric threshold, target, target_price, value, or target_value.
Do not include an indicator wake_plan if the meeting transcript lacks a concrete code and numeric threshold.`
}

func validateModeratorRecapActionEvidence(recap map[string]any) error {
	if !ModeratorRecapHasActions(recap) {
		return nil
	}
	claims := append(stringList(recap["facts"]), stringList(recap["inferences"])...)
	if len(claims) == 0 {
		return fmt.Errorf("executable actions require at least one structured fact or inference")
	}
	if len(stringList(recap["citations"])) == 0 {
		return fmt.Errorf("executable actions require at least one citation linking the action to transcript, referenced meetings, or tool results")
	}
	return nil
}

func ModeratorRecapHasActions(recap map[string]any) bool {
	return len(objectList(recap["watchlist_actions"])) > 0 ||
		len(objectList(recap["prediction_watchlist_actions"])) > 0 ||
		len(objectList(recap["wake_plans"])) > 0 ||
		len(objectList(recap["orders"])) > 0
}

func sanitizeQuestions(raw any, validRoleKeys map[string]struct{}, fallbackTarget string) []map[string]string {
	items := objectList(raw)
	out := []map[string]string{}
	for _, item := range items {
		question := truncatePlain(firstNonEmptyString(stringFromAny(item["question"]), stringFromAny(item["content"])), 400)
		if question == "" {
			continue
		}
		target := normalizeRoleKey(item["target"], validRoleKeys)
		if target == "" {
			if _, ok := validRoleKeys[fallbackTarget]; ok {
				target = fallbackTarget
			} else {
				target = "all"
			}
		}
		out = append(out, map[string]string{"target": target, "question": question})
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func sanitizeMentions(raw any, validRoleKeys map[string]struct{}) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, item := range anyList(raw) {
		key := normalizeRoleKey(item, validRoleKeys)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, key)
		if len(out) >= 8 {
			break
		}
	}
	return out
}

func sanitizeCitations(raw any) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, item := range anyList(raw) {
		text := truncatePlain(stringFromAny(item), 160)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		out = append(out, text)
		if len(out) >= 12 {
			break
		}
	}
	return out
}

func sanitizeClaimList(raw any, limit int) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, item := range anyList(raw) {
		text := ""
		if obj, ok := item.(map[string]any); ok {
			text = firstNonEmptyString(stringFromAny(obj["text"]), stringFromAny(obj["claim"]), stringFromAny(obj["content"]))
		} else {
			text = stringFromAny(item)
		}
		text = truncatePlain(text, 220)
		if text == "" {
			continue
		}
		if _, ok := seen[text]; ok {
			continue
		}
		seen[text] = struct{}{}
		out = append(out, text)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func stringList(raw any) []string {
	out := []string{}
	for _, item := range anyList(raw) {
		text := strings.TrimSpace(stringFromAny(item))
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func normalizeRoleKey(value any, validRoleKeys map[string]struct{}) string {
	text := strings.TrimPrefix(strings.TrimSpace(stringFromAny(value)), "@")
	if text == "" {
		return ""
	}
	if _, ok := validRoleKeys[text]; ok {
		return text
	}
	return ""
}

func objectList(value any) []map[string]any {
	rawItems := anyList(value)
	out := make([]map[string]any, 0, len(rawItems))
	for _, item := range rawItems {
		if obj, ok := item.(map[string]any); ok {
			out = append(out, obj)
		}
	}
	return out
}

func anyList(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []map[string]any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	case []string:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func firstNonEmptyAny(values ...any) any {
	for _, value := range values {
		if value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
			return value
		}
	}
	return nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}

func truncatePlain(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}
