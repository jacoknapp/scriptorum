package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/db"
)

// TestSearchDispatchRetriesTransientFailure pins the fix for a real incident:
// search dispatch batches every pending job into one SearchBooks call per
// format, and a single failure marked every request in that batch as "error".
// A brief DNS blip during a restart therefore flipped 17 healthy requests to
// "error" in one go — they were only being re-dispatched because the server
// had restarted, not because anything was wrong with them. A transient
// failure must leave the request queued and retry on a later tick.
func TestSearchDispatchRetriesTransientFailure(t *testing.T) {
	var calls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/command" && r.Method == http.MethodPost {
			// Fail the first dispatch, succeed on the retry.
			if calls.Add(1) == 1 {
				http.Error(w, "boom", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":1,"name":"BookSearch"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer backend.Close()

	s := newServerForTest(t)
	cfg := *s.settings.Get()
	cfg.BookBackend = "readarr"
	cfg.Readarr.Ebooks.BaseURL = backend.URL
	cfg.Readarr.Ebooks.APIKey = "key"
	if err := s.settings.Update(&cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	id, err := s.db.CreateRequest(context.Background(), &db.Request{
		RequesterEmail:   "user",
		Title:            "Retry Me",
		Format:           "ebook",
		Status:           "queued",
		MatchedReadarrID: 77,
	})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	s.searchDispatchQueue <- searchDispatchJob{requestID: id, readarrID: 77, format: "ebook", username: "user"}

	// First flush fails; the request must stay queued, not become an error.
	s.flushSearchDispatchQueue(context.Background())
	got, err := s.db.GetRequest(context.Background(), id)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if got.Status != "queued" {
		t.Fatalf("after transient failure: expected status queued, got %q (%s)", got.Status, got.StatusReason)
	}

	// Second flush drains the re-queued job and succeeds.
	s.flushSearchDispatchQueue(context.Background())
	got, err = s.db.GetRequest(context.Background(), id)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if got.Status != "queued" {
		t.Fatalf("after retry: expected status queued, got %q (%s)", got.Status, got.StatusReason)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 dispatch attempts, got %d", calls.Load())
	}
}

// A backend that never recovers must still land the request in "error" so it
// doesn't retry forever silently.
func TestSearchDispatchGivesUpAfterMaxAttempts(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "always down", http.StatusInternalServerError)
	}))
	defer backend.Close()

	s := newServerForTest(t)
	cfg := *s.settings.Get()
	cfg.BookBackend = "readarr"
	cfg.Readarr.Ebooks.BaseURL = backend.URL
	cfg.Readarr.Ebooks.APIKey = "key"
	if err := s.settings.Update(&cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	id, err := s.db.CreateRequest(context.Background(), &db.Request{
		RequesterEmail:   "user",
		Title:            "Never Works",
		Format:           "ebook",
		Status:           "queued",
		MatchedReadarrID: 88,
	})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	s.searchDispatchQueue <- searchDispatchJob{requestID: id, readarrID: 88, format: "ebook", username: "user"}
	for i := 0; i < maxSearchDispatchAttempts; i++ {
		s.flushSearchDispatchQueue(context.Background())
	}

	got, err := s.db.GetRequest(context.Background(), id)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if got.Status != "error" {
		t.Fatalf("expected status error after %d attempts, got %q (%s)", maxSearchDispatchAttempts, got.Status, got.StatusReason)
	}
}
