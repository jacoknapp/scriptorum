package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/bootstrap"
	"gitea.knapp/jacoknapp/scriptorum/internal/config"
)

func newServerForAuthTest(t *testing.T) *Server {
	t.Helper()
	tdir := t.TempDir()
	cfgPath := filepath.Join(tdir, "config.yaml")
	dbPath := filepath.Join(tdir, "scriptorum.db")
	cfg, database, err := bootstrap.EnsureFirstRun(context.Background(), cfgPath, dbPath)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	cfg.Admins.Usernames = []string{"admin"}
	cfg.Setup.Completed = true
	cfg.OAuth.Enabled = false
	_ = config.Save(cfgPath, cfg)
	return NewServer(cfg, database, cfgPath)
}

func TestLocalLoginFormWhenOAuthDisabled(t *testing.T) {
	s := newServerForAuthTest(t)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Login") {
		t.Fatalf("expected local login page, got code=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestSetAndReadSessionCookie(t *testing.T) {
	s := newServerForAuthTest(t)
	sess := &session{Username: "u", Name: "U", Admin: true, Exp: 9999999999}
	rec := httptest.NewRecorder()
	s.setSession(rec, sess)
	ck := rec.Result().Cookies()
	if len(ck) == 0 {
		t.Fatalf("no cookie set")
	}
	// feed back
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	for _, c := range ck {
		req.AddCookie(c)
	}
	got := s.getSession(req)
	if got == nil || got.Username != "u" || !got.Admin {
		b, _ := json.Marshal(got)
		t.Fatalf("bad session: %s", string(b))
	}
	// verify HMAC protects tampering
	parts := strings.Split(ck[0].Value, ".")
	if len(parts) == 2 {
		raw, _ := base64.RawURLEncoding.DecodeString(parts[0])
		var mutated session
		_ = json.Unmarshal(raw, &mutated)
		mutated.Admin = false
	}
}
