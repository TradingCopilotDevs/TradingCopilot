package repo

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	appmessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/messaging"
	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainmsg "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/messaging"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type MessagingRepository struct {
	db *gorm.DB
}

func NewMessagingRepository(db *gorm.DB) MessagingRepository {
	return MessagingRepository{db: db}
}

func (r MessagingRepository) FindAppSetting(ctx context.Context, key string) (*domainsettings.AppSetting, bool, error) {
	return NewSettingsRepository(r.db).FindAppSetting(ctx, key)
}

func (r MessagingRepository) SaveAppSetting(ctx context.Context, setting *domainsettings.AppSetting) error {
	return NewSettingsRepository(r.db).SaveAppSetting(ctx, setting)
}

func (r MessagingRepository) DeleteAppSetting(ctx context.Context, key string) error {
	return NewSettingsRepository(r.db).DeleteAppSetting(ctx, key)
}

func (r MessagingRepository) TryAcquireLease(ctx context.Context, key string, value domainkernel.JSON, description string, now time.Time, cutoff time.Time, claimable func(domainsettings.AppSetting) bool) (bool, error) {
	return NewSettingsRepository(r.db).TryAcquireLease(ctx, key, value, description, now, cutoff, claimable)
}

func (r MessagingRepository) FindSecret(ctx context.Context, kind domainkernel.SecretKind, name string) (*domainsettings.Secret, bool, error) {
	return NewSettingsRepository(r.db).FindSecret(ctx, kind, name)
}

func (r MessagingRepository) CreateSecret(ctx context.Context, secret *domainsettings.Secret) error {
	return NewSettingsRepository(r.db).CreateSecret(ctx, secret)
}

func (r MessagingRepository) SaveSecret(ctx context.Context, secret *domainsettings.Secret) error {
	return NewSettingsRepository(r.db).UpdateSecret(ctx, secret)
}

func (r MessagingRepository) UpsertSecret(ctx context.Context, kind domainkernel.SecretKind, name string, encryptedValue string) error {
	row := persistmodel.Secret{Kind: kind, Name: name, EncryptedValue: encryptedValue}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "kind"}, {Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"encrypted_value", "updated_at"}),
	}).Create(&row).Error
}

func (r MessagingRepository) DeleteSecretsByNames(ctx context.Context, kind domainkernel.SecretKind, names []string) error {
	return r.db.WithContext(ctx).Where("kind = ? AND name IN ?", kind, names).Delete(&persistmodel.Secret{}).Error
}

func (r MessagingRepository) ListSubscriptionFilters(ctx context.Context) ([]domainmsg.MessageSubscriptionFilter, error) {
	var rows []persistmodel.MessageSubscriptionFilter
	if err := r.db.WithContext(ctx).Order("is_default desc, id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return messageSubscriptionFiltersToDomain(rows), nil
}

func (r MessagingRepository) CreateSubscriptionFilter(ctx context.Context, filter *domainmsg.MessageSubscriptionFilter) error {
	row := messageSubscriptionFilterToModel(*filter)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*filter = messageSubscriptionFilterFromModel(row)
	return nil
}

func (r MessagingRepository) FindSubscriptionFilter(ctx context.Context, id uint) (*domainmsg.MessageSubscriptionFilter, bool, error) {
	var row persistmodel.MessageSubscriptionFilter
	err := r.db.WithContext(ctx).First(&row, id).Error
	if err == nil {
		out := messageSubscriptionFilterFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r MessagingRepository) FindDefaultSubscriptionFilter(ctx context.Context) (*domainmsg.MessageSubscriptionFilter, bool, error) {
	var row persistmodel.MessageSubscriptionFilter
	err := r.db.WithContext(ctx).Where("is_default = ?", true).Order("id").First(&row).Error
	if err == nil {
		out := messageSubscriptionFilterFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r MessagingRepository) SaveSubscriptionFilter(ctx context.Context, filter *domainmsg.MessageSubscriptionFilter) error {
	row := messageSubscriptionFilterToModel(*filter)
	if err := r.db.WithContext(ctx).Save(&row).Error; err != nil {
		return err
	}
	*filter = messageSubscriptionFilterFromModel(row)
	return nil
}

func (r MessagingRepository) ClearDefaultSubscriptionFiltersExcept(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Model(&persistmodel.MessageSubscriptionFilter{}).Where("id <> ?", id).Update("is_default", false).Error
}

func (r MessagingRepository) AssignDefaultFilterToUnboundSubscriptions(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&persistmodel.MessageSubscription{}).Where("filter_id = ? OR filter_id IS NULL", 0).Update("filter_id", id).Error; err != nil {
			return err
		}
		return tx.Exec(`
			UPDATE message_subscription_assignments
			SET filter_id = ?
			WHERE filter_id = 0 OR filter_id IS NULL
		`, id).Error
	})
}

func (r MessagingRepository) DeleteLegacyNewsFilterRole(ctx context.Context) error {
	return r.db.WithContext(ctx).Delete(&persistmodel.AgentRole{}, "key = ?", "news_filter").Error
}

