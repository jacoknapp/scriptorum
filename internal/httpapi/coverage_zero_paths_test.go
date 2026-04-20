package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	"gitea.knapp/jacoknapp/scriptorum/internal/db"
	"github.com/go-chi/chi/v5"
)

func requestWithTokenParam(path, token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("token", token)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestRoundTripperFunc(t *testing.T) {
	called := false
	rt := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		return &http.Response{StatusCode: http.StatusNoContent, Body: http.NoBody, Header: make(http.Header)}, nil
	})

	req := httptest.NewRequest(http.MethodGet, "http://example.test", nil)
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip error: %v", err)
	}
	if !called || resp.StatusCode != http.StatusNoContent {
		t.Fatalf("unexpected RoundTrip result: called=%v status=%d", called, resp.StatusCode)
	}
}

func TestHandleApprovalTokenInvalidAndExpired(t *testing.T) {
	s := makeTestServer(t)

	invalidRec := httptest.NewRecorder()
	s.handleApprovalToken(invalidRec, requestWithTokenParam("/approve/invalid", "invalid"))
	if invalidRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for invalid token, got %d", invalidRec.Code)
	}

	s.tokenMutex.Lock()
	s.approvalTokens["expired-token"] = approvalTokenData{
		RequestID: 1,
		Action:    "approve",
		ExpiresAt: time.Now().Add(-1 * time.Minute),
	}
	s.tokenMutex.Unlock()

	expiredRec := httptest.NewRecorder()
	s.handleApprovalToken(expiredRec, requestWithTokenParam("/approve/expired-token", "expired-token"))
	if expiredRec.Code != http.StatusGone {
		t.Fatalf("expected 410 for expired token, got %d", expiredRec.Code)
	}
}

func TestHandleApprovalTokenDeclinePath(t *testing.T) {
	s := makeTestServer(t)
	id, err := s.db.CreateRequest(context.Background(), &db.Request{
		RequesterEmail: "user@example.com",
		Title:          "Decline Me",
		Format:         "ebook",
		Status:         "pending",
	})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	token := "decline-token"
	s.tokenMutex.Lock()
	s.approvalTokens[token] = approvalTokenData{
		RequestID: id,
		Action:    "decline",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	s.tokenMutex.Unlock()

	rec := httptest.NewRecorder()
	s.handleApprovalToken(rec, requestWithTokenParam("/approve/decline-token", token))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for decline token, got %d", rec.Code)
	}

	updated, err := s.db.GetRequest(context.Background(), id)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if updated.Status != "declined" {
		t.Fatalf("expected declined status, got %q", updated.Status)
	}
}

func TestProcessApprovalNoReadarrConfigured(t *testing.T) {
	s := makeTestServer(t)
	res := s.processApproval(context.Background(), &db.Request{
		ID:             999,
		RequesterEmail: "user@example.com",
		Title:          "No Readarr",
		Authors:        []string{"Example Author"},
		Format:         "ebook",
	}, "admin")

	if res == nil || res.Error != nil || res.Status != "approved" {
		t.Fatalf("unexpected approval result: %+v", res)
	}
}

func TestStartBackgroundTasksHandlesNilContext(t *testing.T) {
	s := makeTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	s.StartBackgroundTasks(nil)
	s.StartBackgroundTasks(ctx)
}

func TestAsyncNotificationHelpersExecute(t *testing.T) {
	s := makeTestServer(t)

	cfg := &config.Config{}
	cfg.ServerURL = "https://scriptorum.example"

	// Intentionally incomplete provider configs make async calls fail fast
	// without touching external services.
	s.sendRequestNotificationSMTP(cfg, 42, "alice", "Book", "Author")
	s.sendRequestNotificationDiscord(cfg, 42, "alice", "Book", "Author")
	s.sendApprovalNotificationDiscord(cfg, "alice", "Book", "Author")
	s.sendSystemNotificationNtfy(cfg, "Alert", "Something happened")
	s.sendSystemNotificationSMTP(cfg, "Alert", "Something happened")

	time.Sleep(120 * time.Millisecond)
}
