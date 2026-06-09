package market

import (
	"context"
	"fmt"
	domainmarket "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/market"
	domainpaper "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/paper"
	"strings"
	"time"
)

type Usecase struct {
	repo      Repository
	market    MarketDataService
	tx        Transactor
	taskQueue TaskQueue
}

func NewUsecase(repo Repository, market MarketDataService, tx ...Transactor) Usecase {
	u := Usecase{repo: repo, market: market}
	if len(tx) > 0 {
		u.tx = tx[0]
	}
	return u
}

func (u Usecase) WithTaskQueue(queue TaskQueue) Usecase {
	u.taskQueue = queue
	return u
}

type Repository interface {
	ListSymbols(ctx context.Context, q string, limit int, cursor string) ([]domainmarket.Symbol, error)
	CreateSymbol(ctx context.Context, row *domainmarket.Symbol) error
	UpsertSymbol(ctx context.Context, row domainmarket.Symbol) error
	FindSymbol(ctx context.Context, code string) (*domainmarket.Symbol, bool, error)
	UpdateSymbol(ctx context.Context, code string, updates map[string]any) error
	DeleteSymbol(ctx context.Context, code string) error
	DailyBars(ctx context.Context, code string, limit int, before *time.Time) ([]domainmarket.DailyBar, error)
	UpsertDailyBar(ctx context.Context, row domainmarket.DailyBar) error
	CreateRealtimeQuote(ctx context.Context, row *domainmarket.RealtimeQuote) error
	LatestRealtimeQuote(ctx context.Context, code string) (*domainmarket.RealtimeQuote, bool, error)
	ListWatchlistItems(ctx context.Context, researchTeamID uint, limit int, cursor string) ([]domainmarket.WatchlistItem, error)
	UpsertWatchlist(ctx context.Context, researchTeamID uint, code string, note *string, active bool) (*domainmarket.WatchlistItem, error)
	FindSymbolByCode(ctx context.Context, code string) (*domainmarket.Symbol, bool, error)
	WatchlistExists(ctx context.Context, id uint) (bool, error)
	DeleteWatchlist(ctx context.Context, id uint) error
	ActiveWatchlistToolItems(ctx context.Context, researchTeamID uint) ([]domainmarket.WatchlistItem, error)
	ToolDailyBars(ctx context.Context, code string, limit int) ([]domainmarket.DailyBar, error)
	ToolPaperPositions(ctx context.Context) ([]domainpaper.Position, error)
}

type MarketDataService interface {
	EnsureAShareCode(value string) (string, error)
	InferExchange(code string) string
	FetchSymbols(ctx context.Context) (provider string, rows []domainmarket.Symbol, err error)
	Series(ctx context.Context, code string, rangeKey string) ([]map[string]any, error)
	FetchDailyBars(ctx context.Context, code string) ([]domainmarket.DailyBar, error)
	RefreshQuotes(ctx context.Context, codes []string) (map[string]*domainmarket.RealtimeQuote, error)
}

type Transactor interface {
	WithTx(ctx context.Context, fn func(Repository) error) error
}

type TaskQueue interface {
	EnqueueMarketTask(ctx context.Context, task Task) (string, error)
}

type Page struct {
	Limit  int
	Cursor string
}

type SymbolList struct {
	Rows       []domainmarket.Symbol
	NextCursor string
}

type DailyBarList struct {
	Rows       []domainmarket.DailyBar
	NextCursor string
}

type WatchlistRow struct {
	Item  domainmarket.WatchlistItem
	Extra map[string]any
}

type WatchlistList struct {
	Rows       []WatchlistRow
	NextCursor string
}

const (
	TaskSyncSymbols      = "sync_symbols"
	TaskRefreshQuote     = "refresh_quote"
	TaskRefreshDailyBars = "refresh_daily_bars"
)

type Task struct {
	Action string `json:"action"`
	Code   string `json:"code,omitempty"`
}

type TaskResult struct {
	Task     Task
	TaskID   string
	Status   string
	Provider string
	Count    int
}

func (u Usecase) ListSymbols(ctx context.Context, q string, page Page) (SymbolList, error) {
	if q == "" {
		return SymbolList{}, nil
	}
	rows, err := u.repo.ListSymbols(ctx, q, page.Limit+1, page.Cursor)
	if err != nil {
		return SymbolList{}, err
	}
	nextCursor := ""
	if len(rows) > page.Limit {
		nextCursor = rows[page.Limit-1].Code
		rows = rows[:page.Limit]
	}
	return SymbolList{Rows: rows, NextCursor: nextCursor}, nil
}

