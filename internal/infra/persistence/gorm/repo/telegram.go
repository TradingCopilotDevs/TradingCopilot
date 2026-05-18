package repo

import (
	"context"
	"errors"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/meeting"
	domainsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/settings"
	domaintelegram "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/telegram"
	"strings"
	"time"

	apptelegram "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/telegram"
	persistmodel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/model"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type TelegramRepository struct {
	db *gorm.DB
}

func NewTelegramRepository(db *gorm.DB) TelegramRepository {
	return TelegramRepository{db: db}
}

func (r TelegramRepository) FindAppSetting(ctx context.Context, key string) (*domainsettings.AppSetting, bool, error) {
	return NewSettingsRepository(r.db).FindAppSetting(ctx, key)
}

func (r TelegramRepository) SaveAppSetting(ctx context.Context, setting *domainsettings.AppSetting) error {
	return NewSettingsRepository(r.db).SaveAppSetting(ctx, setting)
}

func (r TelegramRepository) TryAcquireLease(ctx context.Context, key string, value domainkernel.JSON, description string, now time.Time, cutoff time.Time, claimable func(domainsettings.AppSetting) bool) (bool, error) {
	return NewSettingsRepository(r.db).TryAcquireLease(ctx, key, value, description, now, cutoff, claimable)
}

