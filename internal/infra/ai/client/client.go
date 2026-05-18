package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	domainai "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/ai"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
	infralogging "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/logging"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/security"
	"go.uber.org/zap"
)

type Client struct {
	Provider   domainai.Provider
	Security   security.Service
	Settings   config.Settings
	HTTPClient *http.Client
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatResult struct {
	Content string `json:"content"`
	Usage   *Usage `json:"usage,omitempty"`
}

type ProviderResponseError struct {
	Message     string
	StatusCode  int
	ContentType string
	Preview     string
	Retryable   bool
}

func (e *ProviderResponseError) Error() string {
	var parts []string
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	if e.StatusCode != 0 {
		parts = append(parts, fmt.Sprintf("status=%d", e.StatusCode))
	}
	if e.ContentType != "" {
		parts = append(parts, "content-type="+e.ContentType)
	}
	if e.Preview != "" {
		parts = append(parts, "response="+e.Preview[:min(len(e.Preview), 500)])
	}
	return strings.Join(parts, " ")
}

func (c Client) Chat(messages []map[string]string, model string) (string, error) {
	return c.ChatWithContext(context.Background(), messages, model)
}

func (c Client) ChatWithContext(ctx context.Context, messages []map[string]string, model string) (string, error) {
	result, err := c.ChatWithUsageWithContext(ctx, messages, model)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

func (c Client) ChatWithUsageWithContext(ctx context.Context, messages []map[string]string, model string) (result ChatResult, err error) {
	start := time.Now()
	defer func() {
		infralogging.LogOperation(start, "ai.chat", "ai", "call", "AI chat call completed", err,
			zap.String("provider", c.Provider.Name),
			zap.String("model", model),
		)
	}()
	if c.Provider.APIKeySecret == nil {
		return ChatResult{}, fmt.Errorf("provider %s has no api key secret", c.Provider.Name)
	}
	apiKey, err := c.Security.DecryptSecret(c.Provider.APIKeySecret.EncryptedValue)
	if err != nil {
		return ChatResult{}, err
	}
	if model == "" {
		model = c.Provider.DefaultModel
	}
	payload := map[string]any{"model": model, "messages": messages, "temperature": 0.2, "stream": false}
	body, _ := json.Marshal(payload)
	attempts := c.Settings.AIChatMaxAttempts
	if attempts < 1 {
		attempts = 1
	}
	var last error
	for i := 1; i <= attempts; i++ {
		if err := ctx.Err(); err != nil {
			return ChatResult{}, err
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.ChatCompletionsURL(), bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpClient(c.Settings.AIChatTimeout).Do(req)
		if err == nil {
			result, decodeErr := DecodeResponseWithUsage(resp)
			if decodeErr == nil {
				return result, nil
			}
			err = decodeErr
		}
		last = err
		if i < attempts && isRetryableChatError(err) {
			select {
			case <-ctx.Done():
				return ChatResult{}, ctx.Err()
			case <-time.After(c.retryDelay(i)):
			}
		} else {
			break
		}
	}
	return ChatResult{}, fmt.Errorf("provider chat failed after %d attempts: %w", attempts, last)
}

func (c Client) ListModels() (models []map[string]any, err error) {
	start := time.Now()
	defer func() {
		infralogging.LogOperation(start, "ai.models", "ai", "list", "AI model list completed", err,
			zap.String("provider", c.Provider.Name),
			zap.Int("modelCount", len(models)),
		)
	}()
	if c.Provider.APIKeySecret == nil {
		return nil, fmt.Errorf("provider %s has no api key secret", c.Provider.Name)
	}
	apiKey, err := c.Security.DecryptSecret(c.Provider.APIKeySecret.EncryptedValue)
	if err != nil {
		return nil, err
	}
	req, _ := http.NewRequest(http.MethodGet, c.ModelsURL(), nil)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	resp, err := c.httpClient(30 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("provider returned HTTP %d: %s", resp.StatusCode, string(body[:min(len(body), 500)]))
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	switch data := payload.(type) {
	case map[string]any:
		items, _ := data["data"].([]any)
		return modelList(items), nil
	case []any:
		return modelList(data), nil
	default:
		return nil, errors.New("model list response is not OpenAI-compatible")
	}
}

func (c Client) httpClient(timeout time.Duration) *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &http.Client{Timeout: timeout, Transport: transport}
}

func (c Client) ChatCompletionsURL() string {
	base := strings.TrimRight(c.Provider.BaseURL, "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	return base + "/chat/completions"
}

func (c Client) ModelsURL() string {
	base := strings.TrimRight(c.Provider.BaseURL, "/")
	if strings.HasSuffix(base, "/chat/completions") {
		return strings.TrimSuffix(base, "/chat/completions") + "/models"
	}
	if strings.HasSuffix(base, "/models") {
		return base
	}
	return base + "/models"
}

func DecodeResponse(resp *http.Response) (string, error) {
	result, err := DecodeResponseWithUsage(resp)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

func DecodeResponseWithUsage(resp *http.Response) (ChatResult, error) {
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	contentType := resp.Header.Get("Content-Type")
	if resp.StatusCode >= 400 {
		return ChatResult{}, &ProviderResponseError{Message: "provider returned HTTP error", StatusCode: resp.StatusCode, ContentType: contentType, Preview: string(body[:min(len(body), 500)]), Retryable: retryableStatusCode(resp.StatusCode)}
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return ChatResult{}, &ProviderResponseError{Message: "provider returned empty response body", StatusCode: resp.StatusCode, ContentType: contentType, Retryable: true}
	}
	if strings.HasPrefix(text, "data:") {
		return DecodeSSEWithUsage(text)
	}
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return ChatResult{}, &ProviderResponseError{Message: fmt.Sprintf("provider response is not valid JSON: %v", err), StatusCode: resp.StatusCode, ContentType: contentType, Preview: string(body[:min(len(body), 500)]), Retryable: true}
	}
	content, err := ExtractChatContent(payload)
	if err != nil {
		return ChatResult{}, err
	}
	return ChatResult{Content: content, Usage: ExtractUsage(payload)}, nil
}

func DecodeSSE(text string) (string, error) {
	result, err := DecodeSSEWithUsage(text)
	if err != nil {
		return "", err
	}
	return result.Content, nil
}

func DecodeSSEWithUsage(text string) (ChatResult, error) {
	var parts []string
	var usage *Usage
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		raw := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if raw == "[DONE]" {
			break
		}
		var payload map[string]any
		if json.Unmarshal([]byte(raw), &payload) != nil {
			continue
		}
		if parsed := ExtractUsage(payload); parsed != nil {
			usage = parsed
		}
		content, _ := ExtractChatContent(payload)
		if content != "" {
			parts = append(parts, content)
		}
	}
	if len(parts) == 0 {
		return ChatResult{}, &ProviderResponseError{Message: "provider returned SSE without text content", Retryable: true}
	}
	return ChatResult{Content: strings.Join(parts, ""), Usage: usage}, nil
}

func ExtractChatContent(data any) (string, error) {
	root, ok := data.(map[string]any)
	if !ok {
		return "", errors.New("provider JSON has no assistant content")
	}
	if text, ok := root["output_text"].(string); ok && strings.TrimSpace(text) != "" {
		return text, nil
	}
	choices, _ := root["choices"].([]any)
	if len(choices) > 0 {
		choice, _ := choices[0].(map[string]any)
		if msg, _ := choice["message"].(map[string]any); msg != nil {
			if content := contentToString(msg["content"]); strings.TrimSpace(content) != "" {
				return content, nil
			}
			if content := contentToString(msg["reasoning_content"]); strings.TrimSpace(content) != "" {
				return content, nil
			}
		}
		if delta, _ := choice["delta"].(map[string]any); delta != nil {
			if content := contentToString(delta["content"]); strings.TrimSpace(content) != "" {
				return content, nil
			}
		}
		if text, ok := choice["text"].(string); ok && strings.TrimSpace(text) != "" {
			return text, nil
		}
	}
	output, _ := root["output"].([]any)
	var parts []string
	for _, item := range output {
		obj, _ := item.(map[string]any)
		contentItems, _ := obj["content"].([]any)
		for _, contentItem := range contentItems {
			contentObj, _ := contentItem.(map[string]any)
			if text, ok := contentObj["text"].(string); ok {
				parts = append(parts, text)
			}
		}
	}
	if len(parts) > 0 {
		return strings.Join(parts, ""), nil
	}
	return "", &ProviderResponseError{Message: "provider JSON has no assistant content", Retryable: true}
}

func contentToString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if obj, ok := item.(map[string]any); ok {
				if text, ok := obj["text"].(string); ok {
					parts = append(parts, text)
				} else if text, ok := obj["content"].(string); ok {
					parts = append(parts, text)
				}
			} else {
				parts = append(parts, fmt.Sprint(item))
			}
		}
		return strings.Join(parts, "")
	default:
		return ""
	}
}

