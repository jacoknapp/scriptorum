package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
)

func TestReadarrSyncReconcilesAndBlocksDuplicates(t *testing.T) {
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/book":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"id":77,"title":"Burn for Me","foreignBookId":"fb-1","foreignEditionId":"fe-1","monitored":true,"grabbed":false,"statistics":{"bookFileCount":1},"author":{"name":"Ilona Andrews"},"identifiers":[{"type":"isbn13","value":"9780316274147"}]}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer readarr.Close()

	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = readarr.URL
	cfg.Readarr.Ebooks.APIKey = "test-key"
	cfg.Setup.Completed = true
	if err := config.Save(s.cfgPath, cfg); err != nil {
		t.Fatalf("save cfg: %v", err)
	}
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	r := s.Router()

	body := []byte(`{"title":"Burn for Me","authors":["Ilona Andrews"],"isbn13":"9780316274147","format":"ebook","provider_payload":"{\"title\":\"Burn for Me\",\"foreignBookId\":\"fb-1\",\"foreignEditionId\":\"fe-1\",\"author\":{\"name\":\"Ilona Andrews\"}}"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	req.AddCookie(makeCookie(t, s, "user", false))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create request code=%d body=%s", rec.Code, rec.Body.String())
	}

	syncReq := httptest.NewRequest(http.MethodPost, "/api/readarr/sync?kind=ebooks", nil)
	syncReq.AddCookie(makeCookie(t, s, "admin", true))
	syncReq.Header.Set("HX-Request", "true")
	syncRec := httptest.NewRecorder()
	r.ServeHTTP(syncRec, syncReq)
	if syncRec.Code != http.StatusOK {
		t.Fatalf("sync code=%d body=%s", syncRec.Code, syncRec.Body.String())
	}

	count, err := s.db.CountReadarrBooks(syncReq.Context(), "ebook")
	if err != nil {
		t.Fatalf("count synced books: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 synced book, got %d", count)
	}

	items, err := s.db.ListRequests(syncReq.Context(), "", 10)
	if err != nil {
		t.Fatalf("list requests: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 request, got %d", len(items))
	}
	if items[0].ExternalStatus != "available" {
		t.Fatalf("expected external status available, got %q", items[0].ExternalStatus)
	}
	if items[0].MatchedReadarrID != 77 {
		t.Fatalf("expected matched readarr id 77, got %d", items[0].MatchedReadarrID)
	}

	dupReq := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	dupReq.AddCookie(makeCookie(t, s, "user", false))
	dupReq.Header.Set("Content-Type", "application/json")
	dupRec := httptest.NewRecorder()
	r.ServeHTTP(dupRec, dupReq)
	if dupRec.Code != http.StatusConflict {
		t.Fatalf("expected duplicate request conflict, got %d body=%s", dupRec.Code, dupRec.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(dupRec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode duplicate response: %v", err)
	}
	if payload["status"] != "exists" {
		t.Fatalf("expected status exists, got %#v", payload["status"])
	}
}

func TestReadarrAutoSyncLoopImportsCatalog(t *testing.T) {
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/book":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"id":88,"title":"Clean Sweep","foreignBookId":"fb-2","foreignEditionId":"fe-2","monitored":true,"grabbed":false,"statistics":{"bookFileCount":1},"author":{"name":"Ilona Andrews"},"identifiers":[{"type":"isbn13","value":"9780440000179"}]}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer readarr.Close()

	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = readarr.URL
	cfg.Readarr.Ebooks.APIKey = "test-key"
	cfg.Setup.Completed = true
	if err := config.Save(s.cfgPath, cfg); err != nil {
		t.Fatalf("save cfg: %v", err)
	}
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.runReadarrSyncLoop(ctx, 10*time.Millisecond, time.Hour)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		count, err := s.db.CountReadarrBooks(context.Background(), "ebook")
		if err != nil {
			t.Fatalf("count synced books: %v", err)
		}
		if count == 1 {
			view := s.readarrSyncView()
			if !strings.Contains(strings.ToLower(view.LastRunLabel), "automatic") {
				t.Fatalf("expected automatic sync label, got %q", view.LastRunLabel)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("timed out waiting for automatic readarr sync")
}
