package meeting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	appwake "github.com/TradingCopilotDevs/TradingCopilot/internal/app/wake"
	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainwake "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/wake"
	"strconv"
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

func runModeratorPlan(ctx context.Context, db *gorm.DB, meeting domainmeeting.Meeting, moderator domainai.AgentRole, modelName string, roles []domainai.AgentRole, priorDiscussion []map[string]any, pendingQuestions []map[string]string, roundNumber int, kickoff bool) (moderatorPlan, error) {
	validRoleKeys := map[string]struct{}{}
	roleBrief := []string{}
	for _, role := range roles {
		if role.Key == moderator.Key {
			continue
		}
		validRoleKeys[role.Key] = struct{}{}
		roleBrief = append(roleBrief, fmt.Sprintf("- @%s: %s | %s", role.Key, role.Name, role.Responsibility))
	}
	prompt := "You are the moderator of an A-share multi-agent investment research meeting. Return JSON only with schema: " +
		`{"content":"markdown summary","continue_discussion":true,"focus_roles":["role_key"],"questions":[{"target":"role_key|all","question":"..."}]}` +
		" Decide whether discussion should continue, which roles should respond, and what concrete evidence gaps remain."
	if kickoff {
		prompt += " This is kickoff: decompose the topic and assign first-round questions."
	}
	data, raw, err := callRoleModelJSON(ctx, db, meeting.ID, moderator, modelName, []map[string]string{
		{"role": "system", "content": prompt},
		{"role": "user", "content": moderatorPlanContext(db, meeting, roles, roleBrief, priorDiscussion, pendingQuestions, roundNumber, kickoff)},
	}, "Return one JSON object only. Do not include markdown fences or extra prose.", tokenLabel(moderator.Key, "plan"))
	if err != nil {
		if plan, ok := fallbackModeratorPlanFromText(raw, roles, moderator, kickoff); ok {
			return plan, nil
		}
		return moderatorPlan{}, err
	}
	content := strings.TrimSpace(stringFromAny(data["content"]))
	if content == "" {
		content = "Moderator did not provide additional summary."
	}
	plan := moderatorPlan{
		Content:            content,
		Raw:                raw,
		ContinueDiscussion: boolFromAction(data["continue_discussion"], kickoff),
		FocusRoles:         sanitizeMentions(data["focus_roles"], validRoleKeys),
		Questions:          sanitizeQuestions(data["questions"], validRoleKeys, ""),
	}
	if kickoff {
		plan.ContinueDiscussion = true
		if len(plan.Questions) == 0 {
			for _, role := range roles {
				if role.Key == moderator.Key {
					continue
				}
				plan.Questions = append(plan.Questions, map[string]string{"target": role.Key, "question": "Provide the most important judgment and evidence from your responsibility area."})
			}
		}
	}
	return plan, nil
}

func fallbackModeratorPlanFromText(raw string, roles []domainai.AgentRole, moderator domainai.AgentRole, kickoff bool) (moderatorPlan, bool) {
	content := strings.TrimSpace(raw)
	if !looksLikeSubstantiveMarkdown(content) {
		return moderatorPlan{}, false
	}
	plan := moderatorPlan{Content: content, Raw: raw, ContinueDiscussion: kickoff}
	if kickoff {
		plan.ContinueDiscussion = true
		for _, role := range roles {
			if role.Key == moderator.Key {
				continue
			}
			plan.Questions = append(plan.Questions, map[string]string{"target": role.Key, "question": "Provide the most important judgment and evidence from your responsibility area."})
		}
	}
	return plan, true
}

func looksLikeSubstantiveMarkdown(text string) bool {
	text = strings.TrimSpace(text)
	if len([]rune(text)) < 80 {
		return false
	}
	if strings.HasPrefix(text, "#") || strings.Contains(text, "\n#") || strings.Contains(text, "\n- ") || strings.Contains(text, "\n* ") {
		return true
	}
	return strings.Count(text, "\n") >= 2
}