func (r MessagingRepository) ListSubscriptions(ctx context.Context) ([]domainmsg.MessageSubscription, error) {
	var rows []persistmodel.MessageSubscription
	if err := preloadSubscriptionGraph(r.db.WithContext(ctx)).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return messageSubscriptionsToDomain(rows), nil
}

func (r MessagingRepository) CreateSubscription(ctx context.Context, subscription *domainmsg.MessageSubscription) error {
	row := messageSubscriptionToModel(*subscription)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*subscription = messageSubscriptionFromModel(row)
	return nil
}

func (r MessagingRepository) FindSubscription(ctx context.Context, id uint) (*domainmsg.MessageSubscription, bool, error) {
	var row persistmodel.MessageSubscription
	err := preloadSubscriptionGraph(r.db.WithContext(ctx)).First(&row, id).Error
	if err == nil {
		out := messageSubscriptionFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r MessagingRepository) FindSubscriptionByProviderAndSourceRef(ctx context.Context, provider string, sourceRef string) (*domainmsg.MessageSubscription, bool, error) {
	var row persistmodel.MessageSubscription
	err := preloadSubscriptionGraph(r.db.WithContext(ctx)).Where("provider = ? AND source_ref = ?", provider, sourceRef).First(&row).Error
	if err == nil {
		out := messageSubscriptionFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r MessagingRepository) SaveSubscription(ctx context.Context, subscription *domainmsg.MessageSubscription) error {
	row := messageSubscriptionToModel(*subscription)
	if err := r.db.WithContext(ctx).Save(&row).Error; err != nil {
		return err
	}
	*subscription = messageSubscriptionFromModel(row)
	return nil
}

func (r MessagingRepository) ReplaceSubscriptionTeams(ctx context.Context, subscriptionID uint, teamIDs []uint) error {
	subscription, found, err := r.FindSubscription(ctx, subscriptionID)
	if err != nil {
		return err
	}
	if !found || subscription == nil {
		return errors.New("message subscription not found")
	}
	assignments := make([]domainmsg.MessageSubscriptionAssignment, 0, len(teamIDs))
	for _, teamID := range uniqueUintIDs(teamIDs) {
		assignments = append(assignments, domainmsg.MessageSubscriptionAssignment{SubscriptionID: subscriptionID, FilterID: subscription.FilterID, ResearchTeamID: teamID, Enabled: true})
	}
	return r.ReplaceSubscriptionAssignments(ctx, subscriptionID, assignments)
}

func (r MessagingRepository) ReplaceSubscriptionAssignments(ctx context.Context, subscriptionID uint, assignments []domainmsg.MessageSubscriptionAssignment) error {
	assignments = uniqueSubscriptionAssignments(assignments)
	filterIDs := make([]uint, 0, len(assignments))
	teamIDs := make([]uint, 0, len(assignments))
	for _, assignment := range assignments {
		filterIDs = append(filterIDs, assignment.FilterID)
		teamIDs = append(teamIDs, assignment.ResearchTeamID)
	}
	filterIDs = uniqueUintIDs(filterIDs)
	teamIDs = uniqueUintIDs(teamIDs)
	db := r.db.WithContext(ctx)
	if len(filterIDs) > 0 {
		var count int64
		if err := db.Model(&persistmodel.MessageSubscriptionFilter{}).Where("id IN ?", filterIDs).Count(&count).Error; err != nil {
			return err
		}
		if count != int64(len(filterIDs)) {
			return errors.New("one or more message subscription filters were not found")
		}
	}
	if len(teamIDs) > 0 {
		var count int64
		if err := db.Model(&persistmodel.ResearchTeam{}).Where("id IN ?", teamIDs).Count(&count).Error; err != nil {
			return err
		}
		if count != int64(len(teamIDs)) {
			return errors.New("one or more research teams were not found")
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		var existing []persistmodel.MessageSubscriptionAssignment
		if err := tx.Where("subscription_id = ?", subscriptionID).Find(&existing).Error; err != nil {
			return err
		}
		existingByKey := map[string]persistmodel.MessageSubscriptionAssignment{}
		for _, row := range existing {
			existingByKey[subscriptionAssignmentKey(row.FilterID, row.ResearchTeamID)] = row
		}
		kept := map[uint]bool{}
		for _, assignment := range assignments {
			key := subscriptionAssignmentKey(assignment.FilterID, assignment.ResearchTeamID)
			if existing, ok := existingByKey[key]; ok {
				kept[existing.ID] = true
				if !existing.Enabled {
					if err := tx.Model(&persistmodel.MessageSubscriptionAssignment{}).
						Where("id = ?", existing.ID).
						Update("enabled", true).Error; err != nil {
						return err
					}
				}
				continue
			}
			row := messageSubscriptionAssignmentToModel(assignment)
			row.ID = 0
			row.SubscriptionID = subscriptionID
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			kept[row.ID] = true
		}
		deleteIDs := make([]uint, 0, len(existing))
		for _, row := range existing {
			if !kept[row.ID] {
				deleteIDs = append(deleteIDs, row.ID)
			}
		}
		if len(deleteIDs) == 0 {
			return nil
		}
		if err := tx.Model(&persistmodel.IngestedMessageFilterResult{}).
			Where("assignment_id IN ?", deleteIDs).
			Update("assignment_id", gorm.Expr("NULL")).Error; err != nil {
			return err
		}
		return tx.Delete(&persistmodel.MessageSubscriptionAssignment{}, deleteIDs).Error
	})
}

func (r MessagingRepository) ListSubscriptionTeamIDs(ctx context.Context, subscriptionID uint) ([]uint, error) {
	var ids []uint
	if err := r.db.WithContext(ctx).Model(&persistmodel.MessageSubscriptionAssignment{}).
		Where("subscription_id = ? AND enabled = ?", subscriptionID, true).
		Order("research_team_id").
		Distinct().
		Pluck("research_team_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func (r MessagingRepository) ListSubscriptionAssignments(ctx context.Context, subscriptionID uint) ([]domainmsg.MessageSubscriptionAssignment, error) {
	var rows []persistmodel.MessageSubscriptionAssignment
	if err := r.db.WithContext(ctx).Preload("Filter").
		Where("subscription_id = ? AND enabled = ?", subscriptionID, true).
		Order("id").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return messageSubscriptionAssignmentsToDomain(rows), nil
}

func (r MessagingRepository) ResearchTeamAssetClasses(ctx context.Context, teamIDs []uint) (map[uint]string, error) {
	out := map[uint]string{}
	if len(teamIDs) == 0 {
		return out, nil
	}
	var rows []persistmodel.ResearchTeam
	if err := r.db.WithContext(ctx).Select("id", "asset_class").Where("id IN ?", teamIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.ID] = row.AssetClass
	}
	return out, nil
}

func (r MessagingRepository) ResearchTeamReady(ctx context.Context, teamID uint) (bool, string, error) {
	return NewMeetingRepository(r.db).ResearchTeamReady(ctx, teamID)
}

func (r MessagingRepository) ListSubscriptionsForCollect(ctx context.Context, subscriptionID *uint) ([]domainmsg.MessageSubscription, error) {
	var rows []persistmodel.MessageSubscription
	q := r.db.WithContext(ctx).
		Joins("JOIN message_subscription_assignments ON message_subscription_assignments.subscription_id = message_subscriptions.id AND message_subscription_assignments.enabled = ?", true).
		Where("message_subscriptions.enabled = ?", true).
		Distinct("message_subscriptions.*")
	if subscriptionID != nil {
		q = q.Where("message_subscriptions.id = ?", *subscriptionID)
	}
	if err := preloadSubscriptionGraph(q).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return messageSubscriptionsToDomain(rows), nil
}

func (r MessagingRepository) DeleteSubscriptionGraph(ctx context.Context, id uint, externalRefs []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(externalRefs) > 0 {
			if err := tx.Model(&persistmodel.MeetingReference{}).Where("reference_type = ? AND external_ref IN ?", "ingested_message", externalRefs).Updates(map[string]any{"target_deleted": true}).Error; err != nil {
				return err
			}
		}
		if err := tx.Delete(&persistmodel.IngestedMessageFilterResult{}, "subscription_id = ?", id).Error; err != nil {
			return err
		}
		if err := tx.Delete(&persistmodel.IngestedMessage{}, "subscription_id = ?", id).Error; err != nil {
			return err
		}
		if err := tx.Delete(&persistmodel.MessageSubscriptionAssignment{}, "subscription_id = ?", id).Error; err != nil {
			return err
		}
		return tx.Delete(&persistmodel.MessageSubscription{}, id).Error
	})
}

