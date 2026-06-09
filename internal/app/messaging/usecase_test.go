package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainmsg "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/messaging"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
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

func TestProcessFilterTaskReturnsRetryableErrorAfterPersistingFilterFailure(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderTelegramChannel, Title: "News", SourceRef: "@news", Enabled: true, FilterID: 1}
	repo.messages[1] = domainmsg.IngestedMessage{ID: 1, SubscriptionID: 1, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: "100", MessageTime: time.Now(), Text: "600000 triggered"}
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "filter", PromptTemplate: "return JSON", Enabled: true, IsDefault: true, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	usecase := NewUsecase(repo, &fakeMessagingService{filterErr: errors.New("provider timeout")}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	result, found, err := usecase.ProcessFilterTask(ctx, FilterTask{MessageID: 1})
	if !found || err == nil || result == nil {
		t.Fatalf("ProcessFilterTask found=%v result=%+v err=%v, want retryable error with result", found, result, err)
	}
	stored := repo.messages[1]
	if stored.FilterStatus != domainmsg.FilterStatusFailed || stored.FilterReason == nil || !strings.Contains(*stored.FilterReason, "provider timeout") {
		t.Fatalf("filter failure was not persisted for operator visibility: %+v", stored)
	}
	if len(result.CreatedMeetings) != 0 || result.Row.Message.FilterStatus != domainmsg.FilterStatusFailed {
		t.Fatalf("failed filter should not create meetings and should return failed row, got %+v", result)
	}
}

func TestFilterMessageAppliesSourceTrustDownrank(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderTelegramChannel, Title: "Noisy Feed", SourceRef: "@noisy", Enabled: true, FilterID: 1, TeamIDs: []uint{1}}
	repo.messages[10] = domainmsg.IngestedMessage{ID: 10, SubscriptionID: 1, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: "100", MessageTime: time.Now(), Text: "600000 triggered"}
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "filter", PromptTemplate: "return JSON", Enabled: true, IsDefault: true, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	now := time.Now()
	noise := domainmsg.FeedbackNoise
	misclassified := domainmsg.FeedbackMisclassified
	ignore := domainkernel.NewsIgnore
	repo.messages[1] = domainmsg.IngestedMessage{ID: 1, SubscriptionID: 1, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: "1", MessageTime: now, Text: "noise one", FilterDecision: &ignore, FeedbackLabel: &noise, FeedbackAt: &now}
	repo.messages[2] = domainmsg.IngestedMessage{ID: 2, SubscriptionID: 1, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: "2", MessageTime: now, Text: "noise two", FilterDecision: &ignore, FeedbackLabel: &noise, FeedbackAt: &now}
	repo.messages[3] = domainmsg.IngestedMessage{ID: 3, SubscriptionID: 1, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: "3", MessageTime: now, Text: "bad classification", FilterDecision: &ignore, FeedbackLabel: &misclassified, FeedbackAt: &now}
	service := &fakeMessagingService{filter: FilterResult{
		Decision:       ptrDecision(domainkernel.NewsMeeting),
		Reason:         ptrString("model wanted a meeting"),
		RelatedSymbols: jsonBytes(t, []string{"600000"}),
		FilterID:       1,
	}}
	usecase := NewUsecase(repo, service, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	result, found, err := usecase.FilterMessage(ctx, 10)
	if err != nil || !found {
		t.Fatalf("FilterMessage found=%v err=%v", found, err)
	}
	if result.Row.Message.FilterDecision == nil || *result.Row.Message.FilterDecision != domainkernel.NewsObserve {
		t.Fatalf("low-trust source should downrank meeting to observe, got %+v", result.Row.Message.FilterDecision)
	}
	if result.Row.Message.FilterReason == nil || !strings.Contains(*result.Row.Message.FilterReason, "Source trust auto-downrank applied") {
		t.Fatalf("expected downrank reason to be persisted, got %+v", result.Row.Message.FilterReason)
	}
	if len(result.CreatedMeetings) != 0 || repo.createMeetingCalls != 0 {
		t.Fatalf("downranked meeting should not dispatch a meeting, result=%+v createMeetingCalls=%d", result.CreatedMeetings, repo.createMeetingCalls)
	}
	report, err := usecase.SourceTrustReport(ctx, SourceTrustFilter{SubscriptionID: "1"})
	if err != nil {
		t.Fatalf("SourceTrustReport: %v", err)
	}
	if len(report.Items) != 1 || report.Items[0].AutoAction == "" {
		t.Fatalf("expected source trust report to expose auto action: %+v", report.Items)
	}
}