func finalizeManagedMeeting(ctx context.Context, db *gorm.DB, meeting *domainmeeting.Meeting, moderator domainai.AgentRole, modelName string, settings config.Settings, runID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	now := time.Now()
	if err := gormrepo.NewMeetingRepository(db).MarkRecapRunning(ctx, meeting.ID, runID, now); err != nil {
		return errMeetingRunSuperseded
	}
	recap, raw, err := callRoleModelJSONValidated(ctx, db, meeting.ID, moderator, modelName, []map[string]string{
		{"role": "system", "content": moderatorRecapPrompt(moderator)},
		{"role": "user", "content": buildRecapContext(db, *meeting)},
	}, "Your previous response was not a valid JSON object. Return one JSON object only.", tokenLabel(moderator.Key, "recap"), validateModeratorRecapActions, moderatorRecapValidationRetryInstruction)
	if err != nil {
		return err
	}
	if err := ensureActiveMeetingRun(db, meeting.ID, runID); err != nil {
		return err
	}
	topic := truncatePlain(firstNonEmptyString(stringFromAny(recap["topic"]), meeting.Topic), 240)
	tags := sanitizeStringList(anyList(recap["tags"]), 12)
	summary := firstNonEmptyString(stringFromAny(recap["summary"]), valueOrEmpty(meeting.Summary))
	conclusion := firstNonEmptyString(stringFromAny(recap["conclusion"]), valueOrEmpty(meeting.Conclusion), summary, meeting.Topic)
	markdown := formatRecapMarkdown(topic, tags, summary, conclusion, recap)
	recapEvent, err := AppendEvent(db, meeting.ID, domainkernel.EventRoleMessage, &moderator.Key, markdown, map[string]any{
		"status": "recap_completed", "role_name": moderator.Name, "topic": topic, "tags": tags, "raw_json": raw,
	})
	if err != nil {
		return err
	}
	if err := ApplyMeetingRecapActions(db, meeting, recapEvent, moderator.Key, recap); err != nil {
		return err
	}
	if err := ensureActiveMeetingRun(db, meeting.ID, runID); err != nil {
		return err
	}
	done := time.Now()
	refreshMeetingTokenBudget(db, meeting)
	meeting.Topic = topic
	meeting.Tags = JSON(tags)
	completed, err := gormrepo.NewMeetingRepository(db).CompleteRunning(ctx, meeting, runID, summary, conclusion, done)
	if err != nil {
		return err
	}
	if !completed {
		return errMeetingRunSuperseded
	}
	meeting.CompletedAt = &done
	meeting.HeartbeatAt = &done
	_, _ = AppendEvent(db, meeting.ID, domainkernel.EventConclusion, nil, conclusion, map[string]any{"status": "completed"})
	NotifyMeetingFinished(db, meeting, settings, security.New(settings))
	return nil
}

func callRoleModelJSON(ctx context.Context, db *gorm.DB, meetingID uint, role domainai.AgentRole, modelName string, messages []map[string]string, retryInstruction string, label string) (map[string]any, string, error) {
	return callRoleModelJSONValidated(ctx, db, meetingID, role, modelName, messages, retryInstruction, label, nil, nil)
}

