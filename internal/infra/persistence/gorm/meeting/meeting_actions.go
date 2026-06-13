package meeting

import (
	"encoding/json"
	"fmt"
	appmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/app/meeting"
	appprediction "github.com/TradingCopilotDevs/TradingCopilot/internal/app/prediction"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/market"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainwake "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/wake"
	"regexp"
	"strings"
	"time"

	appwake "github.com/TradingCopilotDevs/TradingCopilot/internal/app/wake"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/marketdata/ashare"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	infrapaper "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/paper"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	"gorm.io/gorm"
)

var (
	jsonFence                             = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")
	predictionMarketRecapStockCodePattern = regexp.MustCompile(`(?i)(^|[^a-z0-9])(?:[036]\d{5}|5(?:1|6|8)\d{4}|15\d{4})(?:\.(?:sh|sz|ss))?([^a-z0-9]|$)`)
	predictionMarketTradingPhrasePattern  = regexp.MustCompile(`(?i)(^|[^a-z0-9])(?:buy|sell|paper\s+trade|paper\s+order|position\s+sizing|entry\s+price|stop\s+loss|take\s+profit)([^a-z0-9]|$)`)
)

func TryApplyModeratorRecapFromEvents(db *gorm.DB, meeting *domainmeeting.Meeting) (map[string]any, bool, error) {
	var eventRow persistmodel.MeetingEvent
	if err := db.Where("meeting_id = ? AND type = ? AND role_key = ?", meeting.ID, domainkernel.EventRoleMessage, "moderator").Order("sequence desc").First(&eventRow).Error; err != nil {
		return nil, false, nil
	}
	event := meetingEventFromModel(eventRow)
	recap, err := extractMeetingRecapJSON(event.Content)
	if err != nil {
		return nil, false, nil
	}
	if !recapHasActions(recap) && strings.TrimSpace(stringFromAny(recap["summary"])) == "" && strings.TrimSpace(stringFromAny(recap["conclusion"])) == "" {
		return recap, false, nil
	}
	if isPredictionMarketMeeting(db, meeting) {
		if reason := predictionMarketRecapTextBlockReason(recap); reason != "" {
			roleKey := "moderator"
			_, _ = AppendEvent(db, meeting.ID, domainkernel.EventSystem, &roleKey, "Prediction market recap text was blocked.", predictionMarketRecapTextGatePayload(recap, reason))
			return recap, false, nil
		}
	}
	if err := ApplyMeetingRecapActions(db, meeting, &event, "moderator", recap); err != nil {
		return recap, false, err
	}
	return recap, true, nil
}