func (r MessagingRepository) ListMessagesBySubscription(ctx context.Context, subscriptionID uint) ([]domainmsg.IngestedMessage, error) {
	var rows []persistmodel.IngestedMessage
	if err := preloadMessageFilterResults(r.db.WithContext(ctx)).Where("subscription_id = ?", subscriptionID).Find(&rows).Error; err != nil {
		return nil, err
	}
	return ingestedMessagesToDomain(rows), nil
}

func (r MessagingRepository) ListMessages(ctx context.Context, filter appmessaging.RepositoryMessageFilter) ([]domainmsg.IngestedMessage, error) {
	var rows []persistmodel.IngestedMessage
	q := r.db.WithContext(ctx).Model(&persistmodel.IngestedMessage{}).Joins("JOIN message_subscriptions ON message_subscriptions.id = ingested_messages.subscription_id")
	if strings.TrimSpace(filter.SubscriptionID) != "" {
		if id, err := strconv.ParseUint(filter.SubscriptionID, 10, 64); err == nil && id > 0 {
			q = q.Where("ingested_messages.subscription_id = ?", id)
		}
	}
	if strings.TrimSpace(filter.ResearchTeamID) != "" {
		q = q.Joins("JOIN ingested_message_filter_results ON ingested_message_filter_results.message_id = ingested_messages.id").
			Where("ingested_message_filter_results.research_team_id = ?", strings.TrimSpace(filter.ResearchTeamID)).
			Distinct("ingested_messages.*")
	}
	if strings.TrimSpace(filter.Query) != "" {
		like := "%" + strings.TrimSpace(filter.Query) + "%"
		q = q.Where("ingested_messages.text LIKE ? OR message_subscriptions.title LIKE ?", like, like)
	}
	if filter.OnlyUnfiltered {
		if strings.TrimSpace(filter.ResearchTeamID) != "" {
			q = q.Where("ingested_message_filter_results.filter_status = ? OR (ingested_message_filter_results.filter_status = '' AND ingested_message_filter_results.filter_decision IS NULL)", domainmsg.FilterStatusUnfiltered)
		} else {
			q = q.Where("ingested_messages.filter_status = ? OR (ingested_messages.filter_status = '' AND ingested_messages.filter_decision IS NULL)", domainmsg.FilterStatusUnfiltered)
		}
	}
	if strings.TrimSpace(filter.Decision) != "" {
		decisionColumn := "ingested_messages.filter_decision"
		statusColumn := "ingested_messages.filter_status"
		if strings.TrimSpace(filter.ResearchTeamID) != "" {
			decisionColumn = "ingested_message_filter_results.filter_decision"
			statusColumn = "ingested_message_filter_results.filter_status"
		}
		if filter.Decision == "unfiltered" {
			q = q.Where(statusColumn+" = ? OR ("+statusColumn+" = '' AND "+decisionColumn+" IS NULL)", domainmsg.FilterStatusUnfiltered)
		} else if filter.Decision == domainmsg.FilterStatusFiltering || filter.Decision == domainmsg.FilterStatusFailed {
			q = q.Where(statusColumn+" = ?", filter.Decision)
		} else {
			q = q.Where(decisionColumn+" = ?", filter.Decision)
		}
	}
	if filter.CursorID > 0 {
		q = q.Where("ingested_messages.id < ?", filter.CursorID)
	}
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if err := preloadMessageFilterResults(q).Order("ingested_messages.id desc").Limit(filter.Limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return ingestedMessagesToDomain(rows), nil
}

func (r MessagingRepository) ListFeedbackMessages(ctx context.Context, filter appmessaging.RepositoryFeedbackMessageFilter) ([]domainmsg.IngestedMessage, error) {
	var rows []persistmodel.IngestedMessage
	q := r.db.WithContext(ctx).Model(&persistmodel.IngestedMessage{}).
		Preload("Subscription").
		Where("feedback_label IS NOT NULL AND feedback_label <> ''")
	if strings.TrimSpace(filter.SubscriptionID) != "" {
		if id, err := strconv.ParseUint(filter.SubscriptionID, 10, 64); err == nil && id > 0 {
			q = q.Where("subscription_id = ?", id)
		}
	}
	if provider := strings.TrimSpace(filter.Provider); provider != "" {
		q = q.Where("provider = ?", provider)
	}
	if label := strings.ToLower(strings.TrimSpace(filter.Label)); label != "" {
		q = q.Where("feedback_label = ?", label)
	}
	if filter.CursorID > 0 {
		q = q.Where("id < ?", filter.CursorID)
	}
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if err := preloadMessageFilterResults(q).Order("id desc").Limit(filter.Limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return ingestedMessagesToDomain(rows), nil
}

func (r MessagingRepository) CreateMessage(ctx context.Context, message *domainmsg.IngestedMessage) error {
	row := ingestedMessageToModel(*message)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*message = ingestedMessageFromModel(row)
	return nil
}

func (r MessagingRepository) FindMessage(ctx context.Context, id uint) (*domainmsg.IngestedMessage, bool, error) {
	var row persistmodel.IngestedMessage
	err := preloadMessageFilterResults(r.db.WithContext(ctx)).First(&row, id).Error
	if err == nil {
		out := ingestedMessageFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r MessagingRepository) FindMessageBySource(ctx context.Context, subscriptionID uint, sourceMessageID string) (*domainmsg.IngestedMessage, bool, error) {
	var row persistmodel.IngestedMessage
	err := preloadMessageFilterResults(r.db.WithContext(ctx)).Where("subscription_id = ? AND source_message_id = ?", subscriptionID, sourceMessageID).First(&row).Error
	if err == nil {
		out := ingestedMessageFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r MessagingRepository) SaveMessage(ctx context.Context, message *domainmsg.IngestedMessage) error {
	row := ingestedMessageToModel(*message)
	if err := r.db.WithContext(ctx).Save(&row).Error; err != nil {
		return err
	}
	*message = ingestedMessageFromModel(row)
	return nil
}

func (r MessagingRepository) SaveMessageFilterResult(ctx context.Context, result *domainmsg.IngestedMessageFilterResult) error {
	row := ingestedMessageFilterResultToModel(*result)
	if row.FilterStatus == "" {
		row.FilterStatus = domainmsg.FilterStatusUnfiltered
	}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "message_id"}, {Name: "filter_id"}, {Name: "research_team_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"subscription_id", "assignment_id", "filter_decision", "filter_reason", "filter_status", "related_symbols", "filtered_at", "updated_at",
		}),
	}).Create(&row).Error
	if err != nil {
		return err
	}
	var saved persistmodel.IngestedMessageFilterResult
	if err := r.db.WithContext(ctx).Where("message_id = ? AND filter_id = ? AND research_team_id = ?", row.MessageID, row.FilterID, row.ResearchTeamID).First(&saved).Error; err != nil {
		return err
	}
	*result = ingestedMessageFilterResultFromModel(saved)
	return nil
}

