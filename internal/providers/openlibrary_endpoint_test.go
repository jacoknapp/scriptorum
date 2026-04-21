package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// TestEndpointSearchURLFormat validates the search endpoint URL pattern:
// /search.json?q={query}&limit={limit}&page={page}
func TestEndpointSearchURLFormat(t *testing.T) {
	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		// Validate path
		if r.URL.Path != "/search.json" {
			t.Errorf("search path = %q, want /search.json", r.URL.Path)
		}
		// Validate query params
		if got := r.URL.Query().Get("q"); got != "test+query" {
			t.Errorf("q param = %q, want %q", got, "test+query")
		}
		if got := r.URL.Query().Get("limit"); got != "5" {
			t.Errorf("limit param = %q, want 5", got)
		}
		if got := r.URL.Query().Get("page"); got != "3" {
			t.Errorf("page param = %q, want 3", got)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"docs":[]}`)), Header: make(http.Header)}, nil
	})
	_, _ = ol.Search(context.Background(), "test+query", 5, 3)
}

// TestEndpointSearchPage1OmitsPageParam verifies page=1 does not append &page=
func TestEndpointSearchPage1OmitsPageParam(t *testing.T) {
	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("page") != "" {
			t.Errorf("page param should be omitted for page=1, got %q", r.URL.Query().Get("page"))
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"docs":[]}`)), Header: make(http.Header)}, nil
	})
	_, _ = ol.Search(context.Background(), "x", 10, 1)
}

func TestEndpointSearchWithLanguagesAddsLanguageParams(t *testing.T) {
	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		langs := r.URL.Query()["language"]
		if len(langs) != 2 {
			t.Fatalf("language params = %+v, want 2 entries", langs)
		}
		if langs[0] != "eng" || langs[1] != "spa" {
			t.Fatalf("language params = %+v, want [eng spa]", langs)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"docs":[{"title":"A","language":["eng"]},{"title":"B","language":["ger"]},{"title":"C"}]}`)), Header: make(http.Header)}, nil
	})
	items, err := ol.SearchWithLanguages(context.Background(), "x", 10, 1, []string{"spa", "eng"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items (eng + unknown), got %d", len(items))
	}
}

// TestEndpointTrendingURLFormat validates the trending endpoint:
// /trending/{period}.json?limit={limit}
// Valid periods per OL source: now, daily, weekly, monthly, yearly, forever
func TestEndpointTrendingURLFormat(t *testing.T) {
	periods := []struct {
		input    string
		wantPath string
	}{
		{"daily", "/trending/daily.json"},
		{"weekly", "/trending/weekly.json"},
		{"monthly", "/trending/monthly.json"},
		{"yearly", "/trending/yearly.json"},
		{"forever", "/trending/forever.json"},
		// Invalid periods default to weekly
		{"bad", "/trending/weekly.json"},
		{"all", "/trending/weekly.json"}, // "all" is not valid in OL; should default to weekly
		{"", "/trending/weekly.json"},
	}
	for _, tc := range periods {
		t.Run("period_"+tc.input, func(t *testing.T) {
			ol := NewOpenLibrary()
			ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != tc.wantPath {
					t.Errorf("input=%q: path = %q, want %q", tc.input, r.URL.Path, tc.wantPath)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"works":[]}`)), Header: make(http.Header)}, nil
			})
			_, _ = ol.TrendingWorks(context.Background(), tc.input, 5)
		})
	}
}

