package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestCreateListDeclineLifecycle(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	user := makeCookie(t, s, "user@example.com", false)
	admin := makeCookie(t, s, "admin@example.com", true)

	// Create
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
	id := int(obj["id"].(float64))

	// List
	req2 := httptest.NewRequest(http.MethodGet, "/api/v1/requests", nil)
	req2.AddCookie(user)
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("list: %d", rec2.Code)
	}

	// Decline (admin)
	req3 := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+strconv.Itoa(id)+"/decline", nil)
	req3.AddCookie(admin)
	rec3 := httptest.NewRecorder()
	r.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("decline: %d %s", rec3.Code, rec3.Body.String())
	}
}
