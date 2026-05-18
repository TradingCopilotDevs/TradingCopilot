package telegram

import (
	"bufio"
	"context"
	"encoding/binary"
	"fmt"
	domainai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/ai"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	domainmeeting "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/meeting"
	domainsettings "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/settings"
	domaintelegram "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/telegram"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/persistence/gorm/connect"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/security"
	infratelegram "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/telegram"
	"github.com/glebarez/sqlite"
	peerentities "github.com/gotd/td/telegram/message/peer"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/tg"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestNormalizeTelegramChannelRef(t *testing.T) {
	cases := map[string]string{
		"foo":              "@foo",
		"@foo":             "@foo",
		"https://t.me/foo": "@foo",
		"-100123":          "-100123",
	}
	for input, want := range cases {
		if got := NormalizeTelegramChannelRef(input); got != want {
			t.Fatalf("NormalizeTelegramChannelRef(%q)=%q want %q", input, got, want)
		}
	}
}

func TestParseTelegramProxyURL(t *testing.T) {
	proxy, err := parseTelegramProxyURL("http://proxy.example:8899")
	if err != nil {
		t.Fatal(err)
	}
	if proxy == nil || proxy.ProxyType != "http" || proxy.Addr != "proxy.example" || proxy.Port != 8899 || !proxy.RDNS {
		t.Fatalf("unexpected HTTP proxy config: %+v", proxy)
	}

	proxy, err = parseTelegramProxyURL("socks5h://user:pass@tg-proxy.example:1081")
	if err != nil {
		t.Fatal(err)
	}
	if proxy == nil || proxy.ProxyType != "socks5" || proxy.Addr != "tg-proxy.example" || proxy.Port != 1081 || proxy.Username != "user" || proxy.Password != "pass" || !proxy.RDNS {
		t.Fatalf("unexpected socks5 proxy config: %+v", proxy)
	}

	proxy, err = parseTelegramProxyURL("socks4a://agent@tg-proxy.example")
	if err != nil {
		t.Fatal(err)
	}
	if proxy == nil || proxy.ProxyType != "socks4" || proxy.Addr != "tg-proxy.example" || proxy.Port != 1080 || proxy.Username != "agent" || !proxy.RDNS {
		t.Fatalf("unexpected socks4 proxy config: %+v", proxy)
	}

	if _, err := parseTelegramProxyURL("ftp://proxy.example:21"); err == nil {
		t.Fatal("expected unsupported proxy scheme to fail")
	}
}

