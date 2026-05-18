package httptransport

import (
	"net/http"
	"strconv"
	"strings"

	appmessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/messaging"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainmsg "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/messaging"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/transport/http/jsonapi"
)

func (s *Server) messageSubscriptionAppConfig(w http.ResponseWriter, r *http.Request) {
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("message-subscription-app-configs", "telegram", map[string]any{
		"provider":   domainmsg.ProviderTelegramChannel,
		"hasAppId":   s.messagingUsecase.SubscriptionStatus(r.Context())["has_app_id"],
		"hasAppHash": s.messagingUsecase.SubscriptionStatus(r.Context())["has_app_hash"],
		"hasSession": s.messagingUsecase.SubscriptionStatus(r.Context())["has_session"],
	}))
}

func (s *Server) saveMessageSubscriptionAppConfig(w http.ResponseWriter, r *http.Request) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return
	}
	if err := s.messagingUsecase.SaveTelegramAppConfig(r.Context(), stringAttr(attrs, "appId"), stringAttr(attrs, "appHash")); err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "message-subscription-app-config-save-failed", "Message subscription app config save failed", err.Error(), "")
		return
	}
	s.messageSubscriptionAppConfig(w, r)
}

func (s *Server) messageSubscriptionLoginStart(w http.ResponseWriter, r *http.Request) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return
	}
	hash, err := s.messagingUsecase.StartTelegramLogin(r.Context(), stringAttr(attrs, "phone"))
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "message-subscription-login-start-failed", "Message subscription login start failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, messageSubscriptionLoginSessionResource(map[string]any{"phoneCodeHash": hash, "message": "Telegram verification code sent."}))
}

func (s *Server) messageSubscriptionLoginVerify(w http.ResponseWriter, r *http.Request) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return
	}
	phoneCodeHash := stringAttr(attrs, "phoneCodeHash")
	if strings.TrimSpace(phoneCodeHash) == "" {
		writeJSONAPIError(w, http.StatusBadRequest, "message-subscription-phone-code-hash-required", "Phone code hash required", "phone_code_hash is required", "phoneCodeHash")
		return
	}
	if err := s.messagingUsecase.VerifyTelegramLogin(r.Context(), stringAttr(attrs, "phone"), stringAttr(attrs, "code"), phoneCodeHash, stringAttr(attrs, "password")); err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "message-subscription-login-verify-failed", "Message subscription login verify failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, messageSubscriptionLoginSessionResource(map[string]any{"status": "stored", "message": "Telegram MTProto session stored."}))
}

func (s *Server) listMessageSubscriptions(w http.ResponseWriter, r *http.Request) {
	rows, err := s.messagingUsecase.ListSubscriptions(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "message-subscriptions-load-failed", "Message subscriptions load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, messageSubscriptionResource(row))
	}
	writeResourceCollection(w, r, out, 100, 500)
}

func (s *Server) listMessageSubscriptionFilters(w http.ResponseWriter, r *http.Request) {
	rows, err := s.messagingUsecase.ListSubscriptionFilters(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "message-subscription-filters-load-failed", "Message subscription filters load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, messageSubscriptionFilterResource(row))
	}
	writeResourceCollection(w, r, out, 100, 500)
}

func (s *Server) createMessageSubscriptionFilter(w http.ResponseWriter, r *http.Request) {
	input, _, ok := decodeMessageSubscriptionFilterInput(w, r)
	if !ok {
		return
	}
	row, err := s.messagingUsecase.CreateSubscriptionFilter(r.Context(), input)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "message-subscription-filter-create-failed", "Message subscription filter create failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, messageSubscriptionFilterResource(*row))
}

func (s *Server) updateMessageSubscriptionFilter(w http.ResponseWriter, r *http.Request) {
	input, fields, ok := decodeMessageSubscriptionFilterInput(w, r)
	if !ok {
		return
	}
	row, found, err := s.messagingUsecase.UpdateSubscriptionFilter(r.Context(), uintParam(r, "filterId"), input, fields)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "message-subscription-filter-update-failed", "Message subscription filter update failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "message-subscription-filter-not-found", "Message subscription filter not found", "filter not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, messageSubscriptionFilterResource(*row))
}

