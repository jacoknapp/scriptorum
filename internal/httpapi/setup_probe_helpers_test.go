package httpapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

func TestHandleTestReadarrUsesSavedSettings(t *testing.T) {
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/book/lookup" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer readarr.Close()

	s := newServerForTest(t)
	ui := &setupUI{}
	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = readarr.URL
	cfg.Readarr.Ebooks.APIKey = "ebooks-key"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	stepFlags = map[string]bool{"rebooks": false}
	req := httptest.NewRequest(http.MethodPost, "/setup/test-readarr?tag=ebooks", nil)
	rec := httptest.NewRecorder()
	ui.handleTestReadarr(s)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if rec.Header().Get("HX-Trigger") != "setup-saved" {
		t.Fatalf("unexpected trigger header: %q", rec.Header().Get("HX-Trigger"))
	}
	if !strings.Contains(rec.Body.String(), "OK") {
		t.Fatalf("expected success body, got %q", rec.Body.String())
	}
	if !stepFlags["rebooks"] {
		t.Fatal("expected ebooks step flag to be set")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("unexpected content type: %q", ct)
	}

	audioFail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer audioFail.Close()
	cfg = s.settings.Get()
	cfg.Readarr.Audiobooks.BaseURL = audioFail.URL
	cfg.Readarr.Audiobooks.APIKey = "audio-key"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	stepFlags["raudio"] = true
	audioReq := httptest.NewRequest(http.MethodPost, "/setup/test-readarr?tag=audiobooks", nil)
	audioRec := httptest.NewRecorder()
	ui.handleTestReadarr(s)(audioRec, audioReq)

	body := audioRec.Body.String()
	if !strings.Contains(body, "Check the Base URL, API key, and TLS setting.") {
		t.Fatalf("expected friendly error body, got %q", body)
	}
	if strings.Contains(body, "audio-key") || strings.Contains(body, "boom") {
		t.Fatalf("expected error body to avoid sensitive details, got %q", body)
	}
	if stepFlags["raudio"] {
		t.Fatal("expected audiobook step flag to be cleared on failure")
	}
}

func TestReadarrProbeMessageIgnoresPortDigits(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "502 with port containing 401 is not misread as auth error",
			err:  fmt.Errorf("book lookup failed (HTTP 502 Bad Gateway) from http://127.0.0.1:40123/api/v1/book/lookup: boom"),
			want: "Could not connect to Readarr. Check the Base URL, API key, and TLS setting.",
		},
		{
			name: "502 with port containing 404 is not misread as unreachable",
			err:  fmt.Errorf("book lookup failed (HTTP 502 Bad Gateway) from http://127.0.0.1:40456/api/v1/book/lookup: boom"),
			want: "Could not connect to Readarr. Check the Base URL, API key, and TLS setting.",
		},
		{
			name: "genuine 401 is classified as auth error",
			err:  fmt.Errorf("book lookup failed (HTTP 401 Unauthorized) from http://127.0.0.1:55555/api/v1/book/lookup: bad key"),
			want: "Could not connect to Readarr. Check the API key.",
		},
		{
			name: "genuine 404 is classified as unreachable",
			err:  fmt.Errorf("book lookup failed (HTTP 404 Not Found) from http://127.0.0.1:55555/api/v1/book/lookup: nope"),
			want: "Could not connect to Readarr. Check the Base URL and that the server is reachable.",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := readarrProbeMessage(tc.err); got != tc.want {
				t.Fatalf("readarrProbeMessage = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHandleTestOAuthMissingFields(t *testing.T) {
	s := newServerForTest(t)
	ui := &setupUI{}
	req := httptest.NewRequest(http.MethodPost, "/setup/test-oauth", nil)
	rec := httptest.NewRecorder()

	ui.handleTestOAuth(s)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "missing issuer/client_id/redirect") {
		t.Fatalf("unexpected body: %q", rec.Body.String())
	}
	if rec.Header().Get("HX-Trigger") != "setup-saved" {
		t.Fatalf("unexpected trigger header: %q", rec.Header().Get("HX-Trigger"))
	}
}

func TestExtractIdentifiersVariants(t *testing.T) {
	book := providers.LookupBook{
		Identifiers: []map[string]any{
			nil,
			{"type": "isbn-10", "value": "1234567890"},
			{"type": "isbn13", "value": "9781234567897"},
			{"asin": "B00TEST"},
			{"type": "isbn10", "value": "ignored-second"},
		},
	}

	isbn10, isbn13, asin := extractIdentifiers(book)
	if isbn10 != "1234567890" || isbn13 != "9781234567897" || asin != "B00TEST" {
		t.Fatalf("unexpected identifiers: isbn10=%q isbn13=%q asin=%q", isbn10, isbn13, asin)
	}

	fallbackBook := providers.LookupBook{
		Identifiers: []map[string]any{{"isbn10": "alt10", "isbn13": "alt13", "asin": "altasin"}},
	}
	isbn10, isbn13, asin = extractIdentifiers(fallbackBook)
	if isbn10 != "alt10" || isbn13 != "alt13" || asin != "altasin" {
		t.Fatalf("unexpected fallback identifiers: isbn10=%q isbn13=%q asin=%q", isbn10, isbn13, asin)
	}
}
