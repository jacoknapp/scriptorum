package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestOpenLibraryEmptyQuery(t *testing.T) {
	ol := NewOpenLibrary()
	items, err := ol.Search(context.Background(), "", 10, 1)
	if err != nil || items != nil {
		t.Fatalf("expected nil,nil got %v,%v", items, err)
	}
}

func TestOpenLibraryEmptySubject(t *testing.T) {
	ol := NewOpenLibrary()
	items, err := ol.SubjectWorks(context.Background(), "", 4)
	if err != nil || items != nil {
		t.Fatalf("expected nil,nil got %v,%v", items, err)
	}
}

func TestOpenLibraryTrendingDefaultsPeriod(t *testing.T) {
	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/trending/weekly.json" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"works":[]}`)), Header: make(http.Header)}, nil
	})
	if _, err := ol.TrendingWorks(context.Background(), "not-a-real-period", 3); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestOpenLibraryHTTPError(t *testing.T) {
	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})
	_, err := ol.Search(context.Background(), "x", 10, 1)
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestOpenLibraryCoverURLNormalizesBookPathEditionKey(t *testing.T) {
	got := openLibraryCoverURL(0, "/books/OL36647151M")
	want := "https://covers.openlibrary.org/b/olid/OL36647151M-M.jpg"
	if got != want {
		t.Fatalf("expected normalized cover URL %q, got %q", want, got)
	}
}

func TestOpenLibrarySetsAPIHeaders(t *testing.T) {
	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("unexpected Accept header: %q", got)
		}
		if got := r.Header.Get("User-Agent"); strings.TrimSpace(got) == "" {
			t.Fatal("expected non-empty User-Agent header")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"docs":[]}`)), Header: make(http.Header)}, nil
	})
	if _, err := ol.Search(context.Background(), "x", 5, 1); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestOpenLibraryUserAgentEnvOverride(t *testing.T) {
	t.Setenv("OPENLIBRARY_USER_AGENT", "Scriptorum-Test/1.0 (+mailto:test@example.com)")
	if got := openLibraryUserAgent(); got != "Scriptorum-Test/1.0 (+mailto:test@example.com)" {
		t.Fatalf("unexpected env user-agent: %q", got)
	}
	_ = os.Unsetenv("OPENLIBRARY_USER_AGENT")
}
