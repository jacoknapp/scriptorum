package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestChaptarrProbeMessage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"nil", nil, ""},
		{"401", fmt.Errorf("http 401"), "Check the API key"},
		{"403", fmt.Errorf("HTTP 403 Forbidden"), "Check the API key"},
		{"unauthorized", fmt.Errorf("unauthorized"), "Check the API key"},
		{"forbidden", fmt.Errorf("forbidden"), "Check the API key"},
		{"x509", fmt.Errorf("x509: certificate signed by unknown authority"), "certificate"},
		{"tls", fmt.Errorf("tls: handshake failure"), "certificate"},
		{"certificate", fmt.Errorf("certificate error"), "certificate"},
		{"handshake", fmt.Errorf("handshake timeout"), "certificate"},
		{"not chaptarr", fmt.Errorf("configured server is not Chaptarr"), "not a Chaptarr server"},
		{"generic", fmt.Errorf("connection refused"), "Check the Base URL"},
	}
	for _, tt := range tests {
		got := chaptarrProbeMessage(tt.err)
		if !strings.Contains(strings.ToLower(got), strings.ToLower(tt.want)) {
			t.Errorf("%s: got %q, want containing %q", tt.name, got, tt.want)
		}
	}
}

func TestAPIChatparrCapabilitiesFakeServer(t *testing.T) {
	fakeChaptarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/status":
			json.NewEncoder(w).Encode(map[string]any{"appName": "Chaptarr", "version": "1.2.3"})
		case "/api/v1/qualityprofile":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "name": "eBook 192kbps", "profileType": "ebook"},
				{"id": 2, "name": "Audiobook 128kbps", "profileType": "audiobook"},
			})
		case "/api/v1/metadataprofile":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "name": "Audiobook Standard", "profileType": float64(1)},
				{"id": 2, "name": "eBook Standard", "profileType": float64(2)},
			})
		case "/api/v1/rootfolder":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "path": "/ebooks", "accessible": true},
				{"id": 2, "path": "/audio", "accessible": true},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fakeChaptarr.Close()

	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.Chaptarr.BaseURL = fakeChaptarr.URL
	cfg.Chaptarr.APIKey = "test-key"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	// GET requires admin
	req := httptest.NewRequest(http.MethodGet, "/api/chaptarr/capabilities", nil)
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var caps map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &caps); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if caps["version"] != "1.2.3" {
		t.Errorf("version = %v, want 1.2.3", caps["version"])
	}
}

func TestAPIChatparrCapabilitiesNotConfigured(t *testing.T) {
	s := newServerForTest(t)

	req := httptest.NewRequest(http.MethodGet, "/api/chaptarr/capabilities", nil)
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestAPIChatparrCapabilitiesNotChaptarr(t *testing.T) {
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/system/status" {
			json.NewEncoder(w).Encode(map[string]any{"appName": "Readarr", "version": "0.4.0"})
		}
	}))
	defer fakeServer.Close()

	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.Chaptarr.BaseURL = fakeServer.URL
	cfg.Chaptarr.APIKey = "test-key"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/chaptarr/capabilities", nil)
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSetupTestChaptarrFakeServer(t *testing.T) {
	fakeChaptarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/status":
			json.NewEncoder(w).Encode(map[string]any{"appName": "Chaptarr", "version": "2.0.0"})
		case "/api/v1/qualityprofile":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 3, "name": "eBook HQ", "profileType": "ebook"},
				{"id": 4, "name": "Audio HQ", "profileType": "audiobook"},
			})
		case "/api/v1/metadataprofile":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "name": "AudioMetadata", "profileType": float64(1)},
				{"id": 2, "name": "eBookMetadata", "profileType": float64(2)},
			})
		case "/api/v1/rootfolder":
			json.NewEncoder(w).Encode([]map[string]any{
				{"id": 1, "path": "/books", "accessible": true},
				{"id": 2, "path": "/audiobooks", "accessible": true},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer fakeChaptarr.Close()

	s := newServerForTest(t)
	// Setup routes are only accessible when setup is NOT completed
	cfg := s.settings.Get()
	cfg.Setup.Completed = false
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	form := url.Values{
		"chaptarr_base": {fakeChaptarr.URL},
		"chaptarr_key":  {"setup-key"},
	}
	req := httptest.NewRequest(http.MethodPost, "/setup/test/chaptarr", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var caps map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &caps); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if caps["version"] != "2.0.0" {
		t.Errorf("version = %v, want 2.0.0", caps["version"])
	}
}
