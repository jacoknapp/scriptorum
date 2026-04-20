package providers

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	appdb "gitea.knapp/jacoknapp/scriptorum/internal/db"
)

func newReadarrWithTempDB(t *testing.T, inst ReadarrInstance) (*Readarr, *sql.DB) {
	t.Helper()

	store, err := appdb.Open(filepath.Join(t.TempDir(), "scriptorum.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() {
		_ = store.Close()
	})
	return NewReadarrWithDB(inst, store.SQL()), store.SQL()
}

func TestReadarrGetBookByAddPayloadVariants(t *testing.T) {
	ctx := context.Background()

	t.Run("object response", func(t *testing.T) {
		ra := NewReadarrWithDB(ReadarrInstance{BaseURL: "http://readarr", APIKey: "secret"}, nil)
		ra.cl.Transport = rtFunc(func(req *http.Request) (*http.Response, error) {
			if req.Method != http.MethodGet {
				t.Fatalf("method=%s", req.Method)
			}
			if req.URL.Path != "/api/v1/book" || !strings.Contains(req.URL.RawQuery, "apikey=secret") {
				t.Fatalf("unexpected URL: %s", req.URL.String())
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("{\"id\":55}")),
				Header:     make(http.Header),
			}, nil
		})

		id, _, err := ra.GetBookByAddPayload(ctx, []byte("{\"foreignBookId\":\"fb-1\"}"))
		if err != nil || id != 55 {
			t.Fatalf("id=%d err=%v", id, err)
		}
	})

	t.Run("array response prefers foreign id match", func(t *testing.T) {
		ra := NewReadarrWithDB(ReadarrInstance{BaseURL: "http://readarr", APIKey: "secret"}, nil)
		ra.cl.Transport = rtFunc(func(req *http.Request) (*http.Response, error) {
			body := "[{\"id\":1,\"foreignBookId\":\"other\"},{\"id\":2,\"foreignBookId\":\"fb-2\",\"foreignEditionId\":\"fe-2\"}]"
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		})

		id, _, err := ra.GetBookByAddPayload(ctx, []byte("{\"foreignBookId\":\"fb-2\",\"foreignEditionId\":\"fe-2\"}"))
		if err != nil || id != 2 {
			t.Fatalf("id=%d err=%v", id, err)
		}
	})

	t.Run("status error is surfaced", func(t *testing.T) {
		ra := NewReadarrWithDB(ReadarrInstance{BaseURL: "http://readarr", APIKey: "secret"}, nil)
		ra.cl.Transport = rtFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadRequest,
				Body:       io.NopCloser(strings.NewReader("problem")),
				Header:     make(http.Header),
			}, nil
		})

		if _, _, err := ra.GetBookByAddPayload(ctx, []byte("{\"foreignBookId\":\"fb-1\"}")); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestReadarrMonitorBooks(t *testing.T) {
	ra := NewReadarrWithDB(ReadarrInstance{BaseURL: "http://readarr", APIKey: "secret"}, nil)
	if _, err := ra.MonitorBooks(context.Background(), nil, true); err == nil {
		t.Fatal("expected error for empty ids")
	}

	var gotPayload map[string]any
	ra.cl.Transport = rtFunc(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPut || req.URL.Path != "/api/v1/book/monitor" {
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
		}
		body, _ := io.ReadAll(req.Body)
		_ = json.Unmarshal(body, &gotPayload)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("{\"ok\":true}")),
			Header:     make(http.Header),
		}, nil
	})

	body, err := ra.MonitorBooks(context.Background(), []int{7, 8}, true)
	if err != nil {
		t.Fatalf("MonitorBooks: %v", err)
	}
	if string(body) != "{\"ok\":true}" {
		t.Fatalf("unexpected body: %s", string(body))
	}
	if gotPayload["monitored"] != true {
		t.Fatalf("unexpected payload: %+v", gotPayload)
	}
}

func TestReadarrGetBookDetailsFallsBackEndpoints(t *testing.T) {
	serverCalls := 0
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls++
		switch r.URL.Path {
		case "/api/v1/book/42/overview":
			http.NotFound(w, r)
		case "/api/v1/book/42":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"id\":42,\"description\":\"Detailed\"}"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer readarr.Close()

	ra, _ := newReadarrWithTempDB(t, ReadarrInstance{BaseURL: readarr.URL, APIKey: "secret"})
	first, err := ra.GetBookDetails(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetBookDetails: %v", err)
	}
	if first["description"] != "Detailed" {
		t.Fatalf("unexpected details: %+v", first)
	}
	if serverCalls != 2 {
		t.Fatalf("expected overview+detail fallback, got %d HTTP calls", serverCalls)
	}
}

func TestReadarrGetBookDetailsUsesCachedValue(t *testing.T) {
	ra, rawDB := newReadarrWithTempDB(t, ReadarrInstance{BaseURL: "http://readarr", APIKey: "secret"})
	_, err := rawDB.Exec("INSERT INTO readarr_cache (cache_key, cache_type, data, expires_at) VALUES (?, ?, ?, NULL)", "book_details:42", "book_details", "{\"id\":42,\"description\":\"Cached\"}")
	if err != nil {
		t.Fatalf("insert cache: %v", err)
	}

	details, err := ra.GetBookDetails(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetBookDetails cache: %v", err)
	}
	if details["description"] != "Cached" {
		t.Fatalf("unexpected cached details: %+v", details)
	}
}

func TestReadarrFindAuthorIDByNameAndCache(t *testing.T) {
	serverCalls := 0
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalls++
		if r.URL.Path != "/api/v1/author/lookup" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("[{\"id\":77,\"name\":\"Ilona Andrews\"},{\"id\":88,\"name\":\"Someone Else\"}]"))
	}))
	defer readarr.Close()

	ra, _ := newReadarrWithTempDB(t, ReadarrInstance{BaseURL: readarr.URL, APIKey: "secret"})
	id, err := ra.FindAuthorIDByName(context.Background(), "Ilona Andrews")
	if err != nil || id != 77 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	cachedID, err := ra.FindAuthorIDByName(context.Background(), "Ilona Andrews")
	if err != nil || cachedID != 77 {
		t.Fatalf("cached id=%d err=%v", cachedID, err)
	}
	if serverCalls != 1 {
		t.Fatalf("expected cached author lookup, got %d HTTP calls", serverCalls)
	}
}

