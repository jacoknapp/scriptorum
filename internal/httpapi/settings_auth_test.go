package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/bootstrap"
	"gitea.knapp/jacoknapp/scriptorum/internal/config"
)

func TestAuthPageAndSave(t *testing.T) {
	tdir := t.TempDir()
	cfgPath := filepath.Join(tdir, "config.yaml")
	dbPath := filepath.Join(tdir, "scriptorum.db")
	cfg, database, err := bootstrap.EnsureFirstRun(context.Background(), cfgPath, dbPath)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	cfg.Admins.Emails = []string{"admin@example.com"}
	cfg.Setup.Completed = true
	_ = config.Save(cfgPath, cfg)
	s := NewServer(cfg, database, cfgPath)
	r := s.Router()

	// GET page (require admin)
	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req.AddCookie(makeCookie(t, s, "admin@example.com", true))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "OAuth") {
		t.Fatalf("auth page: %d body=%q", rec.Code, rec.Body.String())
	}

	// POST save
	form := url.Values{}
	form.Set("oauth_enabled", "true")
	form.Set("oauth_issuer", "https://issuer.example")
	form.Set("oauth_client_id", "cid")
	form.Set("oauth_client_secret", "csecret")
	form.Set("oauth_redirect", "http://localhost:8080/oauth/callback")
	// cookie settings are server-managed
	form.Set("oauth_scopes", "openid, email")
	form.Set("oauth_allow_domains", "example.com, test.dev")
	form.Set("oauth_allow_emails", "one@example.com")
	form.Set("oauth_autocreate", "on")

	req2 := httptest.NewRequest(http.MethodPost, "/settings/save", strings.NewReader(form.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.AddCookie(makeCookie(t, s, "admin@example.com", true))
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != 302 {
		t.Fatalf("auth save code=%d body=%s", rec2.Code, rec2.Body.String())
	}

	// Verify settings persisted
	got := s.settings.Get()
	if !got.OAuth.Enabled || !got.OAuth.AutoCreateUsers {
		t.Fatalf("settings not saved: %+v", got.OAuth)
	}
}
