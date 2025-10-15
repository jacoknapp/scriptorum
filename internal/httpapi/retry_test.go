package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/bootstrap"
	"gitea.knapp/jacoknapp/scriptorum/internal/config"
)

// helper to create server (copied style from api_flow_test)
func newServerForRetryTest(t *testing.T) *Server {
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
	// Ensure Readarr configured for tests that expect processing to start
	cfg.Readarr.Ebooks.BaseURL = "http://127.0.0.1:12345"
	cfg.Readarr.Ebooks.APIKey = "test"
	_ = config.Save(cfgPath, cfg)
	s := NewServer(cfg, database, cfgPath)
	s.disableCSRF = true
	return s
}

func TestRetryApprovedWithPayload(t *testing.T) {
	s := newServerForRetryTest(t)
	r := s.Router()

	// Create request with a provider payload
	body := []byte(`{"title":"RetryBook","authors":["Alice"],"format":"ebook","provider_payload":"{\"fake\":true}"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	req.AddCookie(makeCookie(t, s, "user", false))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	id := int(resp["id"].(float64))

	// Approve via DB directly to mark as approved and keep the stored payload
	if err := s.db.ApproveRequest(context.Background(), int64(id), "admin"); err != nil {
		t.Fatalf("ApproveRequest db error: %v", err)
	}

	// Retry should accept and set processing
	retryReq := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+strconv.Itoa(id)+"/retry", nil)
	retryReq.AddCookie(makeCookie(t, s, "admin", true))
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, retryReq)
	if rec2.Code != 200 {
		t.Fatalf("retry unexpected code=%d body=%s", rec2.Code, rec2.Body.String())
	}

	// Verify DB status updated to processing
	got, err := s.db.GetRequest(context.Background(), int64(id))
	if err != nil {
		t.Fatalf("GetRequest: %v", err)
	}
	if got.Status != "processing" {
		t.Fatalf("expected status processing after retry, got %s", got.Status)
	}
}

func TestRetryFailsWhenNotApproved(t *testing.T) {
	s := newServerForRetryTest(t)
	r := s.Router()

	// Create a new pending request (no approve)
	body := []byte(`{"title":"NoApprove","authors":["Bob"],"format":"ebook","provider_payload":"{\"fake\":true}"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	req.AddCookie(makeCookie(t, s, "user", false))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	id := int(resp["id"].(float64))

	// Retry while still pending should fail (400)
	retryReq := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+strconv.Itoa(id)+"/retry", nil)
	retryReq.AddCookie(makeCookie(t, s, "admin", true))
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, retryReq)
	if rec2.Code != 400 {
		t.Fatalf("expected 400 when retrying non-approved request, got %d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestRetryFailsWithoutPayload(t *testing.T) {
	s := newServerForRetryTest(t)
	r := s.Router()

	// Create a request without provider payload
	body := []byte(`{"title":"NoPayload","authors":["Carol"],"format":"ebook"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	req.AddCookie(makeCookie(t, s, "user", false))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	id := int(resp["id"].(float64))

	// Mark approved in DB but payload is empty
	if err := s.db.ApproveRequest(context.Background(), int64(id), "admin"); err != nil {
		t.Fatalf("ApproveRequest db error: %v", err)
	}

	retryReq := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+strconv.Itoa(id)+"/retry", nil)
	retryReq.AddCookie(makeCookie(t, s, "admin", true))
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, retryReq)
	if rec2.Code != 400 {
		t.Fatalf("expected 400 when retrying without payload, got %d body=%s", rec2.Code, rec2.Body.String())
	}
}
