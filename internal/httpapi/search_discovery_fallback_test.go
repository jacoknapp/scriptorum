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

	prevTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "openlibrary.org" {
			return prevTransport.RoundTrip(r)
		}
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
				Body: io.NopCloser(strings.NewReader(`{"works":[
					{"title":"Fallback Fantasy","authors":[{"name":"A"}],"cover_id":12345,"key":"/works/OL-FALLBACK-1"}
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
	})
	t.Cleanup(func() { http.DefaultTransport = prevTransport })

	ui := &searchUI{}
	categories := ui.loadDiscoveryCategories(context.Background())
	if len(categories) == 0 {
		t.Fatalf("expected fallback categories, got none")
	}
	if categories[0].Name != "Fantasy Spotlight" {
		t.Fatalf("expected subject fallback category, got %+v", categories[0])
	}
	if len(categories[0].Items) == 0 || categories[0].Items[0].Title != "Fallback Fantasy" {
		t.Fatalf("expected fallback item, got %+v", categories[0].Items)
	}
}