func callRoleModelJSONValidated(ctx context.Context, db *gorm.DB, meetingID uint, role domainai.AgentRole, modelName string, messages []map[string]string, retryInstruction string, label string, validate func(map[string]any) error, validationRetryInstruction func(error) string) (map[string]any, string, error) {
	settings := config.Load()
	attempts := settings.AIJSONMaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	previousRaw := ""
	nextRetryInstruction := retryInstruction
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		finalMessages := append([]map[string]string{}, messages...)
		if attempt > 1 {
			finalMessages = append(finalMessages, map[string]string{"role": "user", "content": nextRetryInstruction + "\nPrevious response:\n" + truncateForTool(previousRaw, 2000)})
		}
		raw, err := chatRole(ctx, db, meetingID, role, modelName, finalMessages, label)
		if err != nil {
			return nil, previousRaw, err
		}
		previousRaw = raw
		data, err := extractMeetingRecapJSON(raw)
		if err == nil {
			if validate == nil {
				return data, raw, nil
			}
			if err := validate(data); err == nil {
				return data, raw, nil
			} else {
				lastErr = err
				nextRetryInstruction = retryInstruction
				if validationRetryInstruction != nil {
					nextRetryInstruction = validationRetryInstruction(err)
				}
				continue
			}
		}
		nextRetryInstruction = retryInstruction
		lastErr = err
	}
	if validate == nil {
		return nil, previousRaw, fmt.Errorf("%s returned invalid JSON after retries: %s: %w", role.Key, truncateForTool(previousRaw, 240), lastErr)
	}
	return nil, previousRaw, fmt.Errorf("%s returned invalid JSON/action after retries: %s: %w", role.Key, truncateForTool(previousRaw, 240), lastErr)
}

func validateModeratorRecapActions(recap map[string]any) error {
	for index, raw := range objectList(recap["wake_plans"]) {
		triggerType := domainkernel.WakeTriggerType(strings.ToLower(firstNonEmptyString(stringFromAny(raw["trigger_type"]), string(domainkernel.WakeTime))))
		triggerConfig := raw["trigger_config"]
		if triggerConfig == nil {
			triggerConfig = map[string]any{}
		}
		plan := domainwake.Plan{
			TriggerType:   triggerType,
			TriggerConfig: JSON(triggerConfig),
			Status:        domainkernel.WakeActive,
		}
		if err := appwake.ValidatePlan(&plan); err != nil {
			return fmt.Errorf("wake_plans[%d]: %w", index, err)
		}
	}
	return nil
}

func moderatorRecapValidationRetryInstruction(err error) string {
	return "Your previous response was valid JSON but contained invalid executable actions: " + err.Error() + `. Return one corrected JSON object only.
For indicator wake_plans, trigger_config must include code, symbol, or ticker, plus a numeric threshold, target, target_price, value, or target_value.
Do not include an indicator wake_plan if the meeting transcript lacks a concrete code and numeric threshold.`
}

func chatRole(ctx context.Context, db *gorm.DB, meetingID uint, role domainai.AgentRole, modelName string, messages []map[string]string, label string) (string, error) {
	if role.Provider == nil {
		return "", errors.New("role provider is missing")
	}
	reserved := chatPromptTokens(modelName, messages)
	if err := reserveMeetingTokens(ctx, db, meetingID, reserved, label+"_prompt"); err != nil {
		return "", err
	}
	settings := config.Load()
	client := ai.Client{Provider: *role.Provider, Security: security.New(settings), Settings: settings, HTTPClient: runtimeproxy.NewHTTPClient(db, settings, runtimeproxy.ModuleAI, settings.AIChatTimeout)}
	result, err := client.ChatWithUsageWithContext(ctx, messages, modelName)
	if err != nil {
		return "", err
	}
	actual := actualChatTokens(modelName, messages, result.Content, result.Usage)
	_ = settleMeetingTokenUsage(ctx, db, meetingID, reserved, actual, label)
	return result.Content, nil
}

