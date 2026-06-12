package repo

import (
	"context"
	"testing"
	"time"

	appmessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/messaging"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmsg "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/messaging"
	database "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/connect"
	persistmodel "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/model"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMessagingRepositoryCRUDAndFilters(t *testing.T) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := NewMessagingRepository(db)

	subscription := domainmsg.MessageSubscription{
		Provider:      domainmsg.ProviderTelegramChannel,
		Title:         "News",
		SourceRef:     "@news",
		Enabled:       true,
		BackfillLimit: 20,
		CollectFrom:   time.Now().Add(-time.Hour),
		Config:        domainkernel.NewJSON(map[string]any{"scope": "test"}),
	}
	if err := repo.CreateSubscription(ctx, &subscription); err != nil {
		t.Fatal(err)
	}
	foundSubscription, found, err := repo.FindSubscriptionByProviderAndSourceRef(ctx, domainmsg.ProviderTelegramChannel, "@news")
	if err != nil || !found || foundSubscription == nil || foundSubscription.ID != subscription.ID {
		t.Fatalf("FindSubscriptionByProviderAndSourceRef id=%v found=%v err=%v", foundSubscription, found, err)
	}
	decision := domainkernel.NewsObserve
	message := domainmsg.IngestedMessage{
		SubscriptionID:  subscription.ID,
		Provider:        subscription.Provider,
		SourceMessageID: "42",
		MessageTime:     time.Now(),
		Text:            "600000 fixture",
		Raw:             domainkernel.NewJSON(map[string]any{"manual": true}),
		FilterDecision:  &decision,
		RelatedSymbols:  domainkernel.NewJSON([]string{"600000"}),
	}
	if err := repo.CreateMessage(ctx, &message); err != nil {
		t.Fatal(err)
	}
	rows, err := repo.ListMessages(ctx, appmessaging.RepositoryMessageFilter{SubscriptionID: "1", Query: "fixture", Decision: string(domainkernel.NewsObserve), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != message.ID {
		t.Fatalf("unexpected filtered messages: %+v", rows)
	}
	if _, found, err := repo.FindMessageBySource(ctx, subscription.ID, "42"); err != nil || !found {
		t.Fatalf("FindMessageBySource found=%v err=%v", found, err)
	}
	helpful := domainmsg.FeedbackHelpful
	feedbackAt := time.Now()
	message.FeedbackLabel = &helpful
	message.FeedbackAt = &feedbackAt
	if err := repo.SaveMessage(ctx, &message); err != nil {
		t.Fatal(err)
	}
	feedbackRows, err := repo.ListFeedbackMessages(ctx, appmessaging.RepositoryFeedbackMessageFilter{SubscriptionID: "1", Provider: domainmsg.ProviderTelegramChannel, Label: domainmsg.FeedbackHelpful, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(feedbackRows) != 1 || feedbackRows[0].Subscription == nil || feedbackRows[0].Subscription.SourceRef != "@news" {
		t.Fatalf("unexpected feedback rows: %+v", feedbackRows)
	}
	adapter := domainmsg.PlatformAdapter{Provider: domainmsg.ProviderTelegramBot, DisplayName: "Bot", Enabled: true, Config: domainkernel.NewJSON(map[string]any{"capabilities": []string{"outbound_notifications"}})}
	if err := repo.CreatePlatformAdapter(ctx, &adapter); err != nil {
		t.Fatal(err)
	}
	adapters, err := repo.ListPlatformAdapters(ctx)
	if err != nil || len(adapters) != 1 {
		t.Fatalf("platform adapters len=%d err=%v", len(adapters), err)
	}
	if err := repo.DeleteMessagesByIDsWithRefs(ctx, []uint{message.ID}, nil); err != nil {
		t.Fatal(err)
	}
	remaining, err := repo.ListMessagesBySubscription(ctx, subscription.ID)
	if err != nil || len(remaining) != 0 {
		t.Fatalf("remaining messages len=%d err=%v", len(remaining), err)
	}
}

func TestReplaceSubscriptionAssignmentsPreservesExistingAndDetachesRemovedFilterResults(t *testing.T) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := NewMessagingRepository(db)

	filterOne := domainmsg.MessageSubscriptionFilter{Name: "filter one", Enabled: true}
	filterTwo := domainmsg.MessageSubscriptionFilter{Name: "filter two", Enabled: true}
	if err := repo.CreateSubscriptionFilter(ctx, &filterOne); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateSubscriptionFilter(ctx, &filterTwo); err != nil {
		t.Fatal(err)
	}
	teamOne := persistmodel.ResearchTeam{Name: "assignment team one", Active: true, AssetClass: "a_share"}
	teamTwo := persistmodel.ResearchTeam{Name: "assignment team two", Active: true, AssetClass: "a_share"}
	if err := db.Create(&teamOne).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&teamTwo).Error; err != nil {
		t.Fatal(err)
	}
	subscription := domainmsg.MessageSubscription{
		Provider:  domainmsg.ProviderTelegramChannel,
		Title:     "News",
		SourceRef: "@news",
		Enabled:   true,
		FilterID:  filterOne.ID,
	}
	if err := repo.CreateSubscription(ctx, &subscription); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceSubscriptionAssignments(ctx, subscription.ID, []domainmsg.MessageSubscriptionAssignment{
		{FilterID: filterOne.ID, ResearchTeamID: teamOne.ID, Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	var firstAssignment persistmodel.MessageSubscriptionAssignment
	if err := db.First(&firstAssignment, "subscription_id = ? AND filter_id = ? AND research_team_id = ?", subscription.ID, filterOne.ID, teamOne.ID).Error; err != nil {
		t.Fatal(err)
	}
	message := domainmsg.IngestedMessage{
		SubscriptionID:  subscription.ID,
		Provider:        subscription.Provider,
		SourceMessageID: "42",
		MessageTime:     time.Now(),
		Text:            "600000 fixture",
		Raw:             domainkernel.NewJSON(map[string]any{}),
	}
	if err := repo.CreateMessage(ctx, &message); err != nil {
		t.Fatal(err)
	}
	decision := domainkernel.NewsObserve
	result := domainmsg.IngestedMessageFilterResult{
		MessageID:      message.ID,
		SubscriptionID: subscription.ID,
		AssignmentID:   &firstAssignment.ID,
		FilterID:       filterOne.ID,
		ResearchTeamID: teamOne.ID,
		FilterDecision: &decision,
		FilterStatus:   domainmsg.FilterStatusFiltered,
		RelatedSymbols: domainkernel.NewJSON([]string{"600000"}),
	}
	if err := repo.SaveMessageFilterResult(ctx, &result); err != nil {
		t.Fatal(err)
	}

	if err := repo.ReplaceSubscriptionAssignments(ctx, subscription.ID, []domainmsg.MessageSubscriptionAssignment{
		{FilterID: filterOne.ID, ResearchTeamID: teamOne.ID, Enabled: true},
		{FilterID: filterTwo.ID, ResearchTeamID: teamTwo.ID, Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	var preserved persistmodel.MessageSubscriptionAssignment
	if err := db.First(&preserved, "subscription_id = ? AND filter_id = ? AND research_team_id = ?", subscription.ID, filterOne.ID, teamOne.ID).Error; err != nil {
		t.Fatal(err)
	}
	if preserved.ID != firstAssignment.ID {
		t.Fatalf("unchanged assignment ID changed from %d to %d", firstAssignment.ID, preserved.ID)
	}
	results, err := repo.ListMessageFilterResults(ctx, message.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].AssignmentID == nil || *results[0].AssignmentID != firstAssignment.ID {
		t.Fatalf("existing filter result should keep preserved assignment id, got %+v", results)
	}

	if err := repo.ReplaceSubscriptionAssignments(ctx, subscription.ID, []domainmsg.MessageSubscriptionAssignment{
		{FilterID: filterTwo.ID, ResearchTeamID: teamTwo.ID, Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	results, err = repo.ListMessageFilterResults(ctx, message.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].AssignmentID != nil || results[0].FilterID != filterOne.ID || results[0].ResearchTeamID != teamOne.ID {
		t.Fatalf("removed assignment should detach historical result without deleting result data, got %+v", results)
	}
}

func TestMessagingRepositoryLoadsOnlyEnabledSubscriptionAssignments(t *testing.T) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := NewMessagingRepository(db)

	filterOne := domainmsg.MessageSubscriptionFilter{Name: "filter one", Enabled: true}
	filterTwo := domainmsg.MessageSubscriptionFilter{Name: "filter two", Enabled: true}
	if err := repo.CreateSubscriptionFilter(ctx, &filterOne); err != nil {
		t.Fatal(err)
	}
	if err := repo.CreateSubscriptionFilter(ctx, &filterTwo); err != nil {
		t.Fatal(err)
	}
	teamOne := persistmodel.ResearchTeam{Name: "enabled team", Active: true, AssetClass: "a_share"}
	teamTwo := persistmodel.ResearchTeam{Name: "disabled team", Active: true, AssetClass: "a_share"}
	if err := db.Create(&teamOne).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&teamTwo).Error; err != nil {
		t.Fatal(err)
	}
	subscription := domainmsg.MessageSubscription{
		Provider:  domainmsg.ProviderTelegramChannel,
		Title:     "News",
		SourceRef: "@news",
		Enabled:   true,
		FilterID:  filterOne.ID,
	}
	if err := repo.CreateSubscription(ctx, &subscription); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&persistmodel.MessageSubscriptionAssignment{SubscriptionID: subscription.ID, FilterID: filterOne.ID, ResearchTeamID: teamOne.ID, Enabled: true}).Error; err != nil {
		t.Fatal(err)
	}
	disabled := persistmodel.MessageSubscriptionAssignment{SubscriptionID: subscription.ID, FilterID: filterTwo.ID, ResearchTeamID: teamTwo.ID, Enabled: true}
	if err := db.Create(&disabled).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&persistmodel.MessageSubscriptionAssignment{}).Where("id = ?", disabled.ID).Update("enabled", false).Error; err != nil {
		t.Fatal(err)
	}

	loaded, found, err := repo.FindSubscription(ctx, subscription.ID)
	if err != nil || !found {
		t.Fatalf("FindSubscription found=%v err=%v", found, err)
	}
	if len(loaded.Assignments) != 1 || loaded.Assignments[0].ResearchTeamID != teamOne.ID {
		t.Fatalf("expected only enabled assignment to be loaded, got %+v", loaded.Assignments)
	}
	if len(loaded.TeamIDs) != 1 || loaded.TeamIDs[0] != teamOne.ID {
		t.Fatalf("expected team ids from enabled assignments only, got %+v", loaded.TeamIDs)
	}
}

func TestDeleteSubscriptionGraphDeletesMessagesBeforeAssignments(t *testing.T) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := NewMessagingRepository(db)

	filter := domainmsg.MessageSubscriptionFilter{Name: "filter", Enabled: true}
	if err := repo.CreateSubscriptionFilter(ctx, &filter); err != nil {
		t.Fatal(err)
	}
	team := persistmodel.ResearchTeam{Name: "assignment team", Active: true, AssetClass: "a_share"}
	if err := db.Create(&team).Error; err != nil {
		t.Fatal(err)
	}
	subscription := domainmsg.MessageSubscription{
		Provider:  domainmsg.ProviderTelegramChannel,
		Title:     "News",
		SourceRef: "@news",
		Enabled:   true,
		FilterID:  filter.ID,
	}
	if err := repo.CreateSubscription(ctx, &subscription); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceSubscriptionAssignments(ctx, subscription.ID, []domainmsg.MessageSubscriptionAssignment{
		{FilterID: filter.ID, ResearchTeamID: team.ID, Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	var assignment persistmodel.MessageSubscriptionAssignment
	if err := db.First(&assignment, "subscription_id = ?", subscription.ID).Error; err != nil {
		t.Fatal(err)
	}
	message := domainmsg.IngestedMessage{
		SubscriptionID:  subscription.ID,
		Provider:        subscription.Provider,
		SourceMessageID: "42",
		MessageTime:     time.Now(),
		Text:            "600000 fixture",
		Raw:             domainkernel.NewJSON(map[string]any{}),
	}
	if err := repo.CreateMessage(ctx, &message); err != nil {
		t.Fatal(err)
	}
	decision := domainkernel.NewsMeeting
	result := domainmsg.IngestedMessageFilterResult{
		MessageID:      message.ID,
		SubscriptionID: subscription.ID,
		AssignmentID:   &assignment.ID,
		FilterID:       filter.ID,
		ResearchTeamID: team.ID,
		FilterDecision: &decision,
		FilterStatus:   domainmsg.FilterStatusFiltered,
		RelatedSymbols: domainkernel.NewJSON([]string{"600000"}),
	}
	if err := repo.SaveMessageFilterResult(ctx, &result); err != nil {
		t.Fatal(err)
	}

	if err := repo.DeleteSubscriptionGraph(ctx, subscription.ID, nil); err != nil {
		t.Fatal(err)
	}
	var messageCount int64
	if err := db.Model(&persistmodel.IngestedMessage{}).Where("subscription_id = ?", subscription.ID).Count(&messageCount).Error; err != nil {
		t.Fatal(err)
	}
	var resultCount int64
	if err := db.Model(&persistmodel.IngestedMessageFilterResult{}).Where("subscription_id = ?", subscription.ID).Count(&resultCount).Error; err != nil {
		t.Fatal(err)
	}
	var assignmentCount int64
	if err := db.Model(&persistmodel.MessageSubscriptionAssignment{}).Where("subscription_id = ?", subscription.ID).Count(&assignmentCount).Error; err != nil {
		t.Fatal(err)
	}
	if messageCount != 0 || resultCount != 0 || assignmentCount != 0 {
		t.Fatalf("subscription graph was not fully deleted: messages=%d results=%d assignments=%d", messageCount, resultCount, assignmentCount)
	}
}

func TestDeleteMessagesClearsFilterResultsExplicitly(t *testing.T) {
	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	repo := NewMessagingRepository(db)

	filter := domainmsg.MessageSubscriptionFilter{Name: "filter", Enabled: true}
	if err := repo.CreateSubscriptionFilter(ctx, &filter); err != nil {
		t.Fatal(err)
	}
	team := persistmodel.ResearchTeam{Name: "assignment team", Active: true, AssetClass: "a_share"}
	if err := db.Create(&team).Error; err != nil {
		t.Fatal(err)
	}
	subscription := domainmsg.MessageSubscription{Provider: domainmsg.ProviderTelegramChannel, Title: "News", SourceRef: "@news", Enabled: true, FilterID: filter.ID}
	if err := repo.CreateSubscription(ctx, &subscription); err != nil {
		t.Fatal(err)
	}
	if err := repo.ReplaceSubscriptionAssignments(ctx, subscription.ID, []domainmsg.MessageSubscriptionAssignment{{FilterID: filter.ID, ResearchTeamID: team.ID, Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	var assignment persistmodel.MessageSubscriptionAssignment
	if err := db.First(&assignment, "subscription_id = ?", subscription.ID).Error; err != nil {
		t.Fatal(err)
	}
	createMessageWithResult := func(sourceID string) domainmsg.IngestedMessage {
		t.Helper()
		message := domainmsg.IngestedMessage{
			SubscriptionID:  subscription.ID,
			Provider:        subscription.Provider,
			SourceMessageID: sourceID,
			MessageTime:     time.Now(),
			Text:            "600000 fixture",
			Raw:             domainkernel.NewJSON(map[string]any{}),
		}
		if err := repo.CreateMessage(ctx, &message); err != nil {
			t.Fatal(err)
		}
		decision := domainkernel.NewsObserve
		result := domainmsg.IngestedMessageFilterResult{
			MessageID:      message.ID,
			SubscriptionID: subscription.ID,
			AssignmentID:   &assignment.ID,
			FilterID:       filter.ID,
			ResearchTeamID: team.ID,
			FilterDecision: &decision,
			FilterStatus:   domainmsg.FilterStatusFiltered,
			RelatedSymbols: domainkernel.NewJSON([]string{"600000"}),
		}
		if err := repo.SaveMessageFilterResult(ctx, &result); err != nil {
			t.Fatal(err)
		}
		return message
	}
	first := createMessageWithResult("single-delete")
	second := createMessageWithResult("batch-delete")

	if err := repo.DeleteMessage(ctx, &first, nil); err != nil {
		t.Fatal(err)
	}
	var firstResultCount int64
	if err := db.Model(&persistmodel.IngestedMessageFilterResult{}).Where("message_id = ?", first.ID).Count(&firstResultCount).Error; err != nil {
		t.Fatal(err)
	}
	if firstResultCount != 0 {
		t.Fatalf("single message delete left %d filter results", firstResultCount)
	}

	if err := repo.DeleteMessagesByIDsWithRefs(ctx, []uint{second.ID}, nil); err != nil {
		t.Fatal(err)
	}
	var secondResultCount int64
	if err := db.Model(&persistmodel.IngestedMessageFilterResult{}).Where("message_id = ?", second.ID).Count(&secondResultCount).Error; err != nil {
		t.Fatal(err)
	}
	if secondResultCount != 0 {
		t.Fatalf("batch message delete left %d filter results", secondResultCount)
	}
}
