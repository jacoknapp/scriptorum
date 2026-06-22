package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAccountPageRequiresLogin(t *testing.T) {
	s := newServerForTest(t)
	req := httptest.NewRequest(http.MethodGet, "/account", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("expected unauthenticated /account to not return 200, got 200")
	}
}

func TestAccountSaveAndRender(t *testing.T) {
	s := newServerForTest(t)
	if _, err := s.db.CreateUser(context.Background(), "alice", "hash", false, false); err != nil {
		t.Fatalf("create user: %v", err)
	}
	r := s.Router()
	cookie := makeCookie(t, s, "alice", false)

	form := url.Values{
		"email":               {"alice@example.com"},
		"ntfy_topic":          {"alice-alerts"},
		"discord_webhook":     {"https://discord.com/api/webhooks/abc"},
		"webhook_url":         {"https://hook.example/me"},
		"notify_on_approved":  {"on"},
		"notify_on_available": {"on"},
	}
	saveReq := httptest.NewRequest(http.MethodPost, "/account/save", strings.NewReader(form.Encode()))
	saveReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	saveReq.AddCookie(cookie)
	saveRec := httptest.NewRecorder()
	r.ServeHTTP(saveRec, saveReq)
	if saveRec.Code != http.StatusFound {
		t.Fatalf("save: expected redirect, got %d %s", saveRec.Code, saveRec.Body.String())
	}

	u, err := s.db.GetUserByUsername(context.Background(), "alice")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if u.Email != "alice@example.com" || u.NotifyNtfyTopic != "alice-alerts" || !u.NotifyOnApproved || !u.NotifyOnAvailable {
		t.Fatalf("prefs not saved: %+v", u)
	}

	// The page should render the saved values back.
	getReq := httptest.NewRequest(http.MethodGet, "/account", nil)
	getReq.AddCookie(cookie)
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("account page: %d", getRec.Code)
	}
	if !strings.Contains(getRec.Body.String(), "alice@example.com") || !strings.Contains(getRec.Body.String(), "alice-alerts") {
		t.Fatalf("account page did not render saved values")
	}
}

// TestPersonalNotificationOnApproval verifies a requester gets a notification
// on their own configured channel (here a generic webhook) when their request
// is approved, and that opting out suppresses it.
func TestPersonalNotificationOnApproval(t *testing.T) {
	got := make(chan map[string]any, 4)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		got <- p
		w.WriteHeader(http.StatusOK)
	}))
	defer hook.Close()

	s := newServerForTest(t)
	if _, err := s.db.CreateUser(context.Background(), "alice", "hash", false, false); err != nil {
		t.Fatalf("create user: %v", err)
	}
	id, _ := s.db.GetUserByUsername(context.Background(), "alice")
	if err := s.db.UpdateUserNotificationPrefs(context.Background(), id.ID, "", "", "", hook.URL, true, true); err != nil {
		t.Fatalf("set prefs: %v", err)
	}

	// Requester is identified by the lowercased username stored as requester_email.
	s.SendApprovalNotification("alice", "Some Book", []string{"An Author"})

	select {
	case p := <-got:
		if p["event"] != "request.approved" {
			t.Fatalf("expected request.approved, got %v", p["event"])
		}
		if p["requester"] != "alice" {
			t.Fatalf("expected requester alice, got %v", p["requester"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for personal approval webhook")
	}

	// Opting out should suppress the personal notification.
	if err := s.db.UpdateUserNotificationPrefs(context.Background(), id.ID, "", "", "", hook.URL, false, false); err != nil {
		t.Fatalf("clear prefs: %v", err)
	}
	s.SendApprovalNotification("alice", "Another Book", []string{"An Author"})
	select {
	case p := <-got:
		t.Fatalf("expected no personal notification after opt-out, got %v", p["event"])
	case <-time.After(500 * time.Millisecond):
		// success
	}
}