func buildManagedRoleContext(db *gorm.DB, meeting domainmeeting.Meeting, role domainai.AgentRole, stage string, roundNumber int, priorDiscussion []map[string]any, assignedQuestions []map[string]string) (string, []string) {
	triggerText, relatedSymbols := triggerContext(db, meeting.ID)
	sections := []string{
		"Meeting topic: " + meeting.Topic,
		"Stage: " + stage,
		fmt.Sprintf("Round: %d", roundNumber),
	}
	if triggerText != "" {
		sections = append(sections, "Trigger context:\n"+triggerText)
	}
	if len(relatedSymbols) > 0 {
		sections = append(sections, "Related symbols: "+strings.Join(relatedSymbols, ", "))
	}
	if len(assignedQuestions) > 0 {
		lines := []string{}
		for _, item := range assignedQuestions {
			lines = append(lines, "- "+item["question"])
		}
		sections = append(sections, "Questions you should address now:\n"+strings.Join(lines, "\n"))
	}
	if len(priorDiscussion) > 0 {
		lines := []string{}
		start := max(len(priorDiscussion)-10, 0)
		for _, item := range priorDiscussion[start:] {
			lines = append(lines, fmt.Sprintf("- round %v @%s: %s", item["round"], item["role_key"], truncateForTool(stringFromAny(item["content"]), 320)))
		}
		sections = append(sections, "Recent discussion:\n"+strings.Join(lines, "\n"))
	}
	sections = append(sections, "Referenced meetings and messages:\n"+referenceContext(db, meeting.ID))
	sections = append(sections, "Available tools:\n"+toolDescriptions(stringsFromJSON(role.ToolNames))+"\n\nAvailable skills:\n"+skillDescriptions(stringsFromJSON(role.SkillNames)))
	sections = append(sections, "System execution constraints:\n- The project targets A-share research and paper trading only.\n- Buy-order sizing should normally use position_pct as a decimal fraction like 0.05 for 5%.\n- Do not use quantity to represent RMB notional; quantity means share count only.\n- For explicit share counts, quantity must be a positive integer; do not output 0 or negative values.\n- A-share stocks trade in 100-share lots, and execution is still constrained by cash, max_order_pct, max_position_pct, board scope, and trading session rules.\n- market.upsert_watchlist, paper.create_order, and wake.create_plan are proposal-only during discussion and are executed only during the moderator recap.\n- Do not invent data that is absent from context or tool results.")
	return strings.Join(sections, "\n\n"), relatedSymbols
}

func managedRoleSystemPrompt(role domainai.AgentRole, stage string) string {
	return fmt.Sprintf(`You are %s in an A-share multi-agent investment research meeting.
Responsibility: %s
Stage: %s
Additional instruction: %s

Return JSON only.
If you need tools, return: {"type":"tool_request","tool_calls":[{"tool":"...","arguments":{},"reason":"..."}]}.
If you can speak, return: {"type":"analysis","content":"markdown text","questions":[{"target":"role_key|all","question":"..."}],"mentions":["role_key"],"citations":["@role_key or source"],"confidence":"low|medium|high"}.
Separate facts, assumptions, evidence gaps, risks, invalidation conditions, and decision impact. Do not produce the final meeting recap.`, role.Name, role.Responsibility, stage, role.PromptTemplate)
}

func moderatorPlanContext(db *gorm.DB, meeting domainmeeting.Meeting, roles []domainai.AgentRole, roleBrief []string, priorDiscussion []map[string]any, pendingQuestions []map[string]string, roundNumber int, kickoff bool) string {
	triggerText, relatedSymbols := triggerContext(db, meeting.ID)
	recentLines := []string{}
	start := max(len(priorDiscussion)-12, 0)
	for _, item := range priorDiscussion[start:] {
		recentLines = append(recentLines, fmt.Sprintf("- round %v @%s: %s", item["round"], item["role_key"], truncateForTool(stringFromAny(item["content"]), 260)))
	}
	if len(recentLines) == 0 {
		recentLines = append(recentLines, "No prior discussion yet.")
	}
	questionLines := []string{}
	for _, item := range pendingQuestions {
		questionLines = append(questionLines, fmt.Sprintf("- to @%s: %s", item["target"], item["question"]))
	}
	if len(questionLines) == 0 {
		questionLines = append(questionLines, "No pending questions.")
	}
	return fmt.Sprintf("Meeting topic: %s\nRound: %d\nKickoff: %v\nTrigger context:\n%s\n\nRelated symbols: %s\n\nAvailable roles:\n%s\n\nPending questions:\n%s\n\nRecent discussion:\n%s\n\nReferences:\n%s",
		meeting.Topic, roundNumber, kickoff, firstNonEmptyString(triggerText, "-"), strings.Join(relatedSymbols, ", "), strings.Join(roleBrief, "\n"), strings.Join(questionLines, "\n"), strings.Join(recentLines, "\n"), referenceContext(db, meeting.ID))
}