func ApplyMeetingRecapActions(db *gorm.DB, meeting *domainmeeting.Meeting, recapEvent *domainmeeting.Event, moderatorRoleKey string, recap map[string]any) error {
	if moderatorRoleKey == "" {
		moderatorRoleKey = "moderator"
	}
	if err := appmeeting.ValidateModeratorRecapActions(recap); err != nil {
		if recapHasActions(recap) {
			_, _ = AppendEvent(db, meeting.ID, domainkernel.EventSystem, &moderatorRoleKey, "Executable recap actions were blocked by evidence policy: "+err.Error(), recapActionGatePayload(recap, "recap_actions_blocked", err.Error(), "evidence_policy", "blocked"))
		}
		return nil
	}
	if recapActionsRequireReview(recap) {
		reason := "executable actions cite only role messages; add a tool/source citation or explicit evidence event reference before automation"
		_, _ = AppendEvent(db, meeting.ID, domainkernel.EventSystem, &moderatorRoleKey, "Executable recap actions require manual review because evidence references are weak role-only citations.", recapActionGatePayload(recap, "recap_actions_review_required", reason, "weak_reference_review", "manual_review_required"))
		return nil
	}
	predictionMarketMeeting := isPredictionMarketMeeting(db, meeting)
	if predictionMarketMeeting {
		if reason := predictionMarketRecapTextBlockReason(recap); reason != "" {
			_, _ = AppendEvent(db, meeting.ID, domainkernel.EventSystem, &moderatorRoleKey, "Prediction market recap text was blocked.", predictionMarketRecapTextGatePayload(recap, reason))
			return nil
		}
	}
	if predictionMarketMeeting && predictionMarketHasBlockedExecutableActions(recap) {
		_, _ = AppendEvent(db, meeting.ID, domainkernel.EventSystem, &moderatorRoleKey, "Prediction market recap executable trading actions were blocked.", recapActionGatePayload(recap, "prediction_market_actions_blocked", "prediction market meetings cannot create A-share watchlist actions or paper orders", "prediction_market_action_boundary", "blocked"))
	}
	if predictionMarketMeeting {
		for _, raw := range objectList(recap["prediction_watchlist_actions"]) {
			if err := applyPredictionWatchlistAction(db, meeting, recapEvent, moderatorRoleKey, raw); err != nil {
				_, _ = AppendEvent(db, meeting.ID, domainkernel.EventError, &moderatorRoleKey, "Failed to update prediction watchlist: "+err.Error(), map[string]any{"status": "prediction_watchlist_error"})
			}
		}
	} else if len(objectList(recap["prediction_watchlist_actions"])) > 0 {
		_, _ = AppendEvent(db, meeting.ID, domainkernel.EventSystem, &moderatorRoleKey, "Prediction watchlist actions were ignored for a non-prediction meeting.", recapActionGatePayload(recap, "prediction_watchlist_actions_ignored", "prediction_watchlist_actions require a prediction-market or mixed meeting with prediction context", "prediction_watchlist_action_boundary", "ignored"))
	}
	if !predictionMarketMeeting {
		watchlistActions := recapWatchlistActions(recap, recapEvent, meeting)
		for _, raw := range watchlistActions {
			if err := applyWatchlistAction(db, meeting, moderatorRoleKey, raw); err != nil {
				_, _ = AppendEvent(db, meeting.ID, domainkernel.EventError, &moderatorRoleKey, "Failed to update watchlist: "+err.Error(), map[string]any{"status": "watchlist_error"})
			}
		}
	}
	for _, raw := range objectList(recap["wake_plans"]) {
		if predictionMarketMeeting {
			if reason := predictionMarketWakePlanBlockReason(raw); reason != "" {
				blockedRecap := map[string]any{
					"wake_plans":         []map[string]any{raw},
					"facts":              recap["facts"],
					"inferences":         recap["inferences"],
					"citations":          recap["citations"],
					"evidence_event_ids": firstNonEmptyAny(recap["evidence_event_ids"], recap["evidenceEventIds"], recap["evidence_ids"], recap["evidenceIds"]),
				}
				_, _ = AppendEvent(db, meeting.ID, domainkernel.EventSystem, &moderatorRoleKey, "Prediction market wake plan was blocked.", recapActionGatePayload(blockedRecap, "prediction_market_wake_plan_blocked", reason, "prediction_market_wake_plan_boundary", "blocked"))
				continue
			}
		}
		if err := applyWakePlanAction(db, meeting, recapEvent, moderatorRoleKey, raw); err != nil {
			_, _ = AppendEvent(db, meeting.ID, domainkernel.EventError, &moderatorRoleKey, "Failed to create wake plan: "+err.Error(), map[string]any{"status": "wake_plan_error"})
		}
	}
	if !predictionMarketMeeting {
		for _, raw := range objectList(recap["orders"]) {
			if err := applyPaperOrderAction(db, meeting, recapEvent, moderatorRoleKey, recap, raw); err != nil {
				_, _ = AppendEvent(db, meeting.ID, domainkernel.EventError, &moderatorRoleKey, "Failed to create paper order: "+err.Error(), map[string]any{"status": "paper_order_error"})
			}
		}
	}
	return nil
}

func isPredictionMarketMeeting(db *gorm.DB, meeting *domainmeeting.Meeting) bool {
	if meeting == nil || meeting.ResearchTeamID == 0 {
		return false
	}
	var team persistmodel.ResearchTeam
	if err := db.Select("asset_class").First(&team, meeting.ResearchTeamID).Error; err != nil {
		return false
	}
	switch strings.TrimSpace(team.AssetClass) {
	case "prediction_market":
		return true
	case "mixed":
		return len(MeetingPredictionMarketIDs(db, meeting.ID)) > 0
	default:
		return false
	}
}

