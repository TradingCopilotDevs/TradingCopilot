package meeting

import (
	"context"
	domainai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/ai"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/meeting"
	domainpaper "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/paper"
	domainsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/settings"

	persistmodel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/model"
	gormrepo "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/repo"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

func dbContext(db *gorm.DB) context.Context {
	if db != nil && db.Statement != nil && db.Statement.Context != nil {
		return db.Statement.Context
	}
	return context.Background()
}

func saveMeeting(db *gorm.DB, meeting *domainmeeting.Meeting) error {
	return gormrepo.NewMeetingRepository(db).Save(dbContext(db), meeting)
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

func meetingToModel(row domainmeeting.Meeting) persistmodel.Meeting {
	return persistmodel.Meeting{
		ID:               row.ID,
		ResearchTeamID:   row.ResearchTeamID,
		Topic:            row.Topic,
		Status:           row.Status,
		TriggerSource:    row.TriggerSource,
		Summary:          row.Summary,
		Conclusion:       row.Conclusion,
		Tags:             datatypes.JSON(row.Tags),
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

func meetingEventFromModel(row persistmodel.MeetingEvent) domainmeeting.Event {
	return domainmeeting.Event{
		ID:        row.ID,
		MeetingID: row.MeetingID,
		Sequence:  row.Sequence,
		Type:      row.Type,
		RoleKey:   row.RoleKey,
		Content:   row.Content,
		Payload:   domainkernel.JSON(row.Payload),
		CreatedAt: row.CreatedAt,
	}
}

func meetingEventsFromModel(rows []persistmodel.MeetingEvent) []domainmeeting.Event {
	out := make([]domainmeeting.Event, 0, len(rows))
	for _, row := range rows {
		out = append(out, meetingEventFromModel(row))
	}
	return out
}

func meetingReferenceFromModel(row persistmodel.MeetingReference) domainmeeting.Reference {
	return domainmeeting.Reference{
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

func meetingReferencesFromModel(rows []persistmodel.MeetingReference) []domainmeeting.Reference {
	out := make([]domainmeeting.Reference, 0, len(rows))
	for _, row := range rows {
		out = append(out, meetingReferenceFromModel(row))
	}
	return out
}

func agentRolesFromModel(rows []persistmodel.AgentRole) []domainai.AgentRole {
	out := make([]domainai.AgentRole, 0, len(rows))
	for _, row := range rows {
		out = append(out, agentRoleFromModel(row))
	}
	return out
}

func researchTeamRolesFromModel(rows []persistmodel.ResearchTeamRole) []domainai.AgentRole {
	out := make([]domainai.AgentRole, 0, len(rows))
	for _, row := range rows {
		out = append(out, researchTeamRoleFromModel(row))
	}
	return out
}

func researchTeamRoleFromModel(row persistmodel.ResearchTeamRole) domainai.AgentRole {
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

func paperPositionsFromModel(rows []persistmodel.PaperPosition) []domainpaper.Position {
	out := make([]domainpaper.Position, 0, len(rows))
	for _, row := range rows {
		out = append(out, domainpaper.Position{
			ID:            row.ID,
			AccountID:     row.AccountID,
			Code:          row.Code,
			Quantity:      row.Quantity,
			AvgCost:       row.AvgCost,
			CostAmount:    row.CostAmount,
			LastPrice:     row.LastPrice,
			MarketValue:   row.MarketValue,
			UnrealizedPNL: row.UnrealizedPNL,
			RealizedPNL:   row.RealizedPNL,
			UpdatedAt:     row.UpdatedAt,
		})
	}
	return out
}

func paperOrdersFromModel(rows []persistmodel.PaperOrder) []domainpaper.Order {
	out := make([]domainpaper.Order, 0, len(rows))
	for _, row := range rows {
		out = append(out, domainpaper.Order{
			ID:                   row.ID,
			AccountID:            row.AccountID,
			MeetingID:            row.MeetingID,
			Code:                 row.Code,
			Side:                 row.Side,
			Quantity:             row.Quantity,
			Status:               row.Status,
			SuggestedPrice:       row.SuggestedPrice,
			FilledPrice:          row.FilledPrice,
			Reason:               row.Reason,
			SubmittedAt:          row.SubmittedAt,
			ExecuteAfter:         row.ExecuteAfter,
			ExpireAt:             row.ExpireAt,
			SourceMeetingEventID: row.SourceMeetingEventID,
			ExecutionNote:        row.ExecutionNote,
			Commission:           row.Commission,
			StampDuty:            row.StampDuty,
			TransferFee:          row.TransferFee,
			NetAmount:            row.NetAmount,
			CreatedAt:            row.CreatedAt,
			FilledAt:             row.FilledAt,
		})
	}
	return out
}
