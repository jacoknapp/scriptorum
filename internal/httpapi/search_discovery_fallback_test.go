package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

func TestLoadDiscoveryCategoriesFallsBackToSubjects(t *testing.T) {
	// Disable the rate limiter since HTTP transport is mocked and instant
	restore := providers.TestDisableOLRateLimiter()
	t.Cleanup(restore)

	installOpenLibraryTestClient(t, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Path == "/search.json":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"docs":[]}`)),
				Header:     make(http.Header),
			}, nil
		case r.URL.Path == "/subjects/fantasy.json":
			return &http.Response{
				StatusCode: http.StatusOK,
				// first_publish_year must be recent (>= minYear) to pass the strict year filter
				Body: io.NopCloser(strings.NewReader(`{"works":[
					{"title":"Fallback Fantasy","authors":[{"name":"A"}],"cover_id":12345,"key":"/works/OL-FALLBACK-1","first_publish_year":2020}
				]}`)),
				Header: make(http.Header),
			}, nil
		case strings.HasPrefix(r.URL.Path, "/subjects/"):
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"works":[]}`)),
				Header:     make(http.Header),
			}, nil
		case r.URL.Path == "/works/OL-FALLBACK-1.json":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"description":"Fallback description","covers":[12345]}`)),
				Header:     make(http.Header),
			}, nil
		default:
			t.Fatalf("unexpected Open Library request: %s", r.URL.String())
			return nil, nil
		}
	}))

	ui := &searchUI{}
	categories := ui.loadDiscoveryCategories(context.Background())
	if len(categories) == 0 {
		t.Fatalf("expected fallback categories, got none")
	}
	// With SubjectFallbacks now wired into gatherDiscoveryCategoryBooks, the
	// "fantasy" subject fills the "Fantasy Hits" category directly — the
	// "Fantasy Spotlight" sentinel path is no longer reached.
	if categories[0].Name != "Fantasy Hits" {
		t.Fatalf("expected Fantasy Hits category via subject fallback, got %+v", categories[0])
	}
	if len(categories[0].Items) == 0 || categories[0].Items[0].Title != "Fallback Fantasy" {
		t.Fatalf("expected fallback item, got %+v", categories[0].Items)
	}
}