func (r TelegramRepository) FindSecret(ctx context.Context, kind domainkernel.SecretKind, name string) (*domainsettings.Secret, bool, error) {
	var row persistmodel.Secret
	err := r.db.WithContext(ctx).Where("kind = ? AND name = ?", kind, name).First(&row).Error
	if err == nil {
		out := secretFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r TelegramRepository) CreateSecret(ctx context.Context, secret *domainsettings.Secret) error {
	row := secretToModel(*secret)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*secret = secretFromModel(row)
	return nil
}

func (r TelegramRepository) SaveSecret(ctx context.Context, secret *domainsettings.Secret) error {
	row := secretToModel(*secret)
	if err := r.db.WithContext(ctx).Save(&row).Error; err != nil {
		return err
	}
	*secret = secretFromModel(row)
	return nil
}

func (r TelegramRepository) UpsertSecret(ctx context.Context, kind domainkernel.SecretKind, name string, encryptedValue string) error {
	row, found, err := r.FindSecret(ctx, kind, name)
	if err != nil {
		return err
	}
	if !found {
		row = &domainsettings.Secret{Kind: kind, Name: name, EncryptedValue: encryptedValue}
		return r.CreateSecret(ctx, row)
	}
	row.EncryptedValue = encryptedValue
	return r.SaveSecret(ctx, row)
}

func (r TelegramRepository) DeleteSecretsByNames(ctx context.Context, kind domainkernel.SecretKind, names []string) error {
	return r.db.WithContext(ctx).Where("kind = ? AND name IN ?", kind, names).Delete(&persistmodel.Secret{}).Error
}

func (r TelegramRepository) ListChannels(ctx context.Context) ([]domaintelegram.Channel, error) {
	var rows []persistmodel.TelegramChannel
	if err := r.db.WithContext(ctx).Order("title").Find(&rows).Error; err != nil {
		return nil, err
	}
	return telegramChannelsToDomain(rows), nil
}

func (r TelegramRepository) CreateChannel(ctx context.Context, channel *domaintelegram.Channel) error {
	row := telegramChannelToModel(*channel)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*channel = telegramChannelFromModel(row)
	return nil
}

func (r TelegramRepository) FindChannel(ctx context.Context, id uint) (*domaintelegram.Channel, bool, error) {
	var row persistmodel.TelegramChannel
	err := r.db.WithContext(ctx).First(&row, id).Error
	if err == nil {
		out := telegramChannelFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r TelegramRepository) SaveChannel(ctx context.Context, channel *domaintelegram.Channel) error {
	row := telegramChannelToModel(*channel)
	if err := r.db.WithContext(ctx).Save(&row).Error; err != nil {
		return err
	}
	*channel = telegramChannelFromModel(row)
	return nil
}

func (r TelegramRepository) ListChannelsForCollect(ctx context.Context, channelID *uint) ([]domaintelegram.Channel, error) {
	var rows []persistmodel.TelegramChannel
	q := r.db.WithContext(ctx)
	if channelID != nil {
		q = q.Where("id = ?", *channelID)
	} else {
		q = q.Where("enabled = ?", true)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return telegramChannelsToDomain(rows), nil
}

func (r TelegramRepository) DeleteChannelGraph(ctx context.Context, id uint, externalRefs []string) error {
	db := r.db.WithContext(ctx)
	if len(externalRefs) > 0 {
		if err := db.Model(&persistmodel.MeetingReference{}).Where("reference_type = ? AND external_ref IN ?", "telegram_message", externalRefs).Updates(map[string]any{"target_deleted": true}).Error; err != nil {
			return err
		}
	}
	if err := db.Delete(&persistmodel.TelegramMessage{}, "channel_id = ?", id).Error; err != nil {
		return err
	}
	return db.Delete(&persistmodel.TelegramChannel{}, id).Error
}

func (r TelegramRepository) ListMessagesByChannel(ctx context.Context, channelID uint) ([]domaintelegram.Message, error) {
	var rows []persistmodel.TelegramMessage
	if err := r.db.WithContext(ctx).Where("channel_id = ?", channelID).Find(&rows).Error; err != nil {
		return nil, err
	}
	return telegramMessagesToDomain(rows), nil
}

func (r TelegramRepository) ListMessages(ctx context.Context, filter apptelegram.RepositoryMessageFilter) ([]domaintelegram.Message, error) {
	var rows []persistmodel.TelegramMessage
	q := r.db.WithContext(ctx).Model(&persistmodel.TelegramMessage{}).Joins("JOIN telegram_channels ON telegram_channels.id = telegram_messages.channel_id")
	if filter.ChannelID != "" {
		q = q.Where("telegram_messages.channel_id = ?", filter.ChannelID)
	}
	if search := strings.TrimSpace(filter.Query); search != "" {
		like := "%" + search + "%"
		q = q.Where("telegram_messages.text LIKE ? OR telegram_channels.title LIKE ?", like, like)
	}
	if filter.OnlyUnfiltered {
		q = q.Where("telegram_messages.filter_decision IS NULL")
	} else if decision := strings.TrimSpace(filter.Decision); decision != "" {
		if decision == "unfiltered" {
			q = q.Where("telegram_messages.filter_decision IS NULL")
		} else {
			q = q.Where("telegram_messages.filter_decision = ?", decision)
		}
	}
	if filter.CursorID > 0 {
		q = q.Where("telegram_messages.id < ?", filter.CursorID)
	}
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if err := q.Order("telegram_messages.id desc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return telegramMessagesToDomain(rows), nil
}

func (r TelegramRepository) CreateMessage(ctx context.Context, message *domaintelegram.Message) error {
	row := telegramMessageToModel(*message)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*message = telegramMessageFromModel(row)
	return nil
}

func (r TelegramRepository) FindMessage(ctx context.Context, id uint) (*domaintelegram.Message, bool, error) {
	var row persistmodel.TelegramMessage
	err := r.db.WithContext(ctx).First(&row, id).Error
	if err == nil {
		out := telegramMessageFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r TelegramRepository) SaveMessage(ctx context.Context, message *domaintelegram.Message) error {
	row := telegramMessageToModel(*message)
	if err := r.db.WithContext(ctx).Save(&row).Error; err != nil {
		return err
	}
	*message = telegramMessageFromModel(row)
	return nil
}

func (r TelegramRepository) DeleteMessage(ctx context.Context, message *domaintelegram.Message, externalRef string) error {
	row := telegramMessageToModel(*message)
	db := r.db.WithContext(ctx)
	if externalRef != "" {
		if err := db.Model(&persistmodel.MeetingReference{}).Where("reference_type = ? AND external_ref = ?", "telegram_message", externalRef).Updates(map[string]any{"target_deleted": true}).Error; err != nil {
			return err
		}
	}
	return db.Delete(&row).Error
}

func (r TelegramRepository) DeleteMessagesByIDsWithRefs(ctx context.Context, ids []uint, externalRefs []string) error {
	db := r.db.WithContext(ctx)
	if len(externalRefs) > 0 {
		if err := db.Model(&persistmodel.MeetingReference{}).Where("reference_type = ? AND external_ref IN ?", "telegram_message", externalRefs).Updates(map[string]any{"target_deleted": true}).Error; err != nil {
			return err
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return db.Delete(&persistmodel.TelegramMessage{}, ids).Error
}

func (r TelegramRepository) ListMessagesForRefilter(ctx context.Context, ids []uint, onlyUnfiltered bool, limit int) ([]domaintelegram.Message, error) {
	var rows []persistmodel.TelegramMessage
	q := r.db.WithContext(ctx).Order("message_time desc")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if len(ids) > 0 {
		q = q.Where("id IN ?", ids)
	} else if onlyUnfiltered {
		q = q.Where("filter_decision IS NULL")
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return telegramMessagesToDomain(rows), nil
}

func (r TelegramRepository) FindChannelForMessage(ctx context.Context, channelID uint) (*domaintelegram.Channel, bool, error) {
	return r.FindChannel(ctx, channelID)
}

func (r TelegramRepository) RecentMeetings(ctx context.Context, limit int) ([]domainmeeting.Meeting, error) {
	var rows []persistmodel.Meeting
	query := r.db.WithContext(ctx).Order("created_at desc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if err := query.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domainmeeting.Meeting, 0, len(rows))
	for _, row := range rows {
		out = append(out, meetingFromModel(row))
	}
	return out, nil
}

func (r TelegramRepository) CreateMeeting(ctx context.Context, meeting *domainmeeting.Meeting) error {
	modelRow := meetingToModel(*meeting)
	if err := r.db.WithContext(ctx).Create(&modelRow).Error; err != nil {
		return err
	}
	*meeting = meetingFromModel(modelRow)
	return nil
}

func (r TelegramRepository) AppendMeetingEvent(ctx context.Context, event *domainmeeting.Event) error {
	if event.Sequence == 0 {
		var maxSeq int
		_ = r.db.WithContext(ctx).Model(&persistmodel.MeetingEvent{}).Where("meeting_id = ?", event.MeetingID).Select("COALESCE(MAX(sequence), 0)").Scan(&maxSeq).Error
		event.Sequence = maxSeq + 1
	}
	modelRow := meetingEventToModel(*event)
	if err := r.db.WithContext(ctx).Create(&modelRow).Error; err != nil {
		return err
	}
	*event = meetingEventFromModel(modelRow)
	return nil
}

func telegramChannelsToDomain(rows []persistmodel.TelegramChannel) []domaintelegram.Channel {
	out := make([]domaintelegram.Channel, 0, len(rows))
	for _, row := range rows {
		out = append(out, telegramChannelFromModel(row))
	}
	return out
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

func telegramMessagesToDomain(rows []persistmodel.TelegramMessage) []domaintelegram.Message {
	out := make([]domaintelegram.Message, 0, len(rows))
	for _, row := range rows {
		out = append(out, telegramMessageFromModel(row))
	}
	return out
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

func secretToModel(row domainsettings.Secret) persistmodel.Secret {
	return persistmodel.Secret{
		ID:             row.ID,
		Kind:           row.Kind,
		Name:           row.Name,
		EncryptedValue: row.EncryptedValue,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}
