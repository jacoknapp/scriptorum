package httpapi

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/bootstrap"
	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	"gitea.knapp/jacoknapp/scriptorum/internal/db"
	"golang.org/x/crypto/bcrypt"
)

func newServerForLoginTest(t *testing.T) (*Server, *db.DB, string) {
	t.Helper()
	tdir := t.TempDir()
	cfgPath := filepath.Join(tdir, "config.yaml")
	dbPath := filepath.Join(tdir, "scriptorum.db")
	cfg, database, err := bootstrap.EnsureFirstRun(t.Context(), cfgPath, dbPath)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	cfg.Setup.Completed = true
	cfg.OAuth.Enabled = false
	cfg.Auth.Salt = "testsalt"
	_ = config.Save(cfgPath, cfg)
	return NewServer(cfg, database, cfgPath), database, cfgPath
}

func TestLocalLoginAndDashboard(t *testing.T) {
	s, database, _ := newServerForLoginTest(t)
	t.Cleanup(func() { _ = database.Close() })

	// Create local user
	password := "secret123"
	peppered := s.settings.Get().Auth.Salt + ":" + password
	hash, _ := bcrypt.GenerateFromPassword([]byte(peppered), bcrypt.DefaultCost)
	if _, err := database.CreateUser(t.Context(), "tester", string(hash), true); err != nil {
		t.Fatalf("create user: %v", err)
	}

	r := s.Router()

	// Post login
	form := url.Values{}
	form.Set("username", "tester")
	form.Set("password", password)
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 302 {
		t.Fatalf("login code=%d", rec.Code)
	}
	cookie := rec.Result().Cookies()[0]

	// Access dashboard
	req2 := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("dashboard code=%d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestApproveWithoutReadarrConfigured(t *testing.T) {
	s, database, _ := newServerForLoginTest(t)
	t.Cleanup(func() { _ = database.Close() })

	// Admin cookie
	cookie := makeCookie(t, s, "admin@example.com", true)

	// Seed a request
	id, err := database.CreateRequest(t.Context(), &db.Request{RequesterEmail: "user@example.com", Title: "Book", Authors: []string{"A"}, Format: "ebook", Status: "pending"})
	if err != nil {
		t.Fatalf("seed request: %v", err)
	}

	r := s.Router()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+itoa(id)+"/approve", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("approve code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func itoa(i int64) string { return fmt.Sprintf("%d", i) }