func predictionMarketHasBlockedExecutableActions(recap map[string]any) bool {
	return len(objectList(recap["watchlist_actions"])) > 0 || len(objectList(recap["orders"])) > 0
}

func predictionMarketRecapTextBlockReason(recap map[string]any) string {
	if predictionMarketRecapNarrativeHasAShareFields(recap) ||
		predictionMarketPayloadHasAShareFields(recap["prediction_watchlist_actions"]) ||
		predictionMarketPayloadHasAShareFields(recap["predictionWatchlistActions"]) {
		return "prediction market recap cannot use A-share code, symbol, ticker, or related_symbols fields"
	}
	return predictionMarketTextBoundaryBlockReason("recap", predictionMarketRecapTextValues(recap))
}

func predictionMarketTextBoundaryBlockReason(scope string, values []string) string {
	text := strings.ToLower(strings.Join(values, "\n"))
	for _, term := range predictionMarketRecapForbiddenTextTerms {
		if strings.Contains(text, term) {
			return fmt.Sprintf("prediction market %s cannot contain A-share or trading term %q", scope, term)
		}
	}
	if match := strings.TrimSpace(predictionMarketTradingPhrasePattern.FindString(text)); match != "" {
		return fmt.Sprintf("prediction market %s cannot contain trading phrase %q", scope, match)
	}
	if match := strings.TrimSpace(predictionMarketRecapStockCodePattern.FindString(text)); match != "" {
		return fmt.Sprintf("prediction market %s cannot contain China-listed security code %q", scope, match)
	}
	return ""
}

func predictionMarketPayloadHasAShareFields(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			switch strings.TrimSpace(key) {
			case "code", "codes", "symbol", "symbols", "ticker", "tickers", "related_symbols", "relatedSymbols":
				if wakeConfigValuePresent(item) {
					return true
				}
			}
			if predictionMarketPayloadHasAShareFields(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if predictionMarketPayloadHasAShareFields(item) {
				return true
			}
		}
	case []map[string]any:
		for _, item := range typed {
			if predictionMarketPayloadHasAShareFields(item) {
				return true
			}
		}
	}
	return false
}

var predictionMarketRecapForbiddenTextTerms = []string{
	"a股",
	"a-share",
	"a share",
	"股票",
	"个股",
	"证券",
	"模拟盘",
	"paper order",
	"paper-order",
	"paper trading",
	"paper-trading",
	"paper account",
	"paper.orders",
	"paper.create_order",
	"market.realtime_quote",
	"market.daily_bars",
	"自选池",
	"证券自选",
	"真实交易",
	"下单",
	"买入",
	"卖出",
	"仓位",
	"持仓",
}

func predictionMarketRecapNarrativeHasAShareFields(recap map[string]any) bool {
	for _, key := range []string{"code", "codes", "symbol", "symbols", "ticker", "tickers", "related_symbols", "relatedSymbols"} {
		if wakeConfigValuePresent(recap[key]) {
			return true
		}
	}
	return false
}

func predictionMarketRecapTextValues(recap map[string]any) []string {
	out := []string{}
	for _, key := range []string{"topic", "tags", "summary", "conclusion", "facts", "assumptions", "inferences", "evidence_gaps", "evidenceGaps", "citations", "prediction_watchlist_actions", "predictionWatchlistActions"} {
		collectPredictionMarketRecapText(recap[key], &out)
	}
	return out
}

func collectPredictionMarketRecapText(value any, out *[]string) {
	switch typed := value.(type) {
	case string:
		if text := strings.TrimSpace(typed); text != "" {
			*out = append(*out, text)
		}
	case []string:
		for _, item := range typed {
			collectPredictionMarketRecapText(item, out)
		}
	case []any:
		for _, item := range typed {
			collectPredictionMarketRecapText(item, out)
		}
	case []map[string]any:
		for _, item := range typed {
			collectPredictionMarketRecapText(item, out)
		}
	case map[string]any:
		for _, item := range typed {
			collectPredictionMarketRecapText(item, out)
		}
	}
}

