package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	"gitea.knapp/jacoknapp/scriptorum/internal/db"
)

// readarrSearchStub records monitor and search calls so tests can assert which
// Readarr actions a request triggered.
type readarrSearchStub struct {
	server      *httptest.Server
	monitorPUTs atomic.Int32
	searchPOSTs atomic.Int32
	addPOSTs    atomic.Int32
}

func newReadarrSearchStub() *readarrSearchStub {
	stub := &readarrSearchStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/book/monitor" && r.Method == http.MethodPut:
			stub.monitorPUTs.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
		case r.URL.Path == "/api/v1/command" && r.Method == http.MethodPost:
			stub.searchPOSTs.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":1,"name":"BookSearch"}`))
		case r.URL.Path == "/api/v1/book" && r.Method == http.MethodPost:
			stub.addPOSTs.Add(1)
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	return stub
}

func (st *readarrSearchStub) close() { st.server.Close() }

// configureEbooksReadarr points the ebooks Readarr instance at the given URL.
func configureEbooksReadarr(t *testing.T, s *Server, baseURL string) {
	t.Helper()
	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = baseURL
	cfg.Readarr.Ebooks.APIKey = "test-key"
	if err := config.Save(s.cfgPath, cfg); err != nil {
		t.Fatalf("save cfg: %v", err)
	}
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}
}

// seedCatalogBook inserts a single catalog entry that matches the test payload
// used below (Burn for Me / Ilona Andrews).
func seedCatalogBook(t *testing.T, s *Server, book db.ReadarrBook) {
	t.Helper()
	book.SourceKind = "ebook"
	book.Title = "Burn for Me"
	book.AuthorName = "Ilona Andrews"
	book.ISBN13 = "9780316274147"
	book.ForeignBookID = "fb-1"
	book.ForeignEditionID = "fe-1"
	if err := s.db.ReplaceReadarrBooks(context.Background(), "ebook", []db.ReadarrBook{book}); err != nil {
		t.Fatalf("replace readarr books: %v", err)
	}
	s.clearCatalogMatchCache()
}

const createSearchPayload = `{"title":"Burn for Me","authors":["Ilona Andrews"],"isbn13":"9780316274147","format":"ebook","provider_payload":"{\"title\":\"Burn for Me\",\"foreignBookId\":\"fb-1\",\"foreignEditionId\":\"fe-1\",\"author\":{\"name\":\"Ilona Andrews\"}}"}`

func postCreateRequest(t *testing.T, s *Server, hx bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader([]byte(createSearchPayload)))
	req.AddCookie(makeCookie(t, s, "user", false))
	req.Header.Set("Content-Type", "application/json")
	if hx {
		req.Header.Set("HX-Request", "true")
	}
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	return rec
}

// When a matching title exists in Readarr but is unmonitored and undownloaded,
// the request should enable monitoring and trigger a search instead of being
// rejected as a duplicate.
func TestCreateRequestEnablesMonitoringAndSearchesUnmonitoredMatch(t *testing.T) {
	stub := newReadarrSearchStub()
	defer stub.close()

	s := newServerForTest(t)
	configureEbooksReadarr(t, s, stub.server.URL)
	seedCatalogBook(t, s, db.ReadarrBook{
		ReadarrID:     77,
		Monitored:     false,
		BookFileCount: 0,
	})

	rec := postCreateRequest(t, s, false)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v body=%s", err, rec.Body.String())
	}
	if resp["status"] != "queued" {
		t.Fatalf("expected status queued, got %v", resp["status"])
	}
	if resp["external_status"] != "monitored" {
		t.Fatalf("expected external_status monitored, got %v", resp["external_status"])
	}
	if stub.monitorPUTs.Load() != 1 {
		t.Fatalf("expected 1 monitor call, got %d", stub.monitorPUTs.Load())
	}
	if stub.searchPOSTs.Load() != 1 {
		t.Fatalf("expected 1 search call, got %d", stub.searchPOSTs.Load())
	}
	if stub.addPOSTs.Load() != 0 {
		t.Fatalf("expected no add calls for an existing title, got %d", stub.addPOSTs.Load())
	}

	// A DB request should have been created so the user can track it.
	items, err := s.db.ListRequests(context.Background(), "user", 10)
	if err != nil {
		t.Fatalf("list requests: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 request to be created, got %d", len(items))
	}
	if items[0].Status != "queued" {
		t.Fatalf("expected created request status queued, got %s", items[0].Status)
	}
	if items[0].MatchedReadarrID != 77 {
		t.Fatalf("expected matched readarr id 77, got %d", items[0].MatchedReadarrID)
	}
}

