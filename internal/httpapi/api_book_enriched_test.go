package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
