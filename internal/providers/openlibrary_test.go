package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOpenLibrarySearch(t *testing.T) {
	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"docs":[{"title":"Project Hail Mary","author_name":["Andy Weir"],"isbn":["1529157466"],"isbn13":["9781529157468"],"cover_i":12345,"first_publish_year":2021}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	items, err := ol.Search(context.Background(), "project hail mary", 10, 1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 got %d", len(items))
	}
	if items[0].Title == "" || items[0].ISBN13 == "" || items[0].FirstPublishYear != 2021 {
		t.Fatalf("missing fields: %+v", items[0])
	}
}

func TestOpenLibraryTrendingWorks(t *testing.T) {
	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/trending/weekly.json" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "6" {
			t.Fatalf("limit query = %s", got)
		}
		body := `{"works":[{"title":"Atomic Habits","author_name":["James Clear"],"cover_i":12539702,"first_publish_year":2016},{"title":"Project Hail Mary","author_name":["Andy Weir"],"cover_edition_key":"OL36647151M","first_publish_year":2021}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	items, err := ol.TrendingWorks(context.Background(), "weekly", 6)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 got %d", len(items))
	}
	if items[0].Title != "Atomic Habits" || items[0].FirstPublishYear != 2016 {
		t.Fatalf("unexpected first item: %+v", items[0])
	}
	if items[1].CoverMedium != "https://covers.openlibrary.org/b/olid/OL36647151M-M.jpg" {
		t.Fatalf("unexpected second item: %+v", items[1])
	}
}

func TestOpenLibrarySubjectWorks(t *testing.T) {
	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/subjects/fantasy.json" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "4" {
			t.Fatalf("limit query = %s", got)
		}
		body := `{"works":[{"title":"The Hobbit","authors":[{"name":"J.R.R. Tolkien"}],"cover_id":9876},{"title":"A Wizard of Earthsea","authors":[{"name":"Ursula K. Le Guin"}],"cover_edition_key":"OL123M"}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	items, err := ol.SubjectWorks(context.Background(), "fantasy", 4)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2 got %d", len(items))
	}
	if items[0].Title != "The Hobbit" || items[0].CoverMedium == "" {
		t.Fatalf("unexpected first item: %+v", items[0])
	}
	if items[1].CoverMedium != "https://covers.openlibrary.org/b/olid/OL123M-M.jpg" {
		t.Fatalf("unexpected edition cover: %+v", items[1])
	}
}
