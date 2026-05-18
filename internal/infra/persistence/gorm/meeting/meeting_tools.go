package meeting

import (
	"context"
	"encoding/json"
	"fmt"
	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	"strings"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/ai/client"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	inframeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/meeting"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	infrapaper "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/paper"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/proxy/runtime"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"gorm.io/gorm"
)

var webSearchBaseURL = inframeeting.DefaultWebSearchBaseURL

type MeetingToolResult struct {
	Name      string
	Arguments map[string]any
	Rows      []map[string]any
	Error     string
}

func runRoleAnalysisWithTools(db *gorm.DB, meeting domainmeeting.Meeting, role domainai.AgentRole) (string, map[string]any) {
	return runRoleAnalysisWithToolsWithContext(dbContext(db), db, meeting, role)
}

func runRoleAnalysisWithToolsWithContext(ctx context.Context, db *gorm.DB, meeting domainmeeting.Meeting, role domainai.AgentRole) (string, map[string]any) {
	toolResults := ExecuteRoleTools(db, meeting, role)
	for _, result := range toolResults {
		key := role.Key
		_, _ = AppendEvent(db, meeting.ID, domainkernel.EventToolResult, &key, fmt.Sprintf("Tool %s returned %d row(s).", result.Name, len(result.Rows)), map[string]any{"status": "tool_result", "tool_name": result.Name, "rows": result.Rows, "error": result.Error})
		_ = gormrepo.NewMeetingRepository(db).CreateToolCallLog(ctx, &meeting.ID, &key, result.Name, JSON(result.Arguments), JSON(result.Rows))
	}
	if role.ProviderID == nil {
		return fmt.Sprintf("%s skipped because the provider is not configured.", role.Name), map[string]any{"status": "role_skipped", "reason": "provider_not_configured", "tool_results": toolResultsPublic(toolResults)}
	}
	var providerRow persistmodel.AiProvider
	if err := db.Preload("APIKeySecret").First(&providerRow, *role.ProviderID).Error; err != nil {
		return fmt.Sprintf("%s skipped because the provider is not ready.", role.Name), map[string]any{"status": "role_skipped", "reason": "provider_not_ready", "tool_results": toolResultsPublic(toolResults)}
	}
	provider := aiProviderFromModel(providerRow)
	if !provider.Enabled || provider.APIKeySecret == nil {
		return fmt.Sprintf("%s skipped because the provider is not ready.", role.Name), map[string]any{"status": "role_skipped", "reason": "provider_not_ready", "tool_results": toolResultsPublic(toolResults)}
	}
	settings := config.Load()
	client := ai.Client{Provider: provider, Security: security.New(settings), Settings: settings, HTTPClient: runtimeproxy.NewHTTPClient(db, settings, runtimeproxy.ModuleAI, settings.AIChatTimeout)}
	model := ""
	if role.Model != nil {
		model = *role.Model
	}
	if model == "" {
		model = provider.DefaultModel
	}
	messages := []map[string]string{
		{"role": "system", "content": role.PromptTemplate},
		{"role": "user", "content": BuildRolePrompt(db, meeting, role, toolResults)},
	}
	reserved := chatPromptTokens(model, messages)
	if err := reserveMeetingTokens(ctx, db, meeting.ID, reserved, tokenLabel(role.Key, "legacy_prompt")); err != nil {
		return fmt.Sprintf("%s token budget check failed: %v", role.Name, err), map[string]any{"status": "role_error", "error": err.Error(), "tool_results": toolResultsPublic(toolResults)}
	}
	result, err := client.ChatWithUsageWithContext(ctx, messages, model)
	if err != nil {
		return fmt.Sprintf("%s AI provider call failed: %v", role.Name, err), map[string]any{"status": "role_error", "error": err.Error(), "tool_results": toolResultsPublic(toolResults)}
	}
	actual := actualChatTokens(model, messages, result.Content, result.Usage)
	_ = settleMeetingTokenUsage(ctx, db, meeting.ID, reserved, actual, tokenLabel(role.Key, "legacy"))
	return result.Content, map[string]any{"status": "role_completed", "provider_id": provider.ID, "model": firstNonEmptyString(model, provider.DefaultModel), "tool_results": toolResultsPublic(toolResults), "token_usage": result.Usage}
}