func TestFeedbackTrainingSamplesAndSourceTrustReport(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderTelegramChannel, Title: "Useful Feed", SourceRef: "@useful", Enabled: true}
	repo.subscriptions[2] = domainmsg.MessageSubscription{ID: 2, Provider: domainmsg.ProviderRSSFeed, Title: "Noisy Feed", SourceRef: "https://example.test/feed.xml", Enabled: true}
	now := time.Now()
	helpful := domainmsg.FeedbackHelpful
	noise := domainmsg.FeedbackNoise
	misclassified := domainmsg.FeedbackMisclassified
	observe := domainkernel.NewsObserve
	ignore := domainkernel.NewsIgnore
	meeting := domainkernel.NewsMeeting
	repo.messages[1] = domainmsg.IngestedMessage{ID: 1, SubscriptionID: 1, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: "1", MessageTime: now, Text: "600000 useful", FilterDecision: &observe, FeedbackLabel: &helpful, FeedbackAt: &now}
	repo.messages[2] = domainmsg.IngestedMessage{ID: 2, SubscriptionID: 1, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: "2", MessageTime: now, Text: "600001 useful", FilterDecision: &observe, FeedbackLabel: &helpful, FeedbackAt: &now}
	repo.messages[3] = domainmsg.IngestedMessage{ID: 3, SubscriptionID: 1, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: "3", MessageTime: now, Text: "600002 noisy", FilterDecision: &ignore, FeedbackLabel: &noise, FeedbackAt: &now}
	repo.messages[4] = domainmsg.IngestedMessage{ID: 4, SubscriptionID: 2, Provider: domainmsg.ProviderRSSFeed, SourceMessageID: "4", MessageTime: now, Text: "bad classification", FilterDecision: &meeting, FeedbackLabel: &misclassified, FeedbackAt: &now}
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	samples, err := usecase.ListFeedbackTrainingSamples(ctx, FeedbackTrainingFilter{SubscriptionID: "1", Label: domainmsg.FeedbackHelpful, Limit: 10})
	if err != nil {
		t.Fatalf("ListFeedbackTrainingSamples: %v", err)
	}
	if len(samples.Rows) != 2 || samples.Rows[0].Subscription.SourceRef != "@useful" || samples.Rows[0].TrainingUse != "positive_source_signal" || samples.Rows[0].SampleWeight != 1 {
		t.Fatalf("unexpected feedback samples: %+v", samples.Rows)
	}

	report, err := usecase.SourceTrustReport(ctx, SourceTrustFilter{})
	if err != nil {
		t.Fatalf("SourceTrustReport: %v", err)
	}
	if report.SampleCount != 4 || report.SourceCount != 2 {
		t.Fatalf("unexpected report counts: %+v", report)
	}
	if report.Items[0].SourceRef != "@useful" || report.Items[0].Status != "watch" || report.Items[0].TrustScore <= report.Items[1].TrustScore {
		t.Fatalf("unexpected source trust ranking: %+v", report.Items)
	}
	if report.Items[1].Status != "insufficient_feedback" {
		t.Fatalf("single negative sample should still require more feedback: %+v", report.Items[1])
	}

	evaluation, err := usecase.FeedbackEvaluation(ctx, FeedbackTrainingFilter{})
	if err != nil {
		t.Fatalf("FeedbackEvaluation: %v", err)
	}
	if evaluation.SampleCount != 4 || evaluation.AgreementCount != 3 || evaluation.NeedsDecisionCorrectionCount != 1 || evaluation.Status != "needs_more_feedback" {
		t.Fatalf("unexpected feedback evaluation: %+v", evaluation)
	}

	snapshot, err := usecase.CreateFeedbackTrainingSnapshot(ctx, FeedbackTrainingFilter{})
	if err != nil {
		t.Fatalf("CreateFeedbackTrainingSnapshot: %v", err)
	}
	if snapshot.SampleCount != 4 || snapshot.SourceCount != 2 || snapshot.Fingerprint == "" || snapshot.Version == "" || snapshot.SampleRefCount != 4 {
		t.Fatalf("unexpected feedback snapshot: %+v", snapshot)
	}
	repeated, err := usecase.CreateFeedbackTrainingSnapshot(ctx, FeedbackTrainingFilter{})
	if err != nil {
		t.Fatalf("CreateFeedbackTrainingSnapshot repeated: %v", err)
	}
	if repeated.Version != snapshot.Version {
		t.Fatalf("unchanged training set should reuse snapshot version, got %q then %q", snapshot.Version, repeated.Version)
	}
	snapshots, err := usecase.ListFeedbackTrainingSnapshots(ctx, FeedbackTrainingFilter{})
	if err != nil {
		t.Fatalf("ListFeedbackTrainingSnapshots: %v", err)
	}
	if len(snapshots) != 1 || snapshots[0].Fingerprint != snapshot.Fingerprint {
		t.Fatalf("unexpected persisted snapshots: %+v", snapshots)
	}

	export, err := usecase.CreateFeedbackTrainingExport(ctx, FeedbackTrainingFilter{})
	if err != nil {
		t.Fatalf("CreateFeedbackTrainingExport: %v", err)
	}
	if export.SampleCount != 4 || export.LineCount != 4 || export.ByteCount == 0 || export.ContentSHA256 == "" || export.Content == "" {
		t.Fatalf("unexpected feedback training export: %+v", export)
	}
	if strings.Count(export.Content, "\n") != export.LineCount || !strings.Contains(export.Content, `"feedbackLabel":"helpful"`) {
		t.Fatalf("unexpected feedback training export content: %q", export.Content)
	}
	foundExport, found, err := usecase.FindFeedbackTrainingExport(ctx, export.Version)
	if err != nil || !found || foundExport.ContentSHA256 != export.ContentSHA256 {
		t.Fatalf("FindFeedbackTrainingExport found=%v export=%+v err=%v", found, foundExport, err)
	}
	repeatedExport, err := usecase.CreateFeedbackTrainingExport(ctx, FeedbackTrainingFilter{})
	if err != nil {
		t.Fatalf("CreateFeedbackTrainingExport repeated: %v", err)
	}
	if repeatedExport.Version != export.Version {
		t.Fatalf("unchanged training export should reuse version, got %q then %q", export.Version, repeatedExport.Version)
	}
	exports, err := usecase.ListFeedbackTrainingExports(ctx, FeedbackTrainingFilter{})
	if err != nil {
		t.Fatalf("ListFeedbackTrainingExports: %v", err)
	}
	if len(exports) != 1 || exports[0].ContentSHA256 != export.ContentSHA256 {
		t.Fatalf("unexpected persisted training exports: %+v", exports)
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

func TestSubscriptionDiagnosticsReportTelegramPrivateSourceRequirements(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderTelegramChannel, Title: "Private", SourceRef: "-1001234567890", Enabled: true, FilterID: 1, TeamIDs: []uint{1}}
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "filter", PromptTemplate: "return JSON", Enabled: true, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	diagnostics, err := usecase.SubscriptionDiagnostics(ctx)
	if err != nil {
		t.Fatalf("SubscriptionDiagnostics: %v", err)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics len=%d, want 1", len(diagnostics))
	}
	diagnostic := diagnostics[0]
	if diagnostic.SourceKind != "telegram_private_numeric" || diagnostic.Status != "blocked" || diagnostic.ProxyRoute != "direct" {
		t.Fatalf("unexpected telegram diagnostic: %+v", diagnostic)
	}
	sessionCheck := findSubscriptionDiagnosticCheck(t, diagnostic, "telegram_session")
	if sessionCheck.Status != "blocked" {
		t.Fatalf("telegram_session check = %+v, want blocked", sessionCheck)
	}
	privateCheck := findSubscriptionDiagnosticCheck(t, diagnostic, "telegram_private_source")
	if privateCheck.Status != "warning" || privateCheck.Depends != "telegram_session" {
		t.Fatalf("telegram_private_source check = %+v, want warning depending on telegram_session", privateCheck)
	}
}

