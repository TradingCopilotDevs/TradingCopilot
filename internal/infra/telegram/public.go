package telegram

import (
	"encoding/json"
	"fmt"
	domainkernel "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/kernel"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	runtimeproxy "github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/proxy/runtime"
)

var PublicBaseURL = "https://t.me/s"

var (
	publicPostBlock = regexp.MustCompile(`(?s)<div class="tgme_widget_message\b[^"]*"[^>]*data-post="([^"]+)"[^>]*>(.*?)<div class="tgme_widget_message_footer`)
	publicTime      = regexp.MustCompile(`(?s)<time[^>]*datetime="([^"]+)"`)
	publicText      = regexp.MustCompile(`(?s)<div class="tgme_widget_message_text\b[^"]*"[^>]*>(.*?)</div>`)
	publicOGTitle   = regexp.MustCompile(`(?s)<meta property="og:title" content="([^"]*)"`)
	publicTitle     = regexp.MustCompile(`(?s)<div class="tgme_channel_info_header_title"[^>]*>\s*<span[^>]*>(.*?)</span>`)
	htmlTag         = regexp.MustCompile(`(?s)<[^>]+>`)
)

type PublicMessage struct {
	MessageID   int64             `json:"message_id"`
	MessageTime time.Time         `json:"message_time"`
	Text        string            `json:"text"`
	Raw         domainkernel.JSON `json:"raw"`
}

type PublicResult struct {
	ChannelRef string          `json:"channel_ref"`
	Title      string          `json:"title"`
	Messages   []PublicMessage `json:"messages"`
}

func NormalizeChannelRef(value string) string {
	ref := strings.TrimSpace(value)
	if ref == "" {
		return ref
	}
	for _, prefix := range []string{"https://t.me/", "http://t.me/", "t.me/"} {
		if strings.HasPrefix(ref, prefix) {
			ref = strings.TrimPrefix(ref, prefix)
			break
		}
	}
	if strings.HasPrefix(ref, "@") || strings.HasPrefix(ref, "-100") || strings.HasPrefix(ref, "+") {
		return ref
	}
	return "@" + ref
}

func GetLatestPublicMessage(channelRef string) (map[string]any, error) {
	result, err := FetchPublicMessages(channelRef, 1, nil)
	if err != nil {
		return nil, err
	}
	if len(result.Messages) == 0 {
		return map[string]any{"status": "empty", "channel_ref": result.ChannelRef, "title": result.Title, "message_id": nil, "message_time": nil, "text": nil}, nil
	}
	message := result.Messages[0]
	return map[string]any{"status": "ok", "channel_ref": result.ChannelRef, "title": result.Title, "message_id": message.MessageID, "message_time": message.MessageTime, "text": message.Text}, nil
}

func FetchPublicMessages(channelRef string, limit int, minMessageID *int64) (PublicResult, error) {
	return FetchPublicMessagesWithClient(channelRef, limit, minMessageID, runtimeproxy.HTTPClientForConfig(runtimeproxy.Config{}, runtimeproxy.ModuleTelegram, 20*time.Second))
}

func FetchPublicMessagesWithClient(channelRef string, limit int, minMessageID *int64, client *http.Client) (PublicResult, error) {
	normalized := NormalizeChannelRef(channelRef)
	username, err := PublicUsername(normalized)
	if err != nil {
		return PublicResult{}, err
	}
	if limit <= 0 {
		limit = 20
	}
	pageURL := strings.TrimRight(PublicBaseURL, "/") + "/" + url.PathEscape(username)
	req, err := http.NewRequest(http.MethodGet, pageURL, nil)
	if err != nil {
		return PublicResult{}, err
	}
	req.Header.Set("User-Agent", "TreadingCopilot/1.0")
	if client == nil {
		client = runtimeproxy.HTTPClientForConfig(runtimeproxy.Config{}, runtimeproxy.ModuleTelegram, 20*time.Second)
	}
	resp, err := client.Do(req)
	if err != nil {
		return PublicResult{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return PublicResult{}, fmt.Errorf("Telegram public page returned HTTP %d", resp.StatusCode)
	}
	title := ExtractPublicTitle(string(body), normalized)
	messages := ParsePublicMessages(username, string(body), minMessageID)
	sort.Slice(messages, func(i, j int) bool { return messages[i].MessageID > messages[j].MessageID })
	if len(messages) > limit {
		messages = messages[:limit]
	}
	return PublicResult{ChannelRef: normalized, Title: title, Messages: messages}, nil
}

func PublicUsername(normalized string) (string, error) {
	value := strings.TrimPrefix(strings.TrimSpace(normalized), "@")
	if value == "" {
		return "", fmt.Errorf("telegram channel is required")
	}
	if strings.HasPrefix(value, "+") || strings.HasPrefix(value, "joinchat/") || strings.HasPrefix(value, "-100") || strings.Contains(value, "/") {
		return "", fmt.Errorf("public Telegram page collection supports public @username channels only; MTProto login is required for private or numeric channels")
	}
	return value, nil
}

func ParsePublicMessages(username string, body string, minMessageID *int64) []PublicMessage {
	matches := publicPostBlock.FindAllStringSubmatch(body, -1)
	messages := make([]PublicMessage, 0, len(matches))
	for _, match := range matches {
		postRef := html.UnescapeString(match[1])
		parts := strings.Split(postRef, "/")
		if len(parts) != 2 || !strings.EqualFold(parts[0], username) {
			continue
		}
		messageID, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}
		if minMessageID != nil && messageID <= *minMessageID {
			continue
		}
		textMatch := publicText.FindStringSubmatch(match[2])
		if len(textMatch) != 2 {
			continue
		}
		text := CleanHTMLText(textMatch[1])
		if strings.TrimSpace(text) == "" {
			continue
		}
		timestamp := time.Now()
		if timeMatch := publicTime.FindStringSubmatch(match[2]); len(timeMatch) == 2 {
			if parsed, err := time.Parse(time.RFC3339, html.UnescapeString(timeMatch[1])); err == nil {
				timestamp = parsed
			}
		}
		raw, _ := json.Marshal(map[string]any{"id": messageID, "post": postRef, "source": "telegram_public_page"})
		messages = append(messages, PublicMessage{MessageID: messageID, MessageTime: timestamp, Text: text, Raw: domainkernel.JSON(raw)})
	}
	return messages
}

func ExtractPublicTitle(body string, fallback string) string {
	for _, re := range []*regexp.Regexp{publicOGTitle, publicTitle} {
		if match := re.FindStringSubmatch(body); len(match) == 2 {
			title := strings.TrimSpace(CleanHTMLText(match[1]))
			title = strings.TrimSuffix(title, " - Telegram")
			if title != "" {
				return title
			}
		}
	}
	return fallback
}

func CleanHTMLText(value string) string {
	value = strings.ReplaceAll(value, "<br/>", "\n")
	value = strings.ReplaceAll(value, "<br />", "\n")
	value = strings.ReplaceAll(value, "<br>", "\n")
	value = htmlTag.ReplaceAllString(value, "")
	value = html.UnescapeString(value)
	lines := strings.Split(value, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}