func ExecuteRoleTools(db *gorm.DB, meeting domainmeeting.Meeting, role domainai.AgentRole) []MeetingToolResult {
	toolNames := stringsFromJSON(role.ToolNames)
	results := make([]MeetingToolResult, 0, len(toolNames))
	seen := map[string]struct{}{}
	for _, name := range toolNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		rows, args, err := ExecuteMeetingTool(db, meeting, name)
		result := MeetingToolResult{Name: name, Arguments: args, Rows: rows}
		if err != nil {
			result.Error = err.Error()
		}
		results = append(results, result)
	}
	return results
}

func ExecuteMeetingTool(db *gorm.DB, meeting domainmeeting.Meeting, name string) ([]map[string]any, map[string]any, error) {
	return ExecuteMeetingToolWithArgs(db, meeting, name, nil)
}

func ExecuteMeetingToolWithArgs(db *gorm.DB, meeting domainmeeting.Meeting, name string, inputArgs map[string]any) ([]map[string]any, map[string]any, error) {
	relatedSymbols := MeetingRelatedSymbols(db, meeting.ID)
	args := map[string]any{}
	for key, value := range inputArgs {
		args[key] = value
	}
	switch name {
	case "telegram.recent_messages":
		limit := intArg(args, "limit", 8, 1, 30)
		args["limit"] = limit
		return recentTelegramMessagesForMeeting(db, limit), args, nil
	case "web.search":
		query := strings.TrimSpace(stringFromAny(args["query"]))
		if query == "" {
			query = strings.TrimSpace(meeting.Topic)
		}
		if symbol := firstRelatedSymbol(relatedSymbols); symbol != "" && !strings.Contains(query, symbol) {
			query = strings.TrimSpace(symbol + " " + query)
		}
		args["query"] = query
		limit := intArg(args, "limit", 5, 1, 10)
		args["limit"] = limit
		return SearchWeb(db, query, limit)
	case "meeting.references":
		return meetingReferencesForTool(db, meeting.ID), args, nil
	case "meeting.transcript":
		limit := intArg(args, "limit", 12, 1, 30)
		args["limit"] = limit
		return meetingTranscriptForTool(db, meeting.ID, limit), args, nil
	case "market.watchlist":
		return watchlistForTool(db, meeting.ResearchTeamID), args, nil
	case "market.upsert_watchlist":
		return deferredActionToolResult(name, "Watchlist actions are proposed during discussion and executed only during the moderator recap.", args), args, nil
	case "market.realtime_quote":
		code := strings.TrimSpace(stringFromAny(args["code"]))
		if code == "" {
			code = firstRelatedSymbol(relatedSymbols)
		}
		if code == "" {
			return nil, args, fmt.Errorf("market.realtime_quote requires code")
		}
		args["code"] = code
		return realtimeQuoteForTool(db, code), args, nil
	case "market.daily_bars":
		code := strings.TrimSpace(stringFromAny(args["code"]))
		if code == "" {
			code = firstRelatedSymbol(relatedSymbols)
		}
		if code == "" {
			return nil, args, fmt.Errorf("market.daily_bars requires code")
		}
		args["code"] = code
		limit := intArg(args, "limit", 120, 1, 200)
		args["limit"] = limit
		return dailyBarsForTool(db, code, limit), args, nil
	case "paper.positions":
		return paperPositionsForTool(db, meeting.ResearchTeamID), args, nil
	case "paper.accounts":
		return paperAccountsForTool(db, meeting.ResearchTeamID), args, nil
	case "paper.orders":
		limit := intArg(args, "limit", 20, 1, 100)
		args["limit"] = limit
		return paperOrdersForTool(db, meeting.ResearchTeamID, limit, stringFromAny(args["status"])), args, nil
	case "paper.risk_configs":
		return riskConfigsForTool(db, meeting.ResearchTeamID), args, nil
	case "paper.create_order", "wake.create_plan":
		return deferredActionToolResult(name, "Action tools are not executed mid-discussion. Propose the action spec in analysis; the moderator recap will decide whether to execute it.", args), args, nil
	default:
		return nil, args, fmt.Errorf("unsupported meeting tool: %s", name)
	}
}

func SearchWeb(db *gorm.DB, query string, limit int) ([]map[string]any, map[string]any, error) {
	settings := config.Load()
	client := runtimeproxy.NewHTTPClient(db, settings, runtimeproxy.ModuleWeb, 20*time.Second)
	return inframeeting.SearchWeb(client, webSearchBaseURL, query, limit)
}

func deferredActionToolResult(name string, message string, args map[string]any) []map[string]any {
	return []map[string]any{{"status": "deferred_action_only", "tool": name, "message": message, "arguments": args}}
}