func TestSubscriptionDiagnosticsRejectRSSURLCredentials(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderRSSFeed, Title: "Private RSS", SourceRef: "https://user:pass@example.test/feed.xml", Enabled: false, FilterID: 1}
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "filter", PromptTemplate: "return JSON", Enabled: true, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	diagnostics, err := usecase.SubscriptionDiagnostics(ctx)
	if err != nil {
		t.Fatalf("SubscriptionDiagnostics: %v", err)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics len=%d, want 1", len(diagnostics))
	}
	diagnostic := diagnostics[0]
	if diagnostic.SourceKind != "rss_url_credentials" || diagnostic.Status != "disabled" {
		t.Fatalf("unexpected RSS diagnostic: %+v", diagnostic)
	}
	authCheck := findSubscriptionDiagnosticCheck(t, diagnostic, "rss_url_credentials")
	if authCheck.Status != "blocked" {
		t.Fatalf("rss_url_credentials check = %+v, want blocked", authCheck)
	}
}

func TestCreateRSSSubscriptionStoresAuthSecret(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "filter", PromptTemplate: "return JSON", Enabled: true, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	result, err := usecase.CreateSubscription(ctx, SubscriptionInput{
		Provider:       domainmsg.ProviderRSSFeed,
		Title:          "Private RSS",
		SourceRef:      "https://example.test/feed.xml",
		Enabled:        false,
		FilterID:       1,
		RSSAuthType:    rssAuthTypeBasic,
		RSSAuthTypeSet: true,
		RSSUsername:    "feed-user",
		RSSUsernameSet: true,
		RSSPassword:    "feed-pass",
		RSSPasswordSet: true,
	})
	if err != nil {
		t.Fatalf("CreateSubscription: %v", err)
	}
	row := result.MessageSubscription
	auth := rssAuthConfigFromSubscription(row)
	if auth.Type != rssAuthTypeBasic || auth.Username != "feed-user" || auth.PasswordSecretName != rssPasswordSecretName(row.ID) {
		t.Fatalf("unexpected rss auth config: %+v", auth)
	}
	if secret, ok := repo.secret(domainkernel.SecretKindMessageSubscription, rssPasswordSecretName(row.ID)); !ok || secret.EncryptedValue != "enc:feed-pass" {
		t.Fatalf("rss auth secret not saved: %+v ok=%v", secret, ok)
	}
	config := jsonValueFromDomainJSON(row.Config).(map[string]any)
	rssAuth := config["rssAuth"].(map[string]any)
	if _, ok := rssAuth["password"]; ok {
		t.Fatalf("rss auth config must not contain password: %+v", rssAuth)
	}
}