func (s *Server) createMessageSubscription(w http.ResponseWriter, r *http.Request) {
	input, _, ok := decodeMessageSubscriptionInput(w, r)
	if !ok {
		return
	}
	result, err := s.messagingUsecase.CreateSubscription(r.Context(), input)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "message-subscription-create-failed", "Message subscription create failed", err.Error(), "")
		return
	}
	if !s.dispatchMessagingMeetings(w, r, result.CreatedMeetings) {
		return
	}
	jsonapi.WriteData(w, http.StatusOK, messageSubscriptionResource(result.MessageSubscription))
}

func (s *Server) updateMessageSubscription(w http.ResponseWriter, r *http.Request) {
	input, fields, ok := decodeMessageSubscriptionInput(w, r)
	if !ok {
		return
	}
	result, found, err := s.messagingUsecase.UpdateSubscription(r.Context(), uintParam(r, "subscriptionId"), input, fields)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "message-subscription-update-failed", "Message subscription update failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "message-subscription-not-found", "Message subscription not found", "subscription not found", "")
		return
	}
	if !s.dispatchMessagingMeetings(w, r, result.CreatedMeetings) {
		return
	}
	jsonapi.WriteData(w, http.StatusOK, messageSubscriptionResource(result.MessageSubscription))
}

func (s *Server) deleteMessageSubscription(w http.ResponseWriter, r *http.Request) {
	id := uintParam(r, "subscriptionId")
	if err := s.messagingUsecase.DeleteSubscription(r.Context(), id); err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "message-subscription-delete-failed", "Message subscription delete failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("deletions", "message-subscription:"+strconv.FormatUint(uint64(id), 10), map[string]any{"status": "deleted"}))
}

func (s *Server) testMessageSubscriptionRef(w http.ResponseWriter, r *http.Request) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return
	}
	result, err := s.messagingUsecase.TestSubscriptionRef(r.Context(), stringAttr(attrs, "provider"), stringAttr(attrs, "sourceRef"))
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "message-subscription-test-failed", "Message subscription test failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, messageSubscriptionTestResource(result))
}

func (s *Server) testMessageSubscription(w http.ResponseWriter, r *http.Request) {
	result, found, err := s.messagingUsecase.TestSubscription(r.Context(), uintParam(r, "subscriptionId"))
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "message-subscription-test-failed", "Message subscription test failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "message-subscription-not-found", "Message subscription not found", "subscription not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, messageSubscriptionTestResource(result))
}

func (s *Server) collectMessageSubscriptions(w http.ResponseWriter, r *http.Request) {
	input := appmessaging.CollectInput{}
	if r.Body != nil && r.ContentLength != 0 {
		attrs, ok := decodeJSONAPIAttributesMap(w, r)
		if !ok {
			return
		}
		if value, ok := numberAttrValue(attrs, "subscriptionId"); ok {
			id := uint(value)
			input.SubscriptionID = &id
		}
		if value, ok := numberAttrValue(attrs, "limit"); ok {
			input.Limit = int(value)
		}
	}
	result, found, err := s.messagingUsecase.QueueCollectSubscriptions(r.Context(), input)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "message-subscription-collect-failed", "Message subscription collect failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "message-subscription-not-found", "Message subscription not found", "no enabled message subscriptions", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("message-subscription-collect-results", "current", map[string]any{"status": result.Status, "subscriptions": result.Subscriptions, "queued": result.Queued, "collected": result.Collected, "filtered": result.Filtered}))
}

func (s *Server) listPlatformAdapters(w http.ResponseWriter, r *http.Request) {
	rows, err := s.messagingUsecase.ListPlatformAdapters(r.Context())
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "platform-adapters-load-failed", "Platform adapters load failed", err.Error(), "")
		return
	}
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, platformAdapterResource(row))
	}
	writeResourceCollection(w, r, out, 100, 500)
}

func (s *Server) createPlatformAdapter(w http.ResponseWriter, r *http.Request) {
	input, _, ok := decodePlatformAdapterInput(w, r)
	if !ok {
		return
	}
	row, err := s.messagingUsecase.CreatePlatformAdapter(r.Context(), input)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "platform-adapter-create-failed", "Platform adapter create failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, platformAdapterResource(*row))
}