func TestTelegramProxyDialerSupportsSOCKS4A(t *testing.T) {
	type socks4Request struct {
		Version byte
		Command byte
		Port    uint16
		IP      string
		UserID  string
		Host    string
	}
	requests := make(chan socks4Request, 1)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		reader := bufio.NewReader(conn)
		header := make([]byte, 8)
		if _, err := io.ReadFull(reader, header); err != nil {
			return
		}
		userID, err := reader.ReadString(0)
		if err != nil {
			return
		}
		host, err := reader.ReadString(0)
		if err != nil {
			return
		}
		requests <- socks4Request{
			Version: header[0],
			Command: header[1],
			Port:    binary.BigEndian.Uint16(header[2:4]),
			IP:      net.IP(header[4:8]).String(),
			UserID:  strings.TrimSuffix(userID, "\x00"),
			Host:    strings.TrimSuffix(host, "\x00"),
		}
		_, _ = conn.Write([]byte{0x00, 0x5a, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	}()

	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	dial, ok := telegramProxyDialer(&TelegramProxyConfig{ProxyType: "socks4", Addr: host, Port: port, Username: "agent", RDNS: true})
	if !ok {
		t.Fatal("expected socks4 dialer")
	}
	conn, err := dial(context.Background(), "tcp", "telegram.example:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	req := <-requests
	if req.Version != 4 || req.Command != 1 || req.Port != 443 || req.IP != "0.0.0.1" || req.UserID != "agent" || req.Host != "telegram.example" {
		t.Fatalf("unexpected socks4a request: %+v", req)
	}
}

func TestApplyTelegramCompatibilityFilterExtractsAShareSymbols(t *testing.T) {
	db := newTelegramTestDB(t)
	channel := domaintelegram.Channel{Title: "news", ChannelRef: "@news", Enabled: true, CollectFrom: time.Now()}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	message := domaintelegram.Message{ChannelID: channel.ID, MessageID: 1, MessageTime: time.Now(), Text: "watch 600519 and AVGO; only A-share code should be extracted", RelatedSymbols: JSONList(nil)}
	if err := db.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	if err := ApplyTelegramCompatibilityFilter(db, &message); err != nil {
		t.Fatal(err)
	}
	if message.FilterDecision == nil || *message.FilterDecision != domainkernel.NewsObserve {
		t.Fatalf("decision mismatch: %v", message.FilterDecision)
	}
	if got := string(message.RelatedSymbols); got != `["600519"]` {
		t.Fatalf("related symbols mismatch: %s", got)
	}
}

func TestExtractTelegramFilterJSON(t *testing.T) {
	got, err := ExtractTelegramFilterJSON("prefix\n```json\n{\"decision\":\"observe\",\"reason\":\"r\",\"related_symbols\":[\"600519\"]}\n```\nsuffix")
	if err != nil {
		t.Fatal(err)
	}
	if got["decision"] != "observe" {
		t.Fatalf("unexpected decision %+v", got)
	}
}

func TestApplyTelegramFilterUsesNewsFilterRole(t *testing.T) {
	db := newTelegramTestDB(t)
	settings := config.Settings{
		AppSecretKey:      "test-secret",
		AIChatMaxAttempts: 1,
		AIJSONMaxAttempts: 2,
		AIChatTimeout:     5 * time.Second,
	}
	sec := security.New(settings)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected AI path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			t.Fatalf("missing bearer auth: %s", r.Header.Get("Authorization"))
		}
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"not-json"}}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"decision\":\"meeting\",\"reason\":\"material\",\"related_symbols\":[\"600519\",\"AVGO\",\"600519\"]}"}}]}`))
	}))
	defer srv.Close()

	encrypted, err := sec.EncryptSecret("test-key")
	if err != nil {
		t.Fatal(err)
	}
	secret := domainsettings.Secret{Kind: domainkernel.SecretKindAIProvider, Name: "provider:1:api_key", EncryptedValue: encrypted}
	if err := db.Create(&secret).Error; err != nil {
		t.Fatal(err)
	}
	provider := domainai.Provider{Name: "test", BaseURL: srv.URL, DefaultModel: "model", Enabled: true, APIKeySecretID: &secret.ID}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	role := domainai.AgentRole{Key: "news_filter", Name: "filter", Responsibility: "filter news", PromptTemplate: "return JSON", ProviderID: &provider.ID, Enabled: true}
	if err := db.Create(&role).Error; err != nil {
		t.Fatal(err)
	}
	channel := domaintelegram.Channel{Title: "news", ChannelRef: "@news", Enabled: true, CollectFrom: time.Now()}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	message := domaintelegram.Message{ChannelID: channel.ID, MessageID: 1, MessageTime: time.Now(), Text: "600519 material event", RelatedSymbols: JSONList(nil)}
	if err := db.Create(&message).Error; err != nil {
		t.Fatal(err)
	}

	if err := ApplyTelegramFilter(db, &message, sec, settings); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected JSON repair retry, got %d calls", calls)
	}
	if message.FilterDecision == nil || *message.FilterDecision != domainkernel.NewsMeeting {
		t.Fatalf("decision mismatch: %v", message.FilterDecision)
	}
	if message.FilterReason == nil || *message.FilterReason != "material" {
		t.Fatalf("reason mismatch: %v", message.FilterReason)
	}
	if got := string(message.RelatedSymbols); got != `["600519"]` {
		t.Fatalf("related symbols mismatch: %s", got)
	}
}

func TestFetchPublicTelegramMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/s/news" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`
<html><head><meta property="og:title" content="News Channel - Telegram"></head><body>
<div class="tgme_widget_message text_not_supported_wrap js-widget_message" data-post="news/10">
  <div class="tgme_widget_message_text js-message_text">older<br/>600519 event</div>
  <time datetime="2026-05-08T10:00:00+08:00"></time>
  <div class="tgme_widget_message_footer"></div>