func TestSubscriptionDiagnosticsReportReadyRSSAuth(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.secrets[string(domainkernel.SecretKindMessageSubscription)+":"+rssPasswordSecretName(1)] = domainsettings.Secret{Kind: domainkernel.SecretKindMessageSubscription, Name: rssPasswordSecretName(1), EncryptedValue: "enc:feed-pass"}
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderRSSFeed, Title: "Private RSS", SourceRef: "https://example.test/feed.xml", Enabled: true, FilterID: 1, TeamIDs: []uint{1}, Config: domainkernel.JSON(`{"rssAuth":{"type":"basic","username":"feed-user","passwordSecretName":"rss:1:password"}}`)}
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "filter", PromptTemplate: "return JSON", Enabled: true, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	diagnostics, err := usecase.SubscriptionDiagnostics(ctx)
	if err != nil {
		t.Fatalf("SubscriptionDiagnostics: %v", err)
	}
	diagnostic := diagnostics[0]
	if diagnostic.SourceKind != "rss_private_auth" || diagnostic.Status != "ready" || !diagnostic.PrivateCapable {
		t.Fatalf("unexpected RSS diagnostic: %+v", diagnostic)
	}
	authCheck := findSubscriptionDiagnosticCheck(t, diagnostic, "rss_auth")
	if authCheck.Status != "ok" {
		t.Fatalf("rss_auth check = %+v, want ok", authCheck)
	}
}

func TestCollectRSSSubscriptionPassesStoredAuthCredentials(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.secrets[string(domainkernel.SecretKindMessageSubscription)+":"+rssPasswordSecretName(1)] = domainsettings.Secret{Kind: domainkernel.SecretKindMessageSubscription, Name: rssPasswordSecretName(1), EncryptedValue: "enc:feed-pass"}
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderRSSFeed, Title: "Private RSS", SourceRef: "https://example.test/feed.xml", Enabled: true, FilterID: 1, TeamIDs: []uint{1}, Config: domainkernel.JSON(`{"rssAuth":{"type":"basic","username":"feed-user","passwordSecretName":"rss:1:password"}}`)}
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "filter", PromptTemplate: "return JSON", Enabled: true, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	service := &fakeMessagingService{fetched: FetchedMessages{Title: "Private RSS", Messages: []FetchedMessage{{SourceMessageID: "item-1", MessageTime: time.Now(), Text: "private signal"}}}}
	usecase := NewUsecase(repo, service, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	id := uint(1)
	result, found, err := usecase.CollectSubscriptions(ctx, CollectInput{SubscriptionID: &id, Limit: 1})
	if err != nil || !found {
		t.Fatalf("CollectSubscriptions found=%v err=%v", found, err)
	}
	if result.Collected != 1 {
		t.Fatalf("collected = %d, want 1", result.Collected)
	}
	got := service.lastCredentials.RSSAuth
	if got.Type != rssAuthTypeBasic || got.Username != "feed-user" || got.Password != "feed-pass" {
		t.Fatalf("unexpected RSS auth credentials: %+v", got)
	}
}

