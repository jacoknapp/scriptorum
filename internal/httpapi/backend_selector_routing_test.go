package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	"gitea.knapp/jacoknapp/scriptorum/internal/db"
)

// TestAutoApproveRoutesToSelectedBackend pins the fix for the bug that made
// user requests silently fail: six handlers in api.go (auto-approve, retry,
// search, approve-all, hydrate, and the create-time metadata attach) read
// cfg.Readarr.Ebooks/Audiobooks directly instead of going through
// readarrInstanceForFormat, so they ignored the book_backend selector
// entirely and always talked to the legacy Readarr config. With
// book_backend=chaptarr and stale legacy Readarr entries still on disk, an
// auto-approved request went to a decommissioned Readarr host; once those
// legacy entries were blanked, the same path instead silently marked the
// request "approved" without ever sending it anywhere.
func TestAutoApproveRoutesToSelectedBackend(t *testing.T) {
	var chaptarrAdds atomic.Int32
	chaptarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/system/status":
			_, _ = w.Write([]byte(`{"appName":"Chaptarr","version":"0.9.911.0"}`))
		case r.URL.Path == "/api/v1/qualityprofile":
			_, _ = w.Write([]byte(`[{"id":1,"name":"E-Book","profileType":"ebook"},{"id":2,"name":"Audiobook","profileType":"audiobook"}]`))
		case r.URL.Path == "/api/v1/metadataprofile":
			_, _ = w.Write([]byte(`[{"id":1,"name":"Audiobook Default","profileType":1},{"id":2,"name":"Ebook Default","profileType":2}]`))
		case r.URL.Path == "/api/v1/rootfolder":
			_, _ = w.Write([]byte(`[{"id":1,"path":"/books","accessible":true},{"id":2,"path":"/audiobooks","accessible":true}]`))
		case r.URL.Path == "/api/v1/author":
			_, _ = w.Write([]byte(`[]`))
		case r.URL.Path == "/api/v1/book" && r.Method == http.MethodPost:
			chaptarrAdds.Add(1)
			_, _ = w.Write([]byte(`{"id":501,"authorId":9,"monitored":true}`))
		case r.URL.Path == "/api/v1/book":
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer chaptarr.Close()

	s := newServerForTest(t)
	cfg := *s.settings.Get()
	cfg.BookBackend = "chaptarr"
	cfg.Chaptarr.BaseURL = chaptarr.URL
	cfg.Chaptarr.APIKey = "chaptarr-key"
	cfg.Chaptarr.Ebooks = config.ChaptarrMediaConfig{QualityProfileID: 1, MetadataProfileID: 2, RootFolderPath: "/books"}
	cfg.Chaptarr.Audiobooks = config.ChaptarrMediaConfig{QualityProfileID: 2, MetadataProfileID: 1, RootFolderPath: "/audiobooks"}
	// No legacy Readarr configured, matching the live instance after the dead
	// entries were cleared. Under the old code this handler read the empty
	// legacy config, concluded "no Readarr configured", and marked the
	// request approved without ever contacting chaptarr — the request just
	// silently never happened.
	cfg.Readarr.Ebooks.BaseURL = ""
	cfg.Readarr.Ebooks.APIKey = ""
	cfg.Readarr.Audiobooks.BaseURL = ""
	cfg.Readarr.Audiobooks.APIKey = ""
	if err := s.settings.Update(&cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	// An auto-approve user, like the live instance has several of.
	if _, err := s.db.CreateUser(context.Background(), "autouser", "x", false, true); err != nil {
		t.Fatalf("create user: %v", err)
	}

	body := []byte(`{"title":"Routing Test","authors":["Test Author"],"format":"ebook","provider_payload":"{\"title\":\"Routing Test\",\"foreignBookId\":\"fb-route\",\"foreignEditionId\":\"fe-route\",\"author\":{\"authorName\":\"Test Author\",\"foreignAuthorId\":\"gr:1\"}}"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests", bytes.NewReader(body))
	req.AddCookie(makeCookie(t, s, "autouser", false))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create code=%d body=%s", rec.Code, rec.Body.String())
	}

	// The auto-approve path must hand off to the selected chaptarr backend,
	// and must not short-circuit to a bare "approved" that never sends
	// anything anywhere.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if chaptarrAdds.Load() > 0 {
			return
		}
		items, err := s.db.ListRequests(context.Background(), "", 10)
		if err == nil && len(items) == 1 && items[0].Status == "approved" {
			t.Fatalf("request was marked approved without reaching any backend: %q", items[0].StatusReason)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for the chaptarr backend to receive the add")
}

// TestSearchRequestUsesSelectedBackend covers the Search button path, which
// had the same direct-legacy-config read.
func TestSearchRequestUsesSelectedBackend(t *testing.T) {
	s := newServerForTest(t)
	cfg := *s.settings.Get()
	cfg.BookBackend = "chaptarr"
	cfg.Chaptarr.BaseURL = "https://chaptarr.example"
	cfg.Chaptarr.APIKey = "chaptarr-key"
	// No legacy Readarr configured at all: under the old code this handler
	// read the empty legacy config and rejected with "readarr not
	// configured" even though chaptarr was working fine.
	if err := s.settings.Update(&cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	id, err := s.db.CreateRequest(context.Background(), &db.Request{
		RequesterEmail:   "user",
		Title:            "Search Routing",
		Format:           "ebook",
		Status:           "queued",
		MatchedReadarrID: 42,
	})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	// Search as an admin so the 30-minute non-admin cooldown doesn't mask the
	// backend-resolution behavior this test is actually about.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/requests/"+itoa(id)+"/search", nil)
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search code=%d body=%s (expected the chaptarr backend to be accepted)", rec.Code, rec.Body.String())
	}
}