func (u Usecase) CreateSymbol(ctx context.Context, row domainmarket.Symbol) (*domainmarket.Symbol, error) {
	code, err := u.market.EnsureAShareCode(row.Code)
	if err != nil {
		return nil, err
	}
	row.Code = code
	if row.Exchange == "" {
		row.Exchange = u.market.InferExchange(code)
	}
	if err := u.withTx(ctx, func(repo Repository) error {
		return repo.CreateSymbol(ctx, &row)
	}); err != nil {
		return nil, err
	}
	return &row, nil
}

func (u Usecase) UpdateSymbol(ctx context.Context, code string, payload domainmarket.Symbol) (*domainmarket.Symbol, bool, error) {
	var row *domainmarket.Symbol
	found := false
	if err := u.withTx(ctx, func(tx Repository) error {
		var err error
		row, found, err = tx.FindSymbol(ctx, code)
		if err != nil || !found {
			return err
		}
		updates := map[string]any{
			"name":       payload.Name,
			"exchange":   payload.Exchange,
			"industry":   payload.Industry,
			"active":     payload.Active,
			"updated_at": time.Now(),
		}
		if err := tx.UpdateSymbol(ctx, code, updates); err != nil {
			return err
		}
		row, _, err = tx.FindSymbol(ctx, code)
		return err
	}); err != nil {
		return nil, found, err
	}
	if !found {
		return nil, false, nil
	}
	return row, true, nil
}

func (u Usecase) DeleteSymbol(ctx context.Context, code string) error {
	return u.withTx(ctx, func(tx Repository) error {
		return tx.DeleteSymbol(ctx, code)
	})
}

func (u Usecase) SyncSymbols(ctx context.Context) (string, int, error) {
	var provider string
	var synced int
	rowsProvider, rows, err := u.market.FetchSymbols(ctx)
	if err != nil {
		return "", 0, err
	}
	provider = rowsProvider
	err = u.withTx(ctx, func(repo Repository) error {
		for _, row := range rows {
			if err := repo.UpsertSymbol(ctx, row); err != nil {
				return err
			}
			synced++
		}
		return nil
	})
	return provider, synced, err
}

func (u Usecase) EnqueueTask(ctx context.Context, task Task) (TaskResult, error) {
	normalized, err := u.normalizeTask(task)
	if err != nil {
		return TaskResult{}, err
	}
	if u.taskQueue == nil {
		return TaskResult{}, fmt.Errorf("market task queue is not configured")
	}
	taskID, err := u.taskQueue.EnqueueMarketTask(ctx, normalized)
	if err != nil {
		return TaskResult{}, err
	}
	return TaskResult{Task: normalized, TaskID: taskID, Status: "queued"}, nil
}

