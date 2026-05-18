package repo

import (
	"context"
	"testing"
	"time"

	appmessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/messaging"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmsg "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/messaging"
	database "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/connect"
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
