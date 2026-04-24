package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAPIReadarrProfilesUsesPostedOverrides(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()

	var gotAPIKey string
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.URL.Query().Get("apikey")
		if gotAPIKey == "" {
			gotAPIKey = r.Header.Get("X-Api-Key")
		}
		switch r.URL.Path {
		case "/api/v1/qualityprofile/1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":1,"name":"Any"}`))
		case "/api/v1/qualityprofile/2":
			http.NotFound(w, r)
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer readarr.Close()

	form := url.Values{
		"kind":          {"ebooks"},
		"use_overrides": {"true"},
		"base_url":      {readarr.URL},
		"api_key":       {"override-key"},
		"insecure":      {"false"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/readarr/profiles", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if gotAPIKey != "override-key" {
		t.Fatalf("expected override api key to be used, got %q", gotAPIKey)
	}

	var out map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out["1"] != "Any" {
		t.Fatalf("unexpected profiles response: %+v", out)
	}
}

func TestAPIReadarrProfilesReturnsFriendlyErrors(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()

	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad api key: override-key", http.StatusUnauthorized)
	}))
	defer readarr.Close()

	form := url.Values{
		"kind":          {"ebooks"},
		"use_overrides": {"true"},
		"base_url":      {readarr.URL},
		"api_key":       {"override-key"},
		"insecure":      {"false"},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/readarr/profiles", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Check the API key.") {
		t.Fatalf("expected friendly error, got %q", body)
	}
	if strings.Contains(body, "override-key") {
		t.Fatalf("expected api key to stay redacted, got %q", body)
	}
}