func (s *Server) updatePlatformAdapter(w http.ResponseWriter, r *http.Request) {
	input, fields, ok := decodePlatformAdapterInput(w, r)
	if !ok {
		return
	}
	row, found, err := s.messagingUsecase.UpdatePlatformAdapter(r.Context(), uintParam(r, "adapterId"), input, fields)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "platform-adapter-update-failed", "Platform adapter update failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "platform-adapter-not-found", "Platform adapter not found", "adapter not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, platformAdapterResource(*row))
}

func (s *Server) deletePlatformAdapter(w http.ResponseWriter, r *http.Request) {
	id := uintParam(r, "adapterId")
	if err := s.messagingUsecase.DeletePlatformAdapter(r.Context(), id); err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "platform-adapter-delete-failed", "Platform adapter delete failed", err.Error(), "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("deletions", "platform-adapter:"+strconv.FormatUint(uint64(id), 10), map[string]any{"status": "deleted"}))
}

func (s *Server) testPlatformAdapter(w http.ResponseWriter, r *http.Request) {
	result, found, err := s.messagingUsecase.TestPlatformAdapter(r.Context(), uintParam(r, "adapterId"))
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "platform-adapter-test-failed", "Platform adapter test failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "platform-adapter-not-found", "Platform adapter not found", "adapter not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, platformAdapterTestResource(result))
}

func (s *Server) listIngestedMessages(w http.ResponseWriter, r *http.Request) {
	page := jsonapi.ParsePage(r, 100, 500)
	result, err := s.messagingUsecase.ListMessages(r.Context(), appmessaging.MessageFilter{
		SubscriptionID: r.URL.Query().Get("subscriptionId"),
		ResearchTeamID: r.URL.Query().Get("researchTeamId"),
		Query:          r.URL.Query().Get("q"),
		OnlyUnfiltered: r.URL.Query().Get("onlyUnfiltered") == "true",
		Decision:       firstNonEmptyString(r.URL.Query().Get("filterDecision"), r.URL.Query().Get("filterStatus")),
		Limit:          page.Limit,
		Cursor:         page.Cursor,
	})
	if err != nil {
		writeJSONAPIError(w, http.StatusInternalServerError, "ingested-messages-load-failed", "Ingested messages load failed", err.Error(), "")
		return
	}
	jsonapi.Write(w, http.StatusOK, jsonapi.Document{Data: ingestedMessagesPublic(result.Rows), Links: jsonapi.PageLinks(r, result.NextCursor), Meta: jsonapi.PageMeta(len(result.Rows), result.NextCursor)})
}

func (s *Server) createIngestedMessage(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeIngestedMessageInput(w, r)
	if !ok {
		return
	}
	result, err := s.messagingUsecase.CreateMessage(r.Context(), input)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "ingested-message-create-failed", "Ingested message create failed", err.Error(), "")
		return
	}
	if !s.dispatchMessagingMeetings(w, r, result.CreatedMeetings) {
		return
	}
	jsonapi.WriteData(w, http.StatusOK, ingestedMessageResource(result.Row))
}

func (s *Server) updateIngestedMessage(w http.ResponseWriter, r *http.Request) {
	input, ok := decodeIngestedMessageInput(w, r)
	if !ok {
		return
	}
	result, found, err := s.messagingUsecase.UpdateMessage(r.Context(), uintParam(r, "messageId"), input)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "ingested-message-update-failed", "Ingested message update failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "ingested-message-not-found", "Ingested message not found", "message not found", "")
		return
	}
	if !s.dispatchMessagingMeetings(w, r, result.CreatedMeetings) {
		return
	}
	jsonapi.WriteData(w, http.StatusOK, ingestedMessageResource(result.Row))
}

func (s *Server) deleteIngestedMessage(w http.ResponseWriter, r *http.Request) {
	id := uintParam(r, "messageId")
	found, err := s.messagingUsecase.DeleteMessage(r.Context(), id)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "ingested-message-delete-failed", "Ingested message delete failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "ingested-message-not-found", "Ingested message not found", "message not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, jsonapi.NewResource("deletions", "ingested-message:"+strconv.FormatUint(uint64(id), 10), map[string]any{"status": "deleted"}))
}