func TestReadarrFindAuthorIDByNameFallsBackToFirstResult(t *testing.T) {
	ra := NewReadarrWithDB(ReadarrInstance{BaseURL: "http://readarr", APIKey: "secret"}, nil)
	ra.cl.Transport = rtFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("[{\"id\":\"19\",\"name\":\"Not Exact\"}]")),
			Header:     make(http.Header),
		}, nil
	})

	id, err := ra.FindAuthorIDByName(context.Background(), "Unknown")
	if err != nil || id != 19 {
		t.Fatalf("id=%d err=%v", id, err)
	}
}

func TestReadarrQualityProfilesAndRootFoldersHelpers(t *testing.T) {
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/qualityprofile":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[{\"id\":3,\"name\":\"Any\"},{\"id\":5,\"name\":\"Lossless\"}]"))
		case "/api/v1/qualityprofile/1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"id\":1,\"name\":\"Profile One\"}"))
		case "/api/v1/qualityprofile/2":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"id\":2,\"name\":\"Profile Two\"}"))
		case "/api/v1/qualityprofile/3":
			http.NotFound(w, r)
		case "/api/v1/rootfolder":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("[{\"path\":\"/books\"},{\"path\":\"/audiobooks\"}]"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer readarr.Close()

	ra := NewReadarrWithDB(ReadarrInstance{
		BaseURL:                 readarr.URL,
		APIKey:                  "secret",
		DefaultQualityProfileID: 5,
		DefaultRootFolderPath:   "/audiobooks",
	}, nil)

	profiles, err := ra.fetchQualityProfiles(context.Background())
	if err != nil {
		t.Fatalf("fetchQualityProfiles: %v", err)
	}
	if profiles[5] != "Lossless" {
		t.Fatalf("unexpected profiles: %+v", profiles)
	}

	name, found, err := ra.fetchQualityProfileByID(context.Background(), 2)
	if err != nil || !found || name != "Profile Two" {
		t.Fatalf("name=%q found=%v err=%v", name, found, err)
	}
	allByID, err := ra.GetQualityProfilesByID(context.Background())
	if err != nil {
		t.Fatalf("GetQualityProfilesByID: %v", err)
	}
	if len(allByID) != 2 || allByID[1] != "Profile One" {
		t.Fatalf("unexpected profiles by id: %+v", allByID)
	}

	rootFolders, err := ra.fetchRootFolders(context.Background())
	if err != nil {
		t.Fatalf("fetchRootFolders: %v", err)
	}
	if len(rootFolders) != 2 {
		t.Fatalf("unexpected root folders: %+v", rootFolders)
	}
	if got := ra.getValidQualityProfileID(context.Background()); got != 5 {
		t.Fatalf("unexpected valid quality profile: %d", got)
	}
	if got := ra.getValidRootFolderPath(context.Background(), "/books"); got != "/books" {
		t.Fatalf("unexpected override root folder: %q", got)
	}
	if got := ra.getValidRootFolderPath(context.Background(), "/missing"); got != "/audiobooks" {
		t.Fatalf("unexpected default root folder: %q", got)
	}
}

func TestReadarrCachingHelpersAndRedaction(t *testing.T) {
	ra, rawDB := newReadarrWithTempDB(t, ReadarrInstance{BaseURL: "http://readarr", APIKey: "secret"})

	if _, found := ra.getCachedData("lookup:test", "lookup"); found {
		t.Fatal("expected empty cache initially")
	}
	ra.setCachedData("lookup:test", "lookup", "[1,2,3]", 0)
	if got, found := ra.getCachedData("lookup:test", "lookup"); !found || got != "[1,2,3]" {
		t.Fatalf("unexpected cached data: found=%v got=%q", found, got)
	}

	ra.setCachedAuthor("ilona andrews", 77)
	if got, found := ra.getCachedAuthor("ilona andrews"); !found || got != 77 {
		t.Fatalf("unexpected cached author: found=%v got=%d", found, got)
	}

	_, err := rawDB.Exec("INSERT OR REPLACE INTO readarr_cache (cache_key, cache_type, data, expires_at) VALUES (?, ?, ?, datetime('now', '-1 day'))", "expired", "lookup", "old")
	if err != nil {
		t.Fatalf("insert expired cache: %v", err)
	}
	if _, found := ra.getCachedData("expired", "lookup"); found {
		t.Fatal("expected expired cache entry to be ignored")
	}

	got := redactAPIKey("http://readarr/api/v1/book?term=test&apikey=supersecret")
	if strings.Contains(got, "supersecret") || !strings.Contains(got, "apikey=***") {
		t.Fatalf("unexpected redacted URL: %s", got)
	}
}