func ExtractUsage(data any) *Usage {
	root, ok := data.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := root["usage"].(map[string]any)
	if !ok || raw == nil {
		return nil
	}
	usage := &Usage{
		PromptTokens:     intFromAny(raw["prompt_tokens"]),
		CompletionTokens: intFromAny(raw["completion_tokens"]),
		TotalTokens:      intFromAny(raw["total_tokens"]),
	}
	if usage.TotalTokens == 0 {
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	}
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.TotalTokens == 0 {
		return nil
	}
	return usage
}

func intFromAny(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return int(parsed)
	case string:
		var parsed int
		_, _ = fmt.Sscanf(strings.TrimSpace(typed), "%d", &parsed)
		return parsed
	default:
		return 0
	}
}

func (c Client) retryDelay(attempt int) time.Duration {
	base := c.Settings.AIChatBackoffBase
	if base <= 0 {
		base = time.Second
	}
	capDelay := c.Settings.AIChatBackoffMax
	if capDelay < base {
		capDelay = base
	}
	delay := minDuration(capDelay, base*time.Duration(1<<max(attempt-1, 0)))
	jitter := time.Duration(rand.Int63n(int64(minDuration(delay/4, time.Second)) + 1))
	return delay + jitter
}

func isRetryableChatError(err error) bool {
	if err == nil {
		return false
	}
	var providerErr *ProviderResponseError
	if errors.As(err, &providerErr) {
		return providerErr.Retryable || providerErr.StatusCode == 0 || retryableStatusCode(providerErr.StatusCode)
	}
	var netErr interface{ Timeout() bool }
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	return false
}

func retryableStatusCode(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusConflict, http.StatusTooEarly, http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

func modelList(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if obj, ok := item.(map[string]any); ok {
			out = append(out, obj)
		}
	}
	return out
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