func (r MessagingRepository) DeleteMessageFilterResults(ctx context.Context, messageID uint) error {
	return r.db.WithContext(ctx).Delete(&persistmodel.IngestedMessageFilterResult{}, "message_id = ?", messageID).Error
}

func (r MessagingRepository) ListMessageFilterResults(ctx context.Context, messageID uint) ([]domainmsg.IngestedMessageFilterResult, error) {
	var rows []persistmodel.IngestedMessageFilterResult
	if err := r.db.WithContext(ctx).Preload("Filter").Where("message_id = ?", messageID).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return ingestedMessageFilterResultsToDomain(rows), nil
}

func (r MessagingRepository) DeleteMessage(ctx context.Context, message *domainmsg.IngestedMessage, externalRefs []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(externalRefs) > 0 {
			if err := tx.Model(&persistmodel.MeetingReference{}).Where("reference_type = ? AND external_ref IN ?", "ingested_message", externalRefs).Updates(map[string]any{"target_deleted": true}).Error; err != nil {
				return err
			}
		}
		if err := tx.Delete(&persistmodel.IngestedMessageFilterResult{}, "message_id = ?", message.ID).Error; err != nil {
			return err
		}
		return tx.Delete(&persistmodel.IngestedMessage{}, message.ID).Error
	})
}

