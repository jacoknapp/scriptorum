package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/db"
)

// TestProcessAsyncApprovalResolvesBackendFreshEachRun pins the fix for a real
// bug found live: enqueueAsyncApproval used to capture a resolved
// providers.ReadarrInstance once at approve-click time and carry it through
// the approval queue, which paces jobs out with a real delay
// (nextApprovalQueueDelay). If the admin changed the book_backend selector
// while a job was still sitting in the queue, the stale click-time backend
// would be used instead of whatever is currently selected — which is how a
// request landed on a decommissioned legacy Readarr host instead of
// Chaptarr. processAsyncApproval must resolve the backend itself, at the
// moment it actually runs, not receive it as a parameter.
func TestProcessAsyncApprovalResolvesBackendFreshEachRun(t *testing.T) {
	var hitsA, hitsB atomic.Int32
	backendA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/book" && r.Method == http.MethodPost {
			hitsA.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":101,"monitored":true,"statistics":{"bookFileCount":0}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer backendA.Close()
	backendB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/book" && r.Method == http.MethodPost {
			hitsB.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":202,"monitored":true,"statistics":{"bookFileCount":0}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer backendB.Close()

	s := newServerForTest(t)

	payload := []byte(`{"title":"Fresh Resolution Test","foreignBookId":"fb-fresh","foreignEditionId":"fe-fresh","author":{"name":"Test Author"}}`)
	newReq := func() int64 {
		id, err := s.db.CreateRequest(context.Background(), &db.Request{
			RequesterEmail: "user",
			Title:          "Fresh Resolution Test",
			Authors:        []string{"Test Author"},
			Format:         "ebook",
			Status:         "processing",
			ReadarrReq:     payload,
		})
		if err != nil {
			t.Fatalf("create request: %v", err)
		}
		return id
	}

	// First run: book_backend points at backend A.
	cfg := *s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = backendA.URL
	cfg.Readarr.Ebooks.APIKey = "key-a"
	if err := s.settings.Update(&cfg); err != nil {
		t.Fatalf("update settings (A): %v", err)
	}
	id1 := newReq()
	s.processAsyncApproval(id1, mustGetRequest(t, s, id1), "admin")
	if hitsA.Load() != 1 || hitsB.Load() != 0 {
		t.Fatalf("first run: expected backend A hit once, got A=%d B=%d", hitsA.Load(), hitsB.Load())
	}

	// Switch the selector to backend B, simulating an admin changing it while
	// a previously-enqueued job (which no longer carries a captured inst)
	// would still be waiting in the real queue.
	cfg2 := *s.settings.Get()
	cfg2.Readarr.Ebooks.BaseURL = backendB.URL
	cfg2.Readarr.Ebooks.APIKey = "key-b"
	if err := s.settings.Update(&cfg2); err != nil {
		t.Fatalf("update settings (B): %v", err)
	}
	id2 := newReq()
	s.processAsyncApproval(id2, mustGetRequest(t, s, id2), "admin")
	if hitsB.Load() != 1 {
		t.Fatalf("second run: expected backend B hit once after switching, got B=%d", hitsB.Load())
	}
	if hitsA.Load() != 1 {
		t.Fatalf("second run: backend A should not have been hit again, got A=%d", hitsA.Load())
	}
}

func mustGetRequest(t *testing.T, s *Server, id int64) *db.Request {
	t.Helper()
	req, err := s.db.GetRequest(context.Background(), id)
	if err != nil {
		t.Fatalf("get request %d: %v", id, err)
	}
	return req
}
