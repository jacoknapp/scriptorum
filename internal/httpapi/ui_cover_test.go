package httpapi

import (
	"encoding/json"
	"net/url"
	"strings"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/db"
)

func configureReadarrForCoverTests(t *testing.T, s *Server, baseURL string) {
	t.Helper()
	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = baseURL
	cfg.Readarr.Ebooks.APIKey = "test-key"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}
}

func TestNormalizeRequestCoverRewritesReadarrMediaHost(t *testing.T) {
	s := makeTestServer(t)
	configureReadarrForCoverTests(t, s, "https://readarr.example.internal")

	got := s.normalizeRequestCover("ebook", "http://localhost:8787/MediaCover/12.jpg?lastWrite=123")
	if !strings.HasPrefix(got, "/ui/readarr-cover?u=") {
		t.Fatalf("expected proxied cover URL, got %q", got)
	}

	q, err := url.ParseQuery(strings.TrimPrefix(got, "/ui/readarr-cover?"))
	if err != nil {
		t.Fatalf("parse proxy query: %v", err)
	}
	if q.Get("u") != "https://readarr.example.internal/MediaCover/12.jpg?lastWrite=123" {
		t.Fatalf("unexpected rewritten cover URL: %q", q.Get("u"))
	}
}

func TestNormalizeRequestCoverKeepsExternalCover(t *testing.T) {
	s := makeTestServer(t)
	configureReadarrForCoverTests(t, s, "https://readarr.example.internal")

	cover := "https://covers.openlibrary.org/b/id/11200092-M.jpg"
	if got := s.normalizeRequestCover("ebook", cover); got != cover {
		t.Fatalf("expected external cover unchanged, got %q", got)
	}
}

func TestNormalizeRequestCoverRelativePathUsesProxy(t *testing.T) {
	s := makeTestServer(t)
	configureReadarrForCoverTests(t, s, "http://readarr.example.internal:8787")

	got := s.normalizeRequestCover("ebook", "/MediaCover/42.jpg")
	if !strings.HasPrefix(got, "/ui/readarr-cover?u=") {
		t.Fatalf("expected proxied cover URL, got %q", got)
	}

	q, err := url.ParseQuery(strings.TrimPrefix(got, "/ui/readarr-cover?"))
	if err != nil {
		t.Fatalf("parse proxy query: %v", err)
	}
	if q.Get("u") != "http://readarr.example.internal:8787/MediaCover/42.jpg" {
		t.Fatalf("unexpected proxied cover URL: %q", q.Get("u"))
	}
}

func TestRequestListCoverDataNormalizesStoredReadarrPath(t *testing.T) {
	s := makeTestServer(t)
	configureReadarrForCoverTests(t, s, "http://readarr.example.internal:8787")

	got := s.requestListCoverData(db.Request{
		Format:   "ebook",
		CoverURL: "/MediaCover/42.jpg",
	}, nil)
	if !strings.HasPrefix(got, "/ui/readarr-cover?u=") {
		t.Fatalf("expected proxied stored cover URL, got %q", got)
	}

	q, err := url.ParseQuery(strings.TrimPrefix(got, "/ui/readarr-cover?"))
	if err != nil {
		t.Fatalf("parse proxy query: %v", err)
	}
	if q.Get("u") != "http://readarr.example.internal:8787/MediaCover/42.jpg" {
		t.Fatalf("unexpected proxied stored cover URL: %q", q.Get("u"))
	}
}

func TestRequestListCoverDataFallsBackToStoredRequestPayload(t *testing.T) {
	s := makeTestServer(t)
	configureReadarrForCoverTests(t, s, "https://readarr.example.internal")

	raw, err := json.Marshal(map[string]any{
		"images": []map[string]any{{
			"coverType": "cover",
			"url":       "http://localhost:8787/MediaCover/88.jpg?lastWrite=7",
		}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	got := s.requestListCoverData(db.Request{
		Format:     "ebook",
		ReadarrReq: raw,
	}, nil)
	if !strings.HasPrefix(got, "/ui/readarr-cover?u=") {
		t.Fatalf("expected proxied payload cover URL, got %q", got)
	}

	q, err := url.ParseQuery(strings.TrimPrefix(got, "/ui/readarr-cover?"))
	if err != nil {
		t.Fatalf("parse proxy query: %v", err)
	}
	if q.Get("u") != "https://readarr.example.internal/MediaCover/88.jpg?lastWrite=7" {
		t.Fatalf("unexpected proxied payload cover URL: %q", q.Get("u"))
	}
}
