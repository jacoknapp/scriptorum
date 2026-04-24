package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	"gitea.knapp/jacoknapp/scriptorum/internal/db"
)

func makeTestServer(t *testing.T) *Server {
	t.Helper()
	td := t.TempDir()
	dbPath := filepath.Join(td, "scriptorum.db")
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfgPath := filepath.Join(td, "scriptorum.yaml")
	cfg := &config.Config{}
	// Write initial config file
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatalf("write config: %v", err)
	}

	s := NewServer(cfg, database, cfgPath)
	return s
}

func TestSetupPagesRender(t *testing.T) {
	s := makeTestServer(t)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	// GET /setup
	resp, err := http.Get(ts.URL + "/setup")
	if err != nil {
		t.Fatalf("GET /setup: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html content-type, got %s", ct)
	}

	// GET /setup/step/1
	resp2, err := http.Get(ts.URL + "/setup/step/1")
	if err != nil {
		t.Fatalf("GET /setup/step/1: %v", err)
	}
	defer resp2.Body.Close()
	if ct := resp2.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html content-type for step, got %s", ct)
	}
}

func TestOAuthProbeMissingFields(t *testing.T) {
	s := makeTestServer(t)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	// Call test/oauth without required params
	resp, err := http.Get(ts.URL + "/setup/test/oauth")
	if err != nil {
		t.Fatalf("GET /setup/test/oauth: %v", err)
	}
	defer resp.Body.Close()
	// Expect an HTML probe mentioning missing fields
	bb, _ := io.ReadAll(resp.Body)
	body := string(bb)
	if !strings.Contains(body, "missing issuer/client_id/redirect") {
		t.Fatalf("expected missing fields message, got: %s", body)
	}
}

func TestReadarrProbeUsesFormValues(t *testing.T) {
	s := makeTestServer(t)
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	vals := url.Values{}
	vals.Set("ra_ebooks_base", "https://invalid-readarr.local")
	vals.Set("ra_ebooks_key", "not-a-real-key")
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/setup/test/readarr?tag=ebooks", strings.NewReader(vals.Encode()))
	if err != nil {
		t.Fatalf("build POST /setup/test/readarr: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", ts.URL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /setup/test/readarr: %v", err)
	}
	defer resp.Body.Close()
	bb2, _ := io.ReadAll(resp.Body)
	body := string(bb2)
	if !strings.Contains(body, "Could not connect to Readarr.") {
		t.Fatalf("expected friendly probe, got: %s", body)
	}
	if strings.Contains(body, "not-a-real-key") {
		t.Fatalf("expected probe to avoid echoing the api key, got: %s", body)
	}
}

func TestSetupReadarrStepDoesNotRenderSavedAPIKeys(t *testing.T) {
	s := makeTestServer(t)
	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = "https://ebooks.example"
	cfg.Readarr.Ebooks.APIKey = "ebooks-secret"
	cfg.Readarr.Audiobooks.BaseURL = "https://audio.example"
	cfg.Readarr.Audiobooks.APIKey = "audio-secret"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/setup/step/3")
	if err != nil {
		t.Fatalf("GET /setup/step/3: %v", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)
	if strings.Contains(body, "ebooks-secret") || strings.Contains(body, "audio-secret") {
		t.Fatalf("expected setup step to avoid rendering api keys, got %q", body)
	}
}

func TestSetupSavePreservesExistingReadarrAPIKeysWhenBlank(t *testing.T) {
	s := makeTestServer(t)
	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = "https://ebooks.example"
	cfg.Readarr.Ebooks.APIKey = "ebooks-existing"
	cfg.Readarr.Audiobooks.BaseURL = "https://audio.example"
	cfg.Readarr.Audiobooks.APIKey = "audio-existing"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	vals := url.Values{
		"ra_ebooks_base": {"https://ebooks.example"},
		"ra_audio_base":  {"https://audio.example"},
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/setup/save", strings.NewReader(vals.Encode()))
	if err != nil {
		t.Fatalf("build POST /setup/save: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", ts.URL)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /setup/save: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		bodyBytes, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected save status=%d body=%s", resp.StatusCode, string(bodyBytes))
	}

	got := s.settings.Get()
	if got.Readarr.Ebooks.APIKey != "ebooks-existing" || got.Readarr.Audiobooks.APIKey != "audio-existing" {
		t.Fatalf("expected existing api keys to be preserved, got %+v", got.Readarr)
	}
}
