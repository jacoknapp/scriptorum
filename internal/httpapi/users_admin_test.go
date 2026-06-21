package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

func createTestUser(t *testing.T, s *Server, username string, admin, autoApprove bool) int64 {
	t.Helper()
	id, err := s.db.CreateUser(context.Background(), username, "irrelevant-hash", admin, autoApprove)
	if err != nil {
		t.Fatalf("create user %s: %v", username, err)
	}
	return id
}

func TestUsersDeleteViaQueryParam(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	id := createTestUser(t, s, "deleteme", false, false)

	req := httptest.NewRequest(http.MethodPost, "/users/delete?id="+strconv.FormatInt(id, 10), nil)
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("delete code=%d body=%s", rec.Code, rec.Body.String())
	}

	if _, err := s.db.GetUserByUsername(context.Background(), "deleteme"); err == nil {
		t.Fatal("expected user to be deleted")
	}

	events, err := s.db.ListAuditEvents(context.Background(), 50)
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.EventType == "user.deleted" && strings.Contains(ev.Details, strconv.FormatInt(id, 10)) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected user.deleted audit event, got %+v", events)
	}
}

func TestUsersDeleteViaFormValue(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	id := createTestUser(t, s, "deleteme2", false, false)

	form := url.Values{"id": {strconv.FormatInt(id, 10)}}
	req := httptest.NewRequest(http.MethodPost, "/users/delete", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("delete code=%d body=%s", rec.Code, rec.Body.String())
	}

	if _, err := s.db.GetUserByUsername(context.Background(), "deleteme2"); err == nil {
		t.Fatal("expected user to be deleted")
	}
}

func TestUsersEditTogglesAndPasswordReset(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	id := createTestUser(t, s, "editme", false, false)

	form := url.Values{
		"user_id":          {strconv.FormatInt(id, 10)},
		"is_admin":         {"on"},
		"is_auto_approve":  {"on"},
		"password":         {"newsecurepassword"},
		"confirm_password": {"newsecurepassword"},
	}
	req := httptest.NewRequest(http.MethodPost, "/users/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("edit code=%d body=%s", rec.Code, rec.Body.String())
	}

	u, err := s.db.GetUserByUsername(context.Background(), "editme")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if !u.IsAdmin || !u.AutoApprove {
		t.Fatalf("expected admin and auto-approve to be set, got %+v", u)
	}
	if u.Hash == "irrelevant-hash" {
		t.Fatal("expected password hash to be updated")
	}

	events, err := s.db.ListAuditEvents(context.Background(), 50)
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	var sawUpdated, sawPasswordReset bool
	for _, ev := range events {
		if ev.EventType == "user.updated" {
			sawUpdated = true
		}
		if ev.EventType == "user.password_reset" {
			sawPasswordReset = true
		}
	}
	if !sawUpdated || !sawPasswordReset {
		t.Errorf("expected user.updated and user.password_reset audit events, got %+v", events)
	}
}

func TestUsersEditPasswordMismatch(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	id := createTestUser(t, s, "mismatch", false, false)

	form := url.Values{
		"user_id":          {strconv.FormatInt(id, 10)},
		"password":         {"newpassword1"},
		"confirm_password": {"different-password"},
	}
	req := httptest.NewRequest(http.MethodPost, "/users/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("edit code=%d body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "error=") {
		t.Fatalf("expected redirect with error, got Location=%q", loc)
	}
}

func TestUsersEditInvalidPassword(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	id := createTestUser(t, s, "shortpw", false, false)

	form := url.Values{
		"user_id":          {strconv.FormatInt(id, 10)},
		"password":         {"short"},
		"confirm_password": {"short"},
	}
	req := httptest.NewRequest(http.MethodPost, "/users/edit", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("edit code=%d body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "error=") {
		t.Fatalf("expected redirect with error, got Location=%q", loc)
	}
}

func TestUsersCreateInvalidPassword(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()

	form := url.Values{
		"username": {"newperson"},
		"password": {"short"},
	}
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "error=") {
		t.Fatalf("expected redirect with error, got Location=%q", loc)
	}
	if _, err := s.db.GetUserByUsername(context.Background(), "newperson"); err == nil {
		t.Fatal("expected user not to be created")
	}
}

func TestUsersToggleAdmin(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	id := createTestUser(t, s, "toggleme", false, false)

	form := url.Values{"id": {strconv.FormatInt(id, 10)}}
	req := httptest.NewRequest(http.MethodPost, "/users/toggle", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("toggle code=%d body=%s", rec.Code, rec.Body.String())
	}

	u, err := s.db.GetUserByUsername(context.Background(), "toggleme")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if !u.IsAdmin {
		t.Fatal("expected admin flag to be toggled on")
	}

	events, err := s.db.ListAuditEvents(context.Background(), 50)
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.EventType == "user.updated" && strings.Contains(ev.Details, "admin=true") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected user.updated audit event with admin=true, got %+v", events)
	}
}
