package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"gitea.knapp/jacoknapp/scriptoruminternal/bootstrap"
	"gitea.knapp/jacoknapp/scriptoruminternal/config"
)

func newServerForTest(t *testing.T) *Server {
	t.Helper()
	tdir := t.TempDir()
	cfgPath := filepath.Join(tdir, "config.yaml")
	dbPath := filepath.Join(tdir, "scriptorum.db")
	cfg, database, err := bootstrap.EnsureFirstRun(t.Context(), cfgPath, dbPath)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	cfg.Admins.Emails = []string{"admin@example.com"}
	cfg.Setup.Completed = true
	_ = config.Save(cfgPath, cfg)
	return NewServer(cfg, database, cfgPath)
}

func makeCookie(t *testing.T, s *Server, email string, admin bool) *http.Cookie {
	sess := &session{Email: strings.ToLower(email), Name: "T", Admin: admin, Exp: 9999999999}
	b, _ := json.Marshal(sess)
	sig := s.sign(b)
	val := base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(sig)
	return &http.Cookie{Name: defaultIf(s.cfg.OAuth.CookieName, "scriptorum_session"), Value: val, Path: "/"}
}

func TestHealthAndSetupGate(t *testing.T) {
	s := newServerForTest(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("healthz code=%d", rec.Code)
	}
}

func TestCreateAndApproveFlow(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()

	// Create (requires login)
	body := []byte(`{"title":"Book","authors":["Alice"],"format":"ebook"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	req.AddCookie(makeCookie(t, s, "user@example.com", false))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	id := int(resp["id"].(float64))

	// Approve (requires admin). Readarr endpoints are unset, so it may 502/404. Only assert no panic & valid HTTP.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+strconv.Itoa(id)+"/approve", nil)
	req2.AddCookie(makeCookie(t, s, "admin@example.com", true))
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != 200 && rec2.Code != 502 && rec2.Code != 404 {
		t.Fatalf("approve unexpected code=%d body=%s", rec2.Code, rec2.Body.String())
	}
}
