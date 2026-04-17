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
		body := `{"docs":[{"title":"Project Hail Mary","author_name":["Andy Weir"],"isbn":["1529157466"],"isbn13":["9781529157468"],"cover_i":12345,"cover_edition_key":"OL32553390M","first_publish_year":2021,"key":"/works/OL21745884W"}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	items, err := ol.Search(context.Background(), "project hail mary", 10, 1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 got %d", len(items))
	}
	if items[0].Title == "" || items[0].ISBN13 == "" || items[0].FirstPublishYear != 2021 || items[0].OpenLibraryWorkKey != "/works/OL21745884W" || items[0].OpenLibraryEditionKey != "OL32553390M" {
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
		body := `{"works":[{"title":"Atomic Habits","author_name":["James Clear"],"cover_i":12539702,"first_publish_year":2016,"key":"/works/OL17930368W"},{"title":"Project Hail Mary","author_name":["Andy Weir"],"cover_edition_key":"OL36647151M","first_publish_year":2021,"key":"/works/OL21745884W"}]}`
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
	if items[1].CoverMedium != "https://covers.openlibrary.org/b/olid/OL36647151M-M.jpg" || items[1].OpenLibraryWorkKey != "/works/OL21745884W" {
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
		body := `{"works":[{"title":"The Hobbit","authors":[{"name":"J.R.R. Tolkien"}],"cover_id":9876,"key":"/works/OL51984W"},{"title":"A Wizard of Earthsea","authors":[{"name":"Ursula K. Le Guin"}],"cover_edition_key":"OL123M","key":"/works/OL138822W"}]}`
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

func TestOpenLibraryWorkDetails(t *testing.T) {
	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/works/OL21745884W.json" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body := `{"key":"/works/OL21745884W","title":"Project Hail Mary","description":{"value":"A lone astronaut must save humanity."},"subjects":["Science fiction","Space"],"covers":[11200092],"first_publish_date":"2021-05-04"}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	details, err := ol.WorkDetails(context.Background(), "/works/OL21745884W")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if details == nil || details.Description != "A lone astronaut must save humanity." || details.FirstPublishDate != "2021-05-04" || len(details.Subjects) != 2 {
		t.Fatalf("unexpected details: %+v", details)
	}
	if details.CoverMedium != "https://covers.openlibrary.org/b/id/11200092-M.jpg" {
		t.Fatalf("unexpected cover: %+v", details)
	}
}