func predictionMarketRecapTextGatePayload(recap map[string]any, reason string) map[string]any {
	return map[string]any{
		"status":      "prediction_market_recap_text_blocked",
		"reason":      reason,
		"policy":      "prediction_market_text_boundary",
		"disposition": "blocked",
		"recap": map[string]any{
			"topic":         stringFromAny(recap["topic"]),
			"summary":       stringFromAny(recap["summary"]),
			"conclusion":    stringFromAny(recap["conclusion"]),
			"facts":         stringList(recap["facts"]),
			"inferences":    stringList(recap["inferences"]),
			"evidence_gaps": stringList(recap["evidence_gaps"]),
			"citations":     stringList(recap["citations"]),
		},
	}
}

func predictionMarketWakePlanBlockReason(raw map[string]any) string {
	triggerType := domainkernel.WakeTriggerType(strings.ToLower(firstNonEmptyString(stringFromAny(raw["trigger_type"]), string(domainkernel.WakeTime))))
	switch triggerType {
	case domainkernel.WakeTime:
		return ""
	case domainkernel.WakeEvent:
	case domainkernel.WakeIndicator:
		return "prediction market meetings cannot create A-share indicator wake plans"
	default:
		return fmt.Sprintf("prediction market wake plans only allow time or event triggers, got %s", triggerType)
	}
	if predictionMarketWakeConfigHasAShareFields(raw["trigger_config"]) {
		return "prediction market wake plans cannot use A-share code, symbol, ticker, or related_symbols fields"
	}
	return ""
}

func predictionMarketWakeConfigHasAShareFields(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, inner := range typed {
			if predictionMarketWakeConfigKeyBlocked(key) && wakeConfigValuePresent(inner) {
				return true
			}
			if predictionMarketWakeConfigHasAShareFields(inner) {
				return true
			}
		}
	case []any:
		for _, inner := range typed {
			if predictionMarketWakeConfigHasAShareFields(inner) {
				return true
			}
		}
	case []map[string]any:
		for _, inner := range typed {
			if predictionMarketWakeConfigHasAShareFields(inner) {
				return true
			}
		}
	}
	return false
}

func predictionMarketWakeConfigKeyBlocked(key string) bool {
	normalized := strings.NewReplacer("_", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(key)))
	switch normalized {
	case "code", "codes", "symbol", "symbols", "ticker", "tickers", "relatedsymbol", "relatedsymbols":
		return true
	default:
		return false
	}
}

func wakeConfigValuePresent(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case []string:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return strings.TrimSpace(stringFromAny(value)) != ""
	}
}

func applyWatchlistAction(db *gorm.DB, meeting *domainmeeting.Meeting, roleKey string, raw map[string]any) error {
	code, err := ashare.EnsureCode(stringFromAny(raw["code"]))
	if err != nil {
		return err
	}
	note := strings.TrimSpace(firstNonEmptyString(stringFromAny(raw["note"]), meeting.Topic))
	active := boolFromAction(raw["active"], true)
	_, _ = AppendEvent(db, meeting.ID, domainkernel.EventToolCall, &roleKey, "Moderator is updating the watchlist.", map[string]any{"status": "tool_call", "tool": "market.upsert_watchlist", "arguments": raw})
	name := strings.TrimSpace(stringFromAny(raw["name"]))
	exchange := strings.TrimSpace(stringFromAny(raw["exchange"]))
	if exchange == "" {
		if listing := ashare.Detect(code); listing != nil {
			exchange = listing.Exchange
		}
	}
	marketRepo := gormrepo.NewMarketRepository(db)
	if name != "" {
		_ = marketRepo.UpsertSymbol(dbContext(db), domainmarket.Symbol{Code: code, Name: name, Exchange: exchange, Active: true})
	}
	item, err := marketRepo.UpsertWatchlist(dbContext(db), meeting.ResearchTeamID, code, &note, active)
	if err != nil {
		return err
	}
	_, _ = AppendEvent(db, meeting.ID, domainkernel.EventToolResult, &roleKey, fmt.Sprintf("Watchlist item %s is active=%v.", item.Code, item.Active), map[string]any{"status": "watchlist_updated", "watchlist_item_id": item.ID, "code": item.Code})
	return nil
}