func TestUpdateRSSSubscriptionToNoAuthDeletesSecret(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.secrets[string(domainkernel.SecretKindMessageSubscription)+":"+rssPasswordSecretName(1)] = domainsettings.Secret{Kind: domainkernel.SecretKindMessageSubscription, Name: rssPasswordSecretName(1), EncryptedValue: "enc:feed-pass"}
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderRSSFeed, Title: "Private RSS", SourceRef: "https://example.test/feed.xml", Enabled: false, FilterID: 1, Config: domainkernel.JSON(`{"rssAuth":{"type":"basic","username":"feed-user","passwordSecretName":"rss:1:password"}}`)}
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "filter", PromptTemplate: "return JSON", Enabled: true, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	result, found, err := usecase.UpdateSubscription(ctx, 1, SubscriptionInput{RSSAuthType: rssAuthTypeNone, RSSAuthTypeSet: true}, map[string]bool{"rssAuthType": true})
	if err != nil || !found {
		t.Fatalf("UpdateSubscription found=%v err=%v", found, err)
	}
	if _, ok := repo.secret(domainkernel.SecretKindMessageSubscription, rssPasswordSecretName(1)); ok {
		t.Fatal("rss auth secret should be deleted when auth type is none")
	}
	if auth := rssAuthConfigFromSubscription(result.MessageSubscription); auth.Type != rssAuthTypeNone || auth.PasswordSecretName != "" {
		t.Fatalf("unexpected rss auth config after clearing: %+v", auth)
	}
}

func TestRunSubscriptionMaintenanceRepairsDefaultsAndQueuesCollect(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	collectErr := "previous collect failed"
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "default", PromptTemplate: "return JSON", Enabled: true, IsDefault: true, ProviderID: uintPtr(1)}
	repo.filters[2] = domainmsg.MessageSubscriptionFilter{ID: 2, Name: "disabled", PromptTemplate: "return JSON", Enabled: false, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderRSSFeed, Title: "Feed", SourceRef: "https://example.test/feed.xml", Enabled: true, FilterID: 2, TeamIDs: []uint{1}, LastCollectError: &collectErr}
	queue := &fakeMessagingQueue{}
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo}).WithTaskQueue(queue)

	result, err := usecase.RunSubscriptionMaintenance(ctx, SubscriptionMaintenanceInput{Action: "repair_defaults", OnlyBlocked: true})
	if err != nil {
		t.Fatalf("RunSubscriptionMaintenance: %v", err)
	}
	if result.Matched != 1 || result.Updated != 1 || result.Queued != 1 || len(result.Skipped) != 0 {
		t.Fatalf("unexpected maintenance result: %+v", result)
	}
	row := repo.subscriptions[1]
	if row.FilterID != 1 || row.LastCollectError != nil {
		t.Fatalf("subscription was not repaired: %+v", row)
	}
	if len(queue.collect) != 1 || queue.collect[0].SubscriptionID != 1 || queue.collect[0].Reason != "maintenance_repair" {
		t.Fatalf("unexpected collect tasks: %+v", queue.collect)
	}
}

func TestRunSubscriptionMaintenanceRotatesRSSAuth(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.secrets[string(domainkernel.SecretKindMessageSubscription)+":"+rssPasswordSecretName(1)] = domainsettings.Secret{Kind: domainkernel.SecretKindMessageSubscription, Name: rssPasswordSecretName(1), EncryptedValue: "enc:old-pass"}
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderRSSFeed, Title: "Private RSS", SourceRef: "https://example.test/feed.xml", Enabled: false, FilterID: 1, Config: domainkernel.JSON(`{"rssAuth":{"type":"basic","username":"old-user","passwordSecretName":"rss:1:password"}}`)}
	repo.subscriptions[2] = domainmsg.MessageSubscription{ID: 2, Provider: domainmsg.ProviderTelegramChannel, Title: "Telegram", SourceRef: "@news", Enabled: false, FilterID: 1}
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "filter", PromptTemplate: "return JSON", Enabled: true, IsDefault: true, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	result, err := usecase.RunSubscriptionMaintenance(ctx, SubscriptionMaintenanceInput{
		Action:          "rotate_rss_auth",
		SubscriptionIDs: []uint{1, 2},
		RSSAuthType:     rssAuthTypeBasic,
		RSSAuthTypeSet:  true,
		RSSUsername:     "new-user",
		RSSUsernameSet:  true,
		RSSPassword:     "new-pass",
		RSSPasswordSet:  true,
	})
	if err != nil {
		t.Fatalf("RunSubscriptionMaintenance: %v", err)
	}
	if result.Matched != 2 || result.Updated != 1 || len(result.Skipped) != 1 {
		t.Fatalf("unexpected maintenance result: %+v", result)
	}
	auth := rssAuthConfigFromSubscription(repo.subscriptions[1])
	if auth.Type != rssAuthTypeBasic || auth.Username != "new-user" || auth.PasswordSecretName != rssPasswordSecretName(1) {
		t.Fatalf("unexpected auth config after rotation: %+v", auth)
	}
	if secret, ok := repo.secret(domainkernel.SecretKindMessageSubscription, rssPasswordSecretName(1)); !ok || secret.EncryptedValue != "enc:new-pass" {
		t.Fatalf("rss auth secret not rotated: %+v ok=%v", secret, ok)
	}
}

