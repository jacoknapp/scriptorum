package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func createRequestBody(t *testing.T, title string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"title": title, "authors": []string{"Author"}, "format": "ebook"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestMaxPendingRequestsPerUserQuota(t *testing.T) {
	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.Requests.MaxPendingPerUser = 1
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	r := s.Router()
	user := makeCookie(t, s, "user", false)

	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(createRequestBody(t, "Book One")))
	req1.Header.Set("Content-Type", "application/json")
	req1.AddCookie(user)
	rec1 := httptest.NewRecorder()
	r.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first request: expected 201, got %d %s", rec1.Code, rec1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(createRequestBody(t, "Book Two")))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(user)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: expected 429, got %d %s", rec2.Code, rec2.Body.String())
	}

	// A different user should not be affected by the first user's quota.
	other := makeCookie(t, s, "otheruser", false)
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(createRequestBody(t, "Book Three")))
	req3.Header.Set("Content-Type", "application/json")
	req3.AddCookie(other)
	rec3 := httptest.NewRecorder()
	r.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusCreated {
		t.Fatalf("other user's request: expected 201, got %d %s", rec3.Code, rec3.Body.String())
	}
}

func TestMaxPendingRequestsPerUserQuotaHTMX(t *testing.T) {
	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.Requests.MaxPendingPerUser = 1
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	r := s.Router()
	user := makeCookie(t, s, "user", false)

	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(createRequestBody(t, "Book One")))
	req1.Header.Set("Content-Type", "application/json")
	req1.AddCookie(user)
	rec1 := httptest.NewRecorder()
	r.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusCreated {
		t.Fatalf("first request: expected 201, got %d %s", rec1.Code, rec1.Body.String())
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(createRequestBody(t, "Book Two")))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("HX-Request", "true")
	req2.AddCookie(user)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d %s", rec2.Code, rec2.Body.String())
	}
	if ct := rec2.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("expected HTML response for HTMX request, got Content-Type %q", ct)
	}
	if !bytes.Contains(rec2.Body.Bytes(), []byte("maximum allowed")) {
		t.Errorf("expected quota message in HTML body, got: %s", rec2.Body.String())
	}
}

func TestMaxPendingRequestsPerUserDisabledByDefault(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	user := makeCookie(t, s, "user", false)

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(createRequestBody(t, "Book")))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(user)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("request %d: expected 201, got %d %s", i, rec.Code, rec.Body.String())
		}
	}
}
