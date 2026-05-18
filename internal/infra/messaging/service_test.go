package messaging

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appmessaging "github.com/TreadingCopilotDevs/TreadingCopilot/internal/app/messaging"
	domainmsg "github.com/TreadingCopilotDevs/TreadingCopilot/internal/domain/messaging"
	"github.com/TreadingCopilotDevs/TreadingCopilot/internal/infra/config"
)

func TestFetchRSSFeedMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<rss version="2.0"><channel><title>Fixture Feed</title>
<item><guid>item-1</guid><title>Market update</title><description><![CDATA[<p>600000 earnings beat</p>]]></description><link>https://example.test/item-1</link><pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate></item>
</channel></rss>`))
	}))
	defer server.Close()

	result, err := NewService(testSettings()).FetchSubscriptionMessages(context.Background(), appmessaging.TelegramCredentials{}, domainmsg.MessageSubscription{
		Provider:  domainmsg.ProviderRSSFeed,
		SourceRef: server.URL,
	}, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "Fixture Feed" || len(result.Messages) != 1 {
		t.Fatalf("unexpected feed result: %+v", result)
	}
	message := result.Messages[0]
	if message.SourceMessageID != "item-1" {
		t.Fatalf("source id = %q", message.SourceMessageID)
	}
	if !strings.Contains(message.Text, "Market update") || !strings.Contains(message.Text, "600000 earnings beat") || !strings.Contains(message.Text, "Link: https://example.test/item-1") {
		t.Fatalf("message text did not contain title, summary, and link: %q", message.Text)
	}
}

func TestFetchAtomFeedMessagesUsesLinkAsSourceID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<feed xmlns="http://www.w3.org/2005/Atom"><title>Atom Fixture</title>
<entry><title>Policy update</title><link href="https://example.test/policy"/><updated>2006-01-02T15:04:05Z</updated><summary>important market signal</summary></entry>
</feed>`))
	}))
	defer server.Close()

	result, err := NewService(testSettings()).FetchSubscriptionMessages(context.Background(), appmessaging.TelegramCredentials{}, domainmsg.MessageSubscription{
		Provider:  domainmsg.ProviderRSSFeed,
		SourceRef: server.URL,
	}, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Messages) != 1 {
		t.Fatalf("messages = %+v", result.Messages)
	}
	if result.Messages[0].SourceMessageID != "https://example.test/policy" {
		t.Fatalf("source id = %q", result.Messages[0].SourceMessageID)
	}
}

func testSettings() config.Settings {
	return config.Settings{}
}
