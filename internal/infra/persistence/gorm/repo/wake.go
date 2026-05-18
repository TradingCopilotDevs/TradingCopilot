package repo

import (
	"context"
	"errors"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/market"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domaintelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/telegram"
	domainwake "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/wake"
	"time"

	appwake "github.com/TradingCopilotDevs/TradingCopilot/internal/app/wake"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type WakeRepository struct {
	db *gorm.DB
}

func NewWakeRepository(db *gorm.DB) WakeRepository {
	return WakeRepository{db: db}
}

func (r WakeRepository) List(ctx context.Context, filter appwake.RepositoryListFilter) ([]domainwake.Plan, error) {
	var rows []persistmodel.WakePlan
	q := r.db.WithContext(ctx).Order("created_at desc, id desc")
	if filter.ResearchTeamID != "" {
		q = q.Where("research_team_id = ?", filter.ResearchTeamID)
	}
	if filter.Status != "" {
		q = q.Where("status = ?", filter.Status)
	}
	if filter.MeetingID != "" {
		q = q.Where("meeting_id = ?", filter.MeetingID)
	}
	if filter.CursorID > 0 {
		q = q.Where("id < ?", filter.CursorID)
	}
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return wakePlansToDomain(rows), nil
}

func (r WakeRepository) ListDue(ctx context.Context, limit int) ([]domainwake.Plan, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []persistmodel.WakePlan
	if err := r.db.WithContext(ctx).
		Where("status = ? AND (next_check_at IS NULL OR next_check_at <= ?)", domainkernel.WakeActive, time.Now()).
		Order("COALESCE(next_check_at, created_at) asc, id asc").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return wakePlansToDomain(rows), nil
}

func (r WakeRepository) Create(ctx context.Context, row *domainwake.Plan) error {
	modelRow := wakePlanToModel(*row)
	if err := r.db.WithContext(ctx).Create(&modelRow).Error; err != nil {
		return err
	}
	*row = wakePlanFromModel(modelRow)
	return nil
}

func (r WakeRepository) Find(ctx context.Context, id uint) (*domainwake.Plan, bool, error) {
	var row persistmodel.WakePlan
	err := r.db.WithContext(ctx).First(&row, id).Error
	if err == nil {
		out := wakePlanFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r WakeRepository) Save(ctx context.Context, row *domainwake.Plan) error {
	modelRow := wakePlanToModel(*row)
	if err := r.db.WithContext(ctx).Save(&modelRow).Error; err != nil {
		return err
	}
	*row = wakePlanFromModel(modelRow)
	return nil
}

func (r WakeRepository) Delete(ctx context.Context, id uint) error {
	return r.db.WithContext(ctx).Delete(&persistmodel.WakePlan{}, id).Error
}

func (r WakeRepository) FindMeetingTopic(ctx context.Context, id uint) (string, bool, error) {
	var row persistmodel.Meeting
	err := r.db.WithContext(ctx).Select("topic").First(&row, id).Error
	if err == nil {
		return row.Topic, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	return "", false, err
}

func (r WakeRepository) CreateMeeting(ctx context.Context, meeting *domainmeeting.Meeting) error {
	row := meetingToModel(*meeting)
	if err := r.db.WithContext(ctx).Create(&row).Error; err != nil {
		return err
	}
	*meeting = meetingFromModel(row)
	return nil
}

func (r WakeRepository) AppendMeetingEvent(ctx context.Context, event *domainmeeting.Event) error {
	db := r.db.WithContext(ctx)
	var maxSeq int
	if err := db.Model(&persistmodel.MeetingEvent{}).
		Where("meeting_id = ?", event.MeetingID).
		Select("COALESCE(MAX(sequence), 0)").
		Scan(&maxSeq).Error; err != nil {
		return err
	}
	event.Sequence = maxSeq + 1
	row := meetingEventToModel(*event)
	if err := db.Create(&row).Error; err != nil {
		return err
	}
	*event = meetingEventFromModel(row)
	return nil
}

func (r WakeRepository) ResearchTeamReady(ctx context.Context, teamID uint) (bool, string, error) {
	return NewMeetingRepository(r.db).ResearchTeamReady(ctx, teamID)
}

func (r WakeRepository) LatestRealtimeQuote(ctx context.Context, code string) (*domainmarket.RealtimeQuote, bool, error) {
	var row persistmodel.RealtimeQuote
	err := r.db.WithContext(ctx).Where("code = ?", code).Order("quote_time desc").First(&row).Error
	if err == nil {
		out := wakeRealtimeQuoteFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r WakeRepository) LatestDailyBar(ctx context.Context, code string) (*domainmarket.DailyBar, bool, error) {
	var row persistmodel.DailyBar
	err := r.db.WithContext(ctx).Where("code = ?", code).Order("trade_date desc").First(&row).Error
	if err == nil {
		out := wakeDailyBarFromModel(row)
		return &out, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}

func (r WakeRepository) RecentTelegramMessages(ctx context.Context, filter appwake.MessageFilter) ([]domaintelegram.Message, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	q := r.db.WithContext(ctx).Order("message_time desc").Limit(limit)
	if filter.Since != nil {
		q = q.Where("message_time >= ?", *filter.Since)
	}
	if len(filter.Decisions) > 0 {
		q = q.Where("filter_decision IN ?", filter.Decisions)
	}
	if len(filter.ChannelIDs) > 0 {
		q = q.Where("channel_id IN ?", filter.ChannelIDs)
	}
	var rows []persistmodel.TelegramMessage
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]domaintelegram.Message, 0, len(rows))
	for _, row := range rows {
		out = append(out, wakeTelegramMessageFromModel(row))
	}
	return out, nil
}

func wakePlansToDomain(rows []persistmodel.WakePlan) []domainwake.Plan {
	out := make([]domainwake.Plan, 0, len(rows))
	for _, row := range rows {
		out = append(out, wakePlanFromModel(row))
	}
	return out
}

func wakePlanFromModel(row persistmodel.WakePlan) domainwake.Plan {
	return domainwake.Plan{
		ID:                   row.ID,
		ResearchTeamID:       row.ResearchTeamID,
		MeetingID:            row.MeetingID,
		TriggerType:          row.TriggerType,
		TriggerConfig:        domainkernel.JSON(row.TriggerConfig),
		Reason:               row.Reason,
		SourceMeetingEventID: row.SourceMeetingEventID,
		SourceRoleKey:        row.SourceRoleKey,
		Status:               row.Status,
		NextCheckAt:          row.NextCheckAt,
		FiredAt:              row.FiredAt,
		LastRunAt:            row.LastRunAt,
		ResultSummary:        row.ResultSummary,
		CreatedAt:            row.CreatedAt,
	}
}

func wakePlanToModel(row domainwake.Plan) persistmodel.WakePlan {
	return persistmodel.WakePlan{
		ID:                   row.ID,
		ResearchTeamID:       row.ResearchTeamID,
		MeetingID:            row.MeetingID,
		TriggerType:          row.TriggerType,
		TriggerConfig:        datatypes.JSON(row.TriggerConfig),
		Reason:               row.Reason,
		SourceMeetingEventID: row.SourceMeetingEventID,
		SourceRoleKey:        row.SourceRoleKey,
		Status:               row.Status,
		NextCheckAt:          row.NextCheckAt,
		FiredAt:              row.FiredAt,
		LastRunAt:            row.LastRunAt,
		ResultSummary:        row.ResultSummary,
		CreatedAt:            row.CreatedAt,
	}
}

func wakeRealtimeQuoteFromModel(row persistmodel.RealtimeQuote) domainmarket.RealtimeQuote {
	return domainmarket.RealtimeQuote{
		ID:        row.ID,
		Code:      row.Code,
		QuoteTime: row.QuoteTime,
		Price:     row.Price,
		ChangePct: row.ChangePct,
		Volume:    row.Volume,
		Amount:    row.Amount,
		Raw:       domainkernel.JSON(row.Raw),
		Provider:  row.Provider,
	}
}

func wakeDailyBarFromModel(row persistmodel.DailyBar) domainmarket.DailyBar {
	return domainmarket.DailyBar{
		ID:        row.ID,
		Code:      row.Code,
		TradeDate: row.TradeDate,
		Open:      row.Open,
		High:      row.High,
		Low:       row.Low,
		Close:     row.Close,
		Volume:    row.Volume,
		Amount:    row.Amount,
		Provider:  row.Provider,
	}
}

func wakeTelegramMessageFromModel(row persistmodel.TelegramMessage) domaintelegram.Message {
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
