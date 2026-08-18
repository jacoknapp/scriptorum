package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestNormalizeAuthorsJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"string array", `["Alice","Bob"]`, []string{"Alice", "Bob"}},
		{"bare string", `"Alice"`, []string{"Alice"}},
		{"empty", ``, nil},
		{"null", `null`, nil},
		{"array of objects (readarr name)", `[{"name":"Alice"}]`, []string{"Alice"}},
		{"array of objects (chaptarr authorName)", `[{"authorName":"Cornman","foreignAuthorId":"gr:1"}]`, []string{"Cornman"}},
		{"mixed strings and objects", `["Alice",{"authorName":"Cornman"}]`, []string{"Alice", "Cornman"}},
		{"object without name is skipped", `[{"foreignAuthorId":"gr:1"}]`, nil},
		{"whitespace trimmed", `["  Alice  "]`, []string{"Alice"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeAuthorsJSON(json.RawMessage(tc.in))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("normalizeAuthorsJSON(%s) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}

// Regression: the search-result modal used to send "authors" as an array
// containing a raw Readarr/Chaptarr author object. A strict []string decode
// failed and the handler silently fell back to empty form parsing, so every
// such request returned "400 title or identifier required" (the live "Mage
// Tank" failure). The request must now be accepted.
func TestCreateRequestAcceptsAuthorObjects(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	user := makeCookie(t, s, "user", false)

	body := []byte(`{"title":"Mage Tank: Book One (Mage Tank, #1)","authors":[{"authorName":"Cornman","foreignAuthorId":"gr:51943306"}],"isbn10":"","isbn13":"","asin":"","format":"audiobook"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(user)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

// An empty title with no identifiers must still be rejected.
func TestCreateRequestStillRejectsEmpty(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	user := makeCookie(t, s, "user", false)

	body := []byte(`{"title":"","authors":[],"format":"ebook"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(user)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}
