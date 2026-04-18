package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
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

func TestUserDeleteRequiresPost(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()

	req := httptest.NewRequest(http.MethodGet, "/users/delete?id=1", nil)
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed && rec.Code != http.StatusNotFound {
		t.Fatalf("expected non-success for GET delete, got %d", rec.Code)
	}
}

func TestUserDeletePostSucceedsWithCSRF(t *testing.T) {
	s := newServerForTest(t)
	s.disableCSRF = false
	r := s.Router()

	hash, err := s.hashPassword("pw", s.settings.Get().Auth.Salt)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	userID, err := s.db.CreateUser(context.Background(), "victim", hash, false, false)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	form := url.Values{}
	form.Set("id", strconv.FormatInt(userID, 10))
	req := httptest.NewRequest(http.MethodPost, "/users/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Host = "example.com"
	req.Header.Set("Origin", "https://example.com")
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected 302, got %d", rec.Code)
	}
	if _, err := s.db.GetUserByUsername(context.Background(), "victim"); err == nil {
		t.Fatalf("expected user to be deleted")
	}
}
