package httpapi

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/bootstrap"
	"gitea.knapp/jacoknapp/scriptorum/internal/config"
)

func TestSetupGateRedirectsWhenNeeded(t *testing.T) {
	tdir := t.TempDir()
	cfgPath := filepath.Join(tdir, "config.yaml")
	dbPath := filepath.Join(tdir, "scriptorum.db")
	cfg, database, err := bootstrap.EnsureFirstRun(t.Context(), cfgPath, dbPath)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	// No admin set, should redirect to /setup
	s := NewServer(cfg, database, cfgPath)
	r := s.Router()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 302 {
		t.Fatalf("expected 302 to /setup got %d", rec.Code)
	}
}

func TestLoginRequiredForProtected(t *testing.T) {
	tdir := t.TempDir()
	cfgPath := filepath.Join(tdir, "config.yaml")
	dbPath := filepath.Join(tdir, "scriptorum.db")
	cfg, database, err := bootstrap.EnsureFirstRun(t.Context(), cfgPath, dbPath)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	cfg.Admins.Emails = []string{"a@example.com"}
	cfg.Setup.Completed = true
	_ = config.Save(cfgPath, cfg)
	s := NewServer(cfg, database, cfgPath)
	r := s.Router()
	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil) // protected
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 302 {
		t.Fatalf("expected 302 login got %d", rec.Code)
	}
}