func (s *Server) refilterIngestedMessages(w http.ResponseWriter, r *http.Request) {
	input := appmessaging.RefilterInput{}
	if r.Body != nil && r.ContentLength != 0 {
		attrs, ok := decodeJSONAPIAttributesMap(w, r)
		if !ok {
			return
		}
		if v, ok := attrValue(attrs, "ids"); ok {
			if typed, ok := v.([]any); ok {
				input.IDs = make([]uint, 0, len(typed))
				for _, item := range typed {
					if n, ok := numberAttrValue(map[string]any{"value": item}, "value"); ok {
						input.IDs = append(input.IDs, uint(n))
					}
				}
			}
		}
		if v, ok := numberAttrValue(attrs, "limit"); ok {
			input.Limit = int(v)
		}
		if v, ok := attrValue(attrs, "onlyUnfiltered"); ok {
			if b, ok := v.(bool); ok {
				input.OnlyUnfiltered = b
			}
		}
	}
	result, err := s.messagingUsecase.QueueRefilterMessages(r.Context(), input)
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "ingested-messages-refilter-failed", "Ingested messages refilter failed", err.Error(), "")
		return
	}
	jsonapi.Write(w, http.StatusOK, jsonapi.Document{Data: ingestedMessagesPublic(result.Rows), Meta: map[string]any{"pageSize": len(result.Rows), "hasMore": false}})
}

func (s *Server) filterIngestedMessage(w http.ResponseWriter, r *http.Request) {
	result, found, err := s.messagingUsecase.QueueFilterMessage(r.Context(), uintParam(r, "messageId"))
	if err != nil {
		writeJSONAPIError(w, http.StatusBadRequest, "ingested-message-filter-failed", "Ingested message filter failed", err.Error(), "")
		return
	}
	if !found {
		writeJSONAPIError(w, http.StatusNotFound, "ingested-message-not-found", "Ingested message not found", "message not found", "")
		return
	}
	jsonapi.WriteData(w, http.StatusOK, ingestedMessageResource(result.Row))
}

func (s *Server) dispatchMessagingMeetings(w http.ResponseWriter, r *http.Request, meetings []*domainmeeting.Meeting) bool {
	for _, meeting := range meetings {
		if meeting == nil {
			continue
		}
		if _, err := s.meetingUsecase.DispatchRun(r.Context(), meeting, "queued", "", nil); err != nil {
			writeJSONAPIError(w, http.StatusBadGateway, "meeting-dispatch-failed", "Meeting dispatch failed", err.Error(), "")
			return false
		}
	}
	return true
}

func decodeMessageSubscriptionInput(w http.ResponseWriter, r *http.Request) (appmessaging.SubscriptionInput, map[string]bool, bool) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return appmessaging.SubscriptionInput{}, nil, false
	}
	fields := map[string]bool{}
	input := appmessaging.SubscriptionInput{
		Provider:      firstNonEmptyString(stringAttr(attrs, "provider"), domainmsg.ProviderTelegramChannel),
		Title:         stringAttr(attrs, "title"),
		SourceRef:     stringAttr(attrs, "sourceRef"),
		Enabled:       boolAttrWithDefault(attrs, true, "enabled"),
		BackfillLimit: 20,
	}
	for _, name := range []string{"provider", "title", "sourceRef", "enabled", "filterId", "config"} {
		if _, ok := attrValue(attrs, name); ok {
			fields[name] = true
		}
	}
	if value, ok := numberAttrValue(attrs, "filterId"); ok {
		input.FilterID = uint(value)
	}
	if teamIDs, set := uintSliceAttr(attrs, "teamIds"); set {
		input.TeamIDs = teamIDs
		input.TeamIDsSet = true
		fields["teamIds"] = true
	}
	if value, ok := numberAttrValue(attrs, "backfillLimit"); ok {
		input.BackfillLimit = int(value)
		input.BackfillLimitSpecified = true
		fields["backfillLimit"] = true
	}
	if value, ok := numberAttrValue(attrs, "pollIntervalSeconds"); ok {
		input.PollIntervalSeconds = int(value)
		input.PollIntervalSpecified = true
		fields["pollIntervalSeconds"] = true
	}
	if value, ok := attrValue(attrs, "config"); ok {
		input.Config = value
	}
	return input, fields, true
}

