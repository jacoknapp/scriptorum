package httpapi

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
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
	s := NewServer(cfg, database, cfgPath)
	s.disableCSRF = true // Disable CSRF for tests
	return s, database, cfgPath
}

func TestLocalLoginAndDashboard(t *testing.T) {
	s, database, _ := newServerForLoginTest(t)
	t.Cleanup(func() { _ = database.Close() })

	// Create local user
	password := "secret123"
	peppered := s.settings.Get().Auth.Salt + ":" + password
	hash, _ := bcrypt.GenerateFromPassword([]byte(peppered), bcrypt.DefaultCost)
	if _, err := database.CreateUser(t.Context(), "tester", string(hash), true, false); err != nil {
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

func TestLocalLoginFailureAuditEvents(t *testing.T) {
	s, database, _ := newServerForLoginTest(t)
	t.Cleanup(func() { _ = database.Close() })

	password := "secret123"
	peppered := s.settings.Get().Auth.Salt + ":" + password
	hash, _ := bcrypt.GenerateFromPassword([]byte(peppered), bcrypt.DefaultCost)
	if _, err := database.CreateUser(t.Context(), "tester", string(hash), true, false); err != nil {
		t.Fatalf("create user: %v", err)
	}

	r := s.Router()

	// Unknown username
	form := url.Values{}
	form.Set("username", "nosuchuser")
	form.Set("password", password)
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("login (unknown user) code=%d", rec.Code)
	}

	// Wrong password for a real user
	form2 := url.Values{}
	form2.Set("username", "tester")
	form2.Set("password", "wrong-password")
	req2 := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(form2.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusFound {
		t.Fatalf("login (wrong password) code=%d", rec2.Code)
	}

	events, err := database.ListAuditEvents(t.Context(), 50)
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	var sawUnknownUser, sawWrongPassword bool
	for _, ev := range events {
		if ev.EventType != "user.login_failed" {
			continue
		}
		switch {
		case ev.ActorEmail == "nosuchuser" && ev.Details == "unknown username":
			sawUnknownUser = true
		case ev.ActorEmail == "tester" && ev.Details == "invalid password":
			sawWrongPassword = true
		}
	}
	if !sawUnknownUser {
		t.Errorf("expected login_failed audit event for unknown username, got %+v", events)
	}
	if !sawWrongPassword {
		t.Errorf("expected login_failed audit event for wrong password, got %+v", events)
	}
}

func TestLocalLoginRedirectsToWelcomeWhenOIDCEnabled(t *testing.T) {
	s, database, _ := newServerForLoginTest(t)
	t.Cleanup(func() { _ = database.Close() })

	cfg := s.settings.Get()
	cfg.OAuth.Enabled = true
	cfg.OAuth.Issuer = "https://issuer.invalid"
	_ = s.settings.Update(cfg)
	_ = s.initOIDC()

	form := url.Values{}
	form.Set("username", "tester")
	form.Set("password", "secret123")
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("code=%d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "force_welcome=true") {
		t.Fatalf("expected redirect to welcome page, got Location=%q", loc)
	}
}

func TestLocalLoginMissingCredentials(t *testing.T) {
	s, database, _ := newServerForLoginTest(t)
	t.Cleanup(func() { _ = database.Close() })

	form := url.Values{}
	form.Set("username", "")
	form.Set("password", "")
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewBufferString(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("code=%d", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "error=Invalid") {
		t.Fatalf("expected redirect with invalid-credentials error, got Location=%q", loc)
	}
}

func TestApproveWithoutReadarrConfigured(t *testing.T) {
	s, database, _ := newServerForLoginTest(t)
	t.Cleanup(func() { _ = database.Close() })

	// Admin cookie
	cookie := makeCookie(t, s, "admin", true)

	// Seed a request
	id, err := database.CreateRequest(t.Context(), &db.Request{RequesterEmail: "user", Title: "Book", Authors: []string{"A"}, Format: "ebook", Status: "pending"})
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

func TestRequestsPageUsesStoredCoverWithoutClientHydration(t *testing.T) {
	s, database, _ := newServerForLoginTest(t)
	t.Cleanup(func() { _ = database.Close() })

	if _, err := database.CreateRequest(t.Context(), &db.Request{
		RequesterEmail: "user",
		Title:          "Book",
		Authors:        []string{"Alice"},
		Format:         "ebook",
		Status:         "pending",
		CoverURL:       "https://covers.example.test/book.jpg",
	}); err != nil {
		t.Fatalf("seed request: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/requests", nil)
	req.AddCookie(makeCookie(t, s, "user", false))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("requests code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "https://covers.example.test/book.jpg") {
		t.Fatalf("expected stored cover in requests page: %s", body)
	}
	if strings.Contains(body, "loadRequestCovers") || strings.Contains(body, "fetchRequestCover") {
		t.Fatalf("expected cover hydration script to be removed: %s", body)
	}
}

func itoa(i int64) string { return fmt.Sprintf("%d", i) }