func applyPredictionWatchlistAction(db *gorm.DB, meeting *domainmeeting.Meeting, recapEvent *domainmeeting.Event, roleKey string, raw map[string]any) error {
	marketID := uintFromAny(firstNonEmptyAny(raw["market_id"], raw["marketId"], raw["prediction_market_id"], raw["predictionMarketId"]))
	if marketID == 0 {
		return fmt.Errorf("prediction watchlist action requires market_id")
	}
	noteText := strings.TrimSpace(firstNonEmptyString(stringFromAny(raw["note"]), stringFromAny(raw["reason"]), meeting.Topic))
	var note *string
	if noteText != "" {
		note = &noteText
	}
	active := boolFromAction(raw["active"], true)
	args := map[string]any{"market_id": marketID, "note": noteText, "active": active}
	_, _ = AppendEvent(db, meeting.ID, domainkernel.EventToolCall, &roleKey, "Moderator is updating the prediction watchlist.", map[string]any{"status": "tool_call", "tool": "prediction.upsert_watchlist", "arguments": args})
	source := &appprediction.WatchlistSource{MeetingID: meeting.ID, RoleKey: roleKey}
	if recapEvent != nil {
		source.MeetingEventID = recapEvent.ID
	}
	item, err := appprediction.NewUsecase(gormrepo.NewPredictionRepository(db), nil).UpsertWatchlistWithSource(dbContext(db), meeting.ResearchTeamID, marketID, note, active, source)
	if err != nil {
		return err
	}
	market, _, _ := gormrepo.NewPredictionRepository(db).FindMarket(dbContext(db), item.MarketID)
	payload := map[string]any{
		"status":                       "prediction_watchlist_updated",
		"tool":                         "prediction.upsert_watchlist",
		"prediction_watchlist_item_id": item.ID,
		"market_id":                    item.MarketID,
		"active":                       item.Active,
		"note":                         noteText,
		"source_meeting_id":            item.SourceMeetingID,
		"source_meeting_event_id":      item.SourceMeetingEventID,
		"source_role_key":              nullableStringPointerForTool(item.SourceRoleKey),
	}
	if market != nil {
		payload["market_question"] = market.Question
		payload["market_slug"] = market.Slug
		payload["external_market_id"] = market.ExternalMarketID
		payload["event_id"] = market.EventID
		payload["external_event_id"] = nullableStringForTool(market.EventExternalEventID)
		payload["event_slug"] = nullableStringForTool(market.EventSlug)
		payload["event_title"] = nullableStringForTool(market.EventTitle)
	}
	_, _ = AppendEvent(db, meeting.ID, domainkernel.EventToolResult, &roleKey, fmt.Sprintf("Prediction watchlist item #%d is active=%v for market #%d.", item.ID, item.Active, item.MarketID), payload)
	return nil
}

func applyWakePlanAction(db *gorm.DB, meeting *domainmeeting.Meeting, recapEvent *domainmeeting.Event, roleKey string, raw map[string]any) error {
	triggerType := domainkernel.WakeTriggerType(strings.ToLower(firstNonEmptyString(stringFromAny(raw["trigger_type"]), string(domainkernel.WakeTime))))
	switch triggerType {
	case domainkernel.WakeTime, domainkernel.WakeIndicator, domainkernel.WakeEvent:
	default:
		return fmt.Errorf("unsupported wake trigger type %s", triggerType)
	}
	var nextCheckAt *time.Time
	if parsed, ok := parseActionTime(stringFromAny(raw["next_check_at"])); ok {
		nextCheckAt = &parsed
	}
	reason := strings.TrimSpace(firstNonEmptyString(stringFromAny(raw["reason"]), meeting.Topic))
	triggerConfig := raw["trigger_config"]
	if triggerConfig == nil {
		triggerConfig = map[string]any{}
	}
	var recapEventID *uint
	if recapEvent != nil {
		recapEventID = &recapEvent.ID
	}
	_, _ = AppendEvent(db, meeting.ID, domainkernel.EventToolCall, &roleKey, "Moderator is creating a wake plan.", map[string]any{"status": "tool_call", "tool": "wake.create_plan", "arguments": raw})
	plan := domainwake.Plan{ResearchTeamID: meeting.ResearchTeamID, MeetingID: &meeting.ID, TriggerType: triggerType, TriggerConfig: JSON(triggerConfig), Reason: reason, SourceMeetingEventID: recapEventID, SourceRoleKey: &roleKey, Status: domainkernel.WakeActive, NextCheckAt: nextCheckAt}
	if err := appwake.ValidatePlan(&plan); err != nil {
		return err
	}
	if err := gormrepo.NewWakeRepository(db).Create(dbContext(db), &plan); err != nil {
		return err
	}
	_, _ = AppendEvent(db, meeting.ID, domainkernel.EventToolResult, &roleKey, fmt.Sprintf("Wake plan #%d created.", plan.ID), map[string]any{"status": "wake_plan_created", "wake_plan_id": plan.ID})
	return nil
}

