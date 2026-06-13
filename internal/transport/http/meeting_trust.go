package httptransport

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/transport/http/jsonapi"
)

func meetingResourceWithTrust(row domainmeeting.Meeting, events []domainmeeting.Event) jsonapi.Resource {
	resource := meetingResource(row)
	if attrs, ok := resource.Attributes.(map[string]any); ok {
		attrs["trustReport"] = buildMeetingTrustReport(row, events)
	}
	return resource
}

func buildMeetingTrustReport(row domainmeeting.Meeting, events []domainmeeting.Event) map[string]any {
	evidence := []map[string]any{}
	citations := []map[string]any{}
	modelSnapshots := []map[string]any{}
	promptSnapshots := []map[string]any{}
	claimEvidenceBindings := []map[string]any{}
	conclusionHistory := []map[string]any{}
	sentenceReviews := []map[string]any{}
	recapActionSuggestions := []map[string]any{}
	recapActionReviews := []map[string]any{}
	facts := []string{}
	assumptions := []string{}
	inferences := []string{}
	evidenceGaps := []string{}
	confidences := []string{}

	for _, event := range events {
		payload := payloadMap(event)
		status := strings.TrimSpace(fmt.Sprint(payload["status"]))
		switch status {
		case "trust_review":
			sentenceReviews = append(sentenceReviews, trustReviewFromEvent(event, payload))
		case "recap_actions_blocked", "recap_actions_review_required":
			recapActionSuggestions = append(recapActionSuggestions, recapActionSuggestionsFromEvent(event, payload)...)
		case "recap_action_review":
			recapActionReviews = append(recapActionReviews, recapActionReviewFromEvent(event, payload))
		case "tool_result", "paper_order_created", "watchlist_updated", "prediction_watchlist_updated", "wake_plan_created":
			evidence = append(evidence, trustEvidenceFromEvent(event, payload))
		case "role_completed":
			modelSnapshots = append(modelSnapshots, modelSnapshotFromEvent(event, payload))
			promptSnapshots = appendPromptSnapshot(promptSnapshots, promptSnapshotFromEvent(event, payload))
			if confidence := strings.TrimSpace(fmt.Sprint(payload["confidence"])); confidence != "" {
				confidences = append(confidences, confidence)
			}
			eventCitations := citationsFromEvent(event, payload)
			citations = append(citations, eventCitations...)
			raw := rawJSONPayload(payload)
			eventFacts := stringListFromAny(trustFirstNonNil(payload["facts"], raw["facts"], raw["verified_facts"]))
			eventAssumptions := stringListFromAny(trustFirstNonNil(payload["assumptions"], raw["assumptions"]))
			eventInferences := stringListFromAny(trustFirstNonNil(payload["inferences"], raw["inferences"], raw["analysis"]))
			eventEvidenceGaps := stringListFromAny(trustFirstNonNil(payload["evidence_gaps"], payload["evidenceGaps"], raw["evidence_gaps"], raw["evidenceGaps"], raw["gaps"]))
			facts = append(facts, eventFacts...)
			assumptions = append(assumptions, eventAssumptions...)
			inferences = append(inferences, eventInferences...)
			evidenceGaps = append(evidenceGaps, eventEvidenceGaps...)
			claimEvidenceBindings = append(claimEvidenceBindings, claimBindingsFromEvent(event, "fact", eventFacts, evidence, citations)...)
			claimEvidenceBindings = append(claimEvidenceBindings, claimBindingsFromEvent(event, "assumption", eventAssumptions, evidence, citations)...)
			claimEvidenceBindings = append(claimEvidenceBindings, claimBindingsFromEvent(event, "inference", eventInferences, evidence, citations)...)
			claimEvidenceBindings = append(claimEvidenceBindings, claimBindingsFromEvent(event, "evidence_gap", eventEvidenceGaps, evidence, citations)...)
			_ = eventCitations
		case "model_call", "model_call_completed":
			modelSnapshots = append(modelSnapshots, modelSnapshotFromEvent(event, payload))
			promptSnapshots = appendPromptSnapshot(promptSnapshots, promptSnapshotFromEvent(event, payload))
		case "recap_completed":
			raw := rawJSONPayload(payload)
			modelSnapshots = append(modelSnapshots, modelSnapshotFromEvent(event, payload))
			promptSnapshots = appendPromptSnapshot(promptSnapshots, promptSnapshotFromEvent(event, payload))
			conclusionHistory = append(conclusionHistory, conclusionFromEvent(event, payload, raw))
			recapCitations := citationsFromEvent(event, raw)
			citations = append(citations, recapCitations...)
			eventFacts := stringListFromAny(trustFirstNonNil(raw["facts"], raw["verified_facts"]))
			eventAssumptions := stringListFromAny(raw["assumptions"])
			eventInferences := stringListFromAny(trustFirstNonNil(raw["inferences"], raw["analysis"]))
			eventEvidenceGaps := stringListFromAny(trustFirstNonNil(raw["evidence_gaps"], raw["evidenceGaps"], raw["gaps"]))
			facts = append(facts, eventFacts...)
			assumptions = append(assumptions, eventAssumptions...)
			inferences = append(inferences, eventInferences...)
			evidenceGaps = append(evidenceGaps, eventEvidenceGaps...)
			claimEvidenceBindings = append(claimEvidenceBindings, claimBindingsFromEvent(event, "fact", eventFacts, evidence, citations)...)
			claimEvidenceBindings = append(claimEvidenceBindings, claimBindingsFromEvent(event, "assumption", eventAssumptions, evidence, citations)...)
			claimEvidenceBindings = append(claimEvidenceBindings, claimBindingsFromEvent(event, "inference", eventInferences, evidence, citations)...)
			claimEvidenceBindings = append(claimEvidenceBindings, claimBindingsFromEvent(event, "evidence_gap", eventEvidenceGaps, evidence, citations)...)
		}
		if event.Type == domainkernel.EventConclusion {
			conclusionHistory = append(conclusionHistory, map[string]any{
				"eventId":    event.ID,
				"sequence":   event.Sequence,
				"source":     "event_conclusion",
				"conclusion": event.Content,
				"createdAt":  event.CreatedAt,
			})
		}
	}
	if row.Conclusion != nil && strings.TrimSpace(*row.Conclusion) != "" {
		conclusionHistory = append(conclusionHistory, map[string]any{
			"source":     "meeting_current",
			"summary":    row.Summary,
			"conclusion": row.Conclusion,
			"createdAt":  firstNonZeroTime(row.CompletedAt, row.CreatedAt),
		})
	}
	recapActionSuggestions = recapActionSuggestionsWithReviews(recapActionSuggestions, recapActionReviews)

	gaps := []string{}
	if len(evidence) == 0 {
		gaps = append(gaps, "no structured tool evidence captured")
	}
	if len(citations) == 0 {
		gaps = append(gaps, "no structured citations captured")
	}
	if len(modelSnapshots) == 0 {
		gaps = append(gaps, "no model snapshot captured")
	}
	evidenceGate := buildMeetingEvidenceGate(claimEvidenceBindings)
	if evidenceGate["status"] == "blocked" {
		gaps = append(gaps, "unsupported fact or inference claims without evidence")
	}
	recapActionReviewStatus := recapActionReviewStatus(recapActionSuggestions)
	if recapActionReviewStatus == "blocked" {
		gaps = append(gaps, "blocked recap executable actions require review")
	} else if recapActionReviewStatus == "needs_review" {
		gaps = append(gaps, "weakly cited recap executable actions require review")
	}
	return map[string]any{
		"generatedAt":                time.Now(),
		"eventCount":                 len(events),
		"evidenceCount":              len(evidence),
		"citationCount":              len(citations),
		"modelSnapshotCount":         len(modelSnapshots),
		"promptSnapshotCount":        len(promptSnapshots),
		"confidence":                 aggregateConfidence(confidences),
		"evidence":                   evidence,
		"citations":                  citations,
		"modelSnapshots":             modelSnapshots,
		"promptSnapshots":            promptSnapshots,
		"claimEvidenceBindings":      claimEvidenceBindings,
		"evidenceGate":               evidenceGate,
		"evidenceGateStatus":         evidenceGate["status"],
		"unsupportedClaimCount":      evidenceGate["unsupportedClaimCount"],
		"unsupportedClaims":          evidenceGate["unsupportedClaims"],
		"conclusionHistory":          conclusionHistory,
		"conclusionDiffs":            conclusionDiffs(conclusionHistory),
		"sentenceReviews":            sentenceReviews,
		"recapActionReviewStatus":    recapActionReviewStatus,
		"recapActionSuggestionCount": len(recapActionSuggestions),
		"recapActionSuggestions":     recapActionSuggestions,
		"recapActionReviewCount":     len(recapActionReviews),
		"recapActionReviews":         recapActionReviews,
		"claimBreakdown": map[string]any{
			"facts":       uniqueStrings(facts, 20),
			"assumptions": uniqueStrings(assumptions, 20),
			"inferences":  uniqueStrings(inferences, 20),
		},
		"evidenceGaps": uniqueStrings(evidenceGaps, 20),
		"gaps":         gaps,
	}
}

