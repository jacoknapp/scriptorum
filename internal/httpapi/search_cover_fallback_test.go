package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestOpenLibraryCoverFallbackURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"9780316274147", "https://covers.openlibrary.org/b/isbn/9780316274147-M.jpg?default=false"},
		{"031627414X", "https://covers.openlibrary.org/b/isbn/031627414X-M.jpg?default=false"},
		{"978-0316274147", "https://covers.openlibrary.org/b/isbn/9780316274147-M.jpg?default=false"},
		{"", ""},
		{"B0FCXXBH1C", ""},          // ASIN, not an ISBN
		{"12345", ""},               // wrong length
		{"97803162741 7", ""},       // invalid char
		{"javascript:alert(1)", ""}, // junk
	}
	for _, c := range cases {
		if got := openLibraryCoverFallbackURL(c.in); got != c.want {
			t.Errorf("openLibraryCoverFallbackURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestAppendCoverIsbnFallback(t *testing.T) {
	proxied := "/ui/readarr-cover?u=https%3A%2F%2Freadarr.example.internal%2FMediaCoverProxy%2Fabc%2F1.jpg"
	if got := appendCoverIsbnFallback(proxied, "9780316274147", ""); got != proxied+"&isbn=9780316274147" {
		t.Fatalf("expected isbn13 appended, got %q", got)
	}
	if got := appendCoverIsbnFallback(proxied, "", "031627414X"); got != proxied+"&isbn=031627414X" {
		t.Fatalf("expected isbn10 appended, got %q", got)
	}
	if got := appendCoverIsbnFallback(proxied, "", ""); got != proxied {
		t.Fatalf("expected unchanged without isbn, got %q", got)
	}
	if got := appendCoverIsbnFallback(proxied, "not-an-isbn", ""); got != proxied {
		t.Fatalf("expected unchanged for invalid isbn, got %q", got)
	}
	external := "https://covers.openlibrary.org/b/id/1-M.jpg"
	if got := appendCoverIsbnFallback(external, "9780316274147", ""); got != external {
		t.Fatalf("expected non-proxied cover unchanged, got %q", got)
	}
}

// When the upstream Readarr cover fetch fails (e.g. an auth proxy in front of
// Readarr rejects MediaCoverProxy requests), the handler should redirect to
// the OpenLibrary cover instead of returning 502 — but only when an isbn
// fallback was provided.
func TestServeReadarrCoverFallsBackToOpenLibraryOnUpstreamFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer upstream.Close()

	s := newServerForTest(t)
	configureEbooksReadarr(t, s, upstream.URL)

	coverURL := upstream.URL + "/MediaCoverProxy/abc/1.jpg"

	router := s.Router()
	do := func(target string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.AddCookie(makeCookie(t, s, "user", false))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	rec := do("/ui/readarr-cover?u=" + url.QueryEscape(coverURL) + "&isbn=9780316274147")
	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302 redirect, got %d body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); loc != "https://covers.openlibrary.org/b/isbn/9780316274147-M.jpg?default=false" {
		t.Fatalf("unexpected redirect location %q", loc)
	}

	// Without an isbn the old 502 behavior is preserved.
	rec = do("/ui/readarr-cover?u=" + url.QueryEscape(coverURL))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 without fallback, got %d", rec.Code)
	}
}
