package ai

import (
	"encoding/json"
	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainsettings "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/settings"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
)

func TestExtractChatContent(t *testing.T) {
	text, err := ExtractChatContent(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "hello"}}}})
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello" {
		t.Fatalf("expected hello, got %s", text)
	}
}

func TestDecodeSSE(t *testing.T) {
	text, err := DecodeSSE("data: {\"choices\":[{\"delta\":{\"content\":\"hello \"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"world\"}}]}\n\ndata: [DONE]")
	if err != nil {
		t.Fatal(err)
	}
	if text != "hello world" {
		t.Fatalf("unexpected SSE text: %s", text)
	}
}

func TestExtractUsage(t *testing.T) {
	usage := ExtractUsage(map[string]any{"usage": map[string]any{"prompt_tokens": float64(11), "completion_tokens": float64(7), "total_tokens": float64(18)}})
	if usage == nil || usage.PromptTokens != 11 || usage.CompletionTokens != 7 || usage.TotalTokens != 18 {
		t.Fatalf("usage mismatch: %+v", usage)
	}
}

func TestDecodeSSEWithUsage(t *testing.T) {
	result, err := DecodeSSEWithUsage("data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":1,\"total_tokens\":3}}\n\ndata: [DONE]")
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "hello" || result.Usage == nil || result.Usage.TotalTokens != 3 {
		t.Fatalf("unexpected SSE result: %+v", result)
	}
}

func TestChatRetriesRetryableProviderErrors(t *testing.T) {
	attempts := 0
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}}})
	}))
	result, err := client.Chat([]map[string]string{{"role": "user", "content": "hi"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" || attempts != 2 {
		t.Fatalf("retry mismatch result=%q attempts=%d", result, attempts)
	}
}

func TestChatRetriesMalformedSuccessResponses(t *testing.T) {
	responses := []func(http.ResponseWriter){
		func(w http.ResponseWriter) { _, _ = w.Write([]byte("<html>bad gateway</html>")) },
		func(w http.ResponseWriter) {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": ""}}}})
		},
		func(w http.ResponseWriter) {
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}}})
		},
	}
	attempts := 0
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		responses[attempts](w)
		attempts++
	}))
	result, err := client.Chat([]map[string]string{{"role": "user", "content": "hi"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" || attempts != 3 {
		t.Fatalf("retry malformed mismatch result=%q attempts=%d", result, attempts)
	}
}

func TestChatDoesNotRetryNonRetryableProviderErrors(t *testing.T) {
	attempts := 0
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	_, err := client.Chat([]map[string]string{{"role": "user", "content": "hi"}}, "")
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("401 must not be retried, attempts=%d", attempts)
	}
	if !strings.Contains(err.Error(), "status=401") {
		t.Fatalf("expected status preview in error, got %v", err)
	}
}

func TestClientDefaultHTTPTransportIgnoresEnvironmentProxy(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	client := testClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}}})
	}))
	result, err := client.Chat([]map[string]string{{"role": "user", "content": "hi"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if result != "ok" {
		t.Fatalf("unexpected result %q", result)
	}
}

func testClient(t *testing.T, handler http.Handler) Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	settings := config.Settings{
		AppSecretKey:      "ai-test-secret",
		AIChatMaxAttempts: 3,
		AIChatTimeout:     5 * time.Second,
		AIChatBackoffBase: time.Nanosecond,
		AIChatBackoffMax:  time.Nanosecond,
	}
	sec := security.New(settings)
	encrypted, err := sec.EncryptSecret("provider-key")
	if err != nil {
		t.Fatal(err)
	}
	secret := &domainsettings.Secret{EncryptedValue: encrypted}
	return Client{
		Provider: domainai.Provider{Name: "test", BaseURL: srv.URL, DefaultModel: "m", APIKeySecret: secret},
		Security: sec,
		Settings: settings,
	}
}
