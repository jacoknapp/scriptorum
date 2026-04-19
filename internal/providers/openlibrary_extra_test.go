package providers

import (
	"context"
	"errors"
	"io"
	"net/http"
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