// When a matching title is already monitored (but not downloaded), monitoring
// should not be re-sent, but a search should still be triggered.
func TestCreateRequestSearchesAlreadyMonitoredMatchWithoutRemonitoring(t *testing.T) {
	stub := newReadarrSearchStub()
	defer stub.close()

	s := newServerForTest(t)
	configureEbooksReadarr(t, s, stub.server.URL)
	seedCatalogBook(t, s, db.ReadarrBook{
		ReadarrID:     77,
		Monitored:     true,
		BookFileCount: 0,
	})

	rec := postCreateRequest(t, s, false)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	if stub.monitorPUTs.Load() != 0 {
		t.Fatalf("expected no monitor call for an already-monitored book, got %d", stub.monitorPUTs.Load())
	}
	if stub.searchPOSTs.Load() != 1 {
		t.Fatalf("expected 1 search call, got %d", stub.searchPOSTs.Load())
	}
}

// An HTMX request for an unmonitored match should return a success notice (200)
// rather than the amber duplicate notice.
func TestCreateRequestHXReturnsSearchNotice(t *testing.T) {
	stub := newReadarrSearchStub()
	defer stub.close()

	s := newServerForTest(t)
	configureEbooksReadarr(t, s, stub.server.URL)
	seedCatalogBook(t, s, db.ReadarrBook{
		ReadarrID:     77,
		Monitored:     false,
		BookFileCount: 0,
	})

	rec := postCreateRequest(t, s, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "search has been triggered") {
		t.Fatalf("expected search-triggered notice, got %s", body)
	}
	if stub.searchPOSTs.Load() != 1 {
		t.Fatalf("expected 1 search call, got %d", stub.searchPOSTs.Load())
	}
}

// A title that is already downloaded (available) is a genuine duplicate: no
// monitor/search calls, and a 409 response.
func TestCreateRequestAvailableMatchRemainsDuplicate(t *testing.T) {
	stub := newReadarrSearchStub()
	defer stub.close()

	s := newServerForTest(t)
	configureEbooksReadarr(t, s, stub.server.URL)
	seedCatalogBook(t, s, db.ReadarrBook{
		ReadarrID:     77,
		Monitored:     true,
		BookFileCount: 1,
	})

	rec := postCreateRequest(t, s, false)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for available title, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp["status"] != "exists" {
		t.Fatalf("expected status exists, got %v", resp["status"])
	}
	if stub.monitorPUTs.Load() != 0 || stub.searchPOSTs.Load() != 0 {
		t.Fatalf("expected no monitor/search calls for available title, got monitor=%d search=%d", stub.monitorPUTs.Load(), stub.searchPOSTs.Load())
	}
}

// If the Readarr search command fails, the handler should report an error
// (502) and not create a duplicate request.
func TestCreateRequestSearchFailureReturnsError(t *testing.T) {
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/book/monitor" && r.Method == http.MethodPut {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[]`))
			return
		}
		// Fail the search command.
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer readarr.Close()

	s := newServerForTest(t)
	configureEbooksReadarr(t, s, readarr.URL)
	seedCatalogBook(t, s, db.ReadarrBook{
		ReadarrID:     77,
		Monitored:     false,
		BookFileCount: 0,
	})

	rec := postCreateRequest(t, s, false)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected 502 on search failure, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode resp: %v", err)
	}
	if resp["status"] != "error" {
		t.Fatalf("expected status error, got %v", resp["status"])
	}
}
