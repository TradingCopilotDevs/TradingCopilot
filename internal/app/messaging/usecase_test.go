package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	domainai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/ai"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/meeting"
	domainmsg "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/messaging"
	domainsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/settings"
)

func TestFilterMessageOwnsPersistenceAndMeetingWritesInTransaction(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderTelegramChannel, Title: "News", SourceRef: "@news", Enabled: true, FilterID: 1, TeamIDs: []uint{1}}
	repo.messages[1] = domainmsg.IngestedMessage{ID: 1, SubscriptionID: 1, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: "100", MessageTime: time.Now(), Text: "600000 triggered"}
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "filter", PromptTemplate: "return JSON", Enabled: true, IsDefault: true, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	service := &fakeMessagingService{filter: FilterResult{
		Decision:       ptrDecision(domainkernel.NewsMeeting),
		Reason:         ptrString("important"),
		RelatedSymbols: jsonBytes(t, []string{"600000"}),
		FilterID:       1,
	}}
	tx := &fakeMessagingTx{repo: repo}
	usecase := NewUsecase(repo, service, fakeMessagingSecurity{}, tx)

	result, found, err := usecase.FilterMessage(ctx, 1)
	if err != nil || !found {
		t.Fatalf("FilterMessage found=%v err=%v", found, err)
	}
	if tx.calls != 1 {
		t.Fatalf("expected one app-owned transaction, got %d", tx.calls)
	}
	if repo.saveMessageCalls == 0 || repo.createMeetingCalls != 1 || repo.createReferenceCalls != 1 || repo.appendEventCalls != 2 {
		t.Fatalf("writes did not go through repository: saves=%d meetings=%d refs=%d events=%d", repo.saveMessageCalls, repo.createMeetingCalls, repo.createReferenceCalls, repo.appendEventCalls)
	}
	if len(result.CreatedMeetings) != 1 || result.Row.Message.FilterDecision == nil || *result.Row.Message.FilterDecision != domainkernel.NewsMeeting {
		t.Fatalf("unexpected filter result: %+v", result)
	}
}

func TestFilterMessageDoesNotPersistFailureWhenParentContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo := newFakeMessagingRepo()
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderTelegramChannel, Title: "News", SourceRef: "@news", FilterID: 1}
	repo.messages[1] = domainmsg.IngestedMessage{ID: 1, SubscriptionID: 1, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: "100", MessageTime: time.Now(), Text: "600000 triggered"}
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "filter", PromptTemplate: "return JSON", Enabled: true, IsDefault: true, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	_, found, err := usecase.FilterMessage(ctx, 1)
	if !found || !errors.Is(err, context.Canceled) {
		t.Fatalf("FilterMessage found=%v err=%v, want context canceled", found, err)
	}
	if repo.saveMessageCalls != 0 {
		t.Fatalf("canceled filter should not persist a synthetic failure, saves=%d", repo.saveMessageCalls)
	}
}

