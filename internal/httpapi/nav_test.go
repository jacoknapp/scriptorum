package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNavTabsWorkForAdmin(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	adminCookie := makeCookie(t, s, "admin", true)

	// Root should redirect to /search
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 302 {
		t.Fatalf("root code=%d", rec.Code)
	}

	// Search
	req2 := httptest.NewRequest(http.MethodGet, "/search", nil)
	req2.AddCookie(adminCookie)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("search code=%d body=%s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), "Search books") {
		t.Fatalf("search page missing content: %s", rec2.Body.String())
	}

	// Requests
	req3 := httptest.NewRequest(http.MethodGet, "/requests", nil)
	req3.AddCookie(adminCookie)
	rec3 := httptest.NewRecorder()
	r.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("requests code=%d body=%s", rec3.Code, rec3.Body.String())
	}
	if !strings.Contains(rec3.Body.String(), "Requests") {
		t.Fatalf("requests page missing content: %s", rec3.Body.String())
	}

	// Users (admin only)
	req4 := httptest.NewRequest(http.MethodGet, "/users", nil)
	req4.AddCookie(adminCookie)
	rec4 := httptest.NewRecorder()
	r.ServeHTTP(rec4, req4)
	if rec4.Code != 200 {
		t.Fatalf("users code=%d body=%s", rec4.Code, rec4.Body.String())
	}
	if !strings.Contains(rec4.Body.String(), "Users") {
		t.Fatalf("users page missing content: %s", rec4.Body.String())
	}

	// Settings (admin only)
	req5 := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req5.AddCookie(adminCookie)
	rec5 := httptest.NewRecorder()
	r.ServeHTTP(rec5, req5)
	if rec5.Code != 200 {
		t.Fatalf("settings code=%d body=%s", rec5.Code, rec5.Body.String())
	}
	if !strings.Contains(rec5.Body.String(), "Settings") {
		t.Fatalf("settings page missing content: %s", rec5.Body.String())
	}
}