func TestRunSubscriptionMaintenanceAuditsTelegramAccess(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	oldErr := "telegram access audit failed: old permission issue"
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderTelegramChannel, Title: "Public", SourceRef: "@public", Enabled: true, FilterID: 1, TeamIDs: []uint{1}, LastCollectError: &oldErr}
	repo.subscriptions[2] = domainmsg.MessageSubscription{ID: 2, Provider: domainmsg.ProviderTelegramChannel, Title: "Private", SourceRef: "-100123", Enabled: true, FilterID: 1, TeamIDs: []uint{1}}
	repo.subscriptions[3] = domainmsg.MessageSubscription{ID: 3, Provider: domainmsg.ProviderRSSFeed, Title: "Feed", SourceRef: "https://example.test/feed.xml", Enabled: true, FilterID: 1, TeamIDs: []uint{1}}
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "filter", PromptTemplate: "return JSON", Enabled: true, IsDefault: true, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	service := &fakeMessagingService{testErrors: map[string]error{"-100123": errors.New("peer not accessible")}}
	usecase := NewUsecase(repo, service, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	result, err := usecase.RunSubscriptionMaintenance(ctx, SubscriptionMaintenanceInput{Action: "audit_telegram_access", SubscriptionIDs: []uint{1, 2, 3}})
	if err != nil {
		t.Fatalf("RunSubscriptionMaintenance: %v", err)
	}
	if result.Matched != 3 || result.Checked != 2 || result.Passed != 1 || result.Failed != 1 || result.Updated != 2 || len(result.Skipped) != 1 {
		t.Fatalf("unexpected maintenance result: %+v", result)
	}
	if repo.subscriptions[1].LastCollectError != nil {
		t.Fatalf("successful audit should clear previous telegram access error, got %q", *repo.subscriptions[1].LastCollectError)
	}
	if repo.subscriptions[2].LastCollectError == nil || !strings.Contains(*repo.subscriptions[2].LastCollectError, "peer not accessible") {
		t.Fatalf("failed audit should persist access error, got %+v", repo.subscriptions[2].LastCollectError)
	}
	tested := map[string]bool{}
	for _, call := range service.testCalls {
		tested[call] = true
	}
	if len(service.testCalls) != 2 || !tested["@public"] || !tested["-100123"] {
		t.Fatalf("unexpected tested refs: %+v", service.testCalls)
	}
}