func recapActionSuggestionsFromEvent(event domainmeeting.Event, payload map[string]any) []map[string]any {
	items := anyListFromAny(payload["suggested_actions"])
	out := make([]map[string]any, 0, len(items))
	eventStatus := firstNonEmptyAnyString(payload["status"])
	eventPolicy := firstNonEmptyAnyString(payload["policy"])
	eventDisposition := firstNonEmptyAnyString(payload["disposition"])
	eventReason := firstNonEmptyAnyString(payload["reason"])
	eventEvidenceSummary := mapFromAnyTrust(payload["evidence_summary"])
	for index, item := range items {
		raw := mapFromAnyTrust(item)
		actionIndex := index
		if rawIndex, ok := numericUint(raw["index"]); ok {
			actionIndex = int(rawIndex)
		}
		evidenceSummary := mapFromAnyTrust(raw["evidence_summary"])
		if len(evidenceSummary) == 0 {
			evidenceSummary = eventEvidenceSummary
		}
		out = append(out, map[string]any{
			"id":              fmt.Sprintf("%d:recap-action:%d", event.ID, index+1),
			"eventId":         event.ID,
			"sequence":        event.Sequence,
			"roleKey":         eventRoleKey(event),
			"status":          eventStatus,
			"policy":          eventPolicy,
			"disposition":     firstNonEmptyString(firstNonEmptyAnyString(raw["disposition"]), eventDisposition),
			"reason":          firstNonEmptyString(firstNonEmptyAnyString(raw["reason"]), eventReason),
			"actionType":      firstNonEmptyAnyString(raw["action_type"], raw["actionType"]),
			"actionIndex":     actionIndex,
			"spec":            mapFromAnyTrust(raw["spec"]),
			"evidenceSummary": evidenceSummary,
			"createdAt":       event.CreatedAt,
		})
	}
	return out
}

