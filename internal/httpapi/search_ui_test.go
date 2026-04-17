package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func installOpenLibraryDiscoveryTransport(t *testing.T) {
	prevTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "openlibrary.org" {
			return prevTransport.RoundTrip(r)
		}

		bodyByPath := map[string]string{
			"/trending/weekly.json": `{"works":[
				{"title":"Project Hail Mary","author_name":["Andy Weir"],"cover_i":1,"first_publish_year":2021},
				{"title":"Funny Story","author_name":["Emily Henry"],"cover_i":2,"first_publish_year":2024},
				{"title":"Fourth Wing","author_name":["Rebecca Yarros"],"cover_i":3,"first_publish_year":2023}
			]}`,
		}
		if body, ok := bodyByPath[r.URL.Path]; ok {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}
		if r.URL.Path == "/search.json" {
			queryBodies := map[string]string{
				"romantasy":              `{"docs":[{"title":"Assistant to the Villain","author_name":["Hannah Nicole Maehrer"],"cover_i":10,"first_publish_year":2023},{"title":"The Serpent & the Wings of Night","author_name":["Carissa Broadbent"],"cover_i":11,"first_publish_year":2022}]}`,
				"dragon fantasy":         `{"docs":[{"title":"When the Moon Hatched","author_name":["Sarah A. Parker"],"cover_i":18,"first_publish_year":2024},{"title":"A Fate Inked in Blood","author_name":["Danielle L. Jensen"],"cover_i":19,"first_publish_year":2024}]}`,
				"psychological thriller": `{"docs":[{"title":"Never Lie","author_name":["Freida McFadden"],"cover_i":12,"first_publish_year":2022},{"title":"The Housemaid","author_name":["Freida McFadden"],"cover_i":13,"first_publish_year":2022}]}`,
				"freida mcfadden":        `{"docs":[{"title":"The Crash","author_name":["Freida McFadden"],"cover_i":20,"first_publish_year":2025},{"title":"Ward D","author_name":["Freida McFadden"],"cover_i":21,"first_publish_year":2023}]}`,
				"emily henry":            `{"docs":[{"title":"Funny Story","author_name":["Emily Henry"],"cover_i":14,"first_publish_year":2024},{"title":"Happy Place","author_name":["Emily Henry"],"cover_i":15,"first_publish_year":2023}]}`,
				"ali hazelwood":          `{"docs":[{"title":"Bride","author_name":["Ali Hazelwood"],"cover_i":22,"first_publish_year":2024},{"title":"Not in Love","author_name":["Ali Hazelwood"],"cover_i":23,"first_publish_year":2024}]}`,
				"murderbot":              `{"docs":[{"title":"System Collapse","author_name":["Martha Wells"],"cover_i":16,"first_publish_year":2023},{"title":"Network Effect","author_name":["Martha Wells"],"cover_i":17,"first_publish_year":2020}]}`,
				"space opera":            `{"docs":[{"title":"Some Desperate Glory","author_name":["Emily Tesh"],"cover_i":24,"first_publish_year":2023},{"title":"The Mercy of Gods","author_name":["James S. A. Corey"],"cover_i":25,"first_publish_year":2024}]}`,
			}
			body, ok := queryBodies[r.URL.Query().Get("q")]
			if !ok {
				t.Fatalf("unexpected search query: %s", r.URL.String())
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}
		t.Fatalf("unexpected Open Library request: %s", r.URL.String())
		return nil, nil
	})
	t.Cleanup(func() { http.DefaultTransport = prevTransport })
}

func TestSearchPageServerRendersDiscoveryOnFirstLoad(t *testing.T) {
	installOpenLibraryDiscoveryTransport(t)

	s := newServerForTest(t)
	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	req.AddCookie(makeCookie(t, s, "user", false))
	rec := httptest.NewRecorder()

	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Find trending books before you even type",
		"Trending This Week",
		"Fantasy Hits",
		"Project Hail Mary",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in body: %s", want, body)
		}
	}
	if strings.Contains(body, "Loading discovery shelves...") {
		t.Fatalf("expected initial discovery content instead of loading placeholder: %s", body)
	}
}

func TestSearchUIShowsDiscoveryWhenQueryBlank(t *testing.T) {
	installOpenLibraryDiscoveryTransport(t)

	s := newServerForTest(t)
	req := httptest.NewRequest(http.MethodGet, "/ui/search", nil)
	req.AddCookie(makeCookie(t, s, "user", false))
	rec := httptest.NewRecorder()

	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Trending This Week",
		"Fantasy Hits",
		"Thriller Buzz",
		"Rom-Com Favorites",
		"Sci-Fi Series Hits",
		"4 picks",
		"Project Hail Mary",
		"Assistant to the Villain",
		"When the Moon Hatched",
		"Never Lie",
		"Bride",
		"Some Desperate Glory",
		"View details",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in body: %s", want, body)
		}
	}
	if strings.Contains(body, `Results for "`) {
		t.Fatalf("expected discovery view, got search results wrapper: %s", body)
	}
}
