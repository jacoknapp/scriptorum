package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
)

// TestAvailableNotificationFiresOnceOnTransition verifies that when a request's
// Readarr availability transitions to "available" during catalog sync, a
// request.available webhook fires exactly once (not again on subsequent syncs).
func TestAvailableNotificationFiresOnceOnTransition(t *testing.T) {
	events := make(chan map[string]any, 8)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		events <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/book" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`[{"id":77,"title":"Burn for Me","foreignBookId":"fb-1","foreignEditionId":"fe-1","monitored":true,"grabbed":false,"statistics":{"bookFileCount":1},"author":{"name":"Ilona Andrews"},"identifiers":[{"type":"isbn13","value":"9780316274147"}]}]`))
			return
		}
		http.NotFound(w, r)
	}))
	defer readarr.Close()

	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = readarr.URL
	cfg.Readarr.Ebooks.APIKey = "test-key"
	cfg.Setup.Completed = true
	// Enable only the webhook available channel so the available event is the
	// only thing that can reach our capture server.
	cfg.Notifications.Webhook.Enabled = true
	cfg.Notifications.Webhook.URL = hook.URL
	cfg.Notifications.Webhook.EnableAvailableNotifications = true
	if err := config.Save(s.cfgPath, cfg); err != nil {
		t.Fatalf("save cfg: %v", err)
	}
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	r := s.Router()

	// Create the request while the catalog is still empty (so it lands as
	// pending rather than being matched at create time).
	body := []byte(`{"title":"Burn for Me","authors":["Ilona Andrews"],"isbn13":"9780316274147","format":"ebook","provider_payload":"{\"title\":\"Burn for Me\",\"foreignBookId\":\"fb-1\",\"foreignEditionId\":\"fe-1\",\"author\":{\"name\":\"Ilona Andrews\"}}"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	createReq.AddCookie(makeCookie(t, s, "user", false))
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", createRec.Code, createRec.Body.String())
	}

	runSync := func() {
		syncReq := httptest.NewRequest(http.MethodPost, "/api/readarr/sync?kind=ebooks", nil)
		syncReq.AddCookie(makeCookie(t, s, "admin", true))
		syncRec := httptest.NewRecorder()
		r.ServeHTTP(syncRec, syncReq)
		if syncRec.Code != http.StatusOK {
			t.Fatalf("sync code=%d body=%s", syncRec.Code, syncRec.Body.String())
		}
	}

	// First sync: pending -> available, should fire request.available.
	runSync()
	select {
	case payload := <-events:
		if payload["event"] != "request.available" {
			t.Fatalf("expected request.available, got %v", payload["event"])
		}
		if payload["title"] != "Burn for Me" {
			t.Fatalf("unexpected title: %v", payload["title"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for request.available webhook")
	}

	// Second sync: already available, must NOT fire again.
	runSync()
	select {
	case payload := <-events:
		t.Fatalf("expected no further notification on re-sync, got %v", payload["event"])
	case <-time.After(500 * time.Millisecond):
		// success: no duplicate
	}
}

// TestGlobalApprovedAvailableTogglesAreIndependent verifies the global admin
// channels gate approved and available events on separate toggles.
func TestGlobalApprovedAvailableTogglesAreIndependent(t *testing.T) {
	type ev = map[string]any
	newCapture := func() (*httptest.Server, chan ev) {
		ch := make(chan ev, 4)
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p ev
			_ = json.NewDecoder(r.Body).Decode(&p)
			ch <- p
			w.WriteHeader(http.StatusOK)
		}))
		return srv, ch
	}

	// Approval-only: approved fires, available does not.
	t.Run("approval only", func(t *testing.T) {
		hook, ch := newCapture()
		defer hook.Close()
		s := newServerForTest(t)
		cfg := s.settings.Get()
		cfg.Notifications.Webhook.Enabled = true
		cfg.Notifications.Webhook.URL = hook.URL
		cfg.Notifications.Webhook.EnableApprovalNotifications = true
		cfg.Notifications.Webhook.EnableAvailableNotifications = false
		if err := s.settings.Update(cfg); err != nil {
			t.Fatalf("update: %v", err)
		}
		s.SendApprovalNotification("alice", "Book", nil)
		select {
		case p := <-ch:
			if p["event"] != "request.approved" {
				t.Fatalf("expected request.approved, got %v", p["event"])
			}
		case <-time.After(2 * time.Second):
			t.Fatal("expected approved webhook")
		}
		s.SendAvailableNotification("alice", "Book", nil)
		select {
		case p := <-ch:
			t.Fatalf("did not expect available webhook, got %v", p["event"])
		case <-time.After(400 * time.Millisecond):
		}
	})

	// Available-only: available fires, approved does not.
	t.Run("available only", func(t *testing.T) {
		hook, ch := newCapture()
		defer hook.Close()
		s := newServerForTest(t)
		cfg := s.settings.Get()
		cfg.Notifications.Webhook.Enabled = true
		cfg.Notifications.Webhook.URL = hook.URL
		cfg.Notifications.Webhook.EnableApprovalNotifications = false
		cfg.Notifications.Webhook.EnableAvailableNotifications = true
		if err := s.settings.Update(cfg); err != nil {
			t.Fatalf("update: %v", err)
		}
		s.SendAvailableNotification("alice", "Book", nil)
		select {
		case p := <-ch:
			if p["event"] != "request.available" {
				t.Fatalf("expected request.available, got %v", p["event"])
			}
		case <-time.After(2 * time.Second):
			t.Fatal("expected available webhook")
		}
		s.SendApprovalNotification("alice", "Book", nil)
		select {
		case p := <-ch:
			t.Fatalf("did not expect approved webhook, got %v", p["event"])
		case <-time.After(400 * time.Millisecond):
		}
	})
}