func recapActionReviewFromEvent(event domainmeeting.Event, payload map[string]any) map[string]any {
	sourceEventID, _ := numericUint(trustFirstNonNil(payload["source_event_id"], payload["sourceEventId"]))
	sourceSequence, _ := numericUint(trustFirstNonNil(payload["source_sequence"], payload["sourceSequence"]))
	actionIndex, _ := numericUint(trustFirstNonNil(payload["action_index"], payload["actionIndex"]))
	actionType := firstNonEmptyAnyString(payload["action_type"], payload["actionType"])
	suggestionID := firstNonEmptyAnyString(payload["suggestion_id"], payload["suggestionId"])
	if suggestionID == "" && sourceEventID > 0 {
		suggestionID = fmt.Sprintf("%d:recap-action:%d", sourceEventID, int(actionIndex)+1)
	}
	return map[string]any{
		"meetingId":            event.MeetingID,
		"eventId":              event.ID,
		"sequence":             event.Sequence,
		"suggestionId":         suggestionID,
		"sourceEventId":        sourceEventID,
		"sourceSequence":       sourceSequence,
		"actionIndex":          actionIndex,
		"actionType":           actionType,
		"decision":             firstNonEmptyAnyString(payload["decision"]),
		"executionDisposition": firstNonEmptyAnyString(payload["execution_disposition"], payload["executionDisposition"]),
		"citationIds":          stringListFromAny(trustFirstNonNil(payload["citation_ids"], payload["citationIds"])),
		"evidenceEventIds":     uintListFromAnyTrust(trustFirstNonNil(payload["evidence_event_ids"], payload["evidenceEventIds"])),
		"comment":              firstNonEmptyAnyString(payload["comment"]),
		"reviewer":             firstNonEmptyAnyString(payload["reviewer"]),
		"reviewedAt":           trustFirstNonNil(payload["reviewed_at"], payload["reviewedAt"], event.CreatedAt),
		"actionSpec":           mapFromAnyTrust(trustFirstNonNil(payload["action_spec"], payload["actionSpec"])),
		"originalStatus":       firstNonEmptyAnyString(payload["original_status"], payload["originalStatus"]),
		"originalPolicy":       firstNonEmptyAnyString(payload["original_policy"], payload["originalPolicy"]),
		"originalDisposition":  firstNonEmptyAnyString(payload["original_disposition"], payload["originalDisposition"]),
		"originalReason":       firstNonEmptyAnyString(payload["original_reason"], payload["originalReason"]),
		"createdAt":            event.CreatedAt,
	}
}

