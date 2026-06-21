package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
)

// TestApproveRequestWithReadarrConfiguredQueuesAsync exercises the
// Readarr-enabled branch of apiApproveRequest (as opposed to the
// no-Readarr-configured immediate-approve branch covered elsewhere).
func TestApproveRequestWithReadarrConfiguredQueuesAsync(t *testing.T) {
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/book" && r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":101,"monitored":true,"statistics":{"bookFileCount":0}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer readarr.Close()

	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = readarr.URL
	cfg.Readarr.Ebooks.APIKey = "test-key"
	if err := config.Save(s.cfgPath, cfg); err != nil {
		t.Fatalf("save cfg: %v", err)
	}
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	r := s.Router()
	body := []byte(`{"title":"Async Book","authors":["Alice"],"format":"ebook","provider_payload":"{\"title\":\"Async Book\",\"foreignBookId\":\"fb-async\",\"foreignEditionId\":\"fe-async\",\"author\":{\"name\":\"Alice\"}}"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	createReq.AddCookie(makeCookie(t, s, "user", false))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	id := int64(created["id"].(float64))

	approveReq := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+strconv.FormatInt(id, 10)+"/approve", nil)
	approveReq.AddCookie(makeCookie(t, s, "admin", true))
	approveRec := httptest.NewRecorder()
	r.ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve code=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := s.db.GetRequest(context.Background(), id)
		if err != nil {
			t.Fatalf("get request: %v", err)
		}
		if got.Status == "queued" {
			events, err := s.db.ListAuditEvents(context.Background(), 50)
			if err != nil {
				t.Fatalf("list audit events: %v", err)
			}
			found := false
			for _, ev := range events {
				if ev.EventType == "request.approved" && ev.RequestID != nil && *ev.RequestID == id {
					found = true
				}
			}
			if !found {
				t.Fatalf("expected request.approved audit event for request %d, got %+v", id, events)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Readarr-enabled approval to complete")
}

// TestApproveAllWithNoReadarrConfigured exercises the no-Readarr-configured
// branch of apiApproveAllRequests, distinct from the Readarr-enabled async
// path covered by TestApproveAllSubmitsPendingRequestsToReadarr.
func TestApproveAllWithNoReadarrConfigured(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()

	for _, tc := range []struct{ title, format string }{
		{"Book One", "ebook"},
		{"Book Two", "audiobook"},
	} {
		body := []byte(`{"title":"` + tc.title + `","authors":["Alice"],"format":"` + tc.format + `"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
		req.AddCookie(makeCookie(t, s, "user", false))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	approveAllReq := httptest.NewRequest(http.MethodPost, "/api/v1/requests/approve-all", nil)
	approveAllReq.AddCookie(makeCookie(t, s, "admin", true))
	approveAllRec := httptest.NewRecorder()
	r.ServeHTTP(approveAllRec, approveAllReq)
	if approveAllRec.Code != http.StatusOK {
		t.Fatalf("approve-all code=%d body=%s", approveAllRec.Code, approveAllRec.Body.String())
	}

	items, err := s.db.ListRequests(context.Background(), "", 10)
	if err != nil {
		t.Fatalf("list requests: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(items))
	}
	for _, item := range items {
		if item.Status != "approved" {
			t.Fatalf("expected request %d to be approved, got status %q", item.ID, item.Status)
		}
	}

	events, err := s.db.ListAuditEvents(context.Background(), 50)
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	approvedEvents := 0
	for _, ev := range events {
		if ev.EventType == "request.approved" {
			approvedEvents++
		}
	}
	if approvedEvents != 2 {
		t.Fatalf("expected 2 request.approved audit events, got %d (%+v)", approvedEvents, events)
	}
}

// TestApproveAllWithNoPendingRequests exercises the early-return branch when
// there is nothing to approve.
func TestApproveAllWithNoPendingRequests(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()

	approveAllReq := httptest.NewRequest(http.MethodPost, "/api/v1/requests/approve-all", nil)
	approveAllReq.AddCookie(makeCookie(t, s, "admin", true))
	approveAllRec := httptest.NewRecorder()
	r.ServeHTTP(approveAllRec, approveAllReq)
	if approveAllRec.Code != http.StatusOK {
		t.Fatalf("approve-all code=%d body=%s", approveAllRec.Code, approveAllRec.Body.String())
	}
	var resp map[string]string
	_ = json.Unmarshal(approveAllRec.Body.Bytes(), &resp)
	if resp["status"] != "no pending requests to approve" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}