func (r MessagingRepository) DeleteMessagesByIDsWithRefs(ctx context.Context, ids []uint, externalRefs []string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if len(externalRefs) > 0 {
			if err := tx.Model(&persistmodel.MeetingReference{}).Where("reference_type = ? AND external_ref IN ?", "ingested_message", externalRefs).Updates(map[string]any{"target_deleted": true}).Error; err != nil {
				return err
			}
		}
		if len(ids) == 0 {
			return nil
		}
		if err := tx.Delete(&persistmodel.IngestedMessageFilterResult{}, "message_id IN ?", ids).Error; err != nil {
			return err
		}
		return tx.Delete(&persistmodel.IngestedMessage{}, ids).Error
	})
}

func (r MessagingRepository) ListMessagesForRefilter(ctx context.Context, ids []uint, onlyUnfiltered bool, limit int) ([]domainmsg.IngestedMessage, error) {
	var rows []persistmodel.IngestedMessage
	q := r.db.WithContext(ctx).Model(&persistmodel.IngestedMessage{})
	if len(ids) > 0 {
		q = q.Where("id IN ?", ids)
	}
	if onlyUnfiltered {
		q = q.Where("filter_status = ? OR (filter_status = '' AND filter_decision IS NULL)", domainmsg.FilterStatusUnfiltered)
	}
	if limit <= 0 {
		limit = 100
	}
	if err := preloadMessageFilterResults(q).Order("id desc").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	return ingestedMessagesToDomain(rows), nil
}

func (r MessagingRepository) FindSubscriptionForMessage(ctx context.Context, subscriptionID uint) (*domainmsg.MessageSubscription, bool, error) {
	return r.FindSubscription(ctx, subscriptionID)
}

func (r MessagingRepository) RecentMeetings(ctx context.Context, limit int) ([]domainmeeting.Meeting, error) {
	return NewTelegramRepository(r.db).RecentMeetings(ctx, limit)
}

func (r MessagingRepository) CreateMeeting(ctx context.Context, meeting *domainmeeting.Meeting) error {
	return NewMeetingRepository(r.db).Create(ctx, meeting)
}

func (r MessagingRepository) AppendMeetingEvent(ctx context.Context, event *domainmeeting.Event) error {
	return NewMeetingRepository(r.db).AppendEvent(ctx, event)
}

func (r MessagingRepository) CreateMeetingReference(ctx context.Context, ref *domainmeeting.Reference) error {
	return NewMeetingRepository(r.db).CreateReference(ctx, ref)
}

