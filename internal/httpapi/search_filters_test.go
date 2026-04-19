package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

func TestIsRenderableSearchBookRejectsCompilationsAndGuides(t *testing.T) {
	titles := []string{
		"The Murderbot Diaries: Books 1-3",
		"The Murderbot Diaries: Volumes 1-3",
		"Project Hail Mary Omnibus",
		"Project Hail Mary Box Set",
		"Project Hail Mary Boxset",
		"Project Hail Mary Boxed Sets",
		"Project Hail Mary 3-Book Collection",
		"Project Hail Mary Study Guide",
		"Project Hail Mary Activity Book",
		"Project Hail Mary Guided Journal",
		"Project Hail Mary Crossword Book",
		"Project Hail Mary 3-in-1",
	}

	for _, title := range titles {
		if isRenderableSearchBook(title) {
			t.Fatalf("expected %q to be filtered out", title)
		}
	}
}

func TestIsDiscoveryCandidateRejectsCookbooksAndDecks(t *testing.T) {
	titles := []string{
		"The Romantasy Cookbook",
		"Fourth Wing Recipe Collection",
		"Shadow Daddy Oracle Deck",
		"Dragon Rider Tarot Deck",
	}

	for _, title := range titles {
		if isDiscoveryCandidate(providers.BookItem{Title: title}) {
			t.Fatalf("expected %q to stay out of discovery", title)
		}
	}
}

func TestIsRenderableSearchBookKeepsNormalBooks(t *testing.T) {
	titles := []string{
		"Project Hail Mary",
		"All Systems Red",
		"Volume Control",
		"Funny Story",
	}

	for _, title := range titles {
		if !isRenderableSearchBook(title) {
			t.Fatalf("expected %q to remain renderable", title)
		}
	}
}

func TestBackfillOpenLibraryWorkCoversUsesWorkDetails(t *testing.T) {
	prevTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "openlibrary.org" {
			return prevTransport.RoundTrip(r)
		}
		if r.URL.Path != "/works/OL1W.json" {
			t.Fatalf("unexpected Open Library request: %s", r.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"key":"/works/OL1W","title":"Funny Story","covers":[112233]}`)),
			Header:     make(http.Header),
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = prevTransport })

	books := []providers.BookItem{{
		Title:              "Funny Story",
		OpenLibraryWorkKey: "/works/OL1W",
	}}
	got := backfillOpenLibraryWorkCovers(context.Background(), providers.NewOpenLibrary(), books)
	if got[0].CoverMedium != "https://covers.openlibrary.org/b/id/112233-M.jpg" {
		t.Fatalf("expected cover from work details, got %+v", got[0])
	}
	if got[0].CoverSmall != "https://covers.openlibrary.org/b/id/112233-M.jpg" {
		t.Fatalf("expected small cover from work details, got %+v", got[0])
	}
}

func TestSearchUIFiltersBadReadarrResultsAndNormalizesCover(t *testing.T) {
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/book/lookup" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{
				"title":"Project Hail Mary",
				"foreignBookId":"fb-1",
				"foreignEditionId":"fe-1",
				"author":{"name":"Andy Weir"},
				"remoteCover":"http://localhost:8787/MediaCover/12.jpg?lastWrite=123"
			},
			{
				"title":"The Murderbot Diaries: Books 1-3",
				"foreignBookId":"fb-2",
				"foreignEditionId":"fe-2",
				"author":{"name":"Martha Wells"}
			},
			{
				"title":"Project Hail Mary Study Guide",
				"foreignBookId":"fb-3",
				"foreignEditionId":"fe-3",
				"author":{"name":"Test Author"}
			}
		]`)
	}))
	defer readarr.Close()

	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = readarr.URL
	cfg.Readarr.Ebooks.APIKey = "test-key"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui/search?q=project", nil)
	req.AddCookie(makeCookie(t, s, "user", false))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Project Hail Mary") {
		t.Fatalf("expected good title in body: %s", body)
	}
	for _, unwanted := range []string{
		"The Murderbot Diaries: Books 1-3",
		"Project Hail Mary Study Guide",
	} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("expected %q to be filtered out: %s", unwanted, body)
		}
	}
	if !strings.Contains(body, "/ui/readarr-cover?u=") {
		t.Fatalf("expected proxied cover url in body: %s", body)
	}
}