func moderatorRecapPrompt(moderator domainai.AgentRole) string {
	return fmt.Sprintf(`You are %s. Produce the final meeting recap in JSON only.
Return schema: {"topic":"final topic","tags":["tag"],"summary":"short summary","conclusion":"final actionable conclusion","watchlist_actions":[{"code":"000001","name":"optional","note":"why","active":true}],"wake_plans":[{"trigger_type":"time|indicator|event","next_check_at":"YYYY-MM-DD HH:MM:SS","reason":"...","trigger_config":{"topic":"optional follow-up topic"}}],"orders":[{"account_id":1,"code":"000001","side":"buy|sell","quantity":100,"position_pct":0.05,"suggested_price":12.34,"reason":"..."}]}.
Always think in terms of executable system actions, not prose only.
If the meeting concludes a symbol should be tracked, include it in watchlist_actions.
If the meeting concludes a small pilot position should be opened, include an order.
For indicator wake_plans, trigger_config must include code, symbol, or ticker and a numeric threshold, target, target_price, value, or target_value.
Only output an indicator wake_plan when both the instrument code and threshold are explicit in the discussion.
For event wake_plans, trigger_config must include keywords, regex/pattern, related symbols, decisions, or channel filters.
For time wake_plans, use next_check_at and a trigger_config topic.
For buy orders, prefer position_pct over quantity whenever the intent is a position sizing recommendation.
position_pct must be a decimal fraction like 0.05 for 5%%, never 5 or 5%%.
Use quantity only when you truly mean share count, never when you mean RMB amount.
If you provide quantity, it must be a positive integer share count; never output 0 or negative values.
Remember that A-share stocks use 100-share lots; if the intended size is too small or still conditional, prefer watchlist_actions or wake_plans instead of emitting an invalid order.
Orders may provide either quantity or position_pct.
For paper orders and watchlist actions, only output China-listed 6-digit stock/ETF/LOF codes.
If a stock itself may be blocked by board risk controls, still output the stock code; the execution layer will decide whether an allowed ETF/LOF substitute should be used.
Do not output US stocks, Hong Kong stocks, margin trades, or short-selling ideas.`, moderator.Name)
}

