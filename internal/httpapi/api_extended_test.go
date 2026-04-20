package httpapi

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	"gitea.knapp/jacoknapp/scriptorum/internal/db"
)

func TestDeleteRequestAndDeleteAllRequests(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	user := makeCookie(t, s, "user", false)
	admin := makeCookie(t, s, "admin", true)

	create := func(title string) int {
		t.Helper()
		body := []byte(`{"title":"` + title + `","authors":["Author"],"format":"ebook"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(user)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", title, rec.Code, rec.Body.String())
		}
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return int(out["id"].(float64))
	}

	firstID := create("First")
	_ = create("Second")

	deleteReq := httptest.NewRequest(http.MethodDelete, "/api/v1/requests/"+strconv.Itoa(firstID), nil)
	deleteReq.AddCookie(admin)
	deleteRec := httptest.NewRecorder()
	r.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete request: %d %s", deleteRec.Code, deleteRec.Body.String())
	}
	if _, err := s.db.GetRequest(context.Background(), int64(firstID)); err != sql.ErrNoRows {
		t.Fatalf("expected deleted request to be gone, err=%v", err)
	}

	deleteAllReq := httptest.NewRequest(http.MethodDelete, "/api/v1/requests", nil)
	deleteAllReq.AddCookie(admin)
	deleteAllRec := httptest.NewRecorder()
	r.ServeHTTP(deleteAllRec, deleteAllReq)
	if deleteAllRec.Code != http.StatusOK {
		t.Fatalf("delete all requests: %d %s", deleteAllRec.Code, deleteAllRec.Body.String())
	}

	items, err := s.db.ListRequests(context.Background(), "", 20)
	if err != nil {
		t.Fatalf("list requests: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected all requests deleted, got %d", len(items))
	}
}

func TestHydrateRequestSuccessAndAlreadyAttached(t *testing.T) {
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/book/lookup":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{
				"title":"Burn for Me",
				"titleSlug":"burn-for-me",
				"foreignBookId":"fb-1",
				"foreignEditionId":"fe-1",
				"authorTitle":"andrews, ilona Burn for Me"
			}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer readarr.Close()

	s := newServerForTest(t)
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
		RequesterEmail: "user@example.com",
		Title:          "Burn for Me",
		Authors:        []string{"Ilona Andrews"},
		Format:         "ebook",
		Status:         "pending",
	})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	r := s.Router()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+strconv.FormatInt(requestID, 10)+"/hydrate", nil)
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("hydrate: %d %s", rec.Code, rec.Body.String())
	}

	stored, err := s.db.GetRequest(context.Background(), requestID)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if len(stored.ReadarrReq) == 0 {
		t.Fatal("expected hydrate to store request payload")
	}
	if stored.StatusReason != "hydrated" {
		t.Fatalf("expected hydrated status reason, got %q", stored.StatusReason)
	}
	var payload map[string]any
	if err := json.Unmarshal(stored.ReadarrReq, &payload); err != nil {
		t.Fatalf("unmarshal stored payload: %v", err)
	}
	author, _ := payload["author"].(map[string]any)
	if got, _ := author["name"].(string); got != "Ilona Andrews" {
		t.Fatalf("expected parsed author name, got %q", got)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+strconv.FormatInt(requestID, 10)+"/hydrate", nil)
	req2.AddCookie(makeCookie(t, s, "admin", true))
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK || !strings.Contains(rec2.Body.String(), "already attached") {
		t.Fatalf("rehydrate: %d %s", rec2.Code, rec2.Body.String())
	}
}

func TestHydrateRequestErrors(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()

	notFoundReq := httptest.NewRequest(http.MethodPost, "/api/v1/requests/9999/hydrate", nil)
	notFoundReq.AddCookie(makeCookie(t, s, "admin", true))
	notFoundRec := httptest.NewRecorder()
	r.ServeHTTP(notFoundRec, notFoundReq)
	if notFoundRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing request, got %d", notFoundRec.Code)
	}

	requestID, err := s.db.CreateRequest(context.Background(), &db.Request{
		RequesterEmail: "user@example.com",
		Title:          "No Config",
		Authors:        []string{"Author"},
		Format:         "ebook",
		Status:         "pending",
	})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	noConfigReq := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+strconv.FormatInt(requestID, 10)+"/hydrate", nil)
	noConfigReq.AddCookie(makeCookie(t, s, "admin", true))
	noConfigRec := httptest.NewRecorder()
	r.ServeHTTP(noConfigRec, noConfigReq)
	if noConfigRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when readarr missing, got %d", noConfigRec.Code)
	}
}

func TestBookDetailsVariants(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()

	providerPayload := map[string]any{
		"title":       "Project Hail Mary",
		"overview":    "A science mission gone sideways.",
		"images":      []map[string]any{{"remoteUrl": "https://covers.example/phm.jpg"}},
		"authors":     []string{"Andy Weir"},
		"description": "",
	}
	ppJSON, _ := json.Marshal(providerPayload)
	body, _ := json.Marshal(map[string]any{
		"provider_payload": string(ppJSON),
		"isbn13":           "9780593135204",
		"format":           "ebook",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/book/details", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeCookie(t, s, "user", false))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("book details via provider payload: %d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("json: %v", err)
	}
	if out["title"] != "Project Hail Mary" || out["isbn13"] != "9780593135204" {
		t.Fatalf("unexpected provider payload details: %+v", out)
	}
	if got := out["authors"].([]any); len(got) != 1 || got[0].(string) != "Andy Weir" {
		t.Fatalf("unexpected authors: %+v", out["authors"])
	}
	if out["cover"] != "https://covers.example/phm.jpg" {
		t.Fatalf("unexpected cover: %+v", out)
	}

	fallbackBody, _ := json.Marshal(map[string]any{
		"title":   "Fallback Book",
		"authors": []string{"Fallback Author"},
		"format":  "ebook",
	})
	fallbackReq := httptest.NewRequest(http.MethodPost, "/api/v1/book/details", bytes.NewReader(fallbackBody))
	fallbackReq.Header.Set("Content-Type", "application/json")
	fallbackReq.AddCookie(makeCookie(t, s, "user", false))
	fallbackRec := httptest.NewRecorder()
	r.ServeHTTP(fallbackRec, fallbackReq)
	if fallbackRec.Code != http.StatusOK {
		t.Fatalf("book details fallback: %d %s", fallbackRec.Code, fallbackRec.Body.String())
	}

	emptyReq := httptest.NewRequest(http.MethodPost, "/api/v1/book/details", bytes.NewReader([]byte(`{}`)))
	emptyReq.Header.Set("Content-Type", "application/json")
	emptyReq.AddCookie(makeCookie(t, s, "user", false))
	emptyRec := httptest.NewRecorder()
	r.ServeHTTP(emptyRec, emptyReq)
	if emptyRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing query, got %d", emptyRec.Code)
	}
}

func TestParseAuthorNameFromTitle(t *testing.T) {
	if got := parseAuthorNameFromTitle("andrews, ilona Burn for Me"); got != "Ilona Andrews" {
		t.Fatalf("unexpected parsed author: %q", got)
	}
	if got := parseAuthorNameFromTitle("becky chambers"); got != "Chambers Becky" {
		t.Fatalf("unexpected plain author formatting: %q", got)
	}
}
