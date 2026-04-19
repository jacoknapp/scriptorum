package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/db"
)

func TestBookEnrichedFallsBackToOpenLibraryDetails(t *testing.T) {
	prevTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "openlibrary.org" {
			return prevTransport.RoundTrip(r)
		}
		if r.URL.Path != "/works/OL21745884W.json" {
			t.Fatalf("unexpected Open Library request: %s", r.URL.String())
		}
		body := `{"key":"/works/OL21745884W","title":"Project Hail Mary","description":"A lone astronaut wakes up to save humanity.","subjects":["Science fiction","Space survival"],"covers":[11200092],"first_publish_date":"2021-05-04"}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = prevTransport })

	s := newServerForTest(t)
	body, _ := json.Marshal(map[string]any{
		"title":   "Project Hail Mary",
		"authors": []string{"Andy Weir"},
		"details_payload": map[string]any{
			"open_library_work_key": "/works/OL21745884W",
			"cover":                 "https://covers.openlibrary.org/b/id/11200092-M.jpg",
			"first_publish_year":    2021,
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/book/enriched", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeCookie(t, s, "user", false))
	rec := httptest.NewRecorder()

	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("json: %v", err)
	}
	if got := strings.TrimSpace(out["description"].(string)); got != "A lone astronaut wakes up to save humanity." {
		t.Fatalf("unexpected description: %+v", out)
	}
	if got := strings.TrimSpace(out["releaseDate"].(string)); got != "2021-05-04" {
		t.Fatalf("unexpected releaseDate: %+v", out)
	}
	if got := strings.TrimSpace(out["cover"].(string)); got != "https://covers.openlibrary.org/b/id/11200092-M.jpg" {
		t.Fatalf("unexpected cover: %+v", out)
	}
}

func TestBookEnrichedSupplementsThinReadarrDetailsWithOpenLibrary(t *testing.T) {
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/book/lookup":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{
				"id": 42,
				"title": "Project Hail Mary",
				"author": {"name": "Andy Weir"},
				"authors": [{"name": "Andy Weir"}],
				"foreignBookId": "phm-42"
			}]`))
		case "/api/v1/book/42/overview":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id": 42,
				"title": "Project Hail Mary",
				"overview": ""
			}`))
		case "/api/v1/book/42":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id": 42,
				"title": "Project Hail Mary",
				"description": ""
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer readarr.Close()

	prevTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "openlibrary.org" {
			return prevTransport.RoundTrip(r)
		}
		if r.URL.Path != "/works/OL21745884W.json" {
			t.Fatalf("unexpected Open Library request: %s", r.URL.String())
		}
		body := `{"key":"/works/OL21745884W","title":"Project Hail Mary","description":"Ryland Grace wakes up alone in deep space with no memory, a failing mission, and the weight of humanity on him. As the pieces come back, he has to solve an extinction-level problem with science, improvisation, and an unlikely ally.","subjects":["Science fiction","Space survival"],"covers":[11200092],"first_publish_date":"2021-05-04"}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = prevTransport })

	s := newServerForTest(t)
	cfg := *s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = readarr.URL
	cfg.Readarr.Ebooks.APIKey = "test-key"
	if err := s.settings.Update(&cfg); err != nil {
		t.Fatalf("update config: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"title":   "Project Hail Mary",
		"authors": []string{"Andy Weir"},
		"format":  "ebook",
		"details_payload": map[string]any{
			"open_library_work_key": "/works/OL21745884W",
			"cover":                 "https://covers.openlibrary.org/b/id/11200092-M.jpg",
			"first_publish_year":    2021,
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/book/enriched", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeCookie(t, s, "user", false))
	rec := httptest.NewRecorder()

	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("json: %v", err)
	}
	if got := int(out["id"].(float64)); got != 42 {
		t.Fatalf("expected Readarr id to be preserved, got %+v", out)
	}
	if got := strings.TrimSpace(out["description"].(string)); !strings.Contains(got, "Ryland Grace wakes up alone in deep space") {
		t.Fatalf("expected Open Library description to supplement Readarr result, got %+v", out)
	}
	if got := strings.TrimSpace(out["releaseDate"].(string)); got != "2021-05-04" {
		t.Fatalf("unexpected releaseDate: %+v", out)
	}
}

func TestBookEnrichedFillsMissingCoverFromOpenLibraryAndPersistsRequestCover(t *testing.T) {
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/book/lookup":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{
				"id": 42,
				"title": "Sea of Tranquility",
				"author": {"name": "Emily St. John Mandel"},
				"authors": [{"name": "Emily St. John Mandel"}],
				"foreignBookId": "sea-42"
			}]`))
		case "/api/v1/book/42/overview":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id": 42,
				"title": "Sea of Tranquility",
				"overview": "This is a complete Readarr description that is already long enough to count as detailed metadata for the modal, but it still arrives without any usable cover art."
			}`))
		case "/api/v1/book/42":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"id": 42,
				"title": "Sea of Tranquility",
				"description": "This is a complete Readarr description that is already long enough to count as detailed metadata for the modal, but it still arrives without any usable cover art."
			}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer readarr.Close()

	prevTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "openlibrary.org" {
			return prevTransport.RoundTrip(r)
		}
		switch r.URL.Path {
		case "/search.json":
			body := `{"docs":[{"title":"Sea of Tranquility","author_name":["Emily St. John Mandel"],"key":"/works/OL314159W"}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		case "/works/OL314159W.json":
			body := `{"key":"/works/OL314159W","title":"Sea of Tranquility","description":"Open Library fallback description.","covers":[445566],"first_publish_date":"2022-04-05"}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		default:
			t.Fatalf("unexpected Open Library request: %s", r.URL.String())
			return nil, nil
		}
	})
	t.Cleanup(func() { http.DefaultTransport = prevTransport })

	s := newServerForTest(t)
	cfg := *s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = readarr.URL
	cfg.Readarr.Ebooks.APIKey = "test-key"
	if err := s.settings.Update(&cfg); err != nil {
		t.Fatalf("update config: %v", err)
	}

	requestID, err := s.db.CreateRequest(t.Context(), &db.Request{
		RequesterEmail: "user",
		Title:          "Sea of Tranquility",
		Authors:        []string{"Emily St. John Mandel"},
		Format:         "ebook",
		Status:         "pending",
	})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"title":     "Sea of Tranquility",
		"authors":   []string{"Emily St. John Mandel"},
		"format":    "ebook",
		"requestId": requestID,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/book/enriched", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(makeCookie(t, s, "user", false))
	rec := httptest.NewRecorder()

	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("json: %v", err)
	}
	wantCover := "https://covers.openlibrary.org/b/id/445566-M.jpg"
	if got := strings.TrimSpace(out["cover"].(string)); got != wantCover {
		t.Fatalf("unexpected cover: %+v", out)
	}

	stored, err := s.db.GetRequest(t.Context(), requestID)
	if err != nil {
		t.Fatalf("get request: %v", err)
	}
	if got := strings.TrimSpace(stored.CoverURL); got != wantCover {
		t.Fatalf("expected stored cover %q, got %q", wantCover, got)
	}
}
