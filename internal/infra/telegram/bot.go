package telegram

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	runtimeproxy "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/proxy/runtime"
)

var BotAPIBase = "https://api.telegram.org"

type BotUpdate map[string]any

func SendBotMessage(apiBase string, token string, chatID string, text string) (map[string]any, error) {
	return SendBotMessageWithClient(apiBase, token, chatID, text, runtimeproxy.HTTPClientForConfig(runtimeproxy.Config{}, runtimeproxy.ModuleTelegram, 15*time.Second))
}

func SendBotMessageWithClient(apiBase string, token string, chatID string, text string, client *http.Client) (map[string]any, error) {
	token = strings.TrimSpace(token)
	chatID = strings.TrimSpace(chatID)
	if token == "" || chatID == "" {
		return nil, errors.New("Telegram Bot Token and Chat ID are required")
	}
	body, _ := json.Marshal(map[string]any{"chat_id": chatID, "text": truncateText(text), "disable_web_page_preview": true})
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(apiBase, "/")+"/bot"+token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if client == nil {
		client = runtimeproxy.HTTPClientForConfig(runtimeproxy.Config{}, runtimeproxy.ModuleTelegram, 15*time.Second)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Telegram Bot sendMessage failed: %d %s", resp.StatusCode, truncateForPrompt(string(respBody), 500))
	}
	var payload map[string]any
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return nil, err
	}
	if ok, _ := payload["ok"].(bool); !ok {
		return nil, fmt.Errorf("Telegram Bot sendMessage failed: %v", payload)
	}
	result, _ := payload["result"].(map[string]any)
	messageID := any(nil)
	if result != nil {
		messageID = result["message_id"]
	}
	return map[string]any{"status": "sent", "chat_id": chatID, "message_id": messageID}, nil
}

func GetBotUpdates(apiBase string, token string, offset int64, timeoutSeconds int) ([]BotUpdate, error) {
	return GetBotUpdatesWithClient(apiBase, token, offset, timeoutSeconds, runtimeproxy.HTTPClientForConfig(runtimeproxy.Config{}, runtimeproxy.ModuleTelegram, time.Duration(timeoutSeconds+10)*time.Second))
}

func GetBotUpdatesWithClient(apiBase string, token string, offset int64, timeoutSeconds int, client *http.Client) ([]BotUpdate, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("Telegram Bot Token is not configured")
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = 20
	}
	payload := map[string]any{"timeout": timeoutSeconds}
	if offset > 0 {
		payload["offset"] = offset
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, strings.TrimRight(apiBase, "/")+"/bot"+strings.TrimSpace(token)+"/getUpdates", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if client == nil {
		client = runtimeproxy.HTTPClientForConfig(runtimeproxy.Config{}, runtimeproxy.ModuleTelegram, time.Duration(timeoutSeconds+10)*time.Second)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("Telegram Bot getUpdates failed: %d %s", resp.StatusCode, truncateForPrompt(string(respBody), 500))
	}
	var root struct {
		OK     bool        `json:"ok"`
		Result []BotUpdate `json:"result"`
	}
	if err := json.Unmarshal(respBody, &root); err != nil {
		return nil, err
	}
	if !root.OK {
		return nil, fmt.Errorf("Telegram Bot getUpdates failed: %s", truncateForPrompt(string(respBody), 500))
	}
	return root.Result, nil
}

func NormalizeBotCommand(text string) (string, string) {
	stripped := strings.TrimSpace(text)
	if !strings.HasPrefix(stripped, "/") {
		return "", stripped
	}
	first, remainder, found := strings.Cut(stripped, " ")
	if !found {
		remainder = ""
	}
	command, _, _ := strings.Cut(first, "@")
	return strings.ToLower(command), strings.TrimSpace(remainder)
}

func MessageFromBotUpdate(update BotUpdate) map[string]any {
	for _, key := range []string{"message", "edited_message"} {
		if message, ok := update[key].(map[string]any); ok {
			return message
		}
	}
	return nil
}

func BotChatID(message map[string]any) string {
	chat, _ := message["chat"].(map[string]any)
	if chat == nil || chat["id"] == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(chat["id"]))
}

func truncateText(value string) string {
	if len(value) <= 4000 {
		return value
	}
	return value[:4000]
}

func truncateForPrompt(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}
