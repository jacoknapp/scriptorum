package httpapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchUISurfacesSeriesFromReadarr(t *testing.T) {
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/book/lookup" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{
				"title":"Project Hail Mary",
				"foreignBookId":"fb-1",
				"foreignEditionId":"fe-1",
				"author":{"name":"Andy Weir"},
				"seriesTitle":"The Hail Mary Saga"
			}
		]`)
	}))
	defer readarr.Close()

	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = readarr.URL
	cfg.Readarr.Ebooks.APIKey = "test-key"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui/search?q=project", nil)
	req.AddCookie(makeCookie(t, s, "user", false))
	rec := httptest.NewRecorder()
	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "The Hail Mary Saga") {
		t.Fatalf("expected series title to be surfaced in body: %s", body)
	}
}
