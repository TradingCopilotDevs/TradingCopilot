package meeting

import (
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const DefaultWebSearchBaseURL = "https://news.google.com/rss/search"

func SearchWeb(client *http.Client, baseURL string, query string, limit int) ([]map[string]any, map[string]any, error) {
	cleaned := strings.Join(strings.Fields(query), " ")
	args := map[string]any{"query": cleaned, "limit": limit}
	if cleaned == "" {
		return []map[string]any{}, args, nil
	}
	if limit <= 0 {
		limit = 5
	}
	if limit > 10 {
		limit = 10
	}
	args["limit"] = limit
	endpoint, err := url.Parse(firstNonEmptyString(baseURL, DefaultWebSearchBaseURL))
	if err != nil {
		return nil, args, err
	}
	q := endpoint.Query()
	q.Set("q", cleaned)
	q.Set("hl", "zh-CN")
	q.Set("gl", "CN")
	q.Set("ceid", "CN:zh-Hans")
	endpoint.RawQuery = q.Encode()
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Get(endpoint.String())
	if err != nil {
		return nil, args, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, args, fmt.Errorf("web search failed: %d", resp.StatusCode)
	}
	var feed struct {
		Channel struct {
			Items []struct {
				Title     string `xml:"title"`
				Link      string `xml:"link"`
				PubDate   string `xml:"pubDate"`
				Source    string `xml:"source"`
				SourceURL string `xml:"source,attr"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, args, err
	}
	rows := make([]map[string]any, 0, limit)
	for _, item := range feed.Channel.Items {
		title := strings.TrimSpace(item.Title)
		link := strings.TrimSpace(item.Link)
		if title == "" || link == "" {
			continue
		}
		rows = append(rows, map[string]any{"title": title, "link": link, "published_at": strings.TrimSpace(item.PubDate), "source": strings.TrimSpace(item.Source)})
		if len(rows) >= limit {
			break
		}
	}
	return rows, args, nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
