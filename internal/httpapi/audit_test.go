package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestAuditLogSwallowsDBError(t *testing.T) {
	s := newServerForTest(t)
	s.db.Close()

	// Must not panic when the underlying insert fails; the failure is logged
	// to stdout and otherwise swallowed since audit logging is best-effort.
	s.auditLog(context.Background(), "admin@example.com", "user.login", nil, "")
}

func TestAuditLogOnApproveDeclineAndUserCRUD(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	user := makeCookie(t, s, "user", false)
	admin := makeCookie(t, s, "admin", true)

	// Create a request, then approve it (no Readarr configured in tests).
	body := []byte(`{"title":"A","authors":["B"],"format":"ebook"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(user)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var obj map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &obj)
	id := int64(obj["id"].(float64))

	approveReq := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+strconv.FormatInt(id, 10)+"/approve", nil)
	approveReq.AddCookie(admin)
	approveRec := httptest.NewRecorder()
	r.ServeHTTP(approveRec, approveReq)
	if approveRec.Code != 200 {
		t.Fatalf("approve: %d %s", approveRec.Code, approveRec.Body.String())
	}

	// Create a second request to decline.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(user)
	r.ServeHTTP(rec2, req2)
	var obj2 map[string]any
	_ = json.Unmarshal(rec2.Body.Bytes(), &obj2)
	id2 := int64(obj2["id"].(float64))

	declineReq := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+strconv.FormatInt(id2, 10)+"/decline", nil)
	declineReq.AddCookie(admin)
	declineRec := httptest.NewRecorder()
	r.ServeHTTP(declineRec, declineReq)
	if declineRec.Code != 200 {
		t.Fatalf("decline: %d %s", declineRec.Code, declineRec.Body.String())
	}

	// Create a user via the admin UI form.
	form := strings.NewReader("username=newbie&password=verysecurepw&is_admin=on")
	createUserReq := httptest.NewRequest(http.MethodPost, "/users", form)
	createUserReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createUserReq.AddCookie(admin)
	createUserRec := httptest.NewRecorder()
	r.ServeHTTP(createUserRec, createUserReq)
	if createUserRec.Code != http.StatusFound {
		t.Fatalf("create user: %d %s", createUserRec.Code, createUserRec.Body.String())
	}

	events, err := s.db.ListAuditEvents(context.Background(), 50)
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}

	want := map[string]bool{
		"request.approved": false,
		"request.declined": false,
		"user.created":     false,
	}
	for _, ev := range events {
		if _, ok := want[ev.EventType]; ok {
			want[ev.EventType] = true
		}
	}
	for evType, found := range want {
		if !found {
			t.Errorf("expected audit event %q to be recorded, events: %+v", evType, events)
		}
	}
}

func TestAuditPageRequiresAdmin(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	user := makeCookie(t, s, "user", false)
	admin := makeCookie(t, s, "admin", true)

	req := httptest.NewRequest(http.MethodGet, "/audit", nil)
	req.AddCookie(user)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("expected non-admin to be denied, got 200")
	}

	req2 := httptest.NewRequest(http.MethodGet, "/audit", nil)
	req2.AddCookie(admin)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected admin to view audit page, got %d %s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), "Audit log") {
		t.Fatalf("expected audit page body to contain heading, got: %s", rec2.Body.String())
	}
}

func TestAuditExportCSV(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	admin := makeCookie(t, s, "admin", true)
	user := makeCookie(t, s, "user", false)

	// Generate at least one audit event via a user creation.
	form := strings.NewReader("username=csvuser&password=verysecurepw&is_admin=on")
	createUserReq := httptest.NewRequest(http.MethodPost, "/users", form)
	createUserReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createUserReq.AddCookie(admin)
	r.ServeHTTP(httptest.NewRecorder(), createUserReq)

	// Non-admin is denied.
	denyReq := httptest.NewRequest(http.MethodGet, "/audit/export", nil)
	denyReq.AddCookie(user)
	denyRec := httptest.NewRecorder()
	r.ServeHTTP(denyRec, denyReq)
	if denyRec.Code == http.StatusOK {
		t.Fatalf("expected non-admin to be denied CSV export, got 200")
	}

	// Admin gets a CSV download with header + the created event.
	req := httptest.NewRequest(http.MethodGet, "/audit/export", nil)
	req.AddCookie(admin)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("export code=%d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("expected text/csv content-type, got %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "scriptorum-audit.csv") {
		t.Fatalf("expected attachment filename, got %q", cd)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "timestamp,actor,event_type,request_id,details") {
		t.Fatalf("expected CSV header row, got: %s", body)
	}
	if !strings.Contains(body, "user.created") {
		t.Fatalf("expected user.created event in CSV, got: %s", body)
	}
}