func TestTelegramLoginSecretsArePersistedByAppUsecase(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.secrets[string(domainkernel.SecretKindMessageSubscription)+":telegram:app_id"] = domainsettings.Secret{Kind: domainkernel.SecretKindMessageSubscription, Name: "telegram:app_id", EncryptedValue: "enc:12345"}
	repo.secrets[string(domainkernel.SecretKindMessageSubscription)+":telegram:app_hash"] = domainsettings.Secret{Kind: domainkernel.SecretKindMessageSubscription, Name: "telegram:app_hash", EncryptedValue: "enc:hash"}
	service := &fakeMessagingService{loginHash: "hash", loginSession: "login-session", session: "session"}
	usecase := NewUsecase(repo, service, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	hash, err := usecase.StartTelegramLogin(ctx, "+8613800000000")
	if err != nil || hash != "hash" {
		t.Fatalf("StartTelegramLogin hash=%q err=%v", hash, err)
	}
	if _, ok := repo.secret(domainkernel.SecretKindMessageSubscription, telegramLoginPhone); !ok {
		t.Fatalf("expected app usecase to persist login phone")
	}
	if _, ok := repo.secret(domainkernel.SecretKindMessageSubscription, telegramLoginSession); !ok {
		t.Fatalf("expected app usecase to persist temporary login session")
	}
	if err := usecase.VerifyTelegramLogin(ctx, "", "12345", "", ""); err != nil {
		t.Fatalf("VerifyTelegramLogin: %v", err)
	}
	if service.completeCredentials.LoginSession != "login-session" {
		t.Fatalf("expected verify to reuse temporary login session, got %q", service.completeCredentials.LoginSession)
	}
	if secret, ok := repo.secret(domainkernel.SecretKindMessageSubscription, "telegram:mtproto_session"); !ok || secret.EncryptedValue != "enc:session" {
		t.Fatalf("expected app usecase to persist session, got %+v ok=%v", secret, ok)
	}
	if _, ok := repo.secret(domainkernel.SecretKindMessageSubscription, telegramLoginPhone); ok {
		t.Fatalf("expected temporary login secrets to be deleted")
	}
}

func TestSubscriptionStatusRequiresDecryptableSecrets(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.secrets[string(domainkernel.SecretKindMessageSubscription)+":telegram:app_id"] = domainsettings.Secret{
		Kind: domainkernel.SecretKindMessageSubscription, Name: "telegram:app_id", EncryptedValue: "stale-ciphertext",
	}
	repo.secrets[string(domainkernel.SecretKindMessageSubscription)+":telegram:app_hash"] = domainsettings.Secret{
		Kind: domainkernel.SecretKindMessageSubscription, Name: "telegram:app_hash", EncryptedValue: "enc:hash",
	}
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	status := usecase.SubscriptionStatus(ctx)
	if status["has_app_id"] {
		t.Fatal("undecryptable app_id secret should not be reported as configured")
	}
	if !status["has_app_hash"] {
		t.Fatal("decryptable app_hash secret should be reported as configured")
	}

	repo.secrets[string(domainkernel.SecretKindMessageSubscription)+":telegram:app_id"] = domainsettings.Secret{
		Kind: domainkernel.SecretKindMessageSubscription, Name: "telegram:app_id", EncryptedValue: "enc:not-a-number",
	}
	status = usecase.SubscriptionStatus(ctx)
	if status["has_app_id"] {
		t.Fatal("non-numeric app_id secret should not be reported as configured")
	}
}

func TestEnsureDefaultSubscriptionFilterSeedsOnlyEmptyFilterTable(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.roles["news_filter"] = domainai.AgentRole{Key: "news_filter", Name: "legacy"}
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	filter, err := usecase.EnsureDefaultSubscriptionFilter(ctx)
	if err != nil {
		t.Fatalf("EnsureDefaultSubscriptionFilter: %v", err)
	}
	if filter == nil || filter.Name != DefaultFilterSeed.Name || !filter.IsDefault {
		t.Fatalf("unexpected default filter: %+v", filter)
	}
	if _, ok := repo.roles["news_filter"]; ok {
		t.Fatal("legacy news_filter role should be removed after default filter seeding")
	}
}

func TestEnsureDefaultSubscriptionFilterDoesNotCreateWhenCustomFilterExists(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.filters[7] = domainmsg.MessageSubscriptionFilter{
		ID:             7,
		Name:           "Custom filter",
		PromptTemplate: "custom prompt",
		Enabled:        true,
		IsDefault:      false,
		ProviderID:     uintPtr(1),
	}
	repo.roles["news_filter"] = domainai.AgentRole{Key: "news_filter", Name: "legacy"}
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	filter, err := usecase.EnsureDefaultSubscriptionFilter(ctx)
	if err != nil {
		t.Fatalf("EnsureDefaultSubscriptionFilter: %v", err)
	}
	if filter != nil {
		t.Fatalf("custom filter table should not receive a generated default, got %+v", filter)
	}
	if len(repo.filters) != 1 || repo.filters[7].Name != "Custom filter" || repo.filters[7].IsDefault {
		t.Fatalf("custom filters were changed: %+v", repo.filters)
	}
	if _, ok := repo.roles["news_filter"]; ok {
		t.Fatal("legacy news_filter role should still be removed")
	}
}

func TestCreateSubscriptionBackfillsMessagesOlderThanCreateTime(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	service := &fakeMessagingService{fetched: FetchedMessages{
		Title: "Fixture News",
		Messages: []FetchedMessage{{
			SourceMessageID: "42",
			MessageTime:     time.Now().Add(-time.Hour),
			Text:            "older message should be backfilled",
			Raw:             jsonBytes(t, map[string]any{"source": "fixture"}),
		}},
	}}
	queue := &fakeMessagingQueue{}
	usecase := NewUsecase(repo, service, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo}).WithTaskQueue(queue)

	subscription, err := usecase.CreateSubscription(ctx, SubscriptionInput{
		Provider:      domainmsg.ProviderTelegramChannel,
		Title:         "Fixture",
		SourceRef:     "@fixture",
		Enabled:       true,
		TeamIDs:       []uint{1},
		BackfillLimit: 1,
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if !subscription.CollectFrom.IsZero() {
		t.Fatalf("new subscription collect_from should not block backfill, got %s", subscription.CollectFrom)
	}
	if len(queue.collect) != 1 {
		t.Fatalf("expected one queued collect task, got %+v", queue.collect)
	}
	if _, _, err := usecase.ProcessCollectTask(ctx, queue.collect[0]); err != nil {
		t.Fatalf("ProcessCollectTask: %v", err)
	}
	if len(repo.messages) != 1 {
		t.Fatalf("expected one backfilled message, got %d", len(repo.messages))
	}
	for _, message := range repo.messages {
		if message.SubscriptionID != subscription.ID || message.SourceMessageID != "42" {
			t.Fatalf("unexpected message: %+v", message)
		}
	}
}

func TestCreateSubscriptionBackfillHonorsConfiguredLimitWhenFetcherReturnsMore(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	fetched := FetchedMessages{Title: "Fixture News"}
	for i := int64(1); i <= 20; i++ {
		fetched.Messages = append(fetched.Messages, FetchedMessage{
			SourceMessageID: strconv.FormatInt(i, 10),
			MessageTime:     time.Now().Add(time.Duration(i) * time.Minute),
			Text:            "backfill limit fixture",
			Raw:             jsonBytes(t, map[string]any{"source": "fixture"}),
		})
	}
	service := &fakeMessagingService{fetched: fetched}
	queue := &fakeMessagingQueue{}
	usecase := NewUsecase(repo, service, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo}).WithTaskQueue(queue)

	subscription, err := usecase.CreateSubscription(ctx, SubscriptionInput{
		Provider:      domainmsg.ProviderTelegramChannel,
		Title:         "Fixture",
		SourceRef:     "@fixture",
		Enabled:       true,
		TeamIDs:       []uint{1},
		BackfillLimit: 5,
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if len(queue.collect) != 1 {
		t.Fatalf("expected one queued collect task, got %+v", queue.collect)
	}
	if _, _, err := usecase.ProcessCollectTask(ctx, queue.collect[0]); err != nil {
		t.Fatalf("ProcessCollectTask: %v", err)
	}
	if len(repo.messages) != 5 {
		t.Fatalf("expected five backfilled messages, got %d", len(repo.messages))
	}
	seen := map[string]bool{}
	for _, message := range repo.messages {
		if message.SubscriptionID != subscription.ID {
			t.Fatalf("unexpected subscription id on message: %+v", message)
		}
		seen[message.SourceMessageID] = true
	}
	for i := int64(16); i <= 20; i++ {
		if !seen[strconv.FormatInt(i, 10)] {
			t.Fatalf("expected latest backfill message %d, got ids %+v", i, seen)
		}
	}
	if len(queue.filter) != 5 {
		t.Fatalf("expected five queued filter tasks, got %+v", queue.filter)
	}
}

func TestCreateSubscriptionUsesDefaultBackfillWhenLimitOmitted(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	service := &fakeMessagingService{fetched: FetchedMessages{
		Messages: []FetchedMessage{{
			SourceMessageID: "42",
			MessageTime:     time.Now().Add(-time.Hour),
			Text:            "default backfill",
			Raw:             jsonBytes(t, map[string]any{"source": "fixture"}),
		}},
	}}
	queue := &fakeMessagingQueue{}
	usecase := NewUsecase(repo, service, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo}).WithTaskQueue(queue)

	subscription, err := usecase.CreateSubscription(ctx, SubscriptionInput{
		Provider:  domainmsg.ProviderTelegramChannel,
		Title:     "Fixture",
		SourceRef: "@fixture",
		Enabled:   true,
		TeamIDs:   []uint{1},
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if subscription.BackfillLimit != 20 {
		t.Fatalf("expected omitted backfill limit to default to 20, got %d", subscription.BackfillLimit)
	}
	if len(queue.collect) != 1 {
		t.Fatalf("expected default backfill to queue collect task, got %+v", queue.collect)
	}
	if _, _, err := usecase.ProcessCollectTask(ctx, queue.collect[0]); err != nil {
		t.Fatalf("ProcessCollectTask: %v", err)
	}
	if service.fetchCalls != 1 || len(repo.messages) != 1 {
		t.Fatalf("expected default backfill fetch and message, fetches=%d messages=%d", service.fetchCalls, len(repo.messages))
	}
}

func TestCreateSubscriptionExplicitZeroDisablesBackfill(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	service := &fakeMessagingService{fetched: FetchedMessages{
		Messages: []FetchedMessage{{
			SourceMessageID: "42",
			MessageTime:     time.Now().Add(-time.Hour),
			Text:            "should not be backfilled",
			Raw:             jsonBytes(t, map[string]any{"source": "fixture"}),
		}},
	}}
	queue := &fakeMessagingQueue{}
	usecase := NewUsecase(repo, service, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo}).WithTaskQueue(queue)

	subscription, err := usecase.CreateSubscription(ctx, SubscriptionInput{
		Provider:               domainmsg.ProviderTelegramChannel,
		Title:                  "Fixture",
		SourceRef:              "@fixture",
		Enabled:                true,
		TeamIDs:                []uint{1},
		BackfillLimit:          0,
		BackfillLimitSpecified: true,
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if subscription.BackfillLimit != 0 {
		t.Fatalf("expected explicit zero backfill limit, got %d", subscription.BackfillLimit)
	}
	if len(queue.collect) != 1 {
		t.Fatalf("expected subscription save to queue collect task, got %+v", queue.collect)
	}
	if _, _, err := usecase.ProcessCollectTask(ctx, queue.collect[0]); err != nil {
		t.Fatalf("ProcessCollectTask: %v", err)
	}
	if service.fetchCalls != 0 || len(repo.messages) != 0 {
		t.Fatalf("explicit zero should not fetch backfill, fetches=%d messages=%d", service.fetchCalls, len(repo.messages))
	}
}

func TestCreateRSSSubscriptionUsesStringSourceIDsAndDefaultPollInterval(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	service := &fakeMessagingService{fetched: FetchedMessages{
		Title: "RSS Fixture",
		Messages: []FetchedMessage{{
			SourceMessageID: "https://example.test/news/1",
			MessageTime:     time.Now().Add(-time.Hour),
			Text:            "rss message",
			Raw:             jsonBytes(t, map[string]any{"source": "rss"}),
		}},
	}}
	queue := &fakeMessagingQueue{}
	usecase := NewUsecase(repo, service, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo}).WithTaskQueue(queue)

	subscription, err := usecase.CreateSubscription(ctx, SubscriptionInput{
		Provider:      domainmsg.ProviderRSSFeed,
		Title:         "RSS",
		SourceRef:     "https://example.test/feed.xml",
		Enabled:       true,
		TeamIDs:       []uint{1},
		BackfillLimit: 1,
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if subscription.PollIntervalSeconds != defaultRSSPollIntervalSeconds {
		t.Fatalf("rss poll interval = %d", subscription.PollIntervalSeconds)
	}
	if _, _, err := usecase.ProcessCollectTask(ctx, queue.collect[0]); err != nil {
		t.Fatalf("ProcessCollectTask: %v", err)
	}
	if len(repo.messages) != 1 {
		t.Fatalf("messages = %d", len(repo.messages))
	}
	for _, message := range repo.messages {
		if message.Provider != domainmsg.ProviderRSSFeed || message.SourceMessageID != "https://example.test/news/1" {
			t.Fatalf("unexpected rss message: %+v", message)
		}
	}
	updated, _, err := repo.FindSubscription(ctx, subscription.ID)
	if err != nil || updated == nil || updated.LastCollectedAt == nil || updated.NextCollectAt == nil || updated.LastCollectError != nil {
		t.Fatalf("collect status was not updated: updated=%+v err=%v", updated, err)
	}
}

func TestRSSSubscriptionRejectsCredentialURLs(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	_, err := usecase.CreateSubscription(ctx, SubscriptionInput{
		Provider:  domainmsg.ProviderRSSFeed,
		SourceRef: "https://user:pass@example.test/feed.xml",
		Enabled:   true,
		TeamIDs:   []uint{1},
	})
	if err == nil || !strings.Contains(err.Error(), "must not include credentials") {
		t.Fatalf("expected rss credential URL rejection, got %v", err)
	}
	if len(repo.subscriptions) != 0 {
		t.Fatalf("credential URL should not create a subscription: %+v", repo.subscriptions)
	}

	_, err = usecase.TestSubscriptionRef(ctx, domainmsg.ProviderRSSFeed, "https://user:pass@example.test/feed.xml")
	if err == nil || !strings.Contains(err.Error(), "must not include credentials") {
		t.Fatalf("expected rss credential URL rejection on test path, got %v", err)
	}
}

func TestCreateSubscriptionBackfillReturnsCreatedMeetings(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "filter", PromptTemplate: "return JSON", Enabled: true, IsDefault: true, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	service := &fakeMessagingService{
		filter: FilterResult{
			Decision:       ptrDecision(domainkernel.NewsMeeting),
			Reason:         ptrString("important"),
			RelatedSymbols: jsonBytes(t, []string{"600000"}),
			FilterID:       1,
		},
		fetched: FetchedMessages{Messages: []FetchedMessage{{
			SourceMessageID: "42",
			MessageTime:     time.Now().Add(-time.Hour),
			Text:            "600000 triggered",
			Raw:             jsonBytes(t, map[string]any{"source": "fixture"}),
		}}},
	}
	queue := &fakeMessagingQueue{}
	usecase := NewUsecase(repo, service, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo}).WithTaskQueue(queue)

	result, err := usecase.CreateSubscription(ctx, SubscriptionInput{
		Provider:      domainmsg.ProviderTelegramChannel,
		Title:         "Fixture",
		SourceRef:     "@fixture",
		Enabled:       true,
		TeamIDs:       []uint{1},
		BackfillLimit: 1,
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	if len(result.CreatedMeetings) != 0 || len(queue.collect) != 1 {
		t.Fatalf("subscription save should queue backfill without synchronous meetings, result=%+v queue=%+v", result, queue.collect)
	}
	if _, _, err := usecase.ProcessCollectTask(ctx, queue.collect[0]); err != nil {
		t.Fatalf("ProcessCollectTask: %v", err)
	}
	if len(queue.filter) != 1 {
		t.Fatalf("expected backfilled message to queue filtering, got %+v", queue.filter)
	}
	filtered, found, err := usecase.ProcessFilterTask(ctx, queue.filter[0])
	if err != nil || !found || filtered == nil || len(filtered.CreatedMeetings) != 1 {
		t.Fatalf("expected filter task to create meeting, found=%v result=%+v err=%v", found, filtered, err)
	}
}

func TestCollectSubscriptionsReturnsCreatedMeetings(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderTelegramChannel, Title: "News", SourceRef: "@news", Enabled: true, FilterID: 1, TeamIDs: []uint{1}}
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "filter", PromptTemplate: "return JSON", Enabled: true, IsDefault: true, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	service := &fakeMessagingService{
		filter: FilterResult{
			Decision:       ptrDecision(domainkernel.NewsMeeting),
			Reason:         ptrString("important"),
			RelatedSymbols: jsonBytes(t, []string{"600000"}),
			FilterID:       1,
		},
		fetched: FetchedMessages{Messages: []FetchedMessage{{
			SourceMessageID: "100",
			MessageTime:     time.Now(),
			Text:            "600000 triggered",
			Raw:             jsonBytes(t, map[string]any{"source": "fixture"}),
		}}},
	}
	usecase := NewUsecase(repo, service, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	result, found, err := usecase.CollectSubscriptions(ctx, CollectInput{Limit: 1})
	if err != nil || !found {
		t.Fatalf("CollectSubscriptions found=%v err=%v", found, err)
	}
	if result.Collected != 1 || result.Filtered != 0 || len(result.CreatedMeetings) != 0 {
		t.Fatalf("unexpected collect result: %+v", result)
	}
}

func TestCreateSubscriptionUpsertsExistingProviderSourceRef(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	first, err := usecase.CreateSubscription(ctx, SubscriptionInput{
		Provider:      domainmsg.ProviderTelegramChannel,
		Title:         "First",
		SourceRef:     "@fixture",
		Enabled:       true,
		TeamIDs:       []uint{1},
		BackfillLimit: 1,
	})
	if err != nil {
		t.Fatalf("first CreateSubscription: %v", err)
	}
	repo.subscriptions[first.ID] = domainmsg.MessageSubscription{
		ID: first.ID, Provider: first.Provider, Title: first.Title, SourceRef: first.SourceRef,
		Enabled: first.Enabled, BackfillLimit: first.BackfillLimit, CollectFrom: time.Now(),
		Config: first.Config, CreatedAt: first.CreatedAt, UpdatedAt: first.UpdatedAt,
	}

	second, err := usecase.CreateSubscription(ctx, SubscriptionInput{
		Provider:      domainmsg.ProviderTelegramChannel,
		Title:         "Second",
		SourceRef:     "@fixture",
		Enabled:       false,
		BackfillLimit: 5,
		Config:        map[string]any{"scope": "updated"},
	})
	if err != nil {
		t.Fatalf("second CreateSubscription: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected duplicate source to update subscription %d, got %d", first.ID, second.ID)
	}
	if len(repo.subscriptions) != 1 {
		t.Fatalf("expected one subscription after upsert, got %d", len(repo.subscriptions))
	}
	stored := repo.subscriptions[first.ID]
	if stored.Title != "Second" || stored.Enabled || stored.BackfillLimit != 5 {
		t.Fatalf("existing subscription was not updated: %+v", stored)
	}
	if !stored.CollectFrom.IsZero() {
		t.Fatalf("existing empty subscription collect_from should be cleared, got %s", stored.CollectFrom)
	}
}

type fakeMessagingSecurity struct{}

func (fakeMessagingSecurity) EncryptSecret(value string) (string, error) { return "enc:" + value, nil }
func (fakeMessagingSecurity) DecryptSecret(value string) (string, error) {
	if len(value) >= 4 && value[:4] == "enc:" {
		return value[4:], nil
	}
	return "", errors.New("bad secret")
}

type fakeMessagingService struct {
	filter              FilterResult
	fetched             FetchedMessages
	fetchCalls          int
	loginHash           string
	loginSession        string
	session             string
	completeCredentials TelegramCredentials
}

func (s *fakeMessagingService) StartSubscriptionLogin(context.Context, TelegramCredentials, string) (string, string, error) {
	return s.loginHash, s.loginSession, nil
}
func (s *fakeMessagingService) CompleteSubscriptionLogin(_ context.Context, credentials TelegramCredentials, _ string, _ string, _ string, _ string) (string, error) {
	s.completeCredentials = credentials
	return s.session, nil
}
func (s *fakeMessagingService) NormalizeSourceRef(_ string, value string) string { return value }
func (s *fakeMessagingService) TestSubscription(context.Context, TelegramCredentials, string, string) (map[string]any, error) {
	return nil, nil
}
func (s *fakeMessagingService) FetchSubscriptionMessages(_ context.Context, _ TelegramCredentials, subscription domainmsg.MessageSubscription, _ int, afterSourceMessageID *string) (FetchedMessages, error) {
	s.fetchCalls++
	if afterSourceMessageID == nil || subscription.Provider == domainmsg.ProviderRSSFeed {
		return s.fetched, nil
	}
	minMessageID, err := strconv.ParseInt(*afterSourceMessageID, 10, 64)
	if err != nil {
		return s.fetched, nil
	}
	filtered := FetchedMessages{SourceRef: s.fetched.SourceRef, Title: s.fetched.Title}
	for _, message := range s.fetched.Messages {
		sourceMessageID, err := strconv.ParseInt(message.SourceMessageID, 10, 64)
		if err == nil && sourceMessageID > minMessageID {
			filtered.Messages = append(filtered.Messages, message)
		}
	}
	return filtered, nil
}
func (s *fakeMessagingService) SendAdapterTest(context.Context, string, string, ProxyConfig) (map[string]any, error) {
	return nil, nil
}
func (s *fakeMessagingService) JSON(value any) domainkernel.JSON {
	raw, _ := json.Marshal(value)
	return domainkernel.JSON(raw)
}
func (s *fakeMessagingService) ExtractRelatedSymbols(string) []string { return nil }
func (s *fakeMessagingService) ApplyFilter(context.Context, domainmsg.IngestedMessage, SecurityService, domainmsg.MessageSubscriptionFilter, domainai.Provider, ProxyConfig) (FilterResult, error) {
	return s.filter, nil
}

type fakeMessagingTx struct {
	repo  *fakeMessagingRepo
	calls int
}

func (t *fakeMessagingTx) WithTx(ctx context.Context, fn func(Repository) error) error {
	t.calls++
	return fn(t.repo)
}

type fakeMessagingQueue struct {
	collect []CollectTask
	filter  []FilterTask
}

func (q *fakeMessagingQueue) EnqueueCollect(_ context.Context, task CollectTask) error {
	q.collect = append(q.collect, task)
	return nil
}

func (q *fakeMessagingQueue) EnqueueFilter(_ context.Context, task FilterTask) error {
	q.filter = append(q.filter, task)
	return nil
}

type fakeMessagingRepo struct {
	secrets              map[string]domainsettings.Secret
	settings             map[string]domainsettings.AppSetting
	roles                map[string]domainai.AgentRole
	providers            map[uint]domainai.Provider
	filters              map[uint]domainmsg.MessageSubscriptionFilter
	subscriptions        map[uint]domainmsg.MessageSubscription
	messages             map[uint]domainmsg.IngestedMessage
	meetings             map[uint]domainmeeting.Meeting
	nextMeetingID        uint
	nextSubscriptionID   uint
	nextMessageID        uint
	saveMessageCalls     int
	createMeetingCalls   int
	appendEventCalls     int
	createReferenceCalls int
}

func newFakeMessagingRepo() *fakeMessagingRepo {
	return &fakeMessagingRepo{
		secrets:            map[string]domainsettings.Secret{},
		settings:           map[string]domainsettings.AppSetting{},
		roles:              map[string]domainai.AgentRole{},
		providers:          map[uint]domainai.Provider{},
		filters:            map[uint]domainmsg.MessageSubscriptionFilter{},
		subscriptions:      map[uint]domainmsg.MessageSubscription{},
		messages:           map[uint]domainmsg.IngestedMessage{},
		meetings:           map[uint]domainmeeting.Meeting{},
		nextMeetingID:      1,
		nextSubscriptionID: 1,
		nextMessageID:      1,
	}
}

func (r *fakeMessagingRepo) ensureDefaultTeamIDs(subscription *domainmsg.MessageSubscription) {
	if subscription.Enabled && len(subscription.TeamIDs) == 0 {
		subscription.TeamIDs = []uint{1}
	}
}

func (r *fakeMessagingRepo) secret(kind domainkernel.SecretKind, name string) (domainsettings.Secret, bool) {
	row, ok := r.secrets[string(kind)+":"+name]
	return row, ok
}

func (r *fakeMessagingRepo) FindAppSetting(_ context.Context, key string) (*domainsettings.AppSetting, bool, error) {
	row, ok := r.settings[key]
	return &row, ok, nil
}
func (r *fakeMessagingRepo) SaveAppSetting(context.Context, *domainsettings.AppSetting) error {
	return nil
}
func (r *fakeMessagingRepo) TryAcquireLease(context.Context, string, domainkernel.JSON, string, time.Time, time.Time, func(domainsettings.AppSetting) bool) (bool, error) {
	return false, nil
}
func (r *fakeMessagingRepo) FindSecret(_ context.Context, kind domainkernel.SecretKind, name string) (*domainsettings.Secret, bool, error) {
	row, ok := r.secret(kind, name)
	return &row, ok, nil
}
func (r *fakeMessagingRepo) CreateSecret(_ context.Context, secret *domainsettings.Secret) error {
	r.secrets[string(secret.Kind)+":"+secret.Name] = *secret
	return nil
}
func (r *fakeMessagingRepo) SaveSecret(_ context.Context, secret *domainsettings.Secret) error {
	r.secrets[string(secret.Kind)+":"+secret.Name] = *secret
	return nil
}
func (r *fakeMessagingRepo) UpsertSecret(_ context.Context, kind domainkernel.SecretKind, name string, encryptedValue string) error {
	r.secrets[string(kind)+":"+name] = domainsettings.Secret{Kind: kind, Name: name, EncryptedValue: encryptedValue}
	return nil
}
func (r *fakeMessagingRepo) DeleteSecretsByNames(_ context.Context, kind domainkernel.SecretKind, names []string) error {
	for _, name := range names {
		delete(r.secrets, string(kind)+":"+name)
	}
	return nil
}
func (r *fakeMessagingRepo) ListSubscriptionFilters(context.Context) ([]domainmsg.MessageSubscriptionFilter, error) {
	out := make([]domainmsg.MessageSubscriptionFilter, 0, len(r.filters))
	for _, row := range r.filters {
		out = append(out, row)
	}
	return out, nil
}
func (r *fakeMessagingRepo) CreateSubscriptionFilter(_ context.Context, filter *domainmsg.MessageSubscriptionFilter) error {
	if filter.ID == 0 {
		filter.ID = uint(len(r.filters) + 1)
	}
	r.filters[filter.ID] = *filter
	return nil
}
func (r *fakeMessagingRepo) FindSubscriptionFilter(_ context.Context, id uint) (*domainmsg.MessageSubscriptionFilter, bool, error) {
	row, ok := r.filters[id]
	return &row, ok, nil
}
func (r *fakeMessagingRepo) FindDefaultSubscriptionFilter(context.Context) (*domainmsg.MessageSubscriptionFilter, bool, error) {
	for _, row := range r.filters {
		if row.IsDefault {
			found := row
			return &found, true, nil
		}
	}
	return nil, false, nil
}
func (r *fakeMessagingRepo) SaveSubscriptionFilter(_ context.Context, filter *domainmsg.MessageSubscriptionFilter) error {
	r.filters[filter.ID] = *filter
	return nil
}
func (r *fakeMessagingRepo) ClearDefaultSubscriptionFiltersExcept(_ context.Context, id uint) error {
	for key, row := range r.filters {
		if row.ID != id {
			row.IsDefault = false
			r.filters[key] = row
		}
	}
	return nil
}
func (r *fakeMessagingRepo) AssignDefaultFilterToUnboundSubscriptions(_ context.Context, id uint) error {
	for key, row := range r.subscriptions {
		if row.FilterID == 0 {
			row.FilterID = id
			r.subscriptions[key] = row
		}
	}
	return nil
}
func (r *fakeMessagingRepo) DeleteLegacyNewsFilterRole(context.Context) error {
	delete(r.roles, "news_filter")
	return nil
}
func (r *fakeMessagingRepo) ListSubscriptions(context.Context) ([]domainmsg.MessageSubscription, error) {
	out := make([]domainmsg.MessageSubscription, 0, len(r.subscriptions))
	for _, row := range r.subscriptions {
		out = append(out, row)
	}
	return out, nil
}
func (r *fakeMessagingRepo) CreateSubscription(_ context.Context, subscription *domainmsg.MessageSubscription) error {
	if subscription.ID == 0 {
		subscription.ID = r.nextSubscriptionID
		r.nextSubscriptionID++
	}
	r.ensureDefaultTeamIDs(subscription)
	r.subscriptions[subscription.ID] = *subscription
	return nil
}
func (r *fakeMessagingRepo) FindSubscription(_ context.Context, id uint) (*domainmsg.MessageSubscription, bool, error) {
	row, ok := r.subscriptions[id]
	return &row, ok, nil
}
func (r *fakeMessagingRepo) FindSubscriptionByProviderAndSourceRef(_ context.Context, provider string, sourceRef string) (*domainmsg.MessageSubscription, bool, error) {
	for _, row := range r.subscriptions {
		if row.Provider == provider && row.SourceRef == sourceRef {
			found := row
			return &found, true, nil
		}
	}
	return nil, false, nil
}
func (r *fakeMessagingRepo) SaveSubscription(_ context.Context, subscription *domainmsg.MessageSubscription) error {
	r.ensureDefaultTeamIDs(subscription)
	r.subscriptions[subscription.ID] = *subscription
	return nil
}
func (r *fakeMessagingRepo) ReplaceSubscriptionTeams(_ context.Context, subscriptionID uint, teamIDs []uint) error {
	row := r.subscriptions[subscriptionID]
	row.TeamIDs = uniqueUintIDs(teamIDs)
	if row.Enabled && len(row.TeamIDs) == 0 {
		row.TeamIDs = []uint{1}
	}
	r.subscriptions[subscriptionID] = row
	return nil
}
func (r *fakeMessagingRepo) ListSubscriptionTeamIDs(_ context.Context, subscriptionID uint) ([]uint, error) {
	row, ok := r.subscriptions[subscriptionID]
	if !ok {
		return nil, nil
	}
	if len(row.TeamIDs) == 0 && row.Enabled {
		return []uint{1}, nil
	}
	return append([]uint(nil), row.TeamIDs...), nil
}
func (r *fakeMessagingRepo) ResearchTeamReady(context.Context, uint) (bool, string, error) {
	return true, "", nil
}
func (r *fakeMessagingRepo) ListSubscriptionsForCollect(_ context.Context, subscriptionID *uint) ([]domainmsg.MessageSubscription, error) {
	out := make([]domainmsg.MessageSubscription, 0, len(r.subscriptions))
	for _, row := range r.subscriptions {
		if !row.Enabled {
			continue
		}
		if subscriptionID != nil && row.ID != *subscriptionID {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}
func (r *fakeMessagingRepo) DeleteSubscriptionGraph(context.Context, uint, []string) error {
	return nil
}
func (r *fakeMessagingRepo) ListMessagesBySubscription(_ context.Context, subscriptionID uint) ([]domainmsg.IngestedMessage, error) {
	out := make([]domainmsg.IngestedMessage, 0)
	for _, row := range r.messages {
		if row.SubscriptionID == subscriptionID {
			out = append(out, row)
		}
	}
	return out, nil
}
func (r *fakeMessagingRepo) ListMessages(context.Context, RepositoryMessageFilter) ([]domainmsg.IngestedMessage, error) {
	return nil, nil
}
func (r *fakeMessagingRepo) CreateMessage(_ context.Context, message *domainmsg.IngestedMessage) error {
	if message.ID == 0 {
		message.ID = r.nextMessageID
		r.nextMessageID++
	}
	r.messages[message.ID] = *message
	return nil
}
func (r *fakeMessagingRepo) FindMessage(_ context.Context, id uint) (*domainmsg.IngestedMessage, bool, error) {
	row, ok := r.messages[id]
	return &row, ok, nil
}
func (r *fakeMessagingRepo) FindMessageBySource(_ context.Context, subscriptionID uint, sourceMessageID string) (*domainmsg.IngestedMessage, bool, error) {
	for _, row := range r.messages {
		if row.SubscriptionID == subscriptionID && row.SourceMessageID == sourceMessageID {
			found := row
			return &found, true, nil
		}
	}
	return nil, false, nil
}
func (r *fakeMessagingRepo) SaveMessage(_ context.Context, message *domainmsg.IngestedMessage) error {
	r.saveMessageCalls++
	r.messages[message.ID] = *message
	return nil
}
func (r *fakeMessagingRepo) DeleteMessage(context.Context, *domainmsg.IngestedMessage, []string) error {
	return nil
}
func (r *fakeMessagingRepo) DeleteMessagesByIDsWithRefs(context.Context, []uint, []string) error {
	return nil
}
func (r *fakeMessagingRepo) ListMessagesForRefilter(context.Context, []uint, bool, int) ([]domainmsg.IngestedMessage, error) {
	return nil, nil
}
func (r *fakeMessagingRepo) FindSubscriptionForMessage(ctx context.Context, subscriptionID uint) (*domainmsg.MessageSubscription, bool, error) {
	return r.FindSubscription(ctx, subscriptionID)
}
func (r *fakeMessagingRepo) RecentMeetings(context.Context, int) ([]domainmeeting.Meeting, error) {
	return nil, nil
}
func (r *fakeMessagingRepo) CreateMeeting(_ context.Context, meeting *domainmeeting.Meeting) error {
	r.createMeetingCalls++
	meeting.ID = r.nextMeetingID
	r.nextMeetingID++
	r.meetings[meeting.ID] = *meeting
	return nil
}
func (r *fakeMessagingRepo) AppendMeetingEvent(context.Context, *domainmeeting.Event) error {
	r.appendEventCalls++
	return nil
}
func (r *fakeMessagingRepo) CreateMeetingReference(context.Context, *domainmeeting.Reference) error {
	r.createReferenceCalls++
	return nil
}
func (r *fakeMessagingRepo) FindMeetingReferenceByExternalRef(context.Context, string, string) (*domainmeeting.Reference, bool, error) {
	return nil, false, nil
}
func (r *fakeMessagingRepo) FindMeeting(_ context.Context, id uint) (*domainmeeting.Meeting, bool, error) {
	row, ok := r.meetings[id]
	return &row, ok, nil
}
func (r *fakeMessagingRepo) FindAgentRole(_ context.Context, key string) (*domainai.AgentRole, bool, error) {
	row, ok := r.roles[key]
	return &row, ok, nil
}
func (r *fakeMessagingRepo) FindAIProvider(_ context.Context, id uint) (*domainai.Provider, bool, error) {
	row, ok := r.providers[id]
	return &row, ok, nil
}
func (r *fakeMessagingRepo) ListPlatformAdapters(context.Context) ([]domainmsg.PlatformAdapter, error) {
	return nil, nil
}
func (r *fakeMessagingRepo) CreatePlatformAdapter(context.Context, *domainmsg.PlatformAdapter) error {
	return nil
}
func (r *fakeMessagingRepo) FindPlatformAdapter(context.Context, uint) (*domainmsg.PlatformAdapter, bool, error) {
	return nil, false, nil
}
func (r *fakeMessagingRepo) SavePlatformAdapter(context.Context, *domainmsg.PlatformAdapter) error {
	return nil
}
func (r *fakeMessagingRepo) DeletePlatformAdapter(context.Context, uint) error { return nil }

func ptrDecision(value domainkernel.NewsDecision) *domainkernel.NewsDecision { return &value }
func ptrString(value string) *string                                         { return &value }
func uintPtr(value uint) *uint                                               { return &value }

func jsonBytes(t testing.TB, value any) domainkernel.JSON {
	if t != nil {
		t.Helper()
	}
	raw, err := json.Marshal(value)
	if err != nil && t != nil {
		t.Fatal(err)
	}
	return domainkernel.JSON(raw)
}