</div>
<div class="tgme_widget_message text_not_supported_wrap js-widget_message" data-post="news/11">
  <div class="tgme_widget_message_text js-message_text">newer &amp; important</div>
  <time datetime="2026-05-08T11:00:00+08:00"></time>
  <div class="tgme_widget_message_footer"></div>
</div>
</body></html>`))
	}))
	defer srv.Close()
	oldBase := telegramPublicBaseURL
	telegramPublicBaseURL = srv.URL + "/s"
	defer func() { telegramPublicBaseURL = oldBase }()

	minID := int64(10)
	result, err := FetchPublicTelegramMessages("@news", 20, &minID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "News Channel" {
		t.Fatalf("title mismatch: %s", result.Title)
	}
	if len(result.Messages) != 1 || result.Messages[0].MessageID != 11 {
		t.Fatalf("unexpected messages %+v", result.Messages)
	}
	if result.Messages[0].Text != "newer & important" {
		t.Fatalf("text mismatch: %q", result.Messages[0].Text)
	}
}

func TestCollectPublicTelegramChannelIngestsMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`
<div class="tgme_widget_message text_not_supported_wrap js-widget_message" data-post="news/20">
  <div class="tgme_widget_message_text js-message_text">600519 event</div>
  <time datetime="2026-05-08T10:00:00+08:00"></time>
  <div class="tgme_widget_message_footer"></div>
</div>
<div class="tgme_widget_message text_not_supported_wrap js-widget_message" data-post="news/21">
  <div class="tgme_widget_message_text js-message_text">plain update</div>
  <time datetime="2026-05-08T11:00:00+08:00"></time>
  <div class="tgme_widget_message_footer"></div>
</div>`))
	}))
	defer srv.Close()
	oldBase := telegramPublicBaseURL
	telegramPublicBaseURL = srv.URL + "/s"
	defer func() { telegramPublicBaseURL = oldBase }()

	db := newTelegramTestDB(t)
	channel := domaintelegram.Channel{Title: "news", ChannelRef: "@news", Enabled: true, CollectFrom: time.Date(2026, 5, 8, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	settings := config.Settings{AppSecretKey: "test-secret", AIChatMaxAttempts: 1, AIJSONMaxAttempts: 1}
	collected, filtered, err := CollectPublicTelegramChannel(db, &channel, 20, security.New(settings), settings)
	if err != nil {
		t.Fatal(err)
	}
	if collected != 2 || filtered != 2 {
		t.Fatalf("unexpected collect counts collected=%d filtered=%d", collected, filtered)
	}
	var rows []domaintelegram.Message
	db.Order("message_id").Find(&rows)
	if len(rows) != 2 || rows[0].MessageID != 20 || rows[1].MessageID != 21 {
		t.Fatalf("unexpected stored messages %+v", rows)
	}
	if rows[0].FilterReason == nil || !strings.Contains(*rows[0].FilterReason, "news_filter role is not enabled") {
		t.Fatalf("expected filter failure reason, got %+v", rows[0].FilterReason)
	}
}

func TestReconcilePublicTelegramChannelBackfillAddsAndRemovesManagedMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`
<div class="tgme_widget_message text_not_supported_wrap js-widget_message" data-post="news/1">
  <div class="tgme_widget_message_text js-message_text">600001 old</div>
  <time datetime="2026-05-08T09:00:00+08:00"></time>
  <div class="tgme_widget_message_footer"></div>
</div>
<div class="tgme_widget_message text_not_supported_wrap js-widget_message" data-post="news/2">
  <div class="tgme_widget_message_text js-message_text">600002 middle</div>
  <time datetime="2026-05-08T10:00:00+08:00"></time>
  <div class="tgme_widget_message_footer"></div>
</div>
<div class="tgme_widget_message text_not_supported_wrap js-widget_message" data-post="news/3">
  <div class="tgme_widget_message_text js-message_text">600003 latest</div>
  <time datetime="2026-05-08T11:00:00+08:00"></time>
  <div class="tgme_widget_message_footer"></div>