func applyPaperOrderAction(db *gorm.DB, meeting *domainmeeting.Meeting, recapEvent *domainmeeting.Event, roleKey string, recap map[string]any, raw map[string]any) error {
	if strings.TrimSpace(stringFromAny(raw["code"])) == "" || strings.TrimSpace(stringFromAny(raw["side"])) == "" {
		return nil
	}
	if stringFromAny(raw["quantity"]) == "" && stringFromAny(raw["position_pct"]) == "" && stringFromAny(raw["allocation_pct"]) == "" {
		return nil
	}
	spec := map[string]any{}
	for key, value := range raw {
		spec[key] = value
	}
	spec["meeting_id"] = meeting.ID
	spec["account_id"] = teamPaperAccountID(db, meeting.ResearchTeamID)
	if recapEvent != nil {
		spec["source_meeting_event_id"] = recapEvent.ID
	}
	if _, ok := spec["meeting_summary"]; !ok {
		spec["meeting_summary"] = recap["summary"]
	}
	if _, ok := spec["meeting_conclusion"]; !ok {
		spec["meeting_conclusion"] = recap["conclusion"]
	}
	_, _ = AppendEvent(db, meeting.ID, domainkernel.EventToolCall, &roleKey, "Moderator is creating a paper order.", map[string]any{"status": "tool_call", "tool": "paper.create_order", "arguments": raw})
	order, err := infrapaper.CreateOrderFromSpec(db, spec)
	if err != nil {
		return err
	}
	_, _ = AppendEvent(db, meeting.ID, domainkernel.EventToolResult, &roleKey, fmt.Sprintf("Paper order #%d created with status %s.", order.ID, order.Status), map[string]any{"status": "paper_order_created", "paper_order_id": order.ID, "order_status": order.Status})
	return nil
}

func extractMeetingRecapJSON(text string) (map[string]any, error) {
	cleaned := strings.TrimSpace(text)
	candidates := []string{}
	if match := jsonFence.FindStringSubmatch(cleaned); len(match) == 2 {
		candidates = append(candidates, strings.TrimSpace(match[1]))
	}
	candidates = append(candidates, firstJSONObjectCandidates(cleaned)...)
	if len(candidates) == 0 {
		candidates = append(candidates, cleaned)
	}
	var lastErr error
	for _, candidate := range candidates {
		var data map[string]any
		if err := json.Unmarshal([]byte(candidate), &data); err != nil {
			lastErr = err
			continue
		}
		return data, nil
	}
	return nil, lastErr
}

func firstJSONObjectCandidates(text string) []string {
	out := []string{}
	for start := 0; start < len(text); start++ {
		if text[start] != '{' {
			continue
		}
		depth := 0
		inString := false
		escaped := false
		for i := start; i < len(text); i++ {
			ch := text[i]
			if inString {
				if escaped {
					escaped = false
					continue
				}
				switch ch {
				case '\\':
					escaped = true
				case '"':
					inString = false
				}
				continue
			}
			switch ch {
			case '"':
				inString = true
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					out = append(out, strings.TrimSpace(text[start:i+1]))
					start = i
					i = len(text)
				}
			}
		}
	}
	return out
}

