package repo

import (
	"context"
	"testing"

	appprediction "github.com/TradingCopilotDevs/TradingCopilot/internal/app/prediction"
	domainprediction "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/prediction"
	database "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/connect"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPredictionRepositorySearchMarketsCoversEventAndIdentifiers(t *testing.T) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}

	event := persistmodel.PredictionEvent{
		Provider:        domainprediction.ProviderPolymarket,
		ExternalEventID: "357807",
		Slug:            "us-x-iran-permanent-peace-deal-by",
		Title:           "US x Iran permanent peace deal by...?",
		Active:          true,
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	market := persistmodel.PredictionMarket{
		EventID:          &event.ID,
		Provider:         domainprediction.ProviderPolymarket,
		ExternalMarketID: "1919417",
		ConditionID:      "0xbbc6689d0f6d57ea42168836712237c7308b3e0118c8914d31b6126d0f3254c5",
		Question:         "US x Iran permanent peace deal by June 30, 2026?",
		Slug:             "us-x-iran-permanent-peace-deal-by-june-30-2026",
		Active:           true,
	}
	if err := db.Create(&market).Error; err != nil {
		t.Fatal(err)
	}

	repo := NewPredictionRepository(db)
	for _, query := range []string{
		"us-x-iran-permanent-peace-deal-by",
		"permanent peace deal",
		"1919417",
		"0xbbc6689d0f6d57ea42168836712237c7308b3e0118c8914d31b6126d0f3254c5",
	} {
		rows, err := repo.SearchMarkets(ctx, query, 10)
		if err != nil {
			t.Fatalf("SearchMarkets(%q): %v", query, err)
		}
		if len(rows) != 1 || rows[0].ExternalMarketID != "1919417" {
			t.Fatalf("SearchMarkets(%q) = %#v", query, rows)
		}
		assertPredictionMarketEventIdentity(t, rows[0], event)
	}
	foundMarket, found, err := repo.FindMarket(ctx, market.ID)
	if err != nil || !found || foundMarket == nil {
		t.Fatalf("FindMarket = market=%#v found=%v err=%v", foundMarket, found, err)
	}
	assertPredictionMarketEventIdentity(t, *foundMarket, event)

	match := persistmodel.PredictionMarketMatch{
		MarketID: market.ID,
		Query:    "Iran peace deal",
		Score:    decimal.NewFromFloat(0.92),
		Status:   appprediction.MatchStatusCandidate,
	}
	if err := db.Create(&match).Error; err != nil {
		t.Fatal(err)
	}
	matches, err := repo.ListMatches(ctx, appprediction.MatchFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("expected one match, got %#v", matches)
	}
	assertPredictionMarketEventIdentity(t, matches[0].Market, event)

	team := persistmodel.ResearchTeam{Name: "Prediction Team", AssetClass: "prediction_market", Active: true}
	if err := db.Create(&team).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&persistmodel.PredictionWatchlistItem{ResearchTeamID: team.ID, MarketID: market.ID, Active: true}).Error; err != nil {
		t.Fatal(err)
	}
	watchlist, err := repo.ListWatchlist(ctx, team.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(watchlist) != 1 {
		t.Fatalf("expected one watchlist row, got %#v", watchlist)
	}
	assertPredictionMarketEventIdentity(t, watchlist[0].Market, event)
}