func BuildRolePrompt(db *gorm.DB, meeting domainmeeting.Meeting, role domainai.AgentRole, toolResults []MeetingToolResult) string {
	var b strings.Builder
	b.WriteString("Meeting topic: ")
	b.WriteString(meeting.Topic)
	b.WriteString("\n\nRole responsibility: ")
	b.WriteString(role.Responsibility)
	b.WriteString("\n\nUse the verified context to provide concise analysis, assumptions, risks, and next validation steps.")
	if len(toolResults) > 0 {
		b.WriteString("\n\nVerified tool context:\n")
		for _, result := range toolResults {
			b.WriteString("\n## ")
			b.WriteString(result.Name)
			b.WriteString("\n")
			if result.Error != "" {
				b.WriteString("ERROR: ")
				b.WriteString(result.Error)
				b.WriteString("\n")
				continue
			}
			b.WriteString(compactJSON(result.Rows, 1800))
			b.WriteString("\n")
		}
	}
	b.WriteString("\nDo not invent data not present in context; state clearly when evidence is insufficient.")
	return b.String()
}

func MeetingRelatedSymbols(db *gorm.DB, meetingID uint) []string {
	var eventRows []persistmodel.MeetingEvent
	db.Where("meeting_id = ?", meetingID).Order("sequence").Find(&eventRows)
	events := meetingEventsFromModel(eventRows)
	seen := map[string]struct{}{}
	out := []string{}
	for _, event := range events {
		var payload map[string]any
		if json.Unmarshal(event.Payload, &payload) != nil {
			continue
		}
		raw, _ := payload["related_symbols"].([]any)
		for _, item := range raw {
			code := strings.TrimSpace(fmt.Sprint(item))
			if len(code) != 6 {
				continue
			}
			if _, ok := seen[code]; ok {
				continue
			}
			seen[code] = struct{}{}
			out = append(out, code)
		}
	}
	return out
}

func recentTelegramMessagesForMeeting(db *gorm.DB, limit int) []map[string]any {
	var rows []persistmodel.TelegramMessage
	db.Order("message_time desc").Limit(limit).Find(&rows)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{"id": row.ID, "channel_id": row.ChannelID, "message_id": row.MessageID, "message_time": row.MessageTime, "text": truncateForTool(row.Text, 260), "filter_decision": row.FilterDecision, "filter_reason": row.FilterReason, "related_symbols": rawJSONForTool(row.RelatedSymbols)})
	}
	return out
}

func meetingReferencesForTool(db *gorm.DB, meetingID uint) []map[string]any {
	var refRows []persistmodel.MeetingReference
	db.Where("source_meeting_id = ?", meetingID).Order("created_at").Find(&refRows)
	rows := meetingReferencesFromModel(refRows)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{"id": row.ID, "target_meeting_id": row.TargetMeetingID, "reference_type": row.ReferenceType, "note": row.Note, "target_topic_snapshot": row.TargetTopicSnapshot, "target_summary_snapshot": row.TargetSummarySnapshot, "target_deleted": row.TargetDeleted, "external_ref": row.ExternalRef})
	}
	return out
}

func meetingTranscriptForTool(db *gorm.DB, meetingID uint, limit int) []map[string]any {
	var eventRows []persistmodel.MeetingEvent
	db.Where("meeting_id = ?", meetingID).Order("sequence desc").Limit(limit).Find(&eventRows)
	rows := meetingEventsFromModel(eventRows)
	out := make([]map[string]any, 0, len(rows))
	for i := len(rows) - 1; i >= 0; i-- {
		row := rows[i]
		out = append(out, map[string]any{"sequence": row.Sequence, "type": row.Type, "role_key": row.RoleKey, "content": truncateForTool(row.Content, 260)})
	}
	return out
}

func watchlistForTool(db *gorm.DB, teamID uint) []map[string]any {
	var rows []persistmodel.WatchlistItem
	db.Where("research_team_id = ? AND active = ?", teamID, true).Order("code").Limit(50).Find(&rows)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{"code": row.Code, "note": row.Note, "active": row.Active})
	}
	return out
}

func realtimeQuoteForTool(db *gorm.DB, code string) []map[string]any {
	var row persistmodel.RealtimeQuote
	if err := db.Where("code = ?", code).Order("quote_time desc").First(&row).Error; err != nil {
		return []map[string]any{{"code": code, "status": "missing"}}
	}
	return []map[string]any{{"code": row.Code, "price": row.Price, "change_pct": row.ChangePct, "volume": row.Volume, "amount": row.Amount, "quote_time": row.QuoteTime, "provider": row.Provider}}
}

