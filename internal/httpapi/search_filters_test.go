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
	// Disable the rate limiter since HTTP transport is mocked and instant
	restore := providers.TestDisableOLRateLimiter()
	t.Cleanup(restore)

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

func TestBackfillOpenLibraryDiscoveryMetadataRequiresDescription(t *testing.T) {
	// Disable the rate limiter since HTTP transport is mocked and instant
	restore := providers.TestDisableOLRateLimiter()
	t.Cleanup(restore)

	prevTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "openlibrary.org" {
			return prevTransport.RoundTrip(r)
		}
		switch r.URL.Path {
		case "/works/OL-DESC.json":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"key":"/works/OL-DESC","description":"A real description.","covers":[445566]}`)),
				Header:     make(http.Header),
			}, nil
		case "/works/OL-NODESC.json":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"key":"/works/OL-NODESC","covers":[778899]}`)),
				Header:     make(http.Header),
			}, nil
		default:
			t.Fatalf("unexpected Open Library request: %s", r.URL.String())
			return nil, nil
		}
	})
	t.Cleanup(func() { http.DefaultTransport = prevTransport })

	books := []providers.BookItem{
		{
			Title:              "Needs Description",
			OpenLibraryWorkKey: "/works/OL-DESC",
		},
		{
			Title:              "No Description Available",
			OpenLibraryWorkKey: "/works/OL-NODESC",
			CoverMedium:        "https://covers.example/keep-me.jpg",
		},
		{
			Title:       "Already Ready",
			Description: "Already has a description.",
			CoverMedium: "https://covers.example/already-ready.jpg",
		},
	}

	got := backfillOpenLibraryDiscoveryMetadata(context.Background(), providers.NewOpenLibrary(), books, 3)
	if len(got) != 2 {
		t.Fatalf("expected only described books to remain, got %+v", got)
	}
	if got[0].Title != "Needs Description" || got[0].Description != "A real description." {
		t.Fatalf("expected first item to be backfilled, got %+v", got[0])
	}
	if got[0].CoverMedium != "https://covers.openlibrary.org/b/id/445566-M.jpg" {
		t.Fatalf("expected backfilled cover, got %+v", got[0])
	}
	if got[1].Title != "Already Ready" {
		t.Fatalf("expected existing rich item to survive, got %+v", got)
	}
}

func TestGatherDiscoveryCategoryBooksReplacesBlockedCandidates(t *testing.T) {
	// Disable the rate limiter since HTTP transport is mocked and instant
	restore := providers.TestDisableOLRateLimiter()
	t.Cleanup(restore)

	prevTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "openlibrary.org" {
			return prevTransport.RoundTrip(r)
		}
		switch {
		case r.URL.Path == "/search.json":
			switch r.URL.Query().Get("q") {
			case "primary":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{"docs":[
						{"title":"Blocked Pick","author_name":["A"],"first_publish_year":2024,"cover_i":1,"key":"/works/OL-BLOCKED"},
						{"title":"Pick 2","author_name":["B"],"first_publish_year":2024,"cover_i":2,"key":"/works/OL-2"},
						{"title":"Pick 3","author_name":["C"],"first_publish_year":2024,"cover_i":3,"key":"/works/OL-3"},
						{"title":"Pick 4","author_name":["D"],"first_publish_year":2024,"cover_i":4,"key":"/works/OL-4"},
						{"title":"Pick 5","author_name":["E"],"first_publish_year":2024,"cover_i":5,"key":"/works/OL-5"},
						{"title":"Pick 6","author_name":["F"],"first_publish_year":2024,"cover_i":6,"key":"/works/OL-6"},
						{"title":"Pick 7","author_name":["G"],"first_publish_year":2024,"cover_i":7,"key":"/works/OL-7"},
						{"title":"Pick 8","author_name":["H"],"first_publish_year":2024,"cover_i":8,"key":"/works/OL-8"}
					]}`)),
					Header: make(http.Header),
				}, nil
			case "backup":
				return &http.Response{
					StatusCode: http.StatusOK,
					Body: io.NopCloser(strings.NewReader(`{"docs":[
						{"title":"Replacement Pick","author_name":["I"],"first_publish_year":2024,"cover_i":9,"key":"/works/OL-9"}
					]}`)),
					Header: make(http.Header),
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"docs":[]}`)),
					Header:     make(http.Header),
				}, nil
			}
		case r.URL.Path == "/works/OL-BLOCKED.json":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"key":"/works/OL-BLOCKED","covers":[1001]}`)),
				Header:     make(http.Header),
			}, nil
		case strings.HasPrefix(r.URL.Path, "/works/OL-") && strings.HasSuffix(r.URL.Path, ".json"):
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"description":"A usable description.","covers":[1002]}`)),
				Header:     make(http.Header),
			}, nil
		default:
			t.Fatalf("unexpected Open Library request: %s", r.URL.String())
			return nil, nil
		}
	})
	t.Cleanup(func() { http.DefaultTransport = prevTransport })

	got := gatherDiscoveryCategoryBooks(context.Background(), providers.NewOpenLibrary(), discoveryQuery{
		Queries: []string{"primary", "backup"},
		MinYear: 2020,
	})
	if len(got) != discoveryCategorySize {
		t.Fatalf("expected full shelf of %d, got %d: %+v", discoveryCategorySize, len(got), got)
	}
	for _, book := range got {
		if book.Title == "Blocked Pick" {
			t.Fatalf("expected blocked book to be replaced: %+v", got)
		}
	}
	foundReplacement := false
	for _, book := range got {
		if book.Title == "Replacement Pick" {
			foundReplacement = true
			break
		}
	}
	if !foundReplacement {
		t.Fatalf("expected replacement pick to fill the shelf: %+v", got)
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