func TestRunSubscriptionMaintenanceAppliesSourceTrustGovernance(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderTelegramChannel, Title: "Noisy", SourceRef: "@noisy", Enabled: true, FilterID: 1, TeamIDs: []uint{1}}
	repo.subscriptions[2] = domainmsg.MessageSubscription{ID: 2, Provider: domainmsg.ProviderRSSFeed, Title: "Unreviewed", SourceRef: "https://example.test/feed.xml", Enabled: true, FilterID: 1, TeamIDs: []uint{1}}
	repo.subscriptions[3] = domainmsg.MessageSubscription{ID: 3, Provider: domainmsg.ProviderTelegramChannel, Title: "Mixed", SourceRef: "@mixed", Enabled: true, FilterID: 1, TeamIDs: []uint{1}}
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "filter", PromptTemplate: "return JSON", Enabled: true, IsDefault: true, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	now := time.Now()
	noise := domainmsg.FeedbackNoise
	helpful := domainmsg.FeedbackHelpful
	ignore := domainkernel.NewsIgnore
	observe := domainkernel.NewsObserve
	for id := uint(1); id <= 4; id++ {
		repo.messages[id] = domainmsg.IngestedMessage{ID: id, SubscriptionID: 1, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: strconv.FormatUint(uint64(id), 10), MessageTime: now, Text: "noise", FilterDecision: &ignore, FeedbackLabel: &noise, FeedbackAt: &now}
	}
	repo.messages[5] = domainmsg.IngestedMessage{ID: 5, SubscriptionID: 3, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: "5", MessageTime: now, Text: "mixed helpful 1", FilterDecision: &observe, FeedbackLabel: &helpful, FeedbackAt: &now}
	repo.messages[6] = domainmsg.IngestedMessage{ID: 6, SubscriptionID: 3, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: "6", MessageTime: now, Text: "mixed helpful 2", FilterDecision: &observe, FeedbackLabel: &helpful, FeedbackAt: &now}
	repo.messages[7] = domainmsg.IngestedMessage{ID: 7, SubscriptionID: 3, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: "7", MessageTime: now, Text: "mixed noise 1", FilterDecision: &observe, FeedbackLabel: &noise, FeedbackAt: &now}
	repo.messages[8] = domainmsg.IngestedMessage{ID: 8, SubscriptionID: 3, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: "8", MessageTime: now, Text: "mixed noise 2", FilterDecision: &observe, FeedbackLabel: &noise, FeedbackAt: &now}
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	result, err := usecase.RunSubscriptionMaintenance(ctx, SubscriptionMaintenanceInput{Action: "apply_source_trust_governance", SubscriptionIDs: []uint{1, 2, 3}})
	if err != nil {
		t.Fatalf("RunSubscriptionMaintenance: %v", err)
	}
	if result.Matched != 3 || result.Checked != 2 || result.Updated != 2 || result.Paused != 1 || result.Passed != 0 || len(result.Skipped) != 1 {
		t.Fatalf("unexpected maintenance result: %+v", result)
	}
	noisy := repo.subscriptions[1]
	if noisy.Enabled || noisy.LastCollectError == nil || !strings.Contains(*noisy.LastCollectError, "paused source") {
		t.Fatalf("severe source should be paused with governance reason: %+v", noisy)
	}
	noisyConfig := jsonValueFromDomainJSON(noisy.Config).(map[string]any)
	noisyGovernance := noisyConfig["sourceTrustGovernance"].(map[string]any)
	if noisyGovernance["action"] != "pause_source" || noisyGovernance["autoAction"] != "downrank_meeting_to_observe_and_observe_to_ignore" {
		t.Fatalf("unexpected noisy governance config: %+v", noisyGovernance)
	}
	mixed := repo.subscriptions[3]
	if !mixed.Enabled || mixed.LastCollectError == nil || !strings.Contains(*mixed.LastCollectError, "governance watch") {
		t.Fatalf("mixed source should stay enabled with watch reason: %+v", mixed)
	}
	mixedConfig := jsonValueFromDomainJSON(mixed.Config).(map[string]any)
	mixedGovernance := mixedConfig["sourceTrustGovernance"].(map[string]any)
	if mixedGovernance["action"] != "watch_source" || mixedGovernance["autoAction"] != "downrank_meeting_to_observe" {
		t.Fatalf("unexpected mixed governance config: %+v", mixedGovernance)
	}
}

func TestSubscriptionDiagnosticsWarnWhenProxyRouteIsNotEnabled(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.secrets[string(domainkernel.SecretKindApp)+":"+proxySecretName] = domainsettings.Secret{Kind: domainkernel.SecretKindApp, Name: proxySecretName, EncryptedValue: "enc:http://127.0.0.1:7890"}
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderRSSFeed, Title: "Feed", SourceRef: "https://example.test/feed.xml", Enabled: false, FilterID: 1}
	repo.filters[1] = domainmsg.MessageSubscriptionFilter{ID: 1, Name: "filter", PromptTemplate: "return JSON", Enabled: true, ProviderID: uintPtr(1)}
	repo.providers[1] = domainai.Provider{ID: 1, Enabled: true, DefaultModel: "fixture-model", APIKeySecret: &domainsettings.Secret{EncryptedValue: "enc:key"}}
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	diagnostics, err := usecase.SubscriptionDiagnostics(ctx)
	if err != nil {
		t.Fatalf("SubscriptionDiagnostics: %v", err)
	}
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics len=%d, want 1", len(diagnostics))
	}
	diagnostic := diagnostics[0]
	if diagnostic.ProxyRoute != "proxy_configured_but_not_enabled" {
		t.Fatalf("proxy route = %q, want proxy_configured_but_not_enabled", diagnostic.ProxyRoute)
	}
	proxyCheck := findSubscriptionDiagnosticCheck(t, diagnostic, "proxy_route")
	if proxyCheck.Status != "warning" {
		t.Fatalf("proxy_route check = %+v, want warning", proxyCheck)
	}
}