func dailyBarsForTool(db *gorm.DB, code string, limit int) []map[string]any {
	var rows []persistmodel.DailyBar
	db.Where("code = ?", code).Order("trade_date desc").Limit(limit).Find(&rows)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{"code": row.Code, "trade_date": row.TradeDate, "open": row.Open, "high": row.High, "low": row.Low, "close": row.Close, "volume": row.Volume, "amount": row.Amount})
	}
	return out
}

func paperPositionsForTool(db *gorm.DB, teamID uint) []map[string]any {
	var rows []persistmodel.PaperPosition
	accountID := teamPaperAccountID(db, teamID)
	db.Where("account_id = ?", accountID).Order("account_id, code").Limit(100).Find(&rows)
	return infrapaper.PaperPositionsPublic(db, paperPositionsFromModel(rows))
}

func paperAccountsForTool(db *gorm.DB, teamID uint) []map[string]any {
	var rows []persistmodel.PaperAccount
	accountID := teamPaperAccountID(db, teamID)
	db.Where("id = ?", accountID).Order("id").Limit(1).Find(&rows)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{"id": row.ID, "name": row.Name, "initial_cash": row.InitialCash, "cash": row.Cash, "risk_config_id": row.RiskConfigID, "active": row.Active})
	}
	return out
}

func paperOrdersForTool(db *gorm.DB, teamID uint, limit int, status string) []map[string]any {
	var rows []persistmodel.PaperOrder
	accountID := teamPaperAccountID(db, teamID)
	query := db.Where("account_id = ?", accountID).Order("created_at desc").Limit(limit)
	if status = strings.TrimSpace(strings.ToLower(status)); status != "" {
		query = query.Where("status = ?", status)
	}
	query.Find(&rows)
	return infrapaper.PaperOrdersPublic(db, paperOrdersFromModel(rows))
}

func riskConfigsForTool(db *gorm.DB, teamID uint) []map[string]any {
	var rows []persistmodel.RiskConfig
	accountID := teamPaperAccountID(db, teamID)
	db.Joins("JOIN paper_accounts ON paper_accounts.risk_config_id = risk_configs.id").
		Where("paper_accounts.id = ?", accountID).
		Order("risk_configs.id").
		Find(&rows)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]any{"id": row.ID, "name": row.Name, "enabled": row.Enabled, "max_order_pct": row.MaxOrderPct, "max_position_pct": row.MaxPositionPct, "allow_sh_main": row.AllowSHMain, "allow_sz_main": row.AllowSZMain, "allow_star": row.AllowSTAR, "allow_chinext": row.AllowChiNext, "allow_etf_lof": row.AllowETFLOF})
	}
	return out
}

func teamPaperAccountID(db *gorm.DB, teamID uint) uint {
	var team persistmodel.ResearchTeam
	if err := db.Select("paper_account_id").First(&team, teamID).Error; err != nil {
		return 0
	}
	return team.PaperAccountID
}

func stringsFromJSON(raw []byte) []string {
	var values []string
	if len(raw) == 0 || json.Unmarshal(raw, &values) != nil {
		return nil
	}
	return values
}

func firstRelatedSymbol(symbols []string) string {
	if len(symbols) == 0 {
		return ""
	}
	return symbols[0]
}

func compactJSON(value any, limit int) string {
	b, _ := json.Marshal(value)
	text := string(b)
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit] + "...<truncated>"
}

func truncateForTool(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return value[:limit] + "...<truncated>"
}

func rawJSONForTool(raw []byte) any {
	var value any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return value
}

func intArg(args map[string]any, key string, fallback int, minValue int, maxValue int) int {
	value := fallback
	switch typed := args[key].(type) {
	case int:
		value = typed
	case int64:
		value = int(typed)
	case float64:
		value = int(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			value = int(parsed)
		}
	case string:
		var parsed int
		if _, err := fmt.Sscanf(strings.TrimSpace(typed), "%d", &parsed); err == nil {
			value = parsed
		}
	}
	if value < minValue {
		value = minValue
	}
	if maxValue > 0 && value > maxValue {
		value = maxValue
	}
	return value
}

func toolResultsPublic(results []MeetingToolResult) []map[string]any {
	out := make([]map[string]any, 0, len(results))
	for _, result := range results {
		out = append(out, map[string]any{"name": result.Name, "arguments": result.Arguments, "rows": result.Rows, "error": result.Error})
	}
	return out
}
