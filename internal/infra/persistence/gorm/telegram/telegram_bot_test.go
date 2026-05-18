package telegram

import (
	"context"
	"encoding/json"
	"errors"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/meeting"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appsystem "github.com/TradingCopilotDevs/TradingCopilot/internal/app/system"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	gormrepo "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/repo"
	gormuow "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/persistence/gorm/uow"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	"gorm.io/gorm"
)

func TestNormalizeTelegramBotCommand(t *testing.T) {
	command, argument := NormalizeTelegramBotCommand("/meeting@AIWarrenBuffettBot  600519 research")
	if command != "/meeting" || argument != "600519 research" {
		t.Fatalf("unexpected command=%q argument=%q", command, argument)
	}
	command, argument = NormalizeTelegramBotCommand("plain text")
	if command != "" || argument != "plain text" {
		t.Fatalf("unexpected non-command parse command=%q argument=%q", command, argument)
	}
}

func TestProcessTelegramBotUpdateCreatesMeeting(t *testing.T) {
	db := newTelegramTestDB(t)
	sec := seedTelegramBotSecrets(t, db, "token", "100")
	var sent []string
	dispatched := 0
	processed, err := ProcessTelegramBotUpdate(
		db,
		sec,
		TelegramBotUpdate{
			"update_id": float64(42),
			"message": map[string]any{
				"text": "/meeting 600519 material event",
				"chat": map[string]any{"id": float64(100)},
			},
		},
		func(chatID string, text string) error {
			sent = append(sent, chatID+":"+text)
			return nil
		},
		func(meeting *domainmeeting.Meeting) error {
			dispatched++
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !processed || dispatched != 1 {
		t.Fatalf("expected processed meeting dispatch, processed=%v dispatched=%d", processed, dispatched)
	}
	var meeting domainmeeting.Meeting
	if err := db.Where("trigger_source = ?", "telegram_bot").First(&meeting).Error; err != nil {
		t.Fatal(err)
	}
	if meeting.Topic != "600519 material event" {
		t.Fatalf("topic mismatch: %s", meeting.Topic)
	}
	var event domainmeeting.Event
	if err := db.Where("meeting_id = ? AND content LIKE ?", meeting.ID, "Telegram Bot triggered%").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || !strings.Contains(sent[0], "Meeting created") {
		t.Fatalf("unexpected bot reply %+v", sent)
	}
}

func TestProcessTelegramBotUpdateRejectsDifferentConfiguredChat(t *testing.T) {
	db := newTelegramTestDB(t)
	sec := seedTelegramBotSecrets(t, db, "token", "100")
	sent := 0
	processed, err := ProcessTelegramBotUpdate(
		db,
		sec,
		TelegramBotUpdate{"message": map[string]any{"text": "/help", "chat": map[string]any{"id": float64(200)}}},
		func(string, string) error {
			sent++
			return nil
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if processed || sent != 0 {
		t.Fatalf("expected ignored update, processed=%v sent=%d", processed, sent)
	}
}

func TestPollTelegramBotOnceStoresOffsetAndReplies(t *testing.T) {
	db := newTelegramTestDB(t)
	sec := seedTelegramBotSecrets(t, db, "token", "100")
	var updatePayload map[string]any
	var replies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/bottoken/getUpdates":
			if err := json.NewDecoder(r.Body).Decode(&updatePayload); err != nil {
				t.Fatal(err)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":[{"update_id":7,"message":{"text":"/help","chat":{"id":100}}}]}`))
		case "/bottoken/sendMessage":
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatal(err)
			}
			replies = append(replies, payload)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":9}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	oldBase := TelegramBotAPIBase
	TelegramBotAPIBase = srv.URL
	defer func() { TelegramBotAPIBase = oldBase }()

	if err := PollTelegramBotOnceForTest(db, sec, nil); err != nil {
		t.Fatal(err)
	}
	if len(replies) != 1 || !strings.Contains(replies[0]["text"].(string), "/meeting <topic>") {
		t.Fatalf("unexpected replies %+v", replies)
	}
	if got := LoadTelegramBotOffset(db); got != 8 {
		t.Fatalf("offset mismatch: %d", got)
	}
	if _, ok := updatePayload["offset"]; ok {
		t.Fatalf("first poll should omit zero offset: %+v", updatePayload)
	}
}

func TestAcquireTelegramBotListenerLeaseExcludesOtherOwners(t *testing.T) {
	db := newTelegramTestDB(t)

	acquired, err := AcquireTelegramBotListenerLease(db, "owner-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("expected owner-a to acquire empty lease")
	}

	acquired, err = AcquireTelegramBotListenerLease(db, "owner-b", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if acquired {
		t.Fatal("expected owner-b to be blocked while owner-a lease is fresh")
	}

	acquired, err = AcquireTelegramBotListenerLease(db, "owner-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("expected current owner to renew lease")
	}

	time.Sleep(20 * time.Millisecond)
	acquired, err = AcquireTelegramBotListenerLease(db, "owner-b", 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("expected owner-b to acquire stale lease")
	}
}

func TestAcquireTelegramBotListenerLeaseRenewsByPayloadOwner(t *testing.T) {
	db := newTelegramTestDB(t)
	description := "legacy lease description"
	if err := db.Create(&domainsettings.AppSetting{
		Key:         TelegramBotListenerLeaseKey,
		Value:       JSON(map[string]any{"owner": "owner-a", "expires_at": time.Now().Add(time.Minute).Format(time.RFC3339Nano)}),
		Description: &description,
		UpdatedAt:   time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	acquired, err := AcquireTelegramBotListenerLease(db, "owner-a", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("expected owner-a to renew lease using payload owner")
	}
	var row domainsettings.AppSetting
	if err := db.First(&row, "key = ?", TelegramBotListenerLeaseKey).Error; err != nil {
		t.Fatal(err)
	}
	if row.Description == nil || !strings.Contains(*row.Description, "owner-a") {
		t.Fatalf("expected renewed owner description, got %+v", row.Description)
	}
}

func TestAcquireTelegramBotListenerLeaseUsesPayloadExpiration(t *testing.T) {
	db := newTelegramTestDB(t)
	description := "telegram bot listener lease: owner-a"
	if err := db.Create(&domainsettings.AppSetting{
		Key:         TelegramBotListenerLeaseKey,
		Value:       JSON(map[string]any{"owner": "owner-a", "expires_at": time.Now().Add(-time.Second).Format(time.RFC3339Nano)}),
		Description: &description,
		UpdatedAt:   time.Now(),
	}).Error; err != nil {
		t.Fatal(err)
	}

	acquired, err := AcquireTelegramBotListenerLease(db, "owner-b", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("expected owner-b to acquire expired payload lease")
	}
}

func TestRunTelegramBotListenerHeartbeatsWhenLeaseHeld(t *testing.T) {
	db := newTelegramTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	sec := seedTelegramBotSecrets(t, db, "token", "100")
	acquired, err := AcquireTelegramBotListenerLease(db, "other-listener", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("expected setup lease acquisition")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunTelegramBotListener(ctx, db, config.Settings{AppSecretKey: testTelegramBotSettings().AppSecretKey, MeetingDispatchMode: "local"}, sec, nil)
	}()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("listener did not stop after context cancellation")
		}
	}()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		heartbeat, err := appsystem.NewUsecase(gormrepo.NewSystemRepository(db), gormuow.NewSystemUnitOfWork(db)).LoadHeartbeat(context.Background(), "telegram_bot_listener")
		if err == nil && heartbeat != nil {
			if heartbeat.Status != "running" || !strings.Contains(heartbeat.Error, "lease") {
				t.Fatalf("unexpected heartbeat: %+v", heartbeat)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("expected heartbeat while bot listener lease is held")
}

func TestRunTelegramBotListenerHeartbeatsBeforeLongPollReturns(t *testing.T) {
	db := newTelegramTestDB(t)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	sec := seedTelegramBotSecrets(t, db, "token", "100")
	enteredGetUpdates := make(chan struct{})
	releaseGetUpdates := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottoken/getUpdates" {
			t.Fatalf("unexpected Telegram API path %s", r.URL.Path)
		}
		close(enteredGetUpdates)
		<-releaseGetUpdates
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": []any{}})
	}))
	defer srv.Close()
	oldBase := TelegramBotAPIBase
	TelegramBotAPIBase = srv.URL
	defer func() { TelegramBotAPIBase = oldBase }()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		RunTelegramBotListener(ctx, db, config.Settings{AppSecretKey: testTelegramBotSettings().AppSecretKey, MeetingDispatchMode: "local"}, sec, nil)
	}()
	defer func() {
		close(releaseGetUpdates)
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("listener did not stop after context cancellation")
		}
	}()

	select {
	case <-enteredGetUpdates:
	case <-time.After(time.Second):
		t.Fatal("listener did not enter Telegram long poll")
	}
	heartbeat, err := appsystem.NewUsecase(gormrepo.NewSystemRepository(db), gormuow.NewSystemUnitOfWork(db)).LoadHeartbeat(context.Background(), "telegram_bot_listener")
	if err != nil {
		t.Fatal(err)
	}
	if heartbeat.Status != "running" || heartbeat.Error != "" {
		t.Fatalf("expected clean startup heartbeat before long poll returns, got %+v", heartbeat)
	}
}

func TestPollTelegramBotOnceSkipsGetUpdatesWhenLeaseHeld(t *testing.T) {
	db := newTelegramTestDB(t)
	sec := seedTelegramBotSecrets(t, db, "token", "100")
	acquired, err := AcquireTelegramBotListenerLease(db, "other-listener", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("expected setup lease acquisition")
	}

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		t.Fatalf("unexpected Telegram API request while lease is held: %s", r.URL.Path)
	}))
	defer srv.Close()
	oldBase := TelegramBotAPIBase
	TelegramBotAPIBase = srv.URL
	defer func() { TelegramBotAPIBase = oldBase }()

	err = PollTelegramBotOnceForTest(db, sec, nil)
	if !errors.Is(err, ErrTelegramBotLeaseHeld) {
		t.Fatalf("expected lease held error, got %v", err)
	}
	if requests != 0 {
		t.Fatalf("expected no Telegram API requests, got %d", requests)
	}
}

func seedTelegramBotSecrets(t *testing.T, db *gorm.DB, token string, chatID string) security.Service {
	t.Helper()
	sec := security.New(testTelegramBotSettings())
	encryptedToken, err := sec.EncryptSecret(token)
	if err != nil {
		t.Fatal(err)
	}
	encryptedChatID, err := sec.EncryptSecret(chatID)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domainsettings.Secret{Kind: domainkernel.SecretKindTelegram, Name: "bot_token", EncryptedValue: encryptedToken}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&domainsettings.Secret{Kind: domainkernel.SecretKindTelegram, Name: "bot_chat_id", EncryptedValue: encryptedChatID}).Error; err != nil {
		t.Fatal(err)
	}
	return sec
}

func testTelegramBotSettings() config.Settings {
	return config.Settings{AppSecretKey: "telegram-bot-test-secret"}
}
