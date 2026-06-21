package httpapi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

func TestDiscoveryProbeErrorSuccess(t *testing.T) {
	restoreLimiter := providers.TestDisableOLRateLimiter()
	defer restoreLimiter()
	restoreClient := providers.TestSetOpenLibraryHTTPClientFactory(func() *http.Client {
		return &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Body:       io.NopCloser(strings.NewReader(`{"works":[]}`)),
				Header:     make(http.Header),
			}, nil
		})}
	})
	defer restoreClient()

	if got := discoveryProbeError(context.Background()); got != "" {
		t.Fatalf("expected no probe error on success, got %q", got)
	}
}

func TestDiscoveryProbeErrorFailure(t *testing.T) {
	restoreLimiter := providers.TestDisableOLRateLimiter()
	defer restoreLimiter()
	restoreClient := providers.TestSetOpenLibraryHTTPClientFactory(func() *http.Client {
		return &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 503,
				Body:       io.NopCloser(strings.NewReader(`service unavailable`)),
				Header:     make(http.Header),
			}, nil
		})}
	})
	defer restoreClient()

	got := discoveryProbeError(context.Background())
	if got == "" {
		t.Fatal("expected a probe error message on failure")
	}
}

func TestDiscoveryProbeErrorTruncatesLongMessages(t *testing.T) {
	restoreLimiter := providers.TestDisableOLRateLimiter()
	defer restoreLimiter()
	longBody := strings.Repeat("x", 500)
	restoreClient := providers.TestSetOpenLibraryHTTPClientFactory(func() *http.Client {
		return &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 500,
				Body:       io.NopCloser(strings.NewReader(longBody)),
				Header:     make(http.Header),
			}, nil
		})}
	})
	defer restoreClient()

	got := discoveryProbeError(context.Background())
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected long error message to be truncated with ellipsis, got %q", got)
	}
	if len(got) > 223 {
		t.Fatalf("expected truncated message to be capped near 220 chars, got len=%d", len(got))
	}
}

func TestLoadFallbackSubjectCategoriesNilClient(t *testing.T) {
	if got := loadFallbackSubjectCategories(context.Background(), nil); got != nil {
		t.Fatalf("expected nil result for nil OpenLibrary client, got %+v", got)
	}
}

func TestLoadFallbackSubjectCategoriesSuccess(t *testing.T) {
	restoreLimiter := providers.TestDisableOLRateLimiter()
	defer restoreLimiter()
	restoreClient := providers.TestSetOpenLibraryHTTPClientFactory(func() *http.Client {
		return &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			switch {
			case strings.Contains(r.URL.Path, "/subjects/"):
				body := `{"works":[{"key":"/works/OL1W","title":"Fantasy Book","cover_id":1,"cover_edition_key":"OL1M","first_publish_year":2020,"authors":[{"name":"Author One"}]}]}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			case strings.Contains(r.URL.Path, "/works/"):
				body := `{"description":"An enchanting fantasy tale."}`
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			default:
				return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("not found")), Header: make(http.Header)}, nil
			}
		})}
	})
	defer restoreClient()

	got := loadFallbackSubjectCategories(context.Background(), providers.NewOpenLibrary())
	if len(got) != 1 {
		t.Fatalf("expected 1 fallback category, got %+v", got)
	}
	if got[0].Name != "Fantasy Spotlight" {
		t.Fatalf("unexpected category name: %q", got[0].Name)
	}
	if len(got[0].Items) != 1 || got[0].Items[0].Title != "Fantasy Book" {
		t.Fatalf("unexpected category items: %+v", got[0].Items)
	}
}

func TestLoadFallbackSubjectCategoriesEmptyOnNoMetadata(t *testing.T) {
	restoreLimiter := providers.TestDisableOLRateLimiter()
	defer restoreLimiter()
	restoreClient := providers.TestSetOpenLibraryHTTPClientFactory(func() *http.Client {
		return &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
			// No works at all means SubjectWorks returns an empty list.
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"works":[]}`)), Header: make(http.Header)}, nil
		})}
	})
	defer restoreClient()

	got := loadFallbackSubjectCategories(context.Background(), providers.NewOpenLibrary())
	if got != nil {
		t.Fatalf("expected nil result when subject lookup returns no works, got %+v", got)
	}
}
