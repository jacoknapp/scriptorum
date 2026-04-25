package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	"gitea.knapp/jacoknapp/scriptorum/internal/db"
)

func TestApprovalQueueCapacityKeepsWorstCaseWaitUnderMax(t *testing.T) {
	interval := 30 * time.Second
	jitter := 15 * time.Second
	maxWait := 3 * time.Hour

	got := approvalQueueCapacity(interval, jitter, maxWait)
	if got != 240 {
		t.Fatalf("expected capacity 240, got %d", got)
	}

	if worstCaseWait := time.Duration(got) * (interval + jitter); worstCaseWait > maxWait {
		t.Fatalf("worst-case queue wait exceeds max: %s > %s", worstCaseWait, maxWait)
	}
}

func TestStartBackgroundTasksRecoversProcessingApprovals(t *testing.T) {
	var addCalls atomic.Int32
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/book" && r.Method == http.MethodPost:
			addCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":901,"monitored":true,"statistics":{"bookFileCount":0}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer readarr.Close()

	s := newServerForTest(t)
	s.approvalQueueInterval = time.Millisecond
	s.approvalQueueJitter = 0
	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = readarr.URL
	cfg.Readarr.Ebooks.APIKey = "test-key"
	if err := config.Save(s.cfgPath, cfg); err != nil {
		t.Fatalf("save cfg: %v", err)
	}
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	requestID, err := s.db.CreateRequest(context.Background(), &db.Request{
		RequesterEmail: "reader@example.com",
		Title:          "Recovered Book",
		Authors:        []string{"Alice"},
		Format:         "ebook",
		Status:         "processing",
		StatusReason:   "approval in progress",
		ReadarrReq:     []byte(`{"title":"Recovered Book","foreignBookId":"fb-recovered","foreignEditionId":"fe-recovered","author":{"name":"Alice"}}`),
	})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.StartBackgroundTasks(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := s.db.GetRequest(context.Background(), requestID)
		if err != nil {
			t.Fatalf("get request: %v", err)
		}
		if got.Status == "queued" {
			if addCalls.Load() != 1 {
				t.Fatalf("expected recovered request to be sent once, got %d", addCalls.Load())
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("timed out waiting for processing approval recovery")
}
