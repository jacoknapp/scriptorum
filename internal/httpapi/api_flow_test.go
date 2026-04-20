package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/bootstrap"
	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	"gitea.knapp/jacoknapp/scriptorum/internal/db"
)

func newServerForTest(t *testing.T) *Server {
	t.Helper()
	tdir := t.TempDir()
	cfgPath := filepath.Join(tdir, "config.yaml")
	dbPath := filepath.Join(tdir, "scriptorum.db")
	cfg, database, err := bootstrap.EnsureFirstRun(context.Background(), cfgPath, dbPath)
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	cfg.Admins.Usernames = []string{"admin"}
	cfg.Setup.Completed = true
	_ = config.Save(cfgPath, cfg)
	s := NewServer(cfg, database, cfgPath)
	s.disableCSRF = true // Disable CSRF for tests
	s.disableDiscoveryWarmup = true
	return s
}

func makeCookie(t *testing.T, s *Server, username string, admin bool) *http.Cookie {
	sess := &session{Username: strings.ToLower(username), Name: "T", Admin: admin, Exp: 9999999999}
	b, _ := json.Marshal(sess)
	sig := s.sign(b)
	val := base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(sig)
	return &http.Cookie{Name: "scriptorum_session", Value: val, Path: "/"}
}

func TestHealthAndSetupGate(t *testing.T) {
	s := newServerForTest(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("healthz code=%d", rec.Code)
	}
}

func TestCreateAndApproveFlow(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()

	// Create (requires login)
	body := []byte(`{"title":"Book","authors":["Alice"],"format":"ebook"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	req.AddCookie(makeCookie(t, s, "user", false))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	id := int(resp["id"].(float64))

	// Approve (requires admin). Readarr endpoints are unset, so it may 502/404. Only assert no panic & valid HTTP.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+strconv.Itoa(id)+"/approve", nil)
	req2.AddCookie(makeCookie(t, s, "admin", true))
	rec2 := httptest.NewRecorder()
	r.ServeHTTP(rec2, req2)
	if rec2.Code != 200 && rec2.Code != 502 && rec2.Code != 404 {
		t.Fatalf("approve unexpected code=%d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestCreateRequestPersistsCoverURLFromProviderPayload(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()

	body := []byte(`{"title":"Book","authors":["Alice"],"format":"ebook","provider_payload":"{\"title\":\"Book\",\"remoteCover\":\"https://covers.example.test/book.jpg\"}"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	req.AddCookie(makeCookie(t, s, "user", false))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != 201 {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	id := int64(resp["id"].(float64))

	stored, err := s.db.GetRequest(req.Context(), id)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if stored.CoverURL != "https://covers.example.test/book.jpg" {
		t.Fatalf("expected stored cover url, got %q", stored.CoverURL)
	}
}

func TestApproveRequestUsesCatalogMatchWithoutSubmittingDuplicate(t *testing.T) {
	var addCalls atomic.Int32
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/book" && r.Method == http.MethodPost {
			addCalls.Add(1)
		}
		http.NotFound(w, r)
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

	r := s.Router()
	body := []byte(`{"title":"Burn for Me","authors":["Ilona Andrews"],"isbn13":"9780316274147","format":"ebook","provider_payload":"{\"title\":\"Burn for Me\",\"foreignBookId\":\"fb-1\",\"foreignEditionId\":\"fe-1\",\"author\":{\"name\":\"Ilona Andrews\"}}"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	req.AddCookie(makeCookie(t, s, "user", false))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}

	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := int(created["id"].(float64))

	matchBody := []byte(`{"id":77,"title":"Burn for Me","monitored":true,"statistics":{"bookFileCount":1},"author":{"name":"Ilona Andrews"}}`)
	if err := s.db.ReplaceReadarrBooks(context.Background(), "ebook", []db.ReadarrBook{{
		SourceKind:       "ebook",
		ReadarrID:        77,
		Title:            "Burn for Me",
		AuthorName:       "Ilona Andrews",
		ISBN13:           "9780316274147",
		ForeignBookID:    "fb-1",
		ForeignEditionID: "fe-1",
		Monitored:        true,
		BookFileCount:    1,
		ReadarrData:      matchBody,
	}}); err != nil {
		t.Fatalf("replace readarr books: %v", err)
	}
	s.clearCatalogMatchCache()

	approveReq := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+strconv.Itoa(id)+"/approve", nil)
	approveReq.AddCookie(makeCookie(t, s, "admin", true))
	approveRec := httptest.NewRecorder()
	r.ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve code=%d body=%s", approveRec.Code, approveRec.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := s.db.GetRequest(context.Background(), int64(id))
		if err != nil {
			t.Fatalf("get request: %v", err)
		}
		if got.Status == "queued" {
			if got.ExternalStatus != "available" {
				t.Fatalf("expected external status available, got %q", got.ExternalStatus)
			}
			if got.MatchedReadarrID != 77 {
				t.Fatalf("expected matched readarr id 77, got %d", got.MatchedReadarrID)
			}
			if addCalls.Load() != 0 {
				t.Fatalf("expected no add calls when catalog already matched, got %d", addCalls.Load())
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("timed out waiting for approval to complete from catalog match")
}

func TestApproveAllSubmitsPendingRequestsToReadarr(t *testing.T) {
	var addCalls atomic.Int32
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/book" && r.Method == http.MethodPost:
			id := addCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":` + strconv.FormatInt(int64(100+id), 10) + `,"monitored":true,"statistics":{"bookFileCount":0}}`))
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

	r := s.Router()
	for _, title := range []string{"One", "Two"} {
		body := []byte(`{"title":"` + title + `","authors":["Alice"],"format":"ebook","provider_payload":"{\"title\":\"` + title + `\",\"foreignBookId\":\"fb-` + title + `\",\"foreignEditionId\":\"fe-` + title + `\",\"author\":{\"name\":\"Alice\"}}"}`)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
		req.AddCookie(makeCookie(t, s, "user", false))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	approveAllReq := httptest.NewRequest(http.MethodPost, "/api/v1/requests/approve-all", nil)
	approveAllReq.AddCookie(makeCookie(t, s, "admin", true))
	approveAllRec := httptest.NewRecorder()
	r.ServeHTTP(approveAllRec, approveAllReq)
	if approveAllRec.Code != http.StatusOK {
		t.Fatalf("approve-all code=%d body=%s", approveAllRec.Code, approveAllRec.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		items, err := s.db.ListRequests(context.Background(), "", 10)
		if err != nil {
			t.Fatalf("list requests: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("expected 2 requests, got %d", len(items))
		}
		allQueued := true
		for _, item := range items {
			if item.Status != "queued" {
				allQueued = false
				break
			}
		}
		if allQueued {
			if addCalls.Load() != 2 {
				t.Fatalf("expected 2 add calls, got %d", addCalls.Load())
			}
			for _, item := range items {
				if item.MatchedReadarrID == 0 {
					t.Fatalf("expected matched readarr id to be stored for request %d", item.ID)
				}
				if item.ExternalStatus != "monitored" {
					t.Fatalf("expected external status monitored for request %d, got %q", item.ID, item.ExternalStatus)
				}
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatal("timed out waiting for approve-all to submit requests")
}
