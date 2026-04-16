package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCSRFMiddlewareRejectsHXRequestWithoutTokenOrOrigin(t *testing.T) {
	s := newServerForTest(t)
	s.disableCSRF = false

	protected := s.csrfProtection(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/readarr/sync", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()

	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestCSRFMiddlewareAcceptsValidToken(t *testing.T) {
	s := newServerForTest(t)
	s.disableCSRF = false

	protected := s.csrfProtection(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/readarr/sync", nil)
	req.Header.Set("X-CSRF-Token", s.getCSRFToken(req))
	rec := httptest.NewRecorder()

	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}

func TestCSRFMiddlewareAcceptsSameOriginFallback(t *testing.T) {
	s := newServerForTest(t)
	s.disableCSRF = false

	protected := s.csrfProtection(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/readarr/sync", nil)
	req.Host = "example.com"
	req.Header.Set("Origin", "https://example.com")
	rec := httptest.NewRecorder()

	protected.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
}