func (r MessagingRepository) FindMeetingReferenceByExternalRef(ctx context.Context, referenceType string, externalRef string) (*domainmeeting.Reference, bool, error) {
	var row persistmodel.MeetingReference
	err := r.db.WithContext(ctx).Where("reference_type = ? AND external_ref = ?", referenceType, externalRef).First(&row).Error
	if err == nil {
		out := domainmeeting.Reference{
			ID: row.ID, SourceMeetingID: row.SourceMeetingID, TargetMeetingID: row.TargetMeetingID,
			ReferenceType: row.ReferenceType, Note: row.Note, TargetTopicSnapshot: row.TargetTopicSnapshot,
			TargetSummarySnapshot: row.TargetSummarySnapshot, TargetDeleted: row.TargetDeleted, ExternalRef: row.ExternalRef,
			CreatedAt: row.CreatedAt,
		}
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r MessagingRepository) FindMeeting(ctx context.Context, id uint) (*domainmeeting.Meeting, bool, error) {
	return NewMeetingRepository(r.db).Find(ctx, id)
}

func (r MessagingRepository) FindAgentRole(ctx context.Context, key string) (*domainai.AgentRole, bool, error) {
	return NewAIRepository(r.db).FindRole(ctx, key)
}

func (r MessagingRepository) FindAIProvider(ctx context.Context, id uint) (*domainai.Provider, bool, error) {
	return NewAIRepository(r.db).FindProvider(ctx, id)
}

func (r MessagingRepository) ListPlatformAdapters(ctx context.Context) ([]domainmsg.PlatformAdapter, error) {
	var rows []persistmodel.PlatformAdapter
	if err := r.db.WithContext(ctx).Order("id").Find(&rows).Error; err != nil {
		return nil, err
	}
	return platformAdaptersToDomain(rows), nil
}

func (r MessagingRepository) CreatePlatformAdapter(ctx context.Context, adapter *domainmsg.PlatformAdapter) error {
	row := platformAdapterToModel(*adapter)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*adapter = platformAdapterFromModel(row)
	return nil
}

func (r MessagingRepository) FindPlatformAdapter(ctx context.Context, id uint) (*domainmsg.PlatformAdapter, bool, error) {
	var row persistmodel.PlatformAdapter
	err := r.db.WithContext(ctx).First(&row, id).Error
	if err == nil {
		out := platformAdapterFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r MessagingRepository) SavePlatformAdapter(ctx context.Context, adapter *domainmsg.PlatformAdapter) error {
	row := platformAdapterToModel(*adapter)
	if err := r.db.WithContext(ctx).Save(&row).Error; err != nil {
		return err
	}
	*adapter = platformAdapterFromModel(row)
	return nil
}

func (r MessagingRepository) DeletePlatformAdapter(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&persistmodel.PlatformAdapter{}, id).Error
}

func messageSubscriptionsToDomain(rows []persistmodel.MessageSubscription) []domainmsg.MessageSubscription {
	out := make([]domainmsg.MessageSubscription, 0, len(rows))
	for _, row := range rows {
		out = append(out, messageSubscriptionFromModel(row))
	}
	return out
}

func preloadSubscriptionGraph(db *gorm.DB) *gorm.DB {
	return db.Preload("Filter").Preload("Assignments", "enabled = ?", true).Preload("Assignments.Filter")
}

func preloadMessageFilterResults(db *gorm.DB) *gorm.DB {
	return db.Preload("FilterResults").Preload("FilterResults.Filter")
}

func messageSubscriptionAssignmentsToDomain(rows []persistmodel.MessageSubscriptionAssignment) []domainmsg.MessageSubscriptionAssignment {
	out := make([]domainmsg.MessageSubscriptionAssignment, 0, len(rows))
	for _, row := range rows {
		out = append(out, messageSubscriptionAssignmentFromModel(row))
	}
	return out
}

func messageSubscriptionAssignmentFromModel(row persistmodel.MessageSubscriptionAssignment) domainmsg.MessageSubscriptionAssignment {
	var filter *domainmsg.MessageSubscriptionFilter
	if row.Filter != nil {
		value := messageSubscriptionFilterFromModel(*row.Filter)
		filter = &value
	}
	return domainmsg.MessageSubscriptionAssignment{
		ID: row.ID, SubscriptionID: row.SubscriptionID, FilterID: row.FilterID, Filter: filter,
		ResearchTeamID: row.ResearchTeamID, Enabled: row.Enabled, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func messageSubscriptionAssignmentToModel(row domainmsg.MessageSubscriptionAssignment) persistmodel.MessageSubscriptionAssignment {
	return persistmodel.MessageSubscriptionAssignment{
		ID: row.ID, SubscriptionID: row.SubscriptionID, FilterID: row.FilterID, ResearchTeamID: row.ResearchTeamID,
		Enabled: row.Enabled, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func messageSubscriptionFiltersToDomain(rows []persistmodel.MessageSubscriptionFilter) []domainmsg.MessageSubscriptionFilter {
	out := make([]domainmsg.MessageSubscriptionFilter, 0, len(rows))
	for _, row := range rows {
		out = append(out, messageSubscriptionFilterFromModel(row))
	}
	return out
}

func messageSubscriptionFilterFromModel(row persistmodel.MessageSubscriptionFilter) domainmsg.MessageSubscriptionFilter {
	return domainmsg.MessageSubscriptionFilter{
		ID: row.ID, Name: row.Name, Description: row.Description, PromptTemplate: row.PromptTemplate,
		ProviderID: row.ProviderID, Model: row.Model, Enabled: row.Enabled, IsDefault: row.IsDefault,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func messageSubscriptionFilterToModel(row domainmsg.MessageSubscriptionFilter) persistmodel.MessageSubscriptionFilter {
	return persistmodel.MessageSubscriptionFilter{
		ID: row.ID, Name: row.Name, Description: row.Description, PromptTemplate: row.PromptTemplate,
		ProviderID: row.ProviderID, Model: row.Model, Enabled: row.Enabled, IsDefault: row.IsDefault,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func messageSubscriptionFromModel(row persistmodel.MessageSubscription) domainmsg.MessageSubscription {
	var filter *domainmsg.MessageSubscriptionFilter
	if row.Filter != nil {
		value := messageSubscriptionFilterFromModel(*row.Filter)
		filter = &value
	}
	assignments := messageSubscriptionAssignmentsToDomain(row.Assignments)
	teamIDs := researchTeamIDsFromAssignments(assignments)
	filterID := row.FilterID
	if filterID == 0 && len(assignments) > 0 {
		filterID = assignments[0].FilterID
	}
	return domainmsg.MessageSubscription{
		ID: row.ID, Provider: row.Provider, Title: row.Title, SourceRef: row.SourceRef, Enabled: row.Enabled,
		FilterID: filterID, Filter: filter, TeamIDs: teamIDs, Assignments: assignments, BackfillLimit: row.BackfillLimit,
		PollIntervalSeconds: row.PollIntervalSeconds, CollectFrom: row.CollectFrom, LastCollectedAt: row.LastCollectedAt,
		NextCollectAt: row.NextCollectAt, LastCollectError: row.LastCollectError, Config: domainkernel.JSON(row.Config),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func researchTeamIDsFromAssignments(rows []domainmsg.MessageSubscriptionAssignment) []uint {
	out := make([]uint, 0, len(rows))
	seen := map[uint]bool{}
	for _, row := range rows {
		if !row.Enabled || row.ResearchTeamID == 0 || seen[row.ResearchTeamID] {
			continue
		}
		seen[row.ResearchTeamID] = true
		out = append(out, row.ResearchTeamID)
	}
	return out
}

func messageSubscriptionToModel(row domainmsg.MessageSubscription) persistmodel.MessageSubscription {
	return persistmodel.MessageSubscription{
		ID: row.ID, Provider: row.Provider, Title: row.Title, SourceRef: row.SourceRef, Enabled: row.Enabled,
		FilterID: row.FilterID, BackfillLimit: row.BackfillLimit, PollIntervalSeconds: row.PollIntervalSeconds,
		CollectFrom: row.CollectFrom, LastCollectedAt: row.LastCollectedAt, NextCollectAt: row.NextCollectAt,
		LastCollectError: row.LastCollectError, Config: datatypes.JSON(row.Config),
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func ingestedMessagesToDomain(rows []persistmodel.IngestedMessage) []domainmsg.IngestedMessage {
	out := make([]domainmsg.IngestedMessage, 0, len(rows))
	for _, row := range rows {
		out = append(out, ingestedMessageFromModel(row))
	}
	return out
}

func ingestedMessageFilterResultsToDomain(rows []persistmodel.IngestedMessageFilterResult) []domainmsg.IngestedMessageFilterResult {
	out := make([]domainmsg.IngestedMessageFilterResult, 0, len(rows))
	for _, row := range rows {
		out = append(out, ingestedMessageFilterResultFromModel(row))
	}
	return out
}

func ingestedMessageFilterResultFromModel(row persistmodel.IngestedMessageFilterResult) domainmsg.IngestedMessageFilterResult {
	var filter *domainmsg.MessageSubscriptionFilter
	if row.Filter != nil {
		value := messageSubscriptionFilterFromModel(*row.Filter)
		filter = &value
	}
	return domainmsg.IngestedMessageFilterResult{
		ID: row.ID, MessageID: row.MessageID, SubscriptionID: row.SubscriptionID, AssignmentID: row.AssignmentID,
		FilterID: row.FilterID, Filter: filter, ResearchTeamID: row.ResearchTeamID, FilterDecision: row.FilterDecision,
		FilterReason: row.FilterReason, FilterStatus: normalizeFilterStatus(row.FilterStatus, row.FilterDecision, row.FilteredAt),
		RelatedSymbols: domainkernel.JSON(row.RelatedSymbols), FilteredAt: row.FilteredAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func ingestedMessageFilterResultToModel(row domainmsg.IngestedMessageFilterResult) persistmodel.IngestedMessageFilterResult {
	return persistmodel.IngestedMessageFilterResult{
		ID: row.ID, MessageID: row.MessageID, SubscriptionID: row.SubscriptionID, AssignmentID: row.AssignmentID,
		FilterID: row.FilterID, ResearchTeamID: row.ResearchTeamID, FilterDecision: row.FilterDecision,
		FilterReason: row.FilterReason, FilterStatus: normalizeFilterStatus(row.FilterStatus, row.FilterDecision, row.FilteredAt),
		RelatedSymbols: datatypes.JSON(row.RelatedSymbols), FilteredAt: row.FilteredAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func ingestedMessageFromModel(row persistmodel.IngestedMessage) domainmsg.IngestedMessage {
	out := domainmsg.IngestedMessage{
		ID: row.ID, SubscriptionID: row.SubscriptionID, Provider: row.Provider, SourceMessageID: row.SourceMessageID,
		MessageTime: row.MessageTime, Text: row.Text, Raw: domainkernel.JSON(row.Raw), FilterDecision: row.FilterDecision,
		FilterReason: row.FilterReason, FilterStatus: normalizeFilterStatus(row.FilterStatus, row.FilterDecision, row.FilteredAt),
		RelatedSymbols: domainkernel.JSON(row.RelatedSymbols), FilteredAt: row.FilteredAt,
		FilterID: row.FilterID, FeedbackLabel: row.FeedbackLabel, FeedbackComment: row.FeedbackComment, FeedbackAt: row.FeedbackAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	out.FilterResults = ingestedMessageFilterResultsToDomain(row.FilterResults)
	if row.Subscription != nil {
		subscription := messageSubscriptionFromModel(*row.Subscription)
		out.Subscription = &subscription
	}
	return out
}

func ingestedMessageToModel(row domainmsg.IngestedMessage) persistmodel.IngestedMessage {
	return persistmodel.IngestedMessage{
		ID: row.ID, SubscriptionID: row.SubscriptionID, Provider: row.Provider, SourceMessageID: row.SourceMessageID,
		MessageTime: row.MessageTime, Text: row.Text, Raw: datatypes.JSON(row.Raw), FilterDecision: row.FilterDecision,
		FilterReason: row.FilterReason, FilterStatus: normalizeFilterStatus(row.FilterStatus, row.FilterDecision, row.FilteredAt),
		RelatedSymbols: datatypes.JSON(row.RelatedSymbols), FilteredAt: row.FilteredAt,
		FilterID: row.FilterID, FeedbackLabel: row.FeedbackLabel, FeedbackComment: row.FeedbackComment, FeedbackAt: row.FeedbackAt,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func normalizeFilterStatus(status string, decision *domainkernel.NewsDecision, filteredAt *time.Time) string {
	switch strings.TrimSpace(status) {
	case domainmsg.FilterStatusUnfiltered, domainmsg.FilterStatusFiltering, domainmsg.FilterStatusFiltered, domainmsg.FilterStatusFailed:
		return strings.TrimSpace(status)
	}
	if filteredAt != nil {
		if decision == nil {
			return domainmsg.FilterStatusFailed
		}
		return domainmsg.FilterStatusFiltered
	}
	return domainmsg.FilterStatusUnfiltered
}

func platformAdaptersToDomain(rows []persistmodel.PlatformAdapter) []domainmsg.PlatformAdapter {
	out := make([]domainmsg.PlatformAdapter, 0, len(rows))
	for _, row := range rows {
		out = append(out, platformAdapterFromModel(row))
	}
	return out
}

func platformAdapterFromModel(row persistmodel.PlatformAdapter) domainmsg.PlatformAdapter {
	return domainmsg.PlatformAdapter{
		ID: row.ID, Provider: row.Provider, DisplayName: row.DisplayName, Enabled: row.Enabled,
		Config: domainkernel.JSON(row.Config), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func platformAdapterToModel(row domainmsg.PlatformAdapter) persistmodel.PlatformAdapter {
	return persistmodel.PlatformAdapter{
		ID: row.ID, Provider: row.Provider, DisplayName: row.DisplayName, Enabled: row.Enabled,
		Config: datatypes.JSON(row.Config), CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func uniqueSubscriptionAssignments(values []domainmsg.MessageSubscriptionAssignment) []domainmsg.MessageSubscriptionAssignment {
	seen := map[string]bool{}
	out := make([]domainmsg.MessageSubscriptionAssignment, 0, len(values))
	for _, value := range values {
		if value.FilterID == 0 || value.ResearchTeamID == 0 {
			continue
		}
		key := subscriptionAssignmentKey(value.FilterID, value.ResearchTeamID)
		if seen[key] {
			continue
		}
		seen[key] = true
		value.Enabled = true
		out = append(out, value)
	}
	return out
}

func subscriptionAssignmentKey(filterID uint, researchTeamID uint) string {
	return strconv.FormatUint(uint64(filterID), 10) + ":" + strconv.FormatUint(uint64(researchTeamID), 10)
}