func TestPredictionRepositorySearchMarketsRanksExactEventSlugBeforePartialHighVolumeMarket(t *testing.T) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}

	exactEvent := persistmodel.PredictionEvent{
		Provider:        domainprediction.ProviderPolymarket,
		ExternalEventID: "357807",
		Slug:            "us-x-iran-permanent-peace-deal-by",
		Title:           "US x Iran permanent peace deal by...?",
		Active:          true,
	}
	if err := db.Create(&exactEvent).Error; err != nil {
		t.Fatal(err)
	}
	exactEventMarket := persistmodel.PredictionMarket{
		EventID:          &exactEvent.ID,
		Provider:         domainprediction.ProviderPolymarket,
		ExternalMarketID: "event-child",
		Question:         "US x Iran permanent peace deal by April 22, 2026?",
		Slug:             "us-x-iran-permanent-peace-deal-by-april-22-2026",
		Active:           true,
		Volume:           decimal.NewFromInt(1),
	}
	partialHighVolumeMarket := persistmodel.PredictionMarket{
		Provider:         domainprediction.ProviderPolymarket,
		ExternalMarketID: "partial-high-volume",
		Question:         "A broader Iran peace topic",
		Slug:             "us-x-iran-permanent-peace-deal-by-but-different-event",
		Active:           true,
		Volume:           decimal.NewFromInt(1000000),
	}
	exactMarketSlug := persistmodel.PredictionMarket{
		Provider:         domainprediction.ProviderPolymarket,
		ExternalMarketID: "exact-market",
		Question:         "Will the exact market resolve yes?",
		Slug:             "standalone-market-slug",
		Active:           true,
		Volume:           decimal.NewFromInt(1),
	}
	if err := db.Create(&exactEventMarket).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&partialHighVolumeMarket).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&exactMarketSlug).Error; err != nil {
		t.Fatal(err)
	}

	repo := NewPredictionRepository(db)
	rows, err := repo.SearchMarkets(ctx, "us-x-iran-permanent-peace-deal-by", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 {
		t.Fatalf("expected event and partial market matches, got %#v", rows)
	}
	if rows[0].ExternalMarketID != "event-child" {
		t.Fatalf("exact event slug match should outrank partial high-volume market, got %#v", rows)
	}

	rows, err = repo.SearchMarkets(ctx, "standalone-market-slug", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 || rows[0].ExternalMarketID != "exact-market" {
		t.Fatalf("exact market slug should rank first, got %#v", rows)
	}
}

func TestPredictionRepositoryUpsertWatchlistKeepsSourceWhenManualUpdateHasNoSource(t *testing.T) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}

	team := persistmodel.ResearchTeam{Name: "Prediction Team", AssetClass: "prediction_market", Active: true}
	if err := db.Create(&team).Error; err != nil {
		t.Fatal(err)
	}
	market := persistmodel.PredictionMarket{Provider: domainprediction.ProviderPolymarket, ExternalMarketID: "m-keep-source", Question: "Will source metadata survive?", Active: true}
	if err := db.Create(&market).Error; err != nil {
		t.Fatal(err)
	}

	repo := NewPredictionRepository(db)
	roleKey := "moderator"
	meetingID := uint(7)
	eventID := uint(11)
	note := "source meeting recap"
	item := domainprediction.WatchlistItem{
		ResearchTeamID: team.ID, MarketID: market.ID, Note: &note, Active: true,
		SourceMeetingID: &meetingID, SourceMeetingEventID: &eventID, SourceRoleKey: &roleKey,
	}
	if err := repo.UpsertWatchlist(ctx, &item); err != nil {
		t.Fatal(err)
	}

	manualNote := "manual refresh"
	item = domainprediction.WatchlistItem{ResearchTeamID: team.ID, MarketID: market.ID, Note: &manualNote, Active: false}
	if err := repo.UpsertWatchlist(ctx, &item); err != nil {
		t.Fatal(err)
	}
	if item.Note == nil || *item.Note != manualNote || item.Active {
		t.Fatalf("manual update should change note and active, got %+v", item)
	}
	if item.SourceMeetingID == nil || *item.SourceMeetingID != meetingID || item.SourceMeetingEventID == nil || *item.SourceMeetingEventID != eventID || item.SourceRoleKey == nil || *item.SourceRoleKey != roleKey {
		t.Fatalf("manual update without source should preserve source link, got %+v", item)
	}
}

func TestMeetingRepositoryDeleteGraphClearsPredictionWatchlistSource(t *testing.T) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}

	team := persistmodel.ResearchTeam{Name: "Prediction Team", AssetClass: "prediction_market", Active: true}
	if err := db.Create(&team).Error; err != nil {
		t.Fatal(err)
	}
	meeting := persistmodel.Meeting{ResearchTeamID: team.ID, Topic: "source meeting", TriggerSource: "manual", Status: "completed"}
	if err := db.Create(&meeting).Error; err != nil {
		t.Fatal(err)
	}
	event := persistmodel.MeetingEvent{MeetingID: meeting.ID, Type: "role_message", Content: "recap"}
	if err := db.Create(&event).Error; err != nil {
		t.Fatal(err)
	}
	market := persistmodel.PredictionMarket{Provider: domainprediction.ProviderPolymarket, ExternalMarketID: "m-delete-source", Question: "Will delete cleanup source?", Active: true}
	if err := db.Create(&market).Error; err != nil {
		t.Fatal(err)
	}
	roleKey := "moderator"
	item := persistmodel.PredictionWatchlistItem{
		ResearchTeamID: team.ID, MarketID: market.ID, Active: true,
		SourceMeetingID: &meeting.ID, SourceMeetingEventID: &event.ID, SourceRoleKey: &roleKey,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatal(err)
	}

	if err := NewMeetingRepository(db).DeleteGraph(ctx, meeting.ID); err != nil {
		t.Fatal(err)
	}
	var saved persistmodel.PredictionWatchlistItem
	if err := db.First(&saved, item.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.SourceMeetingID != nil || saved.SourceMeetingEventID != nil || saved.SourceRoleKey != nil {
		t.Fatalf("delete graph should clear prediction watchlist source link, got %+v", saved)
	}
}

func assertPredictionMarketEventIdentity(t *testing.T, market domainprediction.Market, event persistmodel.PredictionEvent) {
	t.Helper()
	if market.EventID == nil || *market.EventID != event.ID {
		t.Fatalf("market should carry local event id %d, got %#v", event.ID, market)
	}
	if market.EventExternalEventID != event.ExternalEventID || market.EventSlug != event.Slug || market.EventTitle != event.Title {
		t.Fatalf("market should carry readable event identity from preload, got %#v", market)
	}
}
