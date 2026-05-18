package meeting

import (
	"strings"

	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"gorm.io/gorm"
)

func runRequestedToolCalls(db *gorm.DB, meeting domainmeeting.Meeting, role domainai.AgentRole, toolCalls []map[string]any, relatedSymbols []string) []map[string]any {
	allowed := map[string]struct{}{}
	for _, name := range stringsFromJSON(role.ToolNames) {
		allowed[name] = struct{}{}
	}
	results := []map[string]any{}
	for _, call := range toolCalls[:min(len(toolCalls), 3)] {
		toolName := strings.TrimSpace(stringFromAny(call["tool"]))
		args := mapFromAny(call["arguments"])
		if _, ok := args["code"]; !ok && len(relatedSymbols) > 0 {
			args["code"] = relatedSymbols[0]
		}
		if _, ok := allowed[toolName]; !ok {
			results = append(results, map[string]any{"tool": toolName, "ok": false, "error": "tool not allowed for role " + role.Key})
			continue
		}
		_, _ = AppendEvent(db, meeting.ID, domainkernel.EventToolCall, &role.Key, role.Name+" requested tool "+toolName+".", map[string]any{
			"status": "tool_call", "tool": toolName, "arguments": args, "role_name": role.Name,
		})
		rows, normalizedArgs, err := ExecuteMeetingToolWithArgs(db, meeting, toolName, args)
		if err != nil {
			_, _ = AppendEvent(db, meeting.ID, domainkernel.EventError, &role.Key, role.Name+" tool "+toolName+" failed: "+err.Error(), map[string]any{
				"status": "tool_error", "tool": toolName, "role_name": role.Name,
			})
			results = append(results, map[string]any{"tool": toolName, "ok": false, "arguments": normalizedArgs, "error": err.Error()})
			continue
		}
		_ = gormrepo.NewMeetingRepository(db).CreateToolCallLog(dbContext(db), &meeting.ID, &role.Key, toolName, JSON(normalizedArgs), JSON(map[string]any{"preview": truncateForTool(compactJSON(rows, 1200), 1200)}))
		_, _ = AppendEvent(db, meeting.ID, domainkernel.EventToolResult, &role.Key, role.Name+" received tool result from "+toolName+".", map[string]any{
			"status": "tool_result", "tool": toolName, "result_preview": truncateForTool(compactJSON(rows, 1200), 1200), "role_name": role.Name,
		})
		results = append(results, map[string]any{"tool": toolName, "ok": true, "arguments": normalizedArgs, "result": rows})
	}
	return results
}