func (u Usecase) ProcessTask(ctx context.Context, task Task) (TaskResult, error) {
	normalized, err := u.normalizeTask(task)
	if err != nil {
		return TaskResult{}, err
	}
	result := TaskResult{Task: normalized, Status: "completed"}
	switch normalized.Action {
	case TaskSyncSymbols:
		provider, synced, err := u.SyncSymbols(ctx)
		result.Provider = provider
		result.Count = synced
		return result, err
	case TaskRefreshQuote:
		quotes, err := u.market.RefreshQuotes(ctx, []string{normalized.Code})
		if err != nil {
			return result, err
		}
		quote := quotes[normalized.Code]
		if quote == nil {
			return result, fmt.Errorf("market quote not returned for %s", normalized.Code)
		}
		if quote.ID == 0 {
			if err := u.withTx(ctx, func(repo Repository) error {
				return repo.CreateRealtimeQuote(ctx, quote)
			}); err != nil {
				return result, err
			}
		}
		result.Provider = quote.Provider
		result.Count = 1
		return result, nil
	case TaskRefreshDailyBars:
		rows, err := u.market.FetchDailyBars(ctx, normalized.Code)
		if err != nil {
			return result, err
		}
		if err := u.withTx(ctx, func(repo Repository) error {
			for _, row := range rows {
				if err := repo.UpsertDailyBar(ctx, row); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return result, err
		}
		result.Count = len(rows)
		if len(rows) > 0 {
			result.Provider = rows[0].Provider
		}
		return result, nil
	default:
		return TaskResult{}, fmt.Errorf("unknown market task action: %s", normalized.Action)
	}
}

func (u Usecase) CurrentQuote(ctx context.Context, code string, refresh bool) (*domainmarket.RealtimeQuote, error) {
	normalized, err := u.market.EnsureAShareCode(code)
	if err != nil {
		return nil, err
	}
	if refresh {
		quotes, err := u.market.RefreshQuotes(ctx, []string{normalized})
		if err == nil {
			if quote := quotes[normalized]; quote != nil {
				if quote.ID == 0 {
					if txErr := u.withTx(ctx, func(repo Repository) error {
						return repo.CreateRealtimeQuote(ctx, quote)
					}); txErr != nil {
						return nil, txErr
					}
				}
				return quote, nil
			}
		}
		if cached, found, cacheErr := u.repo.LatestRealtimeQuote(ctx, normalized); cacheErr == nil && found {
			return cached, nil
		} else if cacheErr != nil {
			return nil, cacheErr
		}
		return nil, err
	}
	quote, found, err := u.repo.LatestRealtimeQuote(ctx, normalized)
	if err != nil || !found {
		return nil, err
	}
	return quote, nil
}

func (u Usecase) Series(ctx context.Context, code string, rangeKey string) ([]map[string]any, error) {
	return u.market.Series(ctx, code, rangeKey)
}

func (u Usecase) DailyBars(ctx context.Context, code string, refresh bool, page Page) (DailyBarList, error) {
	normalized, err := u.market.EnsureAShareCode(code)
	if err != nil {
		return DailyBarList{}, err
	}
	if refresh {
		rows, err := u.market.FetchDailyBars(ctx, normalized)
		if err != nil {
			return DailyBarList{}, err
		}
		if err := u.withTx(ctx, func(repo Repository) error {
			for _, row := range rows {
				if err := repo.UpsertDailyBar(ctx, row); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return DailyBarList{}, err
		}
	}
	var before *time.Time
	if page.Cursor != "" {
		if cursorTime, err := time.Parse(time.RFC3339, page.Cursor); err == nil {
			before = &cursorTime
		}
	}
	rows, err := u.repo.DailyBars(ctx, normalized, page.Limit+1, before)
	if err != nil {
		return DailyBarList{}, err
	}
	nextCursor := ""
	if len(rows) > page.Limit {
		nextCursor = rows[page.Limit-1].TradeDate.Format(time.RFC3339)
		rows = rows[:page.Limit]
	}
	return DailyBarList{Rows: rows, NextCursor: nextCursor}, nil
}

func (u Usecase) ListWatchlist(ctx context.Context, researchTeamID uint, page Page) (WatchlistList, error) {
	if researchTeamID == 0 {
		return WatchlistList{}, fmt.Errorf("research team is required")
	}
	items, err := u.repo.ListWatchlistItems(ctx, researchTeamID, page.Limit+1, page.Cursor)
	if err != nil {
		return WatchlistList{}, err
	}
	nextCursor := ""
	if len(items) > page.Limit {
		nextCursor = items[page.Limit-1].Code
		items = items[:page.Limit]
	}
	activeCodes := make([]string, 0, len(items))
	for _, item := range items {
		if item.Active {
			activeCodes = append(activeCodes, item.Code)
		}
	}
	quotes, quoteErr := u.market.RefreshQuotes(ctx, activeCodes)
	if quoteErr != nil {
		quotes = map[string]*domainmarket.RealtimeQuote{}
		for _, code := range activeCodes {
			if cached, found, err := u.repo.LatestRealtimeQuote(ctx, code); err == nil && found {
				quotes[code] = cached
			}
		}
	}
	_ = u.withTx(ctx, func(repo Repository) error {
		for _, quote := range quotes {
			if quote == nil || quote.ID != 0 {
				continue
			}
			if err := repo.CreateRealtimeQuote(ctx, quote); err != nil {
				return err
			}
		}
		return nil
	})
	rows := make([]WatchlistRow, 0, len(items))
	for _, item := range items {
		symbol, _, _ := u.repo.FindSymbolByCode(ctx, item.Code)
		symbolName, exchange := "", ""
		if symbol != nil {
			symbolName = symbol.Name
			exchange = symbol.Exchange
		}
		var price, changePct, quoteTime any
		if quote := quotes[item.Code]; quote != nil {
			price = quote.Price
			changePct = quote.ChangePct
			quoteTime = quote.QuoteTime
		}
		rows = append(rows, WatchlistRow{Item: item, Extra: map[string]any{"symbolName": nullable(symbolName), "exchange": nullable(exchange), "price": price, "changePct": changePct, "quoteTime": quoteTime}})
	}
	return WatchlistList{Rows: rows, NextCursor: nextCursor}, nil
}

func (u Usecase) UpsertWatchlist(ctx context.Context, researchTeamID uint, code string, note *string, active bool) (*domainmarket.WatchlistItem, error) {
	var item *domainmarket.WatchlistItem
	if researchTeamID == 0 {
		return nil, fmt.Errorf("research team is required")
	}
	normalized, err := u.market.EnsureAShareCode(code)
	if err != nil {
		return nil, err
	}
	err = u.withTx(ctx, func(repo Repository) error {
		var err error
		item, err = repo.UpsertWatchlist(ctx, researchTeamID, normalized, note, active)
		return err
	})
	return item, err
}

func (u Usecase) WatchlistExists(ctx context.Context, id uint) bool {
	exists, _ := u.repo.WatchlistExists(ctx, id)
	return exists
}

func (u Usecase) DeleteWatchlist(ctx context.Context, id uint) error {
	return u.withTx(ctx, func(tx Repository) error {
		return tx.DeleteWatchlist(ctx, id)
	})
}

func (u Usecase) QueryTool(ctx context.Context, tool string, arguments map[string]any) ([]map[string]any, error) {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "watchlist", "market.watchlist":
		teamID := uintFromToolArgs(arguments["researchTeamId"])
		items, err := u.repo.ActiveWatchlistToolItems(ctx, teamID)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			out = append(out, map[string]any{"code": item.Code, "note": item.Note})
		}
		return out, nil
	case "realtime_quote", "market.realtime_quote":
		code := fmt.Sprint(arguments["code"])
		quote, err := u.CurrentQuote(ctx, code, true)
		if err != nil {
			return nil, err
		}
		return []map[string]any{{"code": quote.Code, "price": quote.Price, "quote_time": quote.QuoteTime}}, nil
	case "daily_bars":
		code, err := u.market.EnsureAShareCode(fmt.Sprint(arguments["code"]))
		if err != nil {
			return nil, err
		}
		rows, err := u.repo.ToolDailyBars(ctx, code, intFromToolArgs(arguments["limit"], 120, 500))
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			out = append(out, map[string]any{"code": row.Code, "trade_date": row.TradeDate, "open": row.Open, "close": row.Close, "volume": row.Volume})
		}
		return out, nil
	case "paper_positions":
		rows, err := u.repo.ToolPaperPositions(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			out = append(out, map[string]any{"account_id": row.AccountID, "code": row.Code, "quantity": row.Quantity, "avg_cost": row.AvgCost})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown tool: %s", tool)
	}
}

func nullable(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func intFromToolArgs(value any, fallback int, maxValue int) int {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(text, "%d", &parsed); err != nil {
		return fallback
	}
	if parsed <= 0 {
		return fallback
	}
	if maxValue > 0 && parsed > maxValue {
		return maxValue
	}
	return parsed
}

func uintFromToolArgs(value any) uint {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return 0
	}
	var parsed uint
	if _, err := fmt.Sscanf(text, "%d", &parsed); err != nil {
		return 0
	}
	return parsed
}

func (u Usecase) normalizeTask(task Task) (Task, error) {
	action := strings.ToLower(strings.TrimSpace(task.Action))
	switch action {
	case TaskSyncSymbols:
		return Task{Action: TaskSyncSymbols}, nil
	case TaskRefreshQuote, TaskRefreshDailyBars:
		code, err := u.market.EnsureAShareCode(task.Code)
		if err != nil {
			return Task{}, err
		}
		return Task{Action: action, Code: code}, nil
	default:
		if action == "" {
			return Task{}, fmt.Errorf("market task action is required")
		}
		return Task{}, fmt.Errorf("unknown market task action: %s", action)
	}
}

func (u Usecase) withTx(ctx context.Context, fn func(Repository) error) error {
	if u.tx != nil {
		return u.tx.WithTx(ctx, fn)
	}
	return fmt.Errorf("market unit of work is not configured")
}