func decodeMessageSubscriptionFilterInput(w http.ResponseWriter, r *http.Request) (appmessaging.SubscriptionFilterInput, map[string]bool, bool) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return appmessaging.SubscriptionFilterInput{}, nil, false
	}
	fields := map[string]bool{}
	for _, name := range []string{"name", "description", "promptTemplate", "providerId", "model", "enabled", "isDefault"} {
		if _, ok := attrValue(attrs, name); ok {
			fields[name] = true
		}
	}
	input := appmessaging.SubscriptionFilterInput{
		Name:           stringAttr(attrs, "name"),
		Description:    stringAttr(attrs, "description"),
		PromptTemplate: stringAttr(attrs, "promptTemplate"),
		ProviderID:     uintPtrAttr(attrs, "providerId"),
		Enabled:        boolAttrWithDefault(attrs, true, "enabled"),
		IsDefault:      boolAttr(attrs, "isDefault"),
	}
	if value, ok := attrValue(attrs, "model"); ok && value != nil && strings.TrimSpace(fmtSprint(value)) != "" {
		model := fmtSprint(value)
		input.Model = &model
	}
	return input, fields, true
}

func decodePlatformAdapterInput(w http.ResponseWriter, r *http.Request) (appmessaging.PlatformAdapterInput, map[string]bool, bool) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return appmessaging.PlatformAdapterInput{}, nil, false
	}
	fields := map[string]bool{}
	input := appmessaging.PlatformAdapterInput{
		Provider:    firstNonEmptyString(stringAttr(attrs, "provider"), domainmsg.ProviderTelegramBot),
		DisplayName: firstNonEmptyString(stringAttr(attrs, "displayName"), "Telegram Bot"),
		Enabled:     boolAttrWithDefault(attrs, true, "enabled"),
		BotToken:    stringAttr(attrs, "botToken"),
		ChatID:      stringAttr(attrs, "chatId"),
	}
	for _, name := range []string{"provider", "displayName", "enabled", "config"} {
		if _, ok := attrValue(attrs, name); ok {
			fields[name] = true
		}
	}
	if value, ok := attrValue(attrs, "config"); ok {
		input.Config = value
	}
	return input, fields, true
}

func decodeIngestedMessageInput(w http.ResponseWriter, r *http.Request) (appmessaging.MessageInput, bool) {
	attrs, ok := decodeJSONAPIAttributesMap(w, r)
	if !ok {
		return appmessaging.MessageInput{}, false
	}
	input := appmessaging.MessageInput{
		SubscriptionID:  uint(numberAttrValueOrZero(attrs, "subscriptionId")),
		SourceMessageID: stringAttr(attrs, "sourceMessageId"),
		Text:            stringAttr(attrs, "text"),
	}
	if value, ok := attrValue(attrs, "messageTime"); ok {
		if t, ok := parseAnyTime(value); ok {
			input.MessageTime = t
			input.HasMessageTime = true
		}
	}
	if value, ok := attrValue(attrs, "filterDecision"); ok {
		input.FilterDecision = value
		input.HasDecision = true
	}
	if value, ok := attrValue(attrs, "filterReason"); ok {
		input.FilterReason = value
		input.HasReason = true
	}
	if value, ok := attrValue(attrs, "relatedSymbols"); ok {
		input.RelatedSymbols = value
		input.HasSymbols = true
	}
	return input, true
}

func messageSubscriptionResource(row domainmsg.MessageSubscription) jsonapi.Resource {
	filterName := ""
	if row.Filter != nil {
		filterName = row.Filter.Name
	}
	resource := jsonapi.NewResource("message-subscriptions", strconv.FormatUint(uint64(row.ID), 10), map[string]any{
		"provider": row.Provider, "title": row.Title, "sourceRef": row.SourceRef, "enabled": row.Enabled,
		"filterId": row.FilterID, "filterName": filterName,
		"teamIds":       row.TeamIDs,
		"backfillLimit": row.BackfillLimit, "pollIntervalSeconds": row.PollIntervalSeconds,
		"collectFrom": row.CollectFrom, "lastCollectedAt": row.LastCollectedAt, "nextCollectAt": row.NextCollectAt,
		"lastCollectError": row.LastCollectError, "config": rawJSONValue(row.Config),
		"createdAt": row.CreatedAt, "updatedAt": row.UpdatedAt,
	})
	if row.FilterID != 0 {
		resource.Relationships = map[string]jsonapi.Relationship{
			"filter": {Data: map[string]string{"type": "message-subscription-filters", "id": strconv.FormatUint(uint64(row.FilterID), 10)}},
		}
	}
	return resource
}

