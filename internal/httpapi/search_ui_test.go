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

func TestSearchUIShowsDiscoveryWhenQueryBlank(t *testing.T) {
	prevTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "openlibrary.org" {
			return prevTransport.RoundTrip(r)
		}

		bodyByPath := map[string]string{
			"/subjects/fantasy.json":         `{"works":[{"title":"The Hobbit","authors":[{"name":"J.R.R. Tolkien"}],"cover_id":1}]}`,
			"/subjects/science_fiction.json": `{"works":[{"title":"Dune","authors":[{"name":"Frank Herbert"}],"cover_id":2}]}`,
			"/subjects/mystery.json":         `{"works":[{"title":"The Hound of the Baskervilles","authors":[{"name":"Arthur Conan Doyle"}],"cover_id":3}]}`,
			"/subjects/romance.json":         `{"works":[{"title":"Pride and Prejudice","authors":[{"name":"Jane Austen"}],"cover_id":4}]}`,
		}
		body, ok := bodyByPath[r.URL.Path]
		if !ok {
			t.Fatalf("unexpected Open Library request: %s", r.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = prevTransport })

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
		"Trending books by category",
		"Fantasy",
		"Science Fiction",
		"Mystery",
		"Romance",
		"The Hobbit",
		"Dune",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in body: %s", want, body)
		}
	}
	if strings.Contains(body, `Results for "`) {
		t.Fatalf("expected discovery view, got search results wrapper: %s", body)
	}
}