</div>`))
	}))
	defer srv.Close()
	oldBase := telegramPublicBaseURL
	telegramPublicBaseURL = srv.URL + "/s"
	defer func() { telegramPublicBaseURL = oldBase }()

	db := newTelegramTestDB(t)
	channel := domaintelegram.Channel{Title: "news", ChannelRef: "@news", Enabled: true, BackfillLimit: 3, CollectFrom: time.Date(2026, 5, 8, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	settings := config.Settings{AppSecretKey: "test-secret", AIChatMaxAttempts: 1, AIJSONMaxAttempts: 1}
	result, err := ReconcilePublicTelegramChannelBackfill(db, &channel, 0, "", security.New(settings), settings)
	if err != nil {
		t.Fatal(err)
	}
	if result["added"] != 3 || result["removed"] != 0 {
		t.Fatalf("unexpected initial reconcile result %+v", result)
	}
	var first domaintelegram.Message
	if err := db.Where("channel_id = ? AND message_id = ?", channel.ID, 1).First(&first).Error; err != nil {
		t.Fatal(err)
	}
	ref := domainmeeting.Reference{SourceMeetingID: 1, ReferenceType: "telegram_message", TargetTopicSnapshot: "old", ExternalRef: strPtrForTelegramTest(TelegramMessageExternalRef(first))}
	if err := db.Create(&ref).Error; err != nil {
		t.Fatal(err)
	}
	manual := domaintelegram.Message{ChannelID: channel.ID, MessageID: 99, MessageTime: time.Date(2026, 5, 8, 8, 0, 0, 0, time.FixedZone("CST", 8*3600)), Text: "manual", Raw: JSON(map[string]any{"manual": true})}
	if err := db.Create(&manual).Error; err != nil {
		t.Fatal(err)
	}

	channel.BackfillLimit = 1
	if err := db.Save(&channel).Error; err != nil {
		t.Fatal(err)
	}
	result, err = ReconcilePublicTelegramChannelBackfill(db, &channel, 3, "@news", security.New(settings), settings)
	if err != nil {
		t.Fatal(err)
	}
	if result["added"] != 0 || result["removed"] != 2 {
		t.Fatalf("unexpected shrink reconcile result %+v", result)
	}
	var remaining []domaintelegram.Message
	db.Where("channel_id = ?", channel.ID).Order("message_id").Find(&remaining)
	gotIDs := make([]int64, 0, len(remaining))
	for _, message := range remaining {
		gotIDs = append(gotIDs, message.MessageID)
	}
	if strings.Join(int64sToStrings(gotIDs), ",") != "3,99" {
		t.Fatalf("unexpected remaining message ids %+v", gotIDs)
	}
	var updatedRef domainmeeting.Reference
	if err := db.First(&updatedRef, ref.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !updatedRef.TargetDeleted {
		t.Fatal("expected removed backfill reference to be marked target_deleted")
	}
}

func TestEnsureMeetingForTelegramMessageCreatesReferenceOnce(t *testing.T) {
	db := newTelegramTestDB(t)
	channel := domaintelegram.Channel{Title: "news", ChannelRef: "@news", Enabled: true, CollectFrom: time.Now()}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	decision := domainkernel.NewsMeeting
	reason := "material event"
	message := domaintelegram.Message{ChannelID: channel.ID, MessageID: 2, MessageTime: time.Now(), Text: "600519 重大事项", FilterDecision: &decision, FilterReason: &reason, RelatedSymbols: JSON([]string{"600519"})}
	if err := db.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	meeting, created, err := EnsureMeetingForTelegramMessage(db, &message, "telegram")
	if err != nil {
		t.Fatal(err)
	}
	if !created || meeting == nil {
		t.Fatalf("expected meeting created, meeting=%v created=%v", meeting, created)
	}
	again, createdAgain, err := EnsureMeetingForTelegramMessage(db, &message, "telegram")
	if err != nil {
		t.Fatal(err)
	}
	if createdAgain || again == nil || again.ID != meeting.ID {
		t.Fatalf("expected idempotent existing meeting, got meeting=%v created=%v", again, createdAgain)
	}
	var refs int64
	db.Model(&domainmeeting.Reference{}).Where("external_ref = ?", TelegramMessageExternalRef(message)).Count(&refs)
	if refs != 1 {
		t.Fatalf("expected one reference, got %d", refs)
	}
}

func TestTelegramMTProtoLoginStoresSessionAndStatus(t *testing.T) {
	db := newTelegramTestDB(t)
	settings := config.Settings{AppSecretKey: "mtproto-test-secret"}
	sec := security.New(settings)
	old := telegramMTProto
	defer func() { telegramMTProto = old }()
	telegramMTProto = fakeTelegramMTProtoClient{}
	hash, err := StartTelegramMTProtoLogin(db, sec, "+15550001111")
	if err != nil {
		t.Fatal(err)
	}
	if hash != "hash-1" {
		t.Fatalf("hash mismatch: %s", hash)
	}
	if status := TelegramMTProtoStatus(db); status["has_session"] {
		t.Fatalf("session should not be complete yet: %+v", status)
	}
	if err := CompleteTelegramMTProtoLogin(db, sec, "+15550001111", "12345", hash, ""); err != nil {
		t.Fatal(err)
	}
	status := TelegramMTProtoStatus(db)
	if !status["has_app_id"] || !status["has_app_hash"] || !status["has_session"] {
		t.Fatalf("status mismatch: %+v", status)
	}
	if _, err := telegramSecretValue(db, sec, telegramSecretLoginSession); err == nil {
		t.Fatal("expected temporary login session to be deleted")
	}
}

func TestCollectTelegramChannelUsesMTProtoForPrivateChannel(t *testing.T) {
	db := newTelegramTestDB(t)
	settings := config.Settings{AppSecretKey: "mtproto-test-secret", AIChatMaxAttempts: 1, AIJSONMaxAttempts: 1}
	sec := security.New(settings)
	old := telegramMTProto
	defer func() { telegramMTProto = old }()
	telegramMTProto = fakeTelegramMTProtoClient{}
	channel := domaintelegram.Channel{Title: "private", ChannelRef: "-1001234567890", Enabled: true, CollectFrom: time.Date(2026, 5, 8, 0, 0, 0, 0, appTZ)}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	collected, filtered, err := CollectTelegramChannel(db, &channel, 20, sec, settings)
	if err != nil {
		t.Fatal(err)
	}
	if collected != 2 || filtered != 2 {
		t.Fatalf("unexpected collect counts collected=%d filtered=%d", collected, filtered)
	}
	var rows []domaintelegram.Message
	if err := db.Where("channel_id = ?", channel.ID).Order("message_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].MessageID != 101 || rows[1].MessageID != 102 {
		t.Fatalf("unexpected mtproto rows: %+v", rows)
	}
	if !strings.Contains(string(rows[0].Raw), `"ingest_source":"mtproto"`) {
		t.Fatalf("raw missing mtproto source: %s", rows[0].Raw)
	}
}

func TestReconcileTelegramChannelBackfillUsesMTProtoForPrivateChannel(t *testing.T) {
	db := newTelegramTestDB(t)
	settings := config.Settings{AppSecretKey: "mtproto-test-secret", AIChatMaxAttempts: 1, AIJSONMaxAttempts: 1}
	sec := security.New(settings)
	old := telegramMTProto
	defer func() { telegramMTProto = old }()
	telegramMTProto = fakeTelegramMTProtoClient{}
	channel := domaintelegram.Channel{Title: "private", ChannelRef: "-1001234567890", Enabled: true, BackfillLimit: 2, CollectFrom: time.Date(2026, 5, 8, 0, 0, 0, 0, appTZ)}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	result, err := ReconcileTelegramChannelBackfill(db, &channel, 0, "", sec, settings)
	if err != nil {
		t.Fatal(err)
	}
	if result["added"] != 2 || result["removed"] != 0 {
		t.Fatalf("unexpected initial private backfill result %+v", result)
	}
	var rows []domaintelegram.Message
	if err := db.Where("channel_id = ?", channel.ID).Order("message_id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || !telegramRawBool(rows[0].Raw, "backfill_managed") {
		t.Fatalf("unexpected private backfill rows: %+v", rows)
	}

	channel.BackfillLimit = 0
	if err := db.Save(&channel).Error; err != nil {
		t.Fatal(err)
	}
	result, err = ReconcileTelegramChannelBackfill(db, &channel, 2, "-1001234567890", sec, settings)
	if err != nil {
		t.Fatal(err)
	}
	if result["removed"] != 2 {
		t.Fatalf("expected private backfill rows removed, got %+v", result)
	}
	var remaining int64
	db.Model(&domaintelegram.Message{}).Where("channel_id = ?", channel.ID).Count(&remaining)
	if remaining != 0 {
		t.Fatalf("expected no remaining private backfill messages, got %d", remaining)
	}
}

func TestGetLatestTelegramMessageUsesMTProtoForPrivateChannel(t *testing.T) {
	db := newTelegramTestDB(t)
	sec := security.New(config.Settings{AppSecretKey: "mtproto-test-secret"})
	old := telegramMTProto
	defer func() { telegramMTProto = old }()
	telegramMTProto = fakeTelegramMTProtoClient{}
	result, err := GetLatestTelegramMessage(db, sec, "-1001234567890")
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "ok" || result["message_id"] != int64(102) || result["title"] != "Private Channel" {
		t.Fatalf("unexpected latest result: %+v", result)
	}
}

func TestHandleTelegramLiveMessageCreatesMeetingAndDispatches(t *testing.T) {
	db := newTelegramTestDB(t)
	settings := config.Settings{
		AppSecretKey:      "live-test-secret",
		AIChatMaxAttempts: 1,
		AIJSONMaxAttempts: 1,
		AIChatTimeout:     5 * time.Second,
	}
	sec := security.New(settings)
	seedTelegramMeetingFilterRole(t, db, sec)
	channel := domaintelegram.Channel{Title: "live", ChannelRef: "-1001234567890", Enabled: true, CollectFrom: time.Date(2026, 5, 8, 0, 0, 0, 0, appTZ)}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	var dispatched []uint
	created, err := HandleTelegramLiveMessage(db, settings, sec, TelegramLiveMessage{
		ChannelID:   channel.ID,
		MessageID:   201,
		MessageTime: time.Date(2026, 5, 8, 12, 0, 0, 0, appTZ),
		Text:        "600519 live material event",
		Raw:         JSON(map[string]any{"id": 201, "source": "mtproto_live"}),
	}, func(meeting *domainmeeting.Meeting) error {
		dispatched = append(dispatched, meeting.ID)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected live message to be created")
	}
	var message domaintelegram.Message
	if err := db.Where("channel_id = ? AND message_id = ?", channel.ID, 201).First(&message).Error; err != nil {
		t.Fatal(err)
	}
	if message.FilterDecision == nil || *message.FilterDecision != domainkernel.NewsMeeting {
		t.Fatalf("expected meeting decision, got %+v", message.FilterDecision)
	}
	var meeting domainmeeting.Meeting
	if err := db.Where("trigger_source = ?", "telegram").First(&meeting).Error; err != nil {
		t.Fatal(err)
	}
	if len(dispatched) != 1 || dispatched[0] != meeting.ID {
		t.Fatalf("expected meeting dispatch, got %+v meeting=%d", dispatched, meeting.ID)
	}
	var refs int64
	db.Model(&domainmeeting.Reference{}).Where("reference_type = ? AND source_meeting_id = ?", "telegram_message", meeting.ID).Count(&refs)
	if refs != 1 {
		t.Fatalf("expected one telegram reference, got %d", refs)
	}
}

func TestRunTelegramChannelListenerOnceUsesEnabledChannels(t *testing.T) {
	db := newTelegramTestDB(t)
	settings := config.Settings{AppSecretKey: "live-test-secret", AIChatMaxAttempts: 1, AIJSONMaxAttempts: 1}
	sec := security.New(settings)
	if err := upsertTelegramSecret(db, sec, telegramSecretAppID, "12345"); err != nil {
		t.Fatal(err)
	}
	if err := upsertTelegramSecret(db, sec, telegramSecretAppHash, "hash"); err != nil {
		t.Fatal(err)
	}
	if err := upsertTelegramSecret(db, sec, telegramSecretMTSession, "session"); err != nil {
		t.Fatal(err)
	}
	channel := domaintelegram.Channel{Title: "live", ChannelRef: "-1001234567890", Enabled: true, CollectFrom: time.Date(2026, 5, 8, 0, 0, 0, 0, appTZ)}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	old := telegramLiveMTProto
	defer func() { telegramLiveMTProto = old }()
	telegramLiveMTProto = fakeTelegramLiveMTProtoClient{}
	if err := RunTelegramChannelListenerOnceForTest(context.Background(), db, settings, sec, nil); err != nil {
		t.Fatal(err)
	}
	var count int64
	db.Model(&domaintelegram.Message{}).Where("channel_id = ? AND message_id = ?", channel.ID, 301).Count(&count)
	if count != 1 {
		t.Fatalf("expected fake live message stored, got %d", count)
	}
}

func TestTelegramChannelFromDialogElemResolvesNumericAccessHash(t *testing.T) {
	elem := dialogs.Elem{
		Dialog: &tg.Dialog{Peer: &tg.PeerChannel{ChannelID: 1234567890}},
		Peer:   &tg.InputPeerChannel{ChannelID: 1234567890, AccessHash: 987654321},
		Entities: peerentities.NewEntities(nil, nil, map[int64]*tg.Channel{
			1234567890: {ID: 1234567890, AccessHash: 987654321, Title: "Private Alpha"},
		}),
	}
	peer, ok := infratelegram.ChannelFromDialogElem(1234567890, elem)
	if !ok {
		t.Fatal("expected numeric channel to resolve from dialog entity")
	}
	input, ok := peer.InputPeer().(*tg.InputPeerChannel)
	if !ok || input.ChannelID != 1234567890 || input.AccessHash != 987654321 {
		t.Fatalf("unexpected input peer: %#v", peer.InputPeer())
	}
	if peer.VisibleName() != "Private Alpha" || int64(peer.TDLibPeerID()) != -1001234567890 {
		t.Fatalf("unexpected peer metadata name=%s tdid=%d", peer.VisibleName(), peer.TDLibPeerID())
	}
}

func TestTelegramChannelFromDialogElemRejectsMissingAccessHash(t *testing.T) {
	elem := dialogs.Elem{
		Dialog: &tg.Dialog{Peer: &tg.PeerChannel{ChannelID: 1234567890}},
		Peer:   &tg.InputPeerChannel{ChannelID: 1234567890},
	}
	if _, ok := infratelegram.ChannelFromDialogElem(1234567890, elem); ok {
		t.Fatal("expected missing access_hash to be rejected")
	}
}

func TestReconcileTelegramMeetingTriggersIsIdempotent(t *testing.T) {
	db := newTelegramTestDB(t)
	channel := domaintelegram.Channel{Title: "news", ChannelRef: "@news", Enabled: true, CollectFrom: time.Now()}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	decision := domainkernel.NewsMeeting
	reason := "material"
	message := domaintelegram.Message{ChannelID: channel.ID, MessageID: 77, MessageTime: time.Now(), Text: "600519 重大事项", FilterDecision: &decision, FilterReason: &reason, RelatedSymbols: JSON([]string{"600519"})}
	if err := db.Create(&message).Error; err != nil {
		t.Fatal(err)
	}
	var dispatched int
	created, err := ReconcileTelegramMeetingTriggers(db, 200, func(meeting *domainmeeting.Meeting) error {
		dispatched++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 || dispatched != 1 {
		t.Fatalf("expected one created/dispatched, got created=%d dispatched=%d", created, dispatched)
	}
	created, err = ReconcileTelegramMeetingTriggers(db, 200, func(meeting *domainmeeting.Meeting) error {
		dispatched++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || dispatched != 1 {
		t.Fatalf("expected idempotent reconcile, got created=%d dispatched=%d", created, dispatched)
	}
}

type fakeTelegramMTProtoClient struct{}

func (fakeTelegramMTProtoClient) StartLogin(ctx context.Context, db *gorm.DB, sec security.Service, phone string) (string, error) {
	if err := upsertTelegramSecret(db, sec, telegramSecretAppID, "12345"); err != nil {
		return "", err
	}
	if err := upsertTelegramSecret(db, sec, telegramSecretAppHash, "hash"); err != nil {
		return "", err
	}
	if err := upsertTelegramSecret(db, sec, telegramSecretLoginSession, "login-session"); err != nil {
		return "", err
	}
	if err := upsertTelegramSecret(db, sec, telegramSecretLoginPhone, phone); err != nil {
		return "", err
	}
	return "hash-1", nil
}

func (fakeTelegramMTProtoClient) CompleteLogin(ctx context.Context, db *gorm.DB, sec security.Service, phone string, code string, phoneCodeHash string, password string) error {
	if err := upsertTelegramSecret(db, sec, telegramSecretMTSession, "complete-session"); err != nil {
		return err
	}
	return db.Where("kind = ? AND name IN ?", domainkernel.SecretKindTelegram, []string{telegramSecretLoginSession, telegramSecretLoginPhone}).Delete(&domainsettings.Secret{}).Error
}

func (fakeTelegramMTProtoClient) LatestMessage(ctx context.Context, db *gorm.DB, sec security.Service, channelRef string) (map[string]any, error) {
	return map[string]any{"status": "ok", "channel_ref": channelRef, "title": "Private Channel", "message_id": int64(102), "message_time": time.Date(2026, 5, 8, 11, 0, 0, 0, appTZ), "text": "new private"}, nil
}

func (fakeTelegramMTProtoClient) RecentMessages(ctx context.Context, db *gorm.DB, sec security.Service, channelRef string, limit int, minMessageID *int64) (TelegramPublicResult, error) {
	messages := []TelegramPublicMessage{
		{MessageID: 102, MessageTime: time.Date(2026, 5, 8, 11, 0, 0, 0, appTZ), Text: "new private 600519", Raw: JSON(map[string]any{"id": 102, "source": "mtproto"})},
		{MessageID: 101, MessageTime: time.Date(2026, 5, 8, 10, 0, 0, 0, appTZ), Text: "old private", Raw: JSON(map[string]any{"id": 101, "source": "mtproto"})},
	}
	if minMessageID != nil {
		filtered := make([]TelegramPublicMessage, 0, len(messages))
		for _, message := range messages {
			if message.MessageID > *minMessageID {
				filtered = append(filtered, message)
			}
		}
		messages = filtered
	}
	return TelegramPublicResult{ChannelRef: channelRef, Title: "Private Channel", Messages: messages}, nil
}

type fakeTelegramLiveMTProtoClient struct{}

func (fakeTelegramLiveMTProtoClient) Listen(ctx context.Context, db *gorm.DB, sec security.Service, channels []domaintelegram.Channel, handle func(context.Context, TelegramLiveMessage) error) error {
	if len(channels) != 1 || channels[0].ChannelRef != "-1001234567890" {
		return fmt.Errorf("unexpected channels: %+v", channels)
	}
	return handle(ctx, TelegramLiveMessage{
		ChannelID:   channels[0].ID,
		MessageID:   301,
		MessageTime: time.Date(2026, 5, 8, 13, 0, 0, 0, appTZ),
		Text:        "live observe 600519",
		Raw:         JSON(map[string]any{"id": 301, "source": "fake_live"}),
	})
}

func seedTelegramMeetingFilterRole(t *testing.T, db *gorm.DB, sec security.Service) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"decision\":\"meeting\",\"reason\":\"live material\",\"related_symbols\":[\"600519\"]}"}}]}`))
	}))
	t.Cleanup(srv.Close)
	encrypted, err := sec.EncryptSecret("test-key")
	if err != nil {
		t.Fatal(err)
	}
	secret := domainsettings.Secret{Kind: domainkernel.SecretKindAIProvider, Name: "provider:live:api_key", EncryptedValue: encrypted}
	if err := db.Create(&secret).Error; err != nil {
		t.Fatal(err)
	}
	provider := domainai.Provider{Name: "live", BaseURL: srv.URL, DefaultModel: "model", Enabled: true, APIKeySecretID: &secret.ID}
	if err := db.Create(&provider).Error; err != nil {
		t.Fatal(err)
	}
	role := domainai.AgentRole{Key: "news_filter", Name: "filter", Responsibility: "filter news", PromptTemplate: "return JSON", ProviderID: &provider.ID, Enabled: true}
	if err := db.Create(&role).Error; err != nil {
		t.Fatal(err)
	}
}

func newTelegramTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func strPtrForTelegramTest(value string) *string { return &value }

func int64sToStrings(values []int64) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strconv.FormatInt(value, 10))
	}
	return out
}