func messageSubscriptionFilterResource(row domainmsg.MessageSubscriptionFilter) jsonapi.Resource {
	resource := jsonapi.NewResource("message-subscription-filters", strconv.FormatUint(uint64(row.ID), 10), map[string]any{
		"name": row.Name, "description": row.Description, "promptTemplate": row.PromptTemplate,
		"providerId": row.ProviderID, "model": row.Model, "enabled": row.Enabled, "isDefault": row.IsDefault,
		"toolNames": appmessaging.DefaultFilterToolNames, "skillNames": appmessaging.DefaultFilterSkillNames,
		"createdAt": row.CreatedAt, "updatedAt": row.UpdatedAt,
	})
	if row.ProviderID != nil {
		resource.Relationships = map[string]jsonapi.Relationship{
			"provider": {Data: map[string]string{"type": "ai-providers", "id": strconv.FormatUint(uint64(*row.ProviderID), 10)}},
		}
	}
	return resource
}

func messageSubscriptionTestResource(result map[string]any) jsonapi.Resource {
	return jsonapi.NewResource("message-subscription-tests", "current", map[string]any{
		"status": result["status"], "sourceRef": firstNonNil(result["source_ref"], result["channel_ref"]),
		"title": result["title"], "sourceMessageId": firstNonNil(result["source_message_id"], result["message_id"]),
		"messageTime": result["message_time"], "text": result["text"],
	})
}

func messageSubscriptionLoginSessionResource(attrs map[string]any) jsonapi.Resource {
	return jsonapi.NewResource("message-subscription-login-sessions", "telegram", attrs)
}

func platformAdapterResource(row appmessaging.PlatformAdapterRow) jsonapi.Resource {
	return jsonapi.NewResource("platform-adapters", strconv.FormatUint(uint64(row.Adapter.ID), 10), map[string]any{
		"provider": row.Adapter.Provider, "displayName": row.Adapter.DisplayName, "enabled": row.Adapter.Enabled,
		"config": rawJSONValue(row.Adapter.Config), "hasBotToken": row.HasBotToken, "hasChatId": row.HasChatID,
		"chatId": row.ChatID, "createdAt": row.Adapter.CreatedAt, "updatedAt": row.Adapter.UpdatedAt,
	})
}

func platformAdapterTestResource(result map[string]any) jsonapi.Resource {
	return jsonapi.NewResource("platform-adapter-test-results", "current", map[string]any{
		"status": result["status"], "chatId": result["chat_id"], "messageId": result["message_id"],
	})
}

func ingestedMessageResource(row appmessaging.MessageRow) jsonapi.Resource {
	message := row.Message
	subscription := row.Subscription
	resource := jsonapi.NewResource("ingested-messages", strconv.FormatUint(uint64(message.ID), 10), map[string]any{
		"subscriptionId": message.SubscriptionID, "subscriptionTitle": subscription.Title, "sourceRef": subscription.SourceRef,
		"provider": message.Provider, "sourceMessageId": message.SourceMessageID, "messageTime": message.MessageTime,
		"text": message.Text, "filterDecision": message.FilterDecision, "filterReason": message.FilterReason,
		"filterStatus": message.FilterStatus, "relatedSymbols": rawJSONValue(message.RelatedSymbols), "filteredAt": message.FilteredAt,
		"filterId": message.FilterID, "createdAt": message.CreatedAt, "updatedAt": message.UpdatedAt,
	})
	resource.Relationships = map[string]jsonapi.Relationship{
		"subscription": {Data: map[string]string{"type": "message-subscriptions", "id": strconv.FormatUint(uint64(message.SubscriptionID), 10)}},
	}
	return resource
}

func ingestedMessagesPublic(rows []appmessaging.MessageRow) []jsonapi.Resource {
	out := make([]jsonapi.Resource, 0, len(rows))
	for _, row := range rows {
		out = append(out, ingestedMessageResource(row))
	}
	return out
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}