func recapHasActions(recap map[string]any) bool {
	return len(objectList(recap["watchlist_actions"])) > 0 ||
		len(objectList(recap["prediction_watchlist_actions"])) > 0 ||
		len(objectList(recap["wake_plans"])) > 0 ||
		len(objectList(recap["orders"])) > 0
}

func recapActionGatePayload(recap map[string]any, status string, reason string, policy string, disposition string) map[string]any {
	suggestions := recapActionSuggestions(recap, disposition, reason)
	return map[string]any{
		"status":                            status,
		"reason":                            reason,
		"policy":                            policy,
		"disposition":                       disposition,
		"watchlist_action_count":            len(objectList(recap["watchlist_actions"])),
		"prediction_watchlist_action_count": len(objectList(recap["prediction_watchlist_actions"])),
		"wake_plan_count":                   len(objectList(recap["wake_plans"])),
		"order_count":                       len(objectList(recap["orders"])),
		"suggestion_count":                  len(suggestions),
		"suggested_actions":                 suggestions,
		"evidence_summary": map[string]any{
			"facts":              stringList(recap["facts"]),
			"inferences":         stringList(recap["inferences"]),
			"citations":          stringList(recap["citations"]),
			"evidence_event_ids": anyList(firstNonEmptyAny(recap["evidence_event_ids"], recap["evidenceEventIds"], recap["evidence_ids"], recap["evidenceIds"])),
		},
	}
}

func recapActionSuggestions(recap map[string]any, disposition string, reason string) []map[string]any {
	out := []map[string]any{}
	out = append(out, recapActionSuggestionsForType("watchlist", objectList(recap["watchlist_actions"]), disposition, reason)...)
	out = append(out, recapActionSuggestionsForType("prediction_watchlist", objectList(recap["prediction_watchlist_actions"]), disposition, reason)...)
	out = append(out, recapActionSuggestionsForType("wake_plan", objectList(recap["wake_plans"]), disposition, reason)...)
	out = append(out, recapActionSuggestionsForType("paper_order", objectList(recap["orders"]), disposition, reason)...)
	return out
}

func recapActionSuggestionsForType(actionType string, actions []map[string]any, disposition string, reason string) []map[string]any {
	out := make([]map[string]any, 0, len(actions))
	for index, spec := range actions {
		out = append(out, map[string]any{
			"action_type": actionType,
			"index":       index,
			"disposition": disposition,
			"reason":      reason,
			"spec":        spec,
		})
	}
	return out
}

func recapActionsRequireReview(recap map[string]any) bool {
	if !recapHasActions(recap) {
		return false
	}
	if hasStrongActionCitation(recap["citations"]) || hasStrongActionCitation(recap["citation_ids"]) || hasExplicitEvidenceReference(recap) {
		return false
	}
	for _, raw := range recapActionObjects(recap) {
		if hasStrongActionCitation(raw["citations"]) || hasStrongActionCitation(raw["citation_ids"]) || hasExplicitEvidenceReference(raw) {
			return false
		}
	}
	return true
}

func recapActionObjects(recap map[string]any) []map[string]any {
	out := []map[string]any{}
	out = append(out, objectList(recap["watchlist_actions"])...)
	out = append(out, objectList(recap["prediction_watchlist_actions"])...)
	out = append(out, objectList(recap["wake_plans"])...)
	out = append(out, objectList(recap["orders"])...)
	return out
}

func hasStrongActionCitation(value any) bool {
	for _, raw := range anyList(value) {
		text := actionCitationText(raw)
		if text == "" || strings.HasPrefix(text, "@") {
			continue
		}
		return true
	}
	return false
}

func actionCitationText(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		return strings.TrimSpace(firstNonEmptyString(stringFromAny(typed["id"]), stringFromAny(typed["source"]), stringFromAny(typed["tool"]), stringFromAny(typed["url"]), stringFromAny(typed["ref"])))
	default:
		return strings.TrimSpace(stringFromAny(value))
	}
}

func hasExplicitEvidenceReference(values map[string]any) bool {
	for _, key := range []string{"evidence_event_ids", "evidenceEventIds", "evidence_ids", "evidenceIds", "source_event_ids", "sourceEventIds"} {
		if hasNonEmptyReferenceValue(values[key]) {
			return true
		}
	}
	return false
}

func hasNonEmptyReferenceValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case []any, []string, []uint:
		for _, item := range anyList(typed) {
			if strings.TrimSpace(stringFromAny(item)) != "" {
				return true
			}
		}
		return false
	default:
		return strings.TrimSpace(stringFromAny(value)) != ""
	}
}

func recapWatchlistActions(recap map[string]any, recapEvent *domainmeeting.Event, meeting *domainmeeting.Meeting) []map[string]any {
	watchlistActions := objectList(recap["watchlist_actions"])
	orders := objectList(recap["orders"])
	if len(watchlistActions) == 0 && watchlistFallbackRequested(recap) {
		for _, code := range fallbackWatchlistSymbols(recapEvent, orders) {
			watchlistActions = append(watchlistActions, map[string]any{
				"code":   code,
				"note":   firstNonEmptyString(stringFromAny(recap["conclusion"]), stringFromAny(recap["summary"]), meeting.Topic),
				"active": true,
			})
		}
	}
	existing := map[string]struct{}{}
	for _, item := range watchlistActions {
		code := strings.TrimSpace(stringFromAny(item["code"]))
		if code != "" {
			existing[code] = struct{}{}
		}
	}
	for _, order := range orders {
		code := strings.TrimSpace(stringFromAny(order["code"]))
		if code == "" {
			continue
		}
		if _, ok := existing[code]; ok {
			continue
		}
		watchlistActions = append(watchlistActions, map[string]any{
			"code":   code,
			"note":   firstNonEmptyString(stringFromAny(order["reason"]), stringFromAny(recap["summary"]), meeting.Topic),
			"active": true,
		})
		existing[code] = struct{}{}
	}
	return watchlistActions
}

func fallbackWatchlistSymbols(recapEvent *domainmeeting.Event, orders []map[string]any) []string {
	seen := map[string]struct{}{}
	out := []string{}
	if recapEvent != nil {
		var payload map[string]any
		if err := json.Unmarshal(recapEvent.Payload, &payload); err == nil {
			for _, raw := range anyList(payload["related_symbols"]) {
				code := strings.TrimSpace(stringFromAny(raw))
				if code == "" {
					continue
				}
				if _, ok := seen[code]; ok {
					continue
				}
				seen[code] = struct{}{}
				out = append(out, code)
			}
		}
	}
	for _, order := range orders {
		code := strings.TrimSpace(stringFromAny(order["code"]))
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	return out
}

func watchlistFallbackRequested(recap map[string]any) bool {
	text := strings.ToLower(strings.Join([]string{stringFromAny(recap["summary"]), stringFromAny(recap["conclusion"])}, " "))
	if strings.TrimSpace(text) == "" {
		return false
	}
	keywords := []string{"watchlist", "worth tracking", "track", "observe"}
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func anyList(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []string:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	case []uint:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	case []int:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	default:
		return nil
	}
}

func objectList(value any) []map[string]any {
	switch raw := value.(type) {
	case []map[string]any:
		return raw
	case []any:
		out := make([]map[string]any, 0, len(raw))
		for _, item := range raw {
			if obj, ok := item.(map[string]any); ok {
				out = append(out, obj)
			}
		}
		return out
	default:
		return nil
	}
}

func stringList(value any) []string {
	raw := anyList(value)
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		text := strings.TrimSpace(fmt.Sprint(item))
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func stringFromAny(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func boolFromAction(value any, fallback bool) bool {
	if value == nil {
		return fallback
	}
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		switch strings.ToLower(strings.TrimSpace(typed)) {
		case "true", "1", "yes", "y":
			return true
		case "false", "0", "no", "n":
			return false
		}
	}
	return fallback
}

func parseActionTime(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04"}
	for _, layout := range layouts {
		if layout == time.RFC3339 {
			parsed, err := time.Parse(layout, value)
			if err == nil {
				return parsed.In(appTZ), true
			}
			continue
		}
		if parsed, err := time.ParseInLocation(layout, value, appTZ); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func sanitizeStringList(values []any, limit int) []string {
	out := []string{}
	seen := map[string]struct{}{}
	for _, value := range values {
		text := strings.TrimSpace(fmt.Sprint(value))
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
