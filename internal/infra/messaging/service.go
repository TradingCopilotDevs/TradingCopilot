package messaging

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	appmessaging "github.com/TradingCopilotDevs/TradingCopilot/internal/app/messaging"
	domainai "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/ai"
	domainkernel "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/kernel"
	domainmsg "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/messaging"
	domaintelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/domain/telegram"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/config"
	proxyruntime "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/proxy/runtime"
	"github.com/TradingCopilotDevs/TradingCopilot/internal/infra/security"
	infratelegram "github.com/TradingCopilotDevs/TradingCopilot/internal/infra/telegram"
	"github.com/mmcdole/gofeed"
)

var appTZ = time.FixedZone("Asia/Shanghai", 8*3600)
var telegramPublicBaseURL = infratelegram.PublicBaseURL
var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

type Service struct {
	settings config.Settings
}

type LiveMessage struct {
	SubscriptionID  uint
	SourceMessageID string
	MessageTime     time.Time
	Text            string
	Raw             domainkernel.JSON
}

func NewService(settings config.Settings) Service {
	return Service{settings: settings}
}

func (s Service) StartSubscriptionLogin(ctx context.Context, credentials appmessaging.TelegramCredentials, phone string) (string, string, error) {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return "", "", errors.New("telegram phone is required")
	}
	client, err := s.mtprotoClient(credentials, false)
	if err != nil {
		return "", "", err
	}
	loginCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	hash, err := client.StartLogin(loginCtx, phone)
	if err != nil {
		return "", "", err
	}
	loginSession := ""
	if client.LoginStorage != nil {
		loginSession = client.LoginStorage.LastDataEncoded()
	}
	if loginSession == "" {
		return "", "", errors.New("telegram verification code was sent but no MTProto login session was stored")
	}
	return hash, loginSession, nil
}

func (s Service) CompleteSubscriptionLogin(ctx context.Context, credentials appmessaging.TelegramCredentials, phone string, code string, phoneCodeHash string, password string) (string, error) {
	phone = strings.TrimSpace(phone)
	code = strings.TrimSpace(code)
	phoneCodeHash = strings.TrimSpace(phoneCodeHash)
	if phone == "" || code == "" || phoneCodeHash == "" {
		return "", errors.New("phone, code, and phone_code_hash are required")
	}
	client, err := s.mtprotoClient(credentials, false)
	if err != nil {
		return "", err
	}
	loginCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	return client.CompleteLogin(loginCtx, phone, code, phoneCodeHash, password)
}

func (s Service) NormalizeSourceRef(provider string, value string) string {
	switch provider {
	case domainmsg.ProviderRSSFeed:
		return normalizeFeedURL(value)
	default:
		return infratelegram.NormalizeChannelRef(value)
	}
}

func (s Service) TestSubscription(ctx context.Context, credentials appmessaging.SubscriptionCredentials, provider string, sourceRef string) (map[string]any, error) {
	if provider == domainmsg.ProviderRSSFeed {
		return s.testFeed(ctx, credentials.Proxy, credentials.RSSAuth, sourceRef)
	}
	if _, err := infratelegram.PublicUsername(infratelegram.NormalizeChannelRef(sourceRef)); err == nil {
		result, err := fetchPublicMessagesWithClient(sourceRef, 1, nil, s.httpClient(credentials.Telegram.Proxy, proxyruntime.ModuleTelegram, 20*time.Second))
		if err != nil {
			return nil, err
		}
		if len(result.Messages) == 0 {
			return map[string]any{"status": "empty", "source_ref": result.ChannelRef, "title": result.Title, "source_message_id": nil, "message_time": nil, "text": nil}, nil
		}
		message := result.Messages[0]
		return map[string]any{"status": "ok", "source_ref": result.ChannelRef, "title": result.Title, "source_message_id": strconv.FormatInt(message.MessageID, 10), "message_time": message.MessageTime, "text": message.Text}, nil
	}
	client, err := s.mtprotoClient(credentials.Telegram, true)
	if err != nil {
		return nil, err
	}
	testCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	result, err := client.LatestMessage(testCtx, sourceRef)
	if result != nil {
		if v, ok := result["channel_ref"]; ok {
			result["source_ref"] = v
		}
		if v, ok := result["message_id"]; ok {
			result["source_message_id"] = fmt.Sprint(v)
		}
	}
	return result, err
}