func recapActionSuggestionsWithReviews(suggestions []map[string]any, reviews []map[string]any) []map[string]any {
	if len(suggestions) == 0 || len(reviews) == 0 {
		return suggestions
	}
	latest := map[string]map[string]any{}
	for _, review := range reviews {
		for _, key := range recapActionReviewKeys(review) {
			if key != "" {
				latest[key] = review
			}
		}
	}
	for _, suggestion := range suggestions {
		for _, key := range recapActionReviewKeys(suggestion) {
			review, ok := latest[key]
			if !ok {
				continue
			}
			suggestion["latestReview"] = review
			suggestion["reviewDecision"] = review["decision"]
			suggestion["reviewer"] = review["reviewer"]
			suggestion["reviewedAt"] = review["reviewedAt"]
			break
		}
	}
	return suggestions
}

func recapActionReviewKeys(value map[string]any) []string {
	keys := []string{}
	if id := firstNonEmptyAnyString(value["suggestionId"], value["suggestion_id"], value["id"]); id != "" {
		keys = append(keys, "suggestion:"+id)
	}
	sourceEventID, _ := numericUint(trustFirstNonNil(value["sourceEventId"], value["source_event_id"], value["eventId"], value["event_id"]))
	actionIndex, _ := numericUint(trustFirstNonNil(value["actionIndex"], value["action_index"]))
	actionType := strings.ToLower(strings.TrimSpace(firstNonEmptyAnyString(value["actionType"], value["action_type"])))
	if sourceEventID > 0 {
		keys = append(keys, fmt.Sprintf("source:%d:%d:%s", sourceEventID, actionIndex, actionType))
		keys = append(keys, fmt.Sprintf("source:%d:%d:", sourceEventID, actionIndex))
	}
	return keys
}

func recapActionReviewStatus(suggestions []map[string]any) string {
	if len(suggestions) == 0 {
		return "clear"
	}
	hasUnreviewed := false
	for _, item := range suggestions {
		review := mapFromAnyTrust(item["latestReview"])
		if len(review) > 0 {
			decision := strings.ToLower(strings.TrimSpace(firstNonEmptyAnyString(review["decision"])))
			if decision == "needs_evidence" {
				return "blocked"
			}
			continue
		}
		status := strings.ToLower(strings.TrimSpace(firstNonEmptyAnyString(item["status"])))
		disposition := strings.ToLower(strings.TrimSpace(firstNonEmptyAnyString(item["disposition"])))
		if status == "recap_actions_blocked" || disposition == "blocked" {
			return "blocked"
		}
		hasUnreviewed = true
	}
	if hasUnreviewed {
		return "needs_review"
	}
	return "clear"
}

func trustReviewFromEvent(event domainmeeting.Event, payload map[string]any) map[string]any {
	return map[string]any{
		"eventId":          event.ID,
		"sequence":         event.Sequence,
		"source":           "manual_review",
		"sentenceId":       firstNonEmptyAnyString(payload["sentence_id"], payload["sentenceId"]),
		"sentence":         firstNonEmptyAnyString(payload["sentence"]),
		"verdict":          firstNonEmptyAnyString(payload["verdict"]),
		"citationIds":      stringListFromAny(trustFirstNonNil(payload["citation_ids"], payload["citationIds"])),
		"evidenceEventIds": uintListFromAnyTrust(trustFirstNonNil(payload["evidence_event_ids"], payload["evidenceEventIds"])),
		"comment":          firstNonEmptyAnyString(payload["comment"]),
		"reviewer":         firstNonEmptyAnyString(payload["reviewer"]),
		"reviewedAt":       trustFirstNonNil(payload["reviewed_at"], payload["reviewedAt"], event.CreatedAt),
		"createdAt":        event.CreatedAt,
	}
}

func trustEvidenceFromEvent(event domainmeeting.Event, payload map[string]any) map[string]any {
	preview := trustFirstNonNil(payload["result_preview"], payload["rows"], payload["result"])
	if preview == nil {
		preview = payload
	}
	return map[string]any{
		"eventId":   event.ID,
		"sequence":  event.Sequence,
		"type":      event.Type,
		"roleKey":   eventRoleKey(event),
		"tool":      firstNonEmptyAnyString(payload["tool"], payload["tool_name"]),
		"status":    payload["status"],
		"preview":   preview,
		"createdAt": event.CreatedAt,
	}
}