func buildRecapContext(db *gorm.DB, meeting domainmeeting.Meeting) string {
	var eventRows []persistmodel.MeetingEvent
	db.Where("meeting_id = ?", meeting.ID).Order("sequence").Find(&eventRows)
	events := meetingEventsFromModel(eventRows)
	lines := []string{}
	for _, event := range events {
		if event.Type != domainkernel.EventRoleMessage && event.Type != domainkernel.EventError && event.Type != domainkernel.EventSystem {
			continue
		}
		title := string(event.Type)
		if event.RoleKey != nil {
			title = *event.RoleKey
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", title, event.Content))
	}
	if len(lines) > 40 {
		lines = lines[len(lines)-40:]
	}
	return "Current topic: " + meeting.Topic + "\n\nReferenced meetings:\n" + referenceContext(db, meeting.ID) + "\n\nPaper accounts and positions:\n" + positionsContext(db, meeting.ResearchTeamID) + "\n\nPaper risk configs:\n" + riskConfigContext(db, meeting.ResearchTeamID) + "\n\nMeeting transcript:\n" + strings.Join(lines, "\n\n")
}

func formatRecapMarkdown(topic string, tags []string, summary string, conclusion string, recap map[string]any) string {
	lines := []string{
		"## Final Topic\n" + topic,
		"## Tags\n" + firstNonEmptyString(strings.Join(tags, ", "), "-"),
		"## Summary\n" + firstNonEmptyString(summary, "-"),
		"## Conclusion\n" + firstNonEmptyString(conclusion, "-"),
	}
	if orders := objectList(recap["orders"]); len(orders) > 0 {
		lines = append(lines, "## Paper Order Proposals\n"+compactJSON(orders, 1800))
	}
	if plans := objectList(recap["wake_plans"]); len(plans) > 0 {
		lines = append(lines, "## Wake Plan Proposals\n"+compactJSON(plans, 1800))
	}
	return strings.Join(lines, "\n\n")
}

func shouldUseManagedMeetingRunner(roles []domainai.AgentRole) bool {
	moderator := findRole(roles, "moderator")
	if moderator == nil || !roleProviderReady(*moderator) || modelForRole(*moderator) == "" {
		return false
	}
	for _, role := range roles {
		if role.Key != "moderator" {
			return true
		}
	}
	return false
}

func ensureActiveMeetingRun(db *gorm.DB, meetingID uint, runID string) error {
	now := time.Now()
	active, err := gormrepo.NewMeetingRepository(db).TouchActiveRun(dbContext(db), meetingID, runID, now)
	if err != nil {
		return err
	}
	if !active {
		return errMeetingRunSuperseded
	}
	return nil
}

func failActiveMeetingRun(db *gorm.DB, meetingID uint, runID string, reason string) error {
	now := time.Now()
	failed, err := gormrepo.NewMeetingRepository(db).FailActiveRun(dbContext(db), meetingID, runID, reason, now)
	if err != nil {
		return err
	}
	if failed {
		_, _ = AppendEvent(db, meetingID, domainkernel.EventError, nil, reason, map[string]any{"status": "failed"})
	}
	return nil
}

func meetingMaxRounds(db *gorm.DB, settings config.Settings) int {
	value := settings.MeetingMaxRounds
	var setting persistmodel.AppSetting
	if err := db.First(&setting, "key = ?", "MEETING_MAX_ROUNDS").Error; err == nil {
		var raw any
		if json.Unmarshal(setting.Value, &raw) == nil {
			if obj, ok := raw.(map[string]any); ok {
				raw = obj["value"]
			}
			if parsed := intSetting(raw); parsed > 0 {
				value = parsed
			}
		}
	}
	if value < 1 {
		value = 1
	}
	if value > 12 {
		value = 12
	}
	return value
}

func triggerContext(db *gorm.DB, meetingID uint) (string, []string) {
	var eventRows []persistmodel.MeetingEvent
	db.Where("meeting_id = ?", meetingID).Order("sequence desc").Find(&eventRows)
	events := meetingEventsFromModel(eventRows)
	if len(events) == 0 {
		return "", nil
	}
	selected := events[len(events)-1]
	for _, event := range events {
		var payload map[string]any
		if json.Unmarshal(event.Payload, &payload) == nil {
			status := stringFromAny(payload["status"])
			if status == "telegram_triggered" || status == "telegram_bot_triggered" || status == "meeting_context" {
				selected = event
				break
			}
		}
	}
	var payload map[string]any
	_ = json.Unmarshal(selected.Payload, &payload)
	symbols := []string{}
	for _, raw := range anyList(payload["related_symbols"]) {
		if code := strings.TrimSpace(stringFromAny(raw)); code != "" {
			symbols = append(symbols, code)
		}
	}
	return selected.Content, symbols
}

func referenceContext(db *gorm.DB, meetingID uint) string {
	rows := meetingReferencesForTool(db, meetingID)
	if len(rows) == 0 {
		return "No referenced context."
	}
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, fmt.Sprintf("- %s %s | note=%s", stringFromAny(row["reference_type"]), stringFromAny(row["target_topic_snapshot"]), stringFromAny(row["note"])))
	}
	return strings.Join(lines, "\n")
}

