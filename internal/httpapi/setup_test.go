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

	// Send a request including form values via query (htmx does this for GET+hx-include)
	vals := url.Values{}
	vals.Set("ra_ebooks_base", "https://invalid-readarr.local")
	vals.Set("ra_ebooks_key", "not-a-real-key")
	url := ts.URL + "/setup/test/readarr?tag=ebooks&" + vals.Encode()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET /setup/test/readarr: %v", err)
	}
	defer resp.Body.Close()
	bb2, _ := io.ReadAll(resp.Body)
	body := string(bb2)
	// We expect an Error probe (since the host/key will not succeed)
	if !strings.Contains(body, "Error:") {
		t.Fatalf("expected Error probe, got: %s", body)
	}
}