func modelSnapshotFromEvent(event domainmeeting.Event, payload map[string]any) map[string]any {
	promptSnapshot := promptSnapshotFromEvent(event, payload)
	return map[string]any{
		"eventId":        event.ID,
		"sequence":       event.Sequence,
		"roleKey":        eventRoleKey(event),
		"roleName":       payload["role_name"],
		"providerId":     payload["provider_id"],
		"providerName":   firstNonEmptyAnyString(payload["provider_name"], payload["providerName"]),
		"model":          payload["model"],
		"promptVersion":  firstNonEmptyString(firstNonEmptyAnyString(payload["prompt_version"], payload["promptVersion"]), "runtime-current"),
		"promptHash":     promptSnapshot["promptHash"],
		"promptSnapshot": promptSnapshot,
		"tokenUsage":     payload["token_usage"],
		"createdAt":      event.CreatedAt,
	}
}

func promptSnapshotFromEvent(event domainmeeting.Event, payload map[string]any) map[string]any {
	snapshot := mapFromAnyTrust(trustFirstNonNil(payload["prompt_snapshot"], payload["promptSnapshot"]))
	if len(snapshot) == 0 {
		return map[string]any{}
	}
	out := map[string]any{
		"eventId":          event.ID,
		"sequence":         event.Sequence,
		"roleKey":          firstNonEmptyAnyString(snapshot["roleKey"], snapshot["role_key"], eventRoleKey(event)),
		"roleName":         trustFirstNonNil(snapshot["roleName"], snapshot["role_name"], payload["role_name"]),
		"providerId":       trustFirstNonNil(snapshot["providerId"], snapshot["provider_id"], payload["provider_id"]),
		"providerName":     trustFirstNonNil(snapshot["providerName"], snapshot["provider_name"], payload["provider_name"], payload["providerName"]),
		"model":            trustFirstNonNil(snapshot["model"], payload["model"]),
		"promptVersion":    firstNonEmptyString(firstNonEmptyAnyString(snapshot["promptVersion"], snapshot["prompt_version"], payload["prompt_version"], payload["promptVersion"]), "runtime-current"),
		"promptHash":       firstNonEmptyAnyString(snapshot["promptHash"], snapshot["prompt_hash"], payload["prompt_hash"], payload["promptHash"]),
		"systemPromptHash": firstNonEmptyAnyString(snapshot["systemPromptHash"], snapshot["system_prompt_hash"]),
		"userPromptHash":   firstNonEmptyAnyString(snapshot["userPromptHash"], snapshot["user_prompt_hash"]),
		"templateHash":     firstNonEmptyAnyString(snapshot["promptTemplateHash"], snapshot["prompt_template_hash"]),
		"messageCount":     trustFirstNonNil(snapshot["messageCount"], snapshot["message_count"]),
		"label":            firstNonEmptyAnyString(snapshot["label"], payload["label"]),
		"toolNames":        trustFirstNonNil(snapshot["toolNames"], snapshot["tool_names"]),
		"skillNames":       trustFirstNonNil(snapshot["skillNames"], snapshot["skill_names"]),
		"createdAt":        event.CreatedAt,
	}
	if out["promptHash"] == "" {
		return map[string]any{}
	}
	return out
}

func appendPromptSnapshot(snapshots []map[string]any, snapshot map[string]any) []map[string]any {
	if len(snapshot) == 0 {
		return snapshots
	}
	hash := fmt.Sprint(snapshot["promptHash"])
	for _, existing := range snapshots {
		if fmt.Sprint(existing["promptHash"]) == hash {
			return snapshots
		}
	}
	return append(snapshots, snapshot)
}

func claimBindingsFromEvent(event domainmeeting.Event, claimType string, claims []string, evidence []map[string]any, citations []map[string]any) []map[string]any {
	out := []map[string]any{}
	for i, claim := range uniqueStrings(claims, 0) {
		out = append(out, map[string]any{
			"id":               strconv.FormatUint(uint64(event.ID), 10) + ":" + claimType + ":" + strconv.Itoa(i+1),
			"eventId":          event.ID,
			"sequence":         event.Sequence,
			"roleKey":          eventRoleKey(event),
			"claimType":        claimType,
			"claim":            claim,
			"evidenceEventIds": evidenceRefsForClaim(event, evidence, 5),
			"citationIds":      citationRefsForClaim(event, citations, 5),
			"createdAt":        event.CreatedAt,
		})
	}
	return out
}

