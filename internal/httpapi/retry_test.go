package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/bootstrap"
	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	"gitea.knapp/jacoknapp/scriptorum/internal/db"
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
	s.approvalQueueInterval = time.Millisecond
	s.approvalQueueJitter = 0
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

func TestSearchRequestQueuesReadarrBookSearch(t *testing.T) {
	s := newServerForRetryTest(t)

	var gotPath, gotMethod, gotBody string
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":5,"name":"BookSearch"}`))
	}))
	t.Cleanup(readarr.Close)

	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = readarr.URL
	cfg.Readarr.Ebooks.APIKey = "test-key"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	id, err := s.db.CreateRequest(context.Background(), &db.Request{
		RequesterEmail:   "reader@example.com",
		Title:            "Search Me",
		Authors:          []string{"Alice"},
		Format:           "ebook",
		Status:           "queued",
		ExternalStatus:   "monitored",
		MatchedReadarrID: 55,
		ReadarrReq:       json.RawMessage(`{"title":"Search Me"}`),
	})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+strconv.FormatInt(id, 10)+"/search", nil)
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("search code=%d body=%s", rec.Code, rec.Body.String())
	}
	if gotMethod != http.MethodPost || gotPath != "/api/v1/command" {
		t.Fatalf("unexpected readarr request %s %s", gotMethod, gotPath)
	}
	if !strings.Contains(gotBody, `"name":"BookSearch"`) || !strings.Contains(gotBody, `"bookIds":[55]`) {
		t.Fatalf("unexpected command body: %s", gotBody)
	}
	got, err := s.db.GetRequest(context.Background(), id)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if got.Status != "queued" || !strings.Contains(got.StatusReason, "Readarr search queued for id 55") {
		t.Fatalf("unexpected request after search: status=%q reason=%q", got.Status, got.StatusReason)
	}
}

func TestSearchRequestRejectsAvailableRequest(t *testing.T) {
	s := newServerForRetryTest(t)
	id, err := s.db.CreateRequest(context.Background(), &db.Request{
		RequesterEmail:   "reader@example.com",
		Title:            "Done",
		Authors:          []string{"Alice"},
		Format:           "ebook",
		Status:           "queued",
		ExternalStatus:   "available",
		MatchedReadarrID: 56,
	})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+strconv.FormatInt(id, 10)+"/search", nil)
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != 400 {
		t.Fatalf("expected 400 for available request, got %d body=%s", rec.Code, rec.Body.String())
	}
}