// TestEndpointSubjectURLFormat validates the subjects endpoint:
// /subjects/{subject}.json?limit={limit}
func TestEndpointSubjectURLFormat(t *testing.T) {
	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/subjects/science_fiction.json" {
			t.Errorf("subject path = %q, want /subjects/science_fiction.json", r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "8" {
			t.Errorf("limit = %q, want 8", got)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"works":[]}`)), Header: make(http.Header)}, nil
	})
	_, _ = ol.SubjectWorks(context.Background(), "science_fiction", 8)
}

// TestEndpointWorkDetailsURLFormat validates the work details endpoint:
// /{workKey}.json
func TestEndpointWorkDetailsURLFormat(t *testing.T) {
	cases := []struct {
		input    string
		wantPath string
	}{
		{"/works/OL45804W", "/works/OL45804W.json"},
		{"works/OL45804W", "/works/OL45804W.json"}, // missing leading slash
		{"/works/OL12345W", "/works/OL12345W.json"},
	}
	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			ol := NewOpenLibrary()
			ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != tc.wantPath {
					t.Errorf("input=%q: path = %q, want %q", tc.input, r.URL.Path, tc.wantPath)
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
			})
			_, _ = ol.WorkDetails(context.Background(), tc.input)
		})
	}
}

// TestEndpointCoverURLConstruction validates cover URL patterns:
// https://covers.openlibrary.org/b/id/{coverID}-M.jpg
// https://covers.openlibrary.org/b/olid/{editionKey}-M.jpg
func TestEndpointCoverURLConstruction(t *testing.T) {
	cases := []struct {
		name            string
		coverID         int
		coverEditionKey string
		want            string
	}{
		{"cover by numeric ID", 12345, "", "https://covers.openlibrary.org/b/id/12345-M.jpg"},
		{"cover by OLID", 0, "OL7353617M", "https://covers.openlibrary.org/b/olid/OL7353617M-M.jpg"},
		{"numeric ID takes priority over OLID", 99999, "OL123M", "https://covers.openlibrary.org/b/id/99999-M.jpg"},
		{"no cover data returns empty", 0, "", ""},
		{"normalized edition key path", 0, "/books/OL123M", "https://covers.openlibrary.org/b/olid/OL123M-M.jpg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := openLibraryCoverURL(tc.coverID, tc.coverEditionKey)
			if got != tc.want {
				t.Errorf("openLibraryCoverURL(%d, %q) = %q, want %q", tc.coverID, tc.coverEditionKey, got, tc.want)
			}
		})
	}
}

// TestSearchResponseISBNSplitting verifies that the mixed "isbn" field from
// OL search is correctly split into ISBN-10 and ISBN-13.
func TestSearchResponseISBNSplitting(t *testing.T) {
	cases := []struct {
		name    string
		body    string
		wantI10 string
		wantI13 string
	}{
		{
			name:    "mixed ISBNs (10-digit first)",
			body:    `{"docs":[{"title":"Book","isbn":["1529157466","9781529157468"]}]}`,
			wantI10: "1529157466",
			wantI13: "9781529157468",
		},
		{
			name:    "mixed ISBNs (13-digit first)",
			body:    `{"docs":[{"title":"Book","isbn":["9781529157468","1529157466"]}]}`,
			wantI10: "1529157466",
			wantI13: "9781529157468",
		},
		{
			name:    "only ISBN-13",
			body:    `{"docs":[{"title":"Book","isbn":["9781529157468"]}]}`,
			wantI10: "",
			wantI13: "9781529157468",
		},
		{
			name:    "only ISBN-10",
			body:    `{"docs":[{"title":"Book","isbn":["1529157466"]}]}`,
			wantI10: "1529157466",
			wantI13: "",
		},
		{
			name:    "no isbn field",
			body:    `{"docs":[{"title":"Book"}]}`,
			wantI10: "",
			wantI13: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ol := NewOpenLibrary()
			ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(tc.body)), Header: make(http.Header)}, nil
			})
			items, err := ol.Search(context.Background(), "x", 1, 1)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(items) == 0 {
				t.Fatal("expected at least 1 item")
			}
			if items[0].ISBN10 != tc.wantI10 {
				t.Errorf("ISBN10 = %q, want %q", items[0].ISBN10, tc.wantI10)
			}
			if items[0].ISBN13 != tc.wantI13 {
				t.Errorf("ISBN13 = %q, want %q", items[0].ISBN13, tc.wantI13)
			}
		})
	}
}

// TestSplitISBNs unit-tests the ISBN splitter directly.
func TestSplitISBNs(t *testing.T) {
	cases := []struct {
		in     []string
		want10 string
		want13 string
	}{
		{nil, "", ""},
		{[]string{}, "", ""},
		{[]string{"1234567890"}, "1234567890", ""},
		{[]string{"1234567890123"}, "", "1234567890123"},
		{[]string{"1234567890123", "1234567890"}, "1234567890", "1234567890123"},
		{[]string{"", "  ", "1234567890"}, "1234567890", ""},
		// Non-standard lengths are ignored
		{[]string{"12345"}, "", ""},
		{[]string{"123456789012345"}, "", ""},
	}
	for _, tc := range cases {
		got10, got13 := splitISBNs(tc.in)
		if got10 != tc.want10 || got13 != tc.want13 {
			t.Errorf("splitISBNs(%v) = (%q, %q), want (%q, %q)", tc.in, got10, got13, tc.want10, tc.want13)
		}
	}
}

// TestEndpointHTTPErrorHandling validates that HTTP >= 400 responses are returned as errors.
func TestEndpointHTTPErrorHandling(t *testing.T) {
	statuses := []int{400, 403, 404, 429, 500, 503}
	for _, code := range statuses {
		t.Run("status_"+http.StatusText(code), func(t *testing.T) {
			ol := NewOpenLibrary()
			ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: code,
					Status:     http.StatusText(code),
					Body:       io.NopCloser(strings.NewReader("error body")),
					Header:     make(http.Header),
				}, nil
			})
			_, err := ol.Search(context.Background(), "test", 5, 1)
			if err == nil {
				t.Errorf("expected error for HTTP %d, got nil", code)
			}
			if !strings.Contains(err.Error(), "HTTP") {
				t.Errorf("error should mention HTTP status: %v", err)
			}
		})
	}
}

// TestEndpointDescriptionParsing validates both string and object description formats.
func TestEndpointDescriptionParsing(t *testing.T) {
	cases := []struct {
		name     string
		descJSON string
		wantDesc string
	}{
		{"plain string", `"A plain description."`, "A plain description."},
		{"object with value", `{"type":"/type/text","value":"An object description."}`, "An object description."},
		{"empty string", `""`, ""},
		{"null", `null`, ""},
		{"missing field", ``, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			descField := ""
			if tc.descJSON != "" {
				descField = `,"description":` + tc.descJSON
			}
			body := `{"key":"/works/OL1W","title":"Test"` + descField + `}`
			ol := NewOpenLibrary()
			ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
			})
			details, err := ol.WorkDetails(context.Background(), "/works/OL1W")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if details == nil {
				t.Fatal("expected non-nil details")
			}
			if details.Description != tc.wantDesc {
				t.Errorf("description = %q, want %q", details.Description, tc.wantDesc)
			}
		})
	}
}

// TestEndpointSubjectResponseFieldMapping validates that the subject endpoint
// response uses different field names than search (cover_id vs cover_i,
// authors[].name vs author_name).
func TestEndpointSubjectResponseFieldMapping(t *testing.T) {
	body := `{
		"works": [{
			"title": "The Hobbit",
			"authors": [{"name": "J.R.R. Tolkien"}, {"name": ""}],
			"cover_id": 9876,
			"cover_edition_key": "OL123M",
			"key": "/works/OL51984W"
		}]
	}`
	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	items, err := ol.SubjectWorks(context.Background(), "fantasy", 6)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 item, got %d", len(items))
	}
	item := items[0]
	if item.Title != "The Hobbit" {
		t.Errorf("title = %q", item.Title)
	}
	// Verify empty author names are filtered out
	if len(item.Authors) != 1 || item.Authors[0] != "J.R.R. Tolkien" {
		t.Errorf("authors = %v, want [J.R.R. Tolkien]", item.Authors)
	}
	// cover_id (not cover_i) should produce a valid cover URL
	if item.CoverMedium != "https://covers.openlibrary.org/b/id/9876-M.jpg" {
		t.Errorf("cover = %q", item.CoverMedium)
	}
	if item.OpenLibraryWorkKey != "/works/OL51984W" {
		t.Errorf("work key = %q", item.OpenLibraryWorkKey)
	}
}