func buildMeetingEvidenceGate(bindings []map[string]any) map[string]any {
	unsupported := []map[string]any{}
	checkedCount := 0
	supportedCount := 0
	for _, binding := range bindings {
		claimType := strings.ToLower(strings.TrimSpace(firstNonEmptyAnyString(binding["claimType"])))
		if !meetingClaimRequiresEvidence(claimType) {
			continue
		}
		checkedCount++
		evidenceIDs := uintListFromAnyTrust(binding["evidenceEventIds"])
		citationIDs := stringListFromAny(binding["citationIds"])
		if len(evidenceIDs) == 0 && len(citationIDs) == 0 {
			unsupported = append(unsupported, unsupportedMeetingClaim(binding))
			continue
		}
		supportedCount++
	}
	status := "pass"
	if len(unsupported) > 0 {
		status = "blocked"
	} else if checkedCount == 0 {
		status = "warning"
	}
	return map[string]any{
		"status":                status,
		"policy":                "facts and inferences require at least one structured evidence event or citation",
		"blockingClaimTypes":    []string{"fact", "inference"},
		"checkedClaimCount":     checkedCount,
		"supportedClaimCount":   supportedCount,
		"unsupportedClaimCount": len(unsupported),
		"unsupportedClaims":     unsupported,
	}
}

func meetingClaimRequiresEvidence(claimType string) bool {
	return claimType == "fact" || claimType == "inference"
}

func unsupportedMeetingClaim(binding map[string]any) map[string]any {
	return map[string]any{
		"id":               firstNonEmptyAnyString(binding["id"]),
		"eventId":          binding["eventId"],
		"sequence":         binding["sequence"],
		"roleKey":          firstNonEmptyAnyString(binding["roleKey"]),
		"claimType":        firstNonEmptyAnyString(binding["claimType"]),
		"claim":            firstNonEmptyAnyString(binding["claim"]),
		"evidenceEventIds": uintListFromAnyTrust(binding["evidenceEventIds"]),
		"citationIds":      stringListFromAny(binding["citationIds"]),
		"reason":           "claim lacks both structured evidence event references and citation references",
		"createdAt":        binding["createdAt"],
	}
}

func conclusionFromEvent(event domainmeeting.Event, payload map[string]any, raw map[string]any) map[string]any {
	return map[string]any{
		"eventId":    event.ID,
		"sequence":   event.Sequence,
		"source":     "moderator_recap",
		"topic":      trustFirstNonNil(raw["topic"], payload["topic"]),
		"summary":    raw["summary"],
		"conclusion": raw["conclusion"],
		"tags":       trustFirstNonNil(raw["tags"], payload["tags"]),
		"createdAt":  event.CreatedAt,
	}
}

func citationsFromEvent(event domainmeeting.Event, payload map[string]any) []map[string]any {
	items := anyListFromAny(trustFirstNonNil(payload["citations"], payload["sources"], payload["references"]))
	out := make([]map[string]any, 0, len(items))
	for i, item := range items {
		out = append(out, map[string]any{
			"id":        strconv.FormatUint(uint64(event.ID), 10) + ":" + strconv.Itoa(i+1),
			"eventId":   event.ID,
			"sequence":  event.Sequence,
			"roleKey":   eventRoleKey(event),
			"citation":  item,
			"createdAt": event.CreatedAt,
		})
	}
	return out
}

func evidenceRefsForClaim(event domainmeeting.Event, evidence []map[string]any, limit int) []uint {
	roleRefs := evidenceRefs(event, evidence, limit, true)
	if len(roleRefs) > 0 {
		return roleRefs
	}
	return evidenceRefs(event, evidence, limit, false)
}

