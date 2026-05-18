package jsonapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteDataUsesJSONAPIEnvelope(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteData(rec, http.StatusOK, NewResource("health-checks", "api", map[string]any{"status": "ok"}))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); got != ContentType {
		t.Fatalf("content type = %q, want %q", got, ContentType)
	}
	var doc Document
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if doc.Data == nil {
		t.Fatalf("data is nil")
	}
	if len(doc.Errors) != 0 {
		t.Fatalf("errors = %v, want none", doc.Errors)
	}
}

func TestWriteErrorUsesErrorsArray(t *testing.T) {
	rec := httptest.NewRecorder()
	WriteError(rec, http.StatusBadRequest, ErrorObject{Code: "invalid-json", Message: "Invalid JSON", Detail: "request body is malformed", Field: "body"})

	var body struct {
		Errors []ErrorObject `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(body.Errors) != 1 {
		t.Fatalf("errors length = %d, want 1", len(body.Errors))
	}
	if body.Errors[0].Code != "invalid-json" || body.Errors[0].Field != "body" {
		t.Fatalf("error object = %+v", body.Errors[0])
	}
}

func TestParsePageClampsLimitAndKeepsCursor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/items?page[limit]=1000&page[cursor]=abc", nil)
	page := ParsePage(req, 25, 100)

	if page.Limit != 100 {
		t.Fatalf("limit = %d, want 100", page.Limit)
	}
	if page.Cursor != "abc" {
		t.Fatalf("cursor = %q, want abc", page.Cursor)
	}
}

func TestPageLinksAddsNextCursor(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/items?page[limit]=25", nil)
	links := PageLinks(req, "cursor-2")

	if links == nil {
		t.Fatalf("links is nil")
	}
	if links.Next != "/api/items?page%5Bcursor%5D=cursor-2&page%5Blimit%5D=25" {
		t.Fatalf("next = %q", links.Next)
	}
}

func TestPageResourcesUsesStableResourceIDCursor(t *testing.T) {
	resources := []Resource{
		NewResource("items", "a", nil),
		NewResource("items", "b", nil),
		NewResource("items", "c", nil),
		NewResource("items", "d", nil),
	}

	first, next := PageResources(resources, Page{Limit: 2})
	if len(first) != 2 || first[0].ID != "a" || first[1].ID != "b" || next != "b" {
		t.Fatalf("first page = %+v next=%q", first, next)
	}
	second, next := PageResources(resources, Page{Limit: 2, Cursor: "b"})
	if len(second) != 2 || second[0].ID != "c" || second[1].ID != "d" || next != "" {
		t.Fatalf("second page = %+v next=%q", second, next)
	}
}
