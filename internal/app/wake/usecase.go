package wake

import (
	"context"
	"fmt"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/market"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domaintelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/telegram"
	domainwake "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/wake"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type Usecase struct {
	repo    Repository
	service Service
	tx      Transactor
}

func NewUsecase(repo Repository, service Service, tx ...Transactor) Usecase {
	u := Usecase{repo: repo, service: service}
	if len(tx) > 0 {
		u.tx = tx[0]
	}
	return u
}

type Repository interface {
	List(ctx context.Context, filter RepositoryListFilter) ([]domainwake.Plan, error)
	ListDue(ctx context.Context, limit int) ([]domainwake.Plan, error)
	Create(ctx context.Context, row *domainwake.Plan) error
	Find(ctx context.Context, id uint) (*domainwake.Plan, bool, error)
	Save(ctx context.Context, row *domainwake.Plan) error
	Delete(ctx context.Context, id uint) error
	FindMeetingSnapshot(ctx context.Context, id uint) (*domainmeeting.Meeting, bool, error)
	CreateMeeting(ctx context.Context, meeting *domainmeeting.Meeting) error
	AppendMeetingEvent(ctx context.Context, event *domainmeeting.Event) error
	CreateMeetingReference(ctx context.Context, ref *domainmeeting.Reference) error
	ResearchTeamReady(ctx context.Context, teamID uint) (bool, string, error)
	LatestRealtimeQuote(ctx context.Context, code string) (*domainmarket.RealtimeQuote, bool, error)
	LatestDailyBar(ctx context.Context, code string) (*domainmarket.DailyBar, bool, error)
	RecentTelegramMessages(ctx context.Context, filter MessageFilter) ([]domaintelegram.Message, error)
}

type RepositoryListFilter struct {
	ResearchTeamID string
	Status         string
	MeetingID      string
	Overdue        bool
	Limit          int
	CursorID       uint64
}

type Service interface {
	JSON(value any) domainkernel.JSON
	NormalizeCode(code string) (string, error)
	RefreshRealtimeQuote(ctx context.Context, code string) (*domainmarket.RealtimeQuote, error)
}

type MessageFilter struct {
	Since      *time.Time
	Decisions  []string
	ChannelIDs []string
	Limit      int
}

type Transactor interface {
	WithTx(ctx context.Context, fn func(Repository) error) error
}

type Page struct {
	Limit  int
	Cursor string
}

type ListFilter struct {
	ResearchTeamID string
	Status         string
	MeetingID      string
	Overdue        bool
	Page           Page
}

type ListResult struct {
	Rows       []domainwake.Plan
	NextCursor string
}

type FireResult struct {
	Plan    domainwake.Plan
	Meeting *domainmeeting.Meeting
}

type DueStats struct {
	Checked    int
	Fired      int
	Dispatched int
	Failed     int
}

func (u Usecase) List(ctx context.Context, filter ListFilter) (ListResult, error) {
	page := filter.Page
	if page.Limit <= 0 {
		page.Limit = 100
	}
	cursorID := uint64(0)
	if page.Cursor != "" {
		cursorID, _ = strconv.ParseUint(page.Cursor, 10, 64)
	}
	rows, err := u.repo.List(ctx, RepositoryListFilter{ResearchTeamID: filter.ResearchTeamID, Status: filter.Status, MeetingID: filter.MeetingID, Overdue: filter.Overdue, Limit: page.Limit + 1, CursorID: cursorID})
	if err != nil {
		return ListResult{}, err
	}
	nextCursor := ""
	if len(rows) > page.Limit {
		nextCursor = strconv.FormatUint(uint64(rows[page.Limit-1].ID), 10)
		rows = rows[:page.Limit]
	}
	return ListResult{Rows: rows, NextCursor: nextCursor}, nil
}

func (u Usecase) ProcessDue(ctx context.Context, limit int, dispatch func(*domainmeeting.Meeting) error) (DueStats, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := u.repo.ListDue(ctx, limit)
	if err != nil {
		return DueStats{}, err
	}
	stats := DueStats{}
	for i := range rows {
		stats.Checked++
		var meeting *domainmeeting.Meeting
		if err := u.withTx(ctx, func(repo Repository) error {
			plan, found, err := repo.Find(ctx, rows[i].ID)
			if err != nil || !found || plan.Status != domainkernel.WakeActive || !wakePlanDue(plan, time.Now()) {
				return err
			}
			if err := ValidatePlan(plan); err != nil {
				return u.cancelInvalidPlan(ctx, repo, plan, err)
			}
			shouldFire, summary, err := u.evaluatePlan(ctx, repo, u.service, plan)
			if err != nil {
				return err
			}
			if !shouldFire {
				return u.reschedulePlan(ctx, repo, plan, summary)
			}
			meeting, err = u.firePlan(ctx, repo, plan, summary)
			return err
		}); err != nil {
			stats.Failed++
			continue
		}
		if meeting == nil {
			continue
		}
		stats.Fired++
		if dispatch != nil {
			if err := dispatch(meeting); err != nil {
				stats.Failed++
				continue
			}
		}
		stats.Dispatched++
	}
	return stats, nil
}

func (u Usecase) Create(ctx context.Context, row domainwake.Plan) (*domainwake.Plan, error) {
	if row.ResearchTeamID == 0 {
		return nil, fmt.Errorf("research team is required")
	}
	if row.Status == "" {
		row.Status = domainkernel.WakeActive
	}
	if row.TriggerConfig == nil {
		row.TriggerConfig = u.service.JSON(map[string]any{})
	}
	if err := ValidatePlan(&row); err != nil {
		return nil, err
	}
	if err := u.withTx(ctx, func(repo Repository) error {
		ready, reason, err := repo.ResearchTeamReady(ctx, row.ResearchTeamID)
		if err != nil {
			return err
		}
		if !ready {
			return fmt.Errorf("%s", reason)
		}
		return repo.Create(ctx, &row)
	}); err != nil {
		return nil, err
	}
	return &row, nil
}

func (u Usecase) Update(ctx context.Context, id uint, input domainwake.Plan) (*domainwake.Plan, bool, error) {
	var row *domainwake.Plan
	found := false
	if err := u.withTx(ctx, func(repo Repository) error {
		var err error
		row, found, err = repo.Find(ctx, id)
		if err != nil || !found {
			return err
		}
		if input.ResearchTeamID == 0 {
			return fmt.Errorf("research team is required")
		}
		if input.Status == "" {
			input.Status = domainkernel.WakeActive
		}
		if input.TriggerConfig == nil {
			input.TriggerConfig = u.service.JSON(map[string]any{})
		}
		input.ID = row.ID
		input.CreatedAt = row.CreatedAt
		if err := ValidatePlan(&input); err != nil {
			return err
		}
		ready, reason, err := repo.ResearchTeamReady(ctx, input.ResearchTeamID)
		if err != nil {
			return err
		}
		if !ready {
			return fmt.Errorf("%s", reason)
		}
		row = &input
		return repo.Save(ctx, row)
	}); err != nil {
		return nil, found, err
	}
	return row, found, nil
}

func (u Usecase) Fire(ctx context.Context, id uint) (*FireResult, bool, error) {
	var row *domainwake.Plan
	var meeting *domainmeeting.Meeting
	found := false
	if err := u.withTx(ctx, func(repo Repository) error {
		var err error
		row, found, err = repo.Find(ctx, id)
		if err != nil || !found {
			return err
		}
		if row.Status != domainkernel.WakeActive {
			return fmt.Errorf("only active wake plans can be fired; current status is %s", row.Status)
		}
		if err := ValidatePlan(row); err != nil {
			return err
		}
		meeting, err = u.firePlan(ctx, repo, row, "")
		return err
	}); err != nil {
		return nil, found, err
	}
	if !found {
		return nil, false, nil
	}
	return &FireResult{Plan: *row, Meeting: meeting}, true, nil
}

func (u Usecase) SetStatus(ctx context.Context, id uint, status domainkernel.WakePlanStatus) (*domainwake.Plan, bool, error) {
	var row *domainwake.Plan
	found := false
	if err := u.withTx(ctx, func(tx Repository) error {
		var err error
		row, found, err = tx.Find(ctx, id)
		if err != nil || !found {
			return err
		}
		if status == domainkernel.WakeActive {
			if err := ValidatePlan(row); err != nil {
				return err
			}
		}
		row.Status = status
		return tx.Save(ctx, row)
	}); err != nil {
		return nil, found, err
	}
	return row, true, nil
}

func (u Usecase) Delete(ctx context.Context, id uint) error {
	return u.withTx(ctx, func(tx Repository) error {
		return tx.Delete(ctx, id)
	})
}

func wakePlanDue(plan *domainwake.Plan, now time.Time) bool {
	return plan.NextCheckAt == nil || !plan.NextCheckAt.After(now)
}

func (u Usecase) evaluatePlan(ctx context.Context, repo Repository, service Service, plan *domainwake.Plan) (bool, string, error) {
	switch plan.TriggerType {
	case domainkernel.WakeTime:
		if plan.NextCheckAt == nil || !plan.NextCheckAt.After(time.Now()) {
			return true, "Time wake condition reached.", nil
		}
		return false, "", nil
	case domainkernel.WakeIndicator:
		return u.evaluateIndicatorWake(ctx, repo, service, plan)
	case domainkernel.WakeEvent:
		return u.evaluateEventWake(ctx, repo, plan)
	default:
		return false, "", fmt.Errorf("unsupported wake trigger type %s", plan.TriggerType)
	}
}

func (u Usecase) evaluateIndicatorWake(ctx context.Context, repo Repository, service Service, plan *domainwake.Plan) (bool, string, error) {
	cfg := wakeConfig(plan)
	conditions := indicatorConditionsFromConfig(cfg)
	if len(conditions) == 0 {
		conditions = []map[string]any{cfg}
	}
	mode := strings.ToLower(firstNonEmptyString(stringFromConfig(cfg["mode"]), stringFromConfig(cfg["logic"]), stringFromConfig(cfg["condition_mode"]), "all"))
	summaries := make([]string, 0, len(conditions))
	matched := 0
	for _, condition := range conditions {
		ok, summary, err := u.evaluateIndicatorCondition(ctx, repo, service, condition)
		if err != nil {
			return false, "", err
		}
		summaries = append(summaries, summary)
		if ok {
			matched++
			if mode == "any" || mode == "or" {
				return true, strings.Join(summaries, "; "), nil
			}
		} else if mode != "any" && mode != "or" {
			return false, strings.Join(summaries, "; "), nil
		}
	}
	if mode == "any" || mode == "or" {
		return matched > 0, strings.Join(summaries, "; "), nil
	}
	return matched == len(conditions), strings.Join(summaries, "; "), nil
}

func (u Usecase) evaluateIndicatorCondition(ctx context.Context, repo Repository, service Service, cfg map[string]any) (bool, string, error) {
	code := strings.TrimSpace(firstNonEmptyString(stringFromConfig(cfg["code"]), stringFromConfig(cfg["symbol"]), stringFromConfig(cfg["ticker"])))
	normalizedCode, err := service.NormalizeCode(code)
	if err != nil {
		normalizedCode = code
	}
	field := normalizeIndicatorField(firstNonEmptyString(stringFromConfig(cfg["field"]), stringFromConfig(cfg["metric"]), "price"))
	operator := strings.TrimSpace(firstNonEmptyString(stringFromConfig(cfg["operator"]), stringFromConfig(cfg["op"]), ">="))
	thresholdText := firstNonEmptyString(stringFromConfig(cfg["threshold"]), stringFromConfig(cfg["target"]), stringFromConfig(cfg["targetPrice"]), stringFromConfig(cfg["target_price"]), stringFromConfig(cfg["value"]), stringFromConfig(cfg["targetValue"]), stringFromConfig(cfg["target_value"]))
	threshold, err := decimal.NewFromString(thresholdText)
	if strings.TrimSpace(normalizedCode) == "" || err != nil {
		return false, "", fmt.Errorf("indicator wake requires code and numeric threshold")
	}
	if field != "price" && field != "change_pct" {
		if value, ok, err := latestDailyBarField(ctx, repo, normalizedCode, field); err != nil {
			return false, "", err
		} else if ok {
			return compareIndicatorValue(normalizedCode, field, value, threshold, operator), indicatorSummary(normalizedCode, field, value, threshold, operator), nil
		}
	}
	quote, found, err := repo.LatestRealtimeQuote(ctx, normalizedCode)
	if err != nil {
		return false, "", err
	}
	if !found {
		refreshed, refreshErr := service.RefreshRealtimeQuote(ctx, normalizedCode)
		if refreshErr != nil {
			if value, ok, err := latestDailyBarField(ctx, repo, normalizedCode, field); err != nil {
				return false, "", err
			} else if ok {
				return compareIndicatorValue(normalizedCode, field, value, threshold, operator), indicatorSummary(normalizedCode, field, value, threshold, operator), nil
			}
			return false, "", refreshErr
		}
		quote = refreshed
	}
	value := quote.Price
	if field == "change_pct" {
		value = quote.ChangePct
	}
	return compareIndicatorValue(normalizedCode, field, value, threshold, operator), indicatorSummary(normalizedCode, field, value, threshold, operator), nil
}

func (u Usecase) evaluateEventWake(ctx context.Context, repo Repository, plan *domainwake.Plan) (bool, string, error) {
	cfg := wakeConfig(plan)
	since := plan.LastRunAt
	if since == nil && !plan.CreatedAt.IsZero() {
		since = &plan.CreatedAt
	}
	rows, err := repo.RecentTelegramMessages(ctx, MessageFilter{
		Since:      since,
		Decisions:  stringsFromConfig(firstNonNil(cfg["decisions"], cfg["decision"], cfg["filterDecision"], cfg["filter_decision"])),
		ChannelIDs: stringsFromConfig(firstNonNil(cfg["channelIds"], cfg["channelID"], cfg["channel_ids"], cfg["channels"], cfg["channel_id"])),
		Limit:      50,
	})
	if err != nil {
		return false, "", err
	}
	keywords := stringsFromConfig(firstNonNil(cfg["keywords"], cfg["keyword"], cfg["contains"], cfg["text_contains"]))
	excludes := stringsFromConfig(firstNonNil(cfg["exclude_keywords"], cfg["not_contains"]))
	symbols := stringsFromConfig(firstNonNil(cfg["relatedSymbols"], cfg["related_symbols"], cfg["symbols"], cfg["codes"]))
	keywordMode := strings.ToLower(firstNonEmptyString(stringFromConfig(cfg["keywordMode"]), stringFromConfig(cfg["keyword_mode"]), stringFromConfig(cfg["matchMode"]), stringFromConfig(cfg["match_mode"]), "any"))
	symbolMode := strings.ToLower(firstNonEmptyString(stringFromConfig(cfg["symbolMode"]), stringFromConfig(cfg["symbol_mode"]), "any"))
	regexText := strings.TrimSpace(firstNonEmptyString(stringFromConfig(cfg["regex"]), stringFromConfig(cfg["pattern"])))
	var compiled *regexp.Regexp
	if regexText != "" {
		re, err := regexp.Compile("(?i)" + regexText)
		if err != nil {
			return false, "", err
		}
		compiled = re
	}
	for _, row := range rows {
		text := strings.ToLower(row.Text)
		if len(excludes) > 0 && containsAnyKeyword(text, excludes) {
			continue
		}
		if len(keywords) > 0 && !keywordsMatch(text, keywords, keywordMode) {
			continue
		}
		if compiled != nil && !compiled.MatchString(row.Text) {
			continue
		}
		if len(symbols) > 0 && !symbolsMatch(row, symbols, symbolMode) {
			continue
		}
		return true, fmt.Sprintf("Matched Telegram event message #%d.", row.ID), nil
	}
	return false, "No matching event message.", nil
}

func (u Usecase) firePlan(ctx context.Context, repo Repository, plan *domainwake.Plan, resultSummary string) (*domainmeeting.Meeting, error) {
	now := time.Now()
	plan.Status = domainkernel.WakeFired
	plan.FiredAt = &now
	plan.LastRunAt = &now
	summary := strings.TrimSpace(resultSummary)
	if summary == "" {
		summary = "Wake plan fired and created a follow-up meeting."
	}
	plan.ResultSummary = &summary
	if err := repo.Save(ctx, plan); err != nil {
		return nil, err
	}

	config := wakeConfig(plan)
	topic := strings.TrimSpace(stringFromConfig(config["topic"]))
	sourceTopic := "Follow-up meeting"
	var sourceMeeting *domainmeeting.Meeting
	if plan.MeetingID != nil {
		if foundMeeting, found, err := repo.FindMeetingSnapshot(ctx, *plan.MeetingID); err != nil {
			return nil, err
		} else if found {
			sourceMeeting = foundMeeting
			if strings.TrimSpace(foundMeeting.Topic) != "" {
				sourceTopic = foundMeeting.Topic
			}
		}
	}
	if topic == "" {
		topic = "Wake follow-up: " + sourceTopic
	}

	meeting := domainmeeting.Meeting{ResearchTeamID: plan.ResearchTeamID, Topic: topic, TriggerSource: "wake_plan", Status: domainkernel.MeetingQueued, Tags: u.service.JSON(nil)}
	if err := repo.CreateMeeting(ctx, &meeting); err != nil {
		return nil, err
	}
	if sourceMeeting != nil {
		note := fmt.Sprintf("Automatically linked from wake plan #%d.", plan.ID)
		ref := domainmeeting.Reference{
			SourceMeetingID:       meeting.ID,
			TargetMeetingID:       &sourceMeeting.ID,
			ReferenceType:         "meeting",
			Note:                  &note,
			TargetTopicSnapshot:   sourceMeeting.Topic,
			TargetSummarySnapshot: sourceMeeting.Summary,
		}
		if err := repo.CreateMeetingReference(ctx, &ref); err != nil {
			return nil, err
		}
	}
	if err := repo.AppendMeetingEvent(ctx, &domainmeeting.Event{
		MeetingID: meeting.ID,
		Type:      domainkernel.EventSystem,
		Content:   "Meeting submitted for execution.",
		Payload:   u.service.JSON(map[string]any{"status": "queued"}),
	}); err != nil {
		return nil, err
	}
	_ = repo.AppendMeetingEvent(ctx, &domainmeeting.Event{
		MeetingID: meeting.ID,
		Type:      domainkernel.EventSystem,
		Content:   fmt.Sprintf("Wake plan #%d fired.\nReason: %s\nSource meeting: %s", plan.ID, plan.Reason, sourceTopic),
		Payload: u.service.JSON(map[string]any{
			"status":            "wake_plan_triggered",
			"wake_plan_id":      plan.ID,
			"source_meeting_id": plan.MeetingID,
			"source_topic":      sourceTopic,
			"trigger_type":      plan.TriggerType,
		}),
	})
	return &meeting, nil
}

func (u Usecase) reschedulePlan(ctx context.Context, repo Repository, plan *domainwake.Plan, resultSummary string) error {
	now := time.Now()
	cfg := wakeConfig(plan)
	interval := intFromConfig(firstNonNil(cfg["intervalSeconds"], cfg["interval_seconds"]), 300)
	if interval < 30 {
		interval = 30
	}
	next := now.Add(time.Duration(interval) * time.Second)
	plan.LastRunAt = &now
	plan.NextCheckAt = &next
	summary := strings.TrimSpace(resultSummary)
	if summary == "" {
		summary = "Wake condition not met; scheduled next check."
	} else {
		summary += "; scheduled next check."
	}
	plan.ResultSummary = &summary
	return repo.Save(ctx, plan)
}

func (u Usecase) cancelInvalidPlan(ctx context.Context, repo Repository, plan *domainwake.Plan, validationErr error) error {
	now := time.Now()
	summary := "Invalid wake plan cancelled: " + validationErr.Error()
	plan.Status = domainkernel.WakeCancelled
	plan.LastRunAt = &now
	plan.ResultSummary = &summary
	return repo.Save(ctx, plan)
}
