package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSendWebhookNotificationEmptyURL(t *testing.T) {
	s := newServerForTest(t)
	if err := s.sendWebhookNotification("", map[string]any{"event": "test"}); err == nil {
		t.Fatal("expected error for empty webhook URL")
	}
}

func TestSendWebhookNotificationNonSuccessStatus(t *testing.T) {
	hookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer hookServer.Close()

	s := newServerForTest(t)
	err := s.sendWebhookNotification(hookServer.URL, map[string]any{"event": "test"})
	if err == nil {
		t.Fatal("expected error for non-2xx webhook response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("expected error to mention status 500, got: %v", err)
	}
}

func TestSendWebhookNotificationUnreachableHost(t *testing.T) {
	s := newServerForTest(t)
	// Port 0 on localhost is never a valid connection target.
	err := s.sendWebhookNotification("http://127.0.0.1:0/hook", map[string]any{"event": "test"})
	if err == nil {
		t.Fatal("expected error for unreachable webhook host")
	}
}

func TestApiTestWebhookInvalidJSON(t *testing.T) {
	s := newServerForTest(t)
	req := httptest.NewRequest("POST", "/api/notifications/test-webhook", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxUser, &session{Username: "admin", Admin: true}))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["success"] != false {
		t.Fatalf("expected success=false, got %+v", resp)
	}
}

func TestApiTestWebhookEmptyURL(t *testing.T) {
	s := newServerForTest(t)
	body, _ := json.Marshal(map[string]string{"url": ""})
	req := httptest.NewRequest("POST", "/api/notifications/test-webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxUser, &session{Username: "admin", Admin: true}))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestApiTestWebhookSendFailure(t *testing.T) {
	s := newServerForTest(t)
	body, _ := json.Marshal(map[string]string{"url": "http://127.0.0.1:0/hook"})
	req := httptest.NewRequest("POST", "/api/notifications/test-webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(context.WithValue(req.Context(), ctxUser, &session{Username: "admin", Admin: true}))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["success"] != false {
		t.Fatalf("expected success=false, got %+v", resp)
	}
}

func TestSendApprovalNotificationWebhookDelivery(t *testing.T) {
	received := make(chan map[string]any, 1)
	hookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		received <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer hookServer.Close()

	server := newServerForTest(t)
	cfg := server.settings.Get()
	cfg.Notifications.Webhook.Enabled = true
	cfg.Notifications.Webhook.URL = hookServer.URL
	cfg.Notifications.Webhook.EnableApprovalNotifications = true
	if err := server.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	server.SendApprovalNotification("alice", "Some Book", []string{"Some Author"})

	select {
	case payload := <-received:
		if payload["event"] != "request.approved" {
			t.Errorf("expected event=request.approved, got %v", payload["event"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for webhook delivery")
	}
}

func TestSendSystemNotificationWebhookDelivery(t *testing.T) {
	received := make(chan map[string]any, 1)
	hookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		received <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer hookServer.Close()

	server := newServerForTest(t)
	cfg := server.settings.Get()
	cfg.Notifications.Webhook.Enabled = true
	cfg.Notifications.Webhook.URL = hookServer.URL
	cfg.Notifications.Webhook.EnableSystemNotifications = true
	if err := server.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	server.SendSystemNotification("Disk space low", "Only 2% free space remaining.")

	select {
	case payload := <-received:
		if payload["event"] != "system.alert" {
			t.Errorf("expected event=system.alert, got %v", payload["event"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for webhook delivery")
	}
}

func TestNotificationsPageContainsWebhookSection(t *testing.T) {
	server := newServerForTest(t)
	adminSession := &session{Username: "admin", Admin: true}

	req := httptest.NewRequest("GET", "/notifications", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxUser, adminSession))
	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "webhook_enabled") {
		t.Error("page should contain webhook enable checkbox")
	}
}

func TestNotificationsSaveWebhookSettings(t *testing.T) {
	server := newServerForTest(t)
	adminSession := &session{Username: "admin", Admin: true}

	formData := url.Values{
		"_csrf_token":                           {server.getCSRFToken(httptest.NewRequest("GET", "/", nil))},
		"webhook_enabled":                       {"on"},
		"webhook_url":                           {"https://example.com/hooks/scriptorum"},
		"webhook_enable_request_notifications":  {"on"},
		"webhook_enable_approval_notifications": {"on"},
		"webhook_enable_system_notifications":   {"on"},
	}

	req := httptest.NewRequest("POST", "/notifications/save", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req = req.WithContext(context.WithValue(req.Context(), ctxUser, adminSession))
	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect, got %d %s", rec.Code, rec.Body.String())
	}

	cfg := server.settings.Get()
	if !cfg.Notifications.Webhook.Enabled {
		t.Error("expected webhook to be enabled")
	}
	if cfg.Notifications.Webhook.URL != "https://example.com/hooks/scriptorum" {
		t.Errorf("unexpected webhook URL: %s", cfg.Notifications.Webhook.URL)
	}
	if !cfg.Notifications.Webhook.EnableRequestNotifications || !cfg.Notifications.Webhook.EnableApprovalNotifications || !cfg.Notifications.Webhook.EnableSystemNotifications {
		t.Error("expected all webhook event toggles to be enabled")
	}
}

func TestApiTestWebhook(t *testing.T) {
	var received map[string]any
	var mu sync.Mutex
	hookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		_ = json.NewDecoder(r.Body).Decode(&received)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer hookServer.Close()

	server := newServerForTest(t)
	body, _ := json.Marshal(map[string]string{"url": hookServer.URL})
	req := httptest.NewRequest("POST", "/api/notifications/test-webhook", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	adminSession := &session{Username: "admin", Admin: true}
	req = req.WithContext(context.WithValue(req.Context(), ctxUser, adminSession))
	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d %s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["success"] != true {
		t.Fatalf("expected success=true, got %+v", resp)
	}

	mu.Lock()
	defer mu.Unlock()
	if received == nil || received["event"] != "system.test" {
		t.Fatalf("expected webhook server to receive system.test event, got %+v", received)
	}
}

func TestSendRequestNotificationWebhookDelivery(t *testing.T) {
	received := make(chan map[string]any, 1)
	hookServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		received <- payload
		w.WriteHeader(http.StatusOK)
	}))
	defer hookServer.Close()

	server := newServerForTest(t)
	cfg := server.settings.Get()
	cfg.Notifications.Webhook.Enabled = true
	cfg.Notifications.Webhook.URL = hookServer.URL
	cfg.Notifications.Webhook.EnableRequestNotifications = true
	if err := server.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	server.SendRequestNotification(42, "alice", "Some Book", []string{"Some Author"})

	select {
	case payload := <-received:
		if payload["event"] != "request.created" {
			t.Errorf("expected event=request.created, got %v", payload["event"])
		}
		if payload["title"] != "Some Book" {
			t.Errorf("expected title=Some Book, got %v", payload["title"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for webhook delivery")
	}
}