func positionsContext(db *gorm.DB, researchTeamID uint) string {
	accounts := paperAccountsForTool(db, researchTeamID)
	positions := paperPositionsForTool(db, researchTeamID)
	return "Accounts:\n" + compactJSON(accounts, 1600) + "\n\nPositions:\n" + compactJSON(positions, 1600)
}

func riskConfigContext(db *gorm.DB, researchTeamID uint) string {
	configs := riskConfigsForTool(db, researchTeamID)
	if len(configs) == 0 {
		return "No risk configs."
	}
	return compactJSON(configs, 1800)
}

func toolDescriptions(toolNames []string) string {
	if len(toolNames) == 0 {
		return "- none"
	}
	defs := map[string]CapabilityDefinition{}
	for _, def := range ToolDefinitions {
		defs[def.Key] = def
	}
	lines := []string{}
	for _, name := range toolNames {
		description := "No description."
		if def, ok := defs[name]; ok {
			description = def.Description
		}
		lines = append(lines, "- "+name+": "+description)
	}
	return strings.Join(lines, "\n")
}

func skillDescriptions(skillNames []string) string {
	if len(skillNames) == 0 {
		return "- none"
	}
	defs := map[string]CapabilityDefinition{}
	for _, def := range SkillDefinitions {
		defs[def.Key] = def
	}
	lines := []string{}
	for _, name := range skillNames {
		description := "No description."
		if def, ok := defs[name]; ok {
			description = def.Description
		}
		lines = append(lines, "- "+name+": "+description)
	}
	return strings.Join(lines, "\n")
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

func focusedParticipantRoles(participants []domainai.AgentRole, questions []map[string]string, extraFocus []string) []domainai.AgentRole {
	keys := []string{}
	for _, item := range questions {
		target := item["target"]
		if target != "" && target != "all" {
			keys = append(keys, target)
		}
	}
	keys = append(keys, extraFocus...)
	seen := map[string]struct{}{}
	out := []domainai.AgentRole{}
	for _, key := range keys {
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		for _, role := range participants {
			if role.Key == key {
				out = append(out, role)
				break
			}
		}
	}
	return out
}

func questionsForRole(questions []map[string]string, roleKey string) []map[string]string {
	out := []map[string]string{}
	for _, item := range questions {
		if item["target"] == roleKey || item["target"] == "all" {
			out = append(out, item)
		}
	}
	return out
}

func findRole(roles []domainai.AgentRole, key string) *domainai.AgentRole {
	for i := range roles {
		if roles[i].Key == key {
			return &roles[i]
		}
	}
	return nil
}

func roleProviderReady(role domainai.AgentRole) bool {
	return role.Provider != nil && role.Provider.Enabled && role.Provider.APIKeySecret != nil
}

func modelForRole(role domainai.AgentRole) string {
	if role.Model != nil && strings.TrimSpace(*role.Model) != "" {
		return strings.TrimSpace(*role.Model)
	}
	if role.Provider != nil {
		return strings.TrimSpace(role.Provider.DefaultModel)
	}
	return ""
}

func providerName(role domainai.AgentRole) any {
	if role.Provider == nil {
		return nil
	}
	return role.Provider.Name
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

func mapFromAny(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if typed, ok := value.(map[string]any); ok {
		out := map[string]any{}
		for key, item := range typed {
			out[key] = item
		}
		return out
	}
	return map[string]any{}
}

func intSetting(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}

func truncatePlain(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit]
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func isMeetingCancelled(db *gorm.DB, meetingID uint) bool {
	var meeting persistmodel.Meeting
	return db.Select("status").First(&meeting, meetingID).Error == nil && meeting.Status == domainkernel.MeetingCancelled
}
