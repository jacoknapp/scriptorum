package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireLoginMiddlewareAsyncRequest(t *testing.T) {
	server := newServerForTest(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ui/requests/table", nil)
	rec := httptest.NewRecorder()

	server.requireLogin(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for async unauthenticated request, got %d", rec.Code)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "/login" {
		t.Fatalf("expected HX-Redirect=/login, got %q", got)
	}
	if got := rec.Header().Get("X-Login-Required"); got != "true" {
		t.Fatalf("expected X-Login-Required=true, got %q", got)
	}
}

func TestRequireLoginMiddlewareHTMXRequest(t *testing.T) {
	server := newServerForTest(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()

	server.requireLogin(handler).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for HTMX unauthenticated request, got %d", rec.Code)
	}
	if got := rec.Header().Get("HX-Redirect"); got != "/login" {
		t.Fatalf("expected HX-Redirect=/login, got %q", got)
	}
}

func TestDynamicRoutesDisableCaching(t *testing.T) {
	server := newServerForTest(t)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()

	server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected login page to render, got %d", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate, private" {
		t.Fatalf("unexpected Cache-Control header %q", got)
	}
	if got := rec.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("unexpected Pragma header %q", got)
	}
	if got := rec.Header().Get("Expires"); got != "0" {
		t.Fatalf("unexpected Expires header %q", got)
	}
}

func TestLogoutClearsBrowserCache(t *testing.T) {
	server := newServerForTest(t)

	req := httptest.NewRequest(http.MethodGet, "/logout", nil)
	rec := httptest.NewRecorder()

	server.handleLogout(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/login?from_logout=true" {
		t.Fatalf("unexpected logout redirect %q", got)
	}
	if got := rec.Header().Get("Clear-Site-Data"); got != "\"cache\"" {
		t.Fatalf("unexpected Clear-Site-Data header %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate, private" {
		t.Fatalf("unexpected Cache-Control header %q", got)
	}
}