func TestFeedbackMessagesBatchUpdatesFoundAndReportsMissing(t *testing.T) {
	ctx := context.Background()
	repo := newFakeMessagingRepo()
	repo.subscriptions[1] = domainmsg.MessageSubscription{ID: 1, Provider: domainmsg.ProviderTelegramChannel, Title: "News", SourceRef: "@news", Enabled: true}
	repo.messages[1] = domainmsg.IngestedMessage{ID: 1, SubscriptionID: 1, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: "1", MessageTime: time.Now(), Text: "one"}
	repo.messages[2] = domainmsg.IngestedMessage{ID: 2, SubscriptionID: 1, Provider: domainmsg.ProviderTelegramChannel, SourceMessageID: "2", MessageTime: time.Now(), Text: "two"}
	usecase := NewUsecase(repo, &fakeMessagingService{}, fakeMessagingSecurity{}, &fakeMessagingTx{repo: repo})

	result, err := usecase.FeedbackMessages(ctx, MessageFeedbackBatchInput{
		IDs: []uint{1, 2, 2, 999, 0},
		MessageFeedbackInput: MessageFeedbackInput{
			Label:      domainmsg.FeedbackNoise,
			Comment:    "duplicated headline",
			HasComment: true,
		},
	})
	if err != nil {
		t.Fatalf("FeedbackMessages: %v", err)
	}
	if result.RequestedCount != 3 || result.UpdatedCount != 2 || len(result.MissingIDs) != 1 || result.MissingIDs[0] != 999 {
		t.Fatalf("unexpected batch result: %+v", result)
	}
	for _, id := range []uint{1, 2} {
		row := repo.messages[id]
		if row.FeedbackLabel == nil || *row.FeedbackLabel != domainmsg.FeedbackNoise {
			t.Fatalf("message %d feedback label = %+v", id, row.FeedbackLabel)
		}
		if row.FeedbackComment == nil || *row.FeedbackComment != "duplicated headline" || row.FeedbackAt == nil {
			t.Fatalf("message %d feedback metadata not persisted: %+v", id, row)
		}
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
	filterErr           error
	fetched             FetchedMessages
	fetchCalls          int
	testErrors          map[string]error
	testCalls           []string
	lastCredentials     SubscriptionCredentials
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
func (s *fakeMessagingService) TestSubscription(_ context.Context, credentials SubscriptionCredentials, _ string, sourceRef string) (map[string]any, error) {
	s.lastCredentials = credentials
	s.testCalls = append(s.testCalls, sourceRef)
	if err, ok := s.testErrors[sourceRef]; ok {
		return nil, err
	}
	return map[string]any{"status": "ok", "source_ref": sourceRef}, nil
}
func (s *fakeMessagingService) FetchSubscriptionMessages(_ context.Context, credentials SubscriptionCredentials, subscription domainmsg.MessageSubscription, _ int, afterSourceMessageID *string) (FetchedMessages, error) {
	s.fetchCalls++
	s.lastCredentials = credentials
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
	return s.filter, s.filterErr
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
func (r *fakeMessagingRepo) SaveAppSetting(_ context.Context, setting *domainsettings.AppSetting) error {
	r.settings[setting.Key] = *setting
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
func (r *fakeMessagingRepo) DeleteSubscriptionGraph(_ context.Context, id uint, _ []string) error {
	delete(r.subscriptions, id)
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
func (r *fakeMessagingRepo) ListFeedbackMessages(_ context.Context, filter RepositoryFeedbackMessageFilter) ([]domainmsg.IngestedMessage, error) {
	var subscriptionID uint64
	if strings.TrimSpace(filter.SubscriptionID) != "" {
		subscriptionID, _ = strconv.ParseUint(strings.TrimSpace(filter.SubscriptionID), 10, 64)
	}
	out := make([]domainmsg.IngestedMessage, 0)
	for _, row := range r.messages {
		if row.FeedbackLabel == nil || strings.TrimSpace(*row.FeedbackLabel) == "" {
			continue
		}
		if subscriptionID > 0 && row.SubscriptionID != uint(subscriptionID) {
			continue
		}
		if strings.TrimSpace(filter.Provider) != "" && row.Provider != strings.TrimSpace(filter.Provider) {
			continue
		}
		if strings.TrimSpace(filter.Label) != "" && strings.ToLower(strings.TrimSpace(*row.FeedbackLabel)) != strings.ToLower(strings.TrimSpace(filter.Label)) {
			continue
		}
		if filter.CursorID > 0 && uint64(row.ID) >= filter.CursorID {
			continue
		}
		if subscription, ok := r.subscriptions[row.SubscriptionID]; ok {
			row.Subscription = &subscription
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
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

func findSubscriptionDiagnosticCheck(t testing.TB, diagnostic SubscriptionDiagnostic, key string) SubscriptionDiagnosticCheck {
	t.Helper()
	for _, check := range diagnostic.Checks {
		if check.Key == key {
			return check
		}
	}
	t.Fatalf("diagnostic check %q not found in %+v", key, diagnostic.Checks)
	return SubscriptionDiagnosticCheck{}
}

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
