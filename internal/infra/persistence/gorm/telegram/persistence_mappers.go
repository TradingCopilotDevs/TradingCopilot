package telegram

import (
	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
	domaintelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/telegram"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"gorm.io/datatypes"
)

func secretFromModel(row persistmodel.Secret) domainsettings.Secret {
	return domainsettings.Secret{
		ID:             row.ID,
		Kind:           row.Kind,
		Name:           row.Name,
		EncryptedValue: row.EncryptedValue,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func appSettingFromModel(row persistmodel.AppSetting) domainsettings.AppSetting {
	return domainsettings.AppSetting{
		Key:         row.Key,
		Value:       domainkernel.JSON(row.Value),
		Description: row.Description,
		UpdatedAt:   row.UpdatedAt,
	}
}

func aiProviderFromModel(row persistmodel.AiProvider) domainai.Provider {
	var secret *domainsettings.Secret
	if row.APIKeySecret != nil {
		converted := secretFromModel(*row.APIKeySecret)
		secret = &converted
	}
	return domainai.Provider{
		ID:             row.ID,
		Name:           row.Name,
		BaseURL:        row.BaseURL,
		APIKeySecretID: row.APIKeySecretID,
		APIKeySecret:   secret,
		DefaultModel:   row.DefaultModel,
		Enabled:        row.Enabled,
		CreatedAt:      row.CreatedAt,
	}
}

func agentRoleFromModel(row persistmodel.AgentRole) domainai.AgentRole {
	var provider *domainai.Provider
	if row.Provider != nil {
		converted := aiProviderFromModel(*row.Provider)
		provider = &converted
	}
	return domainai.AgentRole{
		ID:             row.ID,
		Key:            row.Key,
		Name:           row.Name,
		Responsibility: row.Responsibility,
		PromptTemplate: row.PromptTemplate,
		ProviderID:     row.ProviderID,
		Provider:       provider,
		Model:          row.Model,
		ToolNames:      domainkernel.JSON(row.ToolNames),
		SkillNames:     domainkernel.JSON(row.SkillNames),
		Enabled:        row.Enabled,
		SortOrder:      row.SortOrder,
	}
}

func meetingFromModel(row persistmodel.Meeting) domainmeeting.Meeting {
	return domainmeeting.Meeting{
		ID:               row.ID,
		ResearchTeamID:   row.ResearchTeamID,
		Topic:            row.Topic,
		Status:           row.Status,
		TriggerSource:    row.TriggerSource,
		Summary:          row.Summary,
		Conclusion:       row.Conclusion,
		Tags:             domainkernel.JSON(row.Tags),
		RecapStatus:      row.RecapStatus,
		RecapUpdatedAt:   row.RecapUpdatedAt,
		TokenBudget:      row.TokenBudget,
		RunID:            row.RunID,
		RunAttempt:       row.RunAttempt,
		HeartbeatAt:      row.HeartbeatAt,
		AutoRequeueCount: row.AutoRequeueCount,
		StartedAt:        row.StartedAt,
		CompletedAt:      row.CompletedAt,
		CreatedAt:        row.CreatedAt,
	}
}

func telegramChannelFromModel(row persistmodel.TelegramChannel) domaintelegram.Channel {
	return domaintelegram.Channel{
		ID:            row.ID,
		Title:         row.Title,
		ChannelRef:    row.ChannelRef,
		Enabled:       row.Enabled,
		BackfillLimit: row.BackfillLimit,
		CollectFrom:   row.CollectFrom,
		CreatedAt:     row.CreatedAt,
	}
}

func telegramChannelsFromModel(rows []persistmodel.TelegramChannel) []domaintelegram.Channel {
	out := make([]domaintelegram.Channel, 0, len(rows))
	for _, row := range rows {
		out = append(out, telegramChannelFromModel(row))
	}
	return out
}

func telegramChannelToModel(row domaintelegram.Channel) persistmodel.TelegramChannel {
	return persistmodel.TelegramChannel{
		ID:            row.ID,
		Title:         row.Title,
		ChannelRef:    row.ChannelRef,
		Enabled:       row.Enabled,
		BackfillLimit: row.BackfillLimit,
		CollectFrom:   row.CollectFrom,
		CreatedAt:     row.CreatedAt,
	}
}

func telegramMessageFromModel(row persistmodel.TelegramMessage) domaintelegram.Message {
	return domaintelegram.Message{
		ID:                 row.ID,
		ChannelID:          row.ChannelID,
		MessageID:          row.MessageID,
		MessageTime:        row.MessageTime,
		Text:               row.Text,
		Raw:                domainkernel.JSON(row.Raw),
		FilterDecision:     row.FilterDecision,
		FilterReason:       row.FilterReason,
		RelatedSymbols:     domainkernel.JSON(row.RelatedSymbols),
		FilteredAt:         row.FilteredAt,
		FilterModelRoleKey: row.FilterModelRoleKey,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}
}

func telegramMessagesFromModel(rows []persistmodel.TelegramMessage) []domaintelegram.Message {
	out := make([]domaintelegram.Message, 0, len(rows))
	for _, row := range rows {
		out = append(out, telegramMessageFromModel(row))
	}
	return out
}

func telegramMessageToModel(row domaintelegram.Message) persistmodel.TelegramMessage {
	return persistmodel.TelegramMessage{
		ID:                 row.ID,
		ChannelID:          row.ChannelID,
		MessageID:          row.MessageID,
		MessageTime:        row.MessageTime,
		Text:               row.Text,
		Raw:                datatypes.JSON(row.Raw),
		FilterDecision:     row.FilterDecision,
		FilterReason:       row.FilterReason,
		RelatedSymbols:     datatypes.JSON(row.RelatedSymbols),
		FilteredAt:         row.FilteredAt,
		FilterModelRoleKey: row.FilterModelRoleKey,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}
}

func meetingReferenceToModel(row domainmeeting.Reference) persistmodel.MeetingReference {
	return persistmodel.MeetingReference{
		ID:                    row.ID,
		SourceMeetingID:       row.SourceMeetingID,
		TargetMeetingID:       row.TargetMeetingID,
		ReferenceType:         row.ReferenceType,
		Note:                  row.Note,
		TargetTopicSnapshot:   row.TargetTopicSnapshot,
		TargetSummarySnapshot: row.TargetSummarySnapshot,
		TargetDeleted:         row.TargetDeleted,
		ExternalRef:           row.ExternalRef,
		CreatedAt:             row.CreatedAt,
	}
}
