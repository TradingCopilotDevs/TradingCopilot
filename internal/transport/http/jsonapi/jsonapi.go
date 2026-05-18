package jsonapi

import (
	"encoding/json"
	"net/http"
	"strconv"
)

const ContentType = "application/vnd.api+json"

type Document struct {
	Data     any            `json:"data,omitempty"`
	Errors   []ErrorObject  `json:"errors,omitempty"`
	Links    *Links         `json:"links,omitempty"`
	Meta     map[string]any `json:"meta,omitempty"`
	Included []Resource     `json:"included,omitempty"`
}

type Resource struct {
	Type          string                  `json:"type"`
	ID            string                  `json:"id,omitempty"`
	Attributes    any                     `json:"attributes,omitempty"`
	Relationships map[string]Relationship `json:"relationships,omitempty"`
	Links         *Links                  `json:"links,omitempty"`
	Meta          map[string]any          `json:"meta,omitempty"`
}

type Relationship struct {
	Data  any            `json:"data,omitempty"`
	Links *Links         `json:"links,omitempty"`
	Meta  map[string]any `json:"meta,omitempty"`
}

type Links struct {
	Self string `json:"self,omitempty"`
	Next string `json:"next,omitempty"`
	Prev string `json:"prev,omitempty"`
}

type ErrorObject struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
	Field   string `json:"field,omitempty"`
}

type Page struct {
	Limit  int
	Cursor string
}

func NewResource(resourceType string, id string, attributes any) Resource {
	return Resource{Type: resourceType, ID: id, Attributes: attributes}
}

func Write(w http.ResponseWriter, status int, doc Document) {
	w.Header().Set("Content-Type", ContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(doc)
}

func WriteData(w http.ResponseWriter, status int, data any) {
	Write(w, status, Document{Data: data})
}

func WriteError(w http.ResponseWriter, status int, err ErrorObject) {
	Write(w, status, Document{Errors: []ErrorObject{err}})
}

func ParsePage(r *http.Request, defaultLimit int, maxLimit int) Page {
	if defaultLimit <= 0 {
		defaultLimit = 50
	}
	if maxLimit <= 0 {
		maxLimit = defaultLimit
	}
	limit := defaultLimit
	if raw := r.URL.Query().Get("page[limit]"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	return Page{
		Limit:  limit,
		Cursor: r.URL.Query().Get("page[cursor]"),
	}
}

func PageMeta(pageSize int, nextCursor string) map[string]any {
	return map[string]any{
		"pageSize":   pageSize,
		"nextCursor": nextCursor,
		"hasMore":    nextCursor != "",
	}
}

func PageResources(resources []Resource, page Page) ([]Resource, string) {
	if page.Limit <= 0 {
		page.Limit = len(resources)
	}
	start := 0
	if page.Cursor != "" {
		for i, resource := range resources {
			if resource.ID == page.Cursor || strconv.Itoa(i+1) == page.Cursor {
				start = i + 1
				break
			}
		}
	}
	if start >= len(resources) {
		return []Resource{}, ""
	}
	end := start + page.Limit
	if end >= len(resources) {
		return resources[start:], ""
	}
	nextCursor := resources[end-1].ID
	if nextCursor == "" {
		nextCursor = strconv.Itoa(end)
	}
	return resources[start:end], nextCursor
}

func PageLinks(r *http.Request, nextCursor string) *Links {
	if nextCursor == "" {
		return nil
	}
	nextURL := *r.URL
	query := nextURL.Query()
	query.Set("page[cursor]", nextCursor)
	nextURL.RawQuery = query.Encode()
	return &Links{Next: nextURL.String()}
}