func evidenceRefs(event domainmeeting.Event, evidence []map[string]any, limit int, sameRoleOnly bool) []uint {
	roleKey := eventRoleKey(event)
	out := []uint{}
	for i := len(evidence) - 1; i >= 0; i-- {
		item := evidence[i]
		sequence, _ := numericUint(item["sequence"])
		if sequence > uint(event.Sequence) {
			continue
		}
		if sameRoleOnly && roleKey != "" && firstNonEmptyAnyString(item["roleKey"]) != roleKey {
			continue
		}
		eventID, ok := numericUint(item["eventId"])
		if !ok || eventID == 0 {
			continue
		}
		out = append(out, eventID)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	reverseUintSlice(out)
	return out
}

func citationRefsForClaim(event domainmeeting.Event, citations []map[string]any, limit int) []string {
	roleRefs := citationRefs(event, citations, limit, true)
	if len(roleRefs) > 0 {
		return roleRefs
	}
	return citationRefs(event, citations, limit, false)
}

func citationRefs(event domainmeeting.Event, citations []map[string]any, limit int, sameRoleOnly bool) []string {
	roleKey := eventRoleKey(event)
	out := []string{}
	for i := len(citations) - 1; i >= 0; i-- {
		item := citations[i]
		sequence, _ := numericUint(item["sequence"])
		if sequence > uint(event.Sequence) {
			continue
		}
		if sameRoleOnly && roleKey != "" && firstNonEmptyAnyString(item["roleKey"]) != roleKey {
			continue
		}
		id := firstNonEmptyAnyString(item["id"])
		if id == "" {
			continue
		}
		out = append(out, id)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	reverseStringSlice(out)
	return out
}

func conclusionDiffs(history []map[string]any) []map[string]any {
	out := []map[string]any{}
	for i := 1; i < len(history); i++ {
		before := history[i-1]
		after := history[i]
		beforeText := conclusionText(before)
		afterText := conclusionText(after)
		if beforeText == "" && afterText == "" {
			continue
		}
		changed := beforeText != afterText
		status := "unchanged"
		if changed {
			status = "changed"
		}
		sentenceDiff := conclusionSentenceDiff(beforeText, afterText)
		out = append(out, map[string]any{
			"fromSource":             before["source"],
			"toSource":               after["source"],
			"fromEventId":            before["eventId"],
			"toEventId":              after["eventId"],
			"status":                 status,
			"changed":                changed,
			"beforePreview":          truncateTrustText(beforeText, 180),
			"afterPreview":           truncateTrustText(afterText, 180),
			"changedCharacters":      absInt(len([]rune(afterText)) - len([]rune(beforeText))),
			"addedSentences":         sentenceDiff["addedSentences"],
			"removedSentences":       sentenceDiff["removedSentences"],
			"unchangedSentences":     sentenceDiff["unchangedSentences"],
			"addedSentenceCount":     sentenceDiff["addedSentenceCount"],
			"removedSentenceCount":   sentenceDiff["removedSentenceCount"],
			"unchangedSentenceCount": sentenceDiff["unchangedSentenceCount"],
		})
	}
	return out
}

func conclusionSentenceDiff(beforeText string, afterText string) map[string]any {
	before := splitConclusionSentences(beforeText)
	after := splitConclusionSentences(afterText)
	beforeCounts := sentenceCountMap(before)
	afterCounts := sentenceCountMap(after)
	added := []string{}
	unchanged := []string{}
	for _, sentence := range after {
		key := normalizeTrustSentence(sentence)
		if beforeCounts[key] > 0 {
			unchanged = append(unchanged, sentence)
			beforeCounts[key]--
			continue
		}
		added = append(added, sentence)
	}
	removed := []string{}
	for _, sentence := range before {
		key := normalizeTrustSentence(sentence)
		if afterCounts[key] > 0 {
			afterCounts[key]--
			continue
		}
		removed = append(removed, sentence)
	}
	return map[string]any{
		"addedSentences":         truncateSentenceList(added, 8, 240),
		"removedSentences":       truncateSentenceList(removed, 8, 240),
		"unchangedSentences":     truncateSentenceList(unchanged, 8, 240),
		"addedSentenceCount":     len(added),
		"removedSentenceCount":   len(removed),
		"unchangedSentenceCount": len(unchanged),
	}
}

func splitConclusionSentences(text string) []string {
	sentences := []string{}
	var builder strings.Builder
	for _, r := range text {
		builder.WriteRune(r)
		if isConclusionSentenceBoundary(r) {
			if sentence := strings.TrimSpace(builder.String()); sentence != "" {
				sentences = append(sentences, collapseTrustWhitespace(sentence))
			}
			builder.Reset()
		}
	}
	if sentence := strings.TrimSpace(builder.String()); sentence != "" {
		sentences = append(sentences, collapseTrustWhitespace(sentence))
	}
	return sentences
}

func isConclusionSentenceBoundary(r rune) bool {
	switch r {
	case '\n', '.', '!', '?', ';', 0x3002, 0xff01, 0xff1f, 0xff1b:
		return true
	default:
		return false
	}
}

func sentenceCountMap(sentences []string) map[string]int {
	counts := map[string]int{}
	for _, sentence := range sentences {
		key := normalizeTrustSentence(sentence)
		if key != "" {
			counts[key]++
		}
	}
	return counts
}

func normalizeTrustSentence(sentence string) string {
	return strings.ToLower(strings.TrimSpace(collapseTrustWhitespace(sentence)))
}

func collapseTrustWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func truncateSentenceList(sentences []string, limit int, textLimit int) []string {
	out := []string{}
	for _, sentence := range sentences {
		sentence = strings.TrimSpace(sentence)
		if sentence == "" {
			continue
		}
		out = append(out, truncateTrustText(sentence, textLimit))
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func conclusionText(item map[string]any) string {
	return strings.TrimSpace(firstNonEmptyAnyString(item["conclusion"], item["summary"], item["content"]))
}

func truncateTrustText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "..."
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func payloadMap(event domainmeeting.Event) map[string]any {
	var payload map[string]any
	if err := json.Unmarshal(event.Payload, &payload); err != nil || payload == nil {
		return map[string]any{}
	}
	return payload
}

func rawJSONPayload(payload map[string]any) map[string]any {
	raw := trustFirstNonNil(payload["raw_json"], payload["rawJson"])
	switch typed := raw.(type) {
	case map[string]any:
		return typed
	case string:
		var parsed map[string]any
		if err := json.Unmarshal([]byte(typed), &parsed); err == nil && parsed != nil {
			return parsed
		}
	}
	return map[string]any{}
}

func anyListFromAny(value any) []any {
	switch typed := value.(type) {
	case []any:
		return typed
	case []string:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, item)
		}
		return out
	case nil:
		return nil
	default:
		return []any{typed}
	}
}

func mapFromAnyTrust(value any) map[string]any {
	switch typed := value.(type) {
	case map[string]any:
		return typed
	case string:
		var parsed map[string]any
		if err := json.Unmarshal([]byte(typed), &parsed); err == nil && parsed != nil {
			return parsed
		}
	}
	return map[string]any{}
}

func stringListFromAny(value any) []string {
	items := anyListFromAny(value)
	out := make([]string, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case string:
			if text := strings.TrimSpace(typed); text != "" {
				out = append(out, text)
			}
		case map[string]any:
			if text := strings.TrimSpace(firstNonEmptyAnyString(typed["text"], typed["claim"], typed["content"])); text != "" {
				out = append(out, text)
			}
		}
	}
	return out
}

func uintListFromAnyTrust(value any) []uint {
	items := anyListFromAny(value)
	out := make([]uint, 0, len(items))
	seen := map[uint]struct{}{}
	for _, item := range items {
		id, ok := numericUint(item)
		if !ok || id == 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func eventRoleKey(event domainmeeting.Event) string {
	if event.RoleKey == nil {
		return ""
	}
	return strings.TrimSpace(*event.RoleKey)
}

func numericUint(value any) (uint, bool) {
	switch typed := value.(type) {
	case uint:
		return typed, true
	case uint64:
		return uint(typed), true
	case int:
		if typed >= 0 {
			return uint(typed), true
		}
	case int64:
		if typed >= 0 {
			return uint(typed), true
		}
	case float64:
		if typed >= 0 {
			return uint(typed), true
		}
	case string:
		parsed, err := strconv.ParseUint(strings.TrimSpace(typed), 10, 64)
		if err == nil {
			return uint(parsed), true
		}
	}
	return 0, false
}

func reverseUintSlice(values []uint) {
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}
}

func reverseStringSlice(values []string) {
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}
}

func uniqueStrings(values []string, limit int) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

func aggregateConfidence(values []string) string {
	if len(values) == 0 {
		return "unknown"
	}
	low, high := false, true
	for _, value := range values {
		switch strings.ToLower(strings.TrimSpace(value)) {
		case "low":
			low = true
			high = false
		case "high":
		default:
			high = false
		}
	}
	if low {
		return "low"
	}
	if high {
		return "high"
	}
	return "medium"
}

func trustFirstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
			return value
		}
	}
	return nil
}

func firstNonEmptyAnyString(values ...any) string {
	for _, value := range values {
		if value == nil {
			continue
		}
		text := strings.TrimSpace(trustString(value))
		if text != "" {
			return text
		}
	}
	return ""
}

func trustString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case *string:
		if typed == nil {
			return ""
		}
		return *typed
	default:
		return fmt.Sprint(value)
	}
}

func firstNonZeroTime(value *time.Time, fallback time.Time) time.Time {
	if value != nil && !value.IsZero() {
		return *value
	}
	return fallback
}