func (s Service) FetchSubscriptionMessages(ctx context.Context, credentials appmessaging.SubscriptionCredentials, subscription domainmsg.MessageSubscription, limit int, afterSourceMessageID *string) (appmessaging.FetchedMessages, error) {
	if subscription.Provider == domainmsg.ProviderRSSFeed {
		return s.fetchFeed(ctx, credentials.Proxy, credentials.RSSAuth, subscription.SourceRef, limit)
	}
	if limit <= 0 {
		limit = 20
	}
	minMessageID := int64PtrFromString(afterSourceMessageID)
	if _, err := infratelegram.PublicUsername(infratelegram.NormalizeChannelRef(subscription.SourceRef)); err == nil {
		result, err := fetchPublicMessagesWithClient(subscription.SourceRef, limit, minMessageID, s.httpClient(credentials.Telegram.Proxy, proxyruntime.ModuleTelegram, 20*time.Second))
		return fetchedMessagesFromPublic(result), err
	}
	client, err := s.mtprotoClient(credentials.Telegram, true)
	if err != nil {
		return appmessaging.FetchedMessages{}, err
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	result, err := client.RecentMessages(fetchCtx, subscription.SourceRef, limit, minMessageID)
	return fetchedMessagesFromPublic(result), err
}

func (s Service) SendAdapterTest(ctx context.Context, token string, chatID string, proxy appmessaging.ProxyConfig) (map[string]any, error) {
	return infratelegram.SendBotMessageWithClient(infratelegram.BotAPIBase, token, chatID, "TradingCopilot platform adapter test message.", s.httpClient(proxy, proxyruntime.ModuleTelegram, 15*time.Second))
}

func (s Service) JSON(value any) domainkernel.JSON { return jsonValue(value) }

func (s Service) ExtractRelatedSymbols(text string) []string {
	return infratelegram.ExtractRelatedSymbols(text)
}

func (s Service) ApplyFilter(ctx context.Context, message domainmsg.IngestedMessage, sec appmessaging.SecurityService, filter domainmsg.MessageSubscriptionFilter, provider domainai.Provider, proxy appmessaging.ProxyConfig) (appmessaging.FilterResult, error) {
	sourceMessageID, _ := strconv.ParseInt(strings.TrimSpace(message.SourceMessageID), 10, 64)
	compatibleMessage := domaintelegram.Message{
		ID:             message.ID,
		ChannelID:      message.SubscriptionID,
		MessageID:      sourceMessageID,
		MessageTime:    message.MessageTime,
		Text:           message.Text,
		Raw:            message.Raw,
		FilterDecision: message.FilterDecision,
		FilterReason:   message.FilterReason,
		RelatedSymbols: message.RelatedSymbols,
	}
	role := domainai.AgentRole{
		Key:            fmt.Sprintf("message_filter_%d", filter.ID),
		Name:           filter.Name,
		Responsibility: filter.Description,
		PromptTemplate: filter.PromptTemplate,
		ProviderID:     filter.ProviderID,
		Model:          filter.Model,
		ToolNames:      jsonValue(appmessaging.DefaultFilterToolNames),
		SkillNames:     jsonValue(appmessaging.DefaultFilterSkillNames),
		Enabled:        filter.Enabled,
	}
	result, err := infratelegram.FilterMessageWithRole(ctx, compatibleMessage, role, provider, concreteSecurity(sec), s.settings, s.httpClient(proxy, proxyruntime.ModuleAI, s.settings.AIChatTimeout))
	if err != nil {
		return appmessaging.FilterResult{}, err
	}
	return appmessaging.FilterResult{
		Decision:       &result.Decision,
		Reason:         &result.Reason,
		RelatedSymbols: jsonValue(result.RelatedSymbols),
		FilterID:       filter.ID,
	}, nil
}

func (s Service) Listen(ctx context.Context, credentials appmessaging.TelegramCredentials, subscriptions []domainmsg.MessageSubscription, handle func(context.Context, LiveMessage) error, onResolvedTitle func(context.Context, uint, string) error) error {
	client, err := s.mtprotoClient(credentials, true)
	if err != nil {
		return err
	}
	channels := make([]infratelegram.MTProtoLiveChannel, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		channels = append(channels, infratelegram.MTProtoLiveChannel{ID: subscription.ID, ChannelRef: subscription.SourceRef, Title: subscription.Title})
	}
	return client.Listen(ctx, channels, func(ctx context.Context, message infratelegram.MTProtoLiveMessage) error {
		if handle == nil {
			return nil
		}
		return handle(ctx, LiveMessage{
			SubscriptionID:  message.ChannelID,
			SourceMessageID: strconv.FormatInt(message.MessageID, 10),
			MessageTime:     message.MessageTime,
			Text:            message.Text,
			Raw:             message.Raw,
		})
	}, onResolvedTitle)
}

func (s Service) mtprotoClient(credentials appmessaging.TelegramCredentials, requireSession bool) (infratelegram.MTProtoClient, error) {
	if credentials.AppID <= 0 {
		return infratelegram.MTProtoClient{}, fmt.Errorf("telegram app_id is invalid")
	}
	if strings.TrimSpace(credentials.AppHash) == "" {
		return infratelegram.MTProtoClient{}, fmt.Errorf("message subscription secret telegram app_hash is not configured")
	}
	if requireSession && strings.TrimSpace(credentials.Session) == "" {
		return infratelegram.MTProtoClient{}, fmt.Errorf("message subscription secret telegram mtproto_session is not configured")
	}
	return infratelegram.MTProtoClient{
		Credentials:    infratelegram.MTProtoCredentials{AppID: credentials.AppID, AppHash: strings.TrimSpace(credentials.AppHash)},
		LoginStorage:   &infratelegram.MTProtoSessionStorage{Store: staticSecretStore{value: strings.TrimSpace(credentials.LoginSession)}},
		SessionStorage: &infratelegram.MTProtoSessionStorage{Store: staticSecretStore{value: strings.TrimSpace(credentials.Session)}},
		Proxy:          telegramRuntimeProxy(credentials.Proxy),
		Location:       appTZ,
	}, nil
}

type staticSecretStore struct {
	value string
}

func (s staticSecretStore) SecretValue(context.Context, string) (string, error) {
	if strings.TrimSpace(s.value) == "" {
		return "", errors.New("secret not found")
	}
	return s.value, nil
}

func (s staticSecretStore) UpsertSecret(context.Context, string, string) error {
	return nil
}

func fetchPublicMessagesWithClient(channelRef string, limit int, minMessageID *int64, client *http.Client) (infratelegram.PublicResult, error) {
	infratelegram.PublicBaseURL = telegramPublicBaseURL
	return infratelegram.FetchPublicMessagesWithClient(channelRef, limit, minMessageID, client)
}

func fetchedMessagesFromPublic(result infratelegram.PublicResult) appmessaging.FetchedMessages {
	out := appmessaging.FetchedMessages{SourceRef: result.ChannelRef, Title: result.Title, Messages: make([]appmessaging.FetchedMessage, 0, len(result.Messages))}
	for _, message := range result.Messages {
		out.Messages = append(out.Messages, appmessaging.FetchedMessage{
			SourceMessageID: strconv.FormatInt(message.MessageID, 10),
			MessageTime:     message.MessageTime,
			Text:            message.Text,
			Raw:             domainkernel.JSON(message.Raw),
		})
	}
	return out
}

func (s Service) testFeed(ctx context.Context, proxy appmessaging.ProxyConfig, auth appmessaging.RSSAuthCredentials, sourceRef string) (map[string]any, error) {
	fetched, err := s.fetchFeed(ctx, proxy, auth, sourceRef, 1)
	if err != nil {
		return nil, err
	}
	if len(fetched.Messages) == 0 {
		return map[string]any{"status": "empty", "source_ref": fetched.SourceRef, "title": fetched.Title, "source_message_id": nil, "message_time": nil, "text": nil}, nil
	}
	message := fetched.Messages[0]
	return map[string]any{"status": "ok", "source_ref": fetched.SourceRef, "title": fetched.Title, "source_message_id": message.SourceMessageID, "message_time": message.MessageTime, "text": message.Text}, nil
}

func (s Service) fetchFeed(ctx context.Context, proxy appmessaging.ProxyConfig, auth appmessaging.RSSAuthCredentials, sourceRef string, limit int) (appmessaging.FetchedMessages, error) {
	if limit <= 0 {
		limit = 20
	}
	normalized := normalizeFeedURL(sourceRef)
	if normalized == "" {
		return appmessaging.FetchedMessages{}, errors.New("rss feed url is required")
	}
	feedCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(feedCtx, http.MethodGet, normalized, nil)
	if err != nil {
		return appmessaging.FetchedMessages{}, err
	}
	applyRSSAuthHeader(req, auth)
	resp, err := s.httpClient(proxy, proxyruntime.ModuleWeb, 30*time.Second).Do(req)
	if err != nil {
		return appmessaging.FetchedMessages{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return appmessaging.FetchedMessages{}, fmt.Errorf("rss feed returned status %d", resp.StatusCode)
	}
	parser := gofeed.NewParser()
	feed, err := parser.Parse(resp.Body)
	if err != nil {
		return appmessaging.FetchedMessages{}, err
	}
	title := strings.TrimSpace(feed.Title)
	if title == "" {
		title = normalized
	}
	out := appmessaging.FetchedMessages{SourceRef: normalized, Title: title, Messages: make([]appmessaging.FetchedMessage, 0, len(feed.Items))}
	for _, item := range feed.Items {
		if item == nil {
			continue
		}
		message := feedItemMessage(normalized, item)
		if strings.TrimSpace(message.SourceMessageID) == "" || strings.TrimSpace(message.Text) == "" {
			continue
		}
		out.Messages = append(out.Messages, message)
	}
	if len(out.Messages) > limit {
		out.Messages = out.Messages[:limit]
	}
	return out, nil
}

func applyRSSAuthHeader(req *http.Request, auth appmessaging.RSSAuthCredentials) {
	switch strings.ToLower(strings.TrimSpace(auth.Type)) {
	case "basic":
		req.SetBasicAuth(strings.TrimSpace(auth.Username), strings.TrimSpace(auth.Password))
	case "bearer":
		if token := strings.TrimSpace(auth.Password); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
	}
}

func feedItemMessage(feedURL string, item *gofeed.Item) appmessaging.FetchedMessage {
	messageTime := time.Now()
	if item.PublishedParsed != nil {
		messageTime = *item.PublishedParsed
	} else if item.UpdatedParsed != nil {
		messageTime = *item.UpdatedParsed
	}
	title := cleanFeedText(item.Title)
	summary := cleanFeedText(firstNonEmpty(item.Description, item.Content))
	link := strings.TrimSpace(item.Link)
	parts := []string{}
	if title != "" {
		parts = append(parts, title)
	}
	if summary != "" && summary != title {
		parts = append(parts, summary)
	}
	if link != "" {
		parts = append(parts, "Link: "+link)
	}
	sourceID := feedItemSourceID(feedURL, item, messageTime)
	return appmessaging.FetchedMessage{
		SourceMessageID: sourceID,
		MessageTime:     messageTime,
		Text:            strings.Join(parts, "\n\n"),
		Raw: jsonValue(map[string]any{
			"feed_url":  feedURL,
			"guid":      item.GUID,
			"link":      item.Link,
			"title":     item.Title,
			"published": item.Published,
			"updated":   item.Updated,
		}),
	}
}

func feedItemSourceID(feedURL string, item *gofeed.Item, messageTime time.Time) string {
	for _, value := range []string{item.GUID, item.Link} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	seed := feedURL + "\n" + item.Title + "\n" + messageTime.UTC().Format(time.RFC3339Nano)
	sum := sha256.Sum256([]byte(seed))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeFeedURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Hostname() == "" {
		return value
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return value
	}
	parsed.Scheme = scheme
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Fragment = ""
	return parsed.String()
}

func cleanFeedText(value string) string {
	value = htmlTagPattern.ReplaceAllString(value, " ")
	value = html.UnescapeString(value)
	return strings.Join(strings.Fields(value), " ")
}

func int64PtrFromString(value *string) *int64 {
	if value == nil {
		return nil
	}
	parsed, err := strconv.ParseInt(strings.TrimSpace(*value), 10, 64)
	if err != nil {
		return nil
	}
	return &parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (s Service) httpClient(cfg appmessaging.ProxyConfig, module string, timeout time.Duration) *http.Client {
	return proxyruntime.HTTPClientForConfig(proxyConfig(cfg), module, timeout)
}

func telegramRuntimeProxy(cfg appmessaging.ProxyConfig) *infratelegram.ProxyConfig {
	if !cfg.EnabledTelegram || strings.TrimSpace(cfg.ProxyURL) == "" {
		return nil
	}
	proxyCfg, err := infratelegram.ParseProxyURL(cfg.ProxyURL)
	if err != nil {
		return nil
	}
	return proxyCfg
}

func proxyConfig(cfg appmessaging.ProxyConfig) proxyruntime.Config {
	return proxyruntime.Config{
		ProxyURL:        cfg.ProxyURL,
		EnabledAI:       cfg.EnabledAI,
		EnabledTelegram: cfg.EnabledTelegram,
		EnabledMarket:   cfg.EnabledMarket,
		EnabledWeb:      cfg.EnabledWeb,
		NoProxy:         cfg.NoProxy,
		Revision:        cfg.Revision,
		UpdatedAt:       cfg.UpdatedAt,
	}
}

func concreteSecurity(sec appmessaging.SecurityService) security.Service {
	if typed, ok := sec.(security.Service); ok {
		return typed
	}
	return security.Service{}
}

func jsonValue(v any) domainkernel.JSON {
	b, _ := json.Marshal(v)
	return domainkernel.JSON(b)
}
