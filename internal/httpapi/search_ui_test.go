package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

var openLibraryTestSlugPattern = regexp.MustCompile(`[^a-z0-9]+`)

func openLibraryTestWorkKey(title string) string {
	slug := strings.ToLower(strings.TrimSpace(title))
	slug = openLibraryTestSlugPattern.ReplaceAllString(slug, "-")
	slug = strings.Trim(slug, "-")
	if slug == "" {
		slug = "unknown"
	}
	return "/works/TEST-" + slug
}

func withOpenLibrarySearchWorkKeys(raw string) string {
	var payload struct {
		Docs []map[string]any `json:"docs"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return raw
	}
	for _, doc := range payload.Docs {
		if doc == nil {
			continue
		}
		if _, ok := doc["key"]; ok {
			continue
		}
		title, _ := doc["title"].(string)
		if strings.TrimSpace(title) == "" {
			continue
		}
		doc["key"] = openLibraryTestWorkKey(title)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return raw
	}
	return string(encoded)
}

func installOpenLibraryDiscoveryTransport(t *testing.T) {
	prevTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "openlibrary.org" {
			return prevTransport.RoundTrip(r)
		}

		bodyByPath := map[string]string{
			"/trending/weekly.json": `{"works":[
				{"title":"Project Hail Mary","author_name":["Andy Weir"],"cover_i":1,"first_publish_year":2021,"key":"/works/TEST-project-hail-mary"},
				{"title":"Funny Story","author_name":["Emily Henry"],"cover_i":2,"first_publish_year":2024,"key":"/works/TEST-funny-story"},
				{"title":"Fourth Wing","author_name":["Rebecca Yarros"],"cover_i":3,"first_publish_year":2023,"key":"/works/TEST-fourth-wing"}
			]}`,
		}
		if body, ok := bodyByPath[r.URL.Path]; ok {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}
		if strings.HasPrefix(r.URL.Path, "/works/") && strings.HasSuffix(r.URL.Path, ".json") {
			workKey := strings.TrimSuffix(r.URL.Path, ".json")
			body := fmt.Sprintf(`{"key":%q,"description":"Detailed discovery metadata for %s.","covers":[112233],"first_publish_date":"2024-01-01"}`, workKey, workKey)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}
		if r.URL.Path == "/search.json" {
			nowYear := time.Now().Year()
			queryBodies := map[string]string{
				"fantasy novel bestseller":        `{"docs":[{"title":"Assistant to the Villain","author_name":["Hannah Nicole Maehrer"],"cover_i":10,"first_publish_year":2023},{"title":"The Serpent & the Wings of Night","author_name":["Carissa Broadbent"],"cover_i":11,"first_publish_year":2022},{"title":"The Romantasy Cookbook","author_name":["Test Author"],"cover_i":60,"first_publish_year":2024},{"title":"The Romantasy Boxset","author_name":["Test Author"],"cover_i":61,"first_publish_year":2024}]}`,
				"epic fantasy novel bestseller":   `{"docs":[{"title":"The Will of the Many","author_name":["James Islington"],"cover_i":26,"first_publish_year":2023},{"title":"Faebound","author_name":["Saara El-Arifi"],"cover_i":27,"first_publish_year":2024}]}`,
				"fantasy fiction":                 `{"docs":[{"title":"When the Moon Hatched","author_name":["Sarah A. Parker"],"cover_i":18,"first_publish_year":2024},{"title":"The Tainted Cup","author_name":["Robert Jackson Bennett"],"cover_i":28,"first_publish_year":2024}]}`,
				"dark fantasy novel":              `{"docs":[{"title":"The Familiar","author_name":["Leigh Bardugo"],"cover_i":29,"first_publish_year":2024},{"title":"A Fate Inked in Blood","author_name":["Danielle L. Jensen"],"cover_i":19,"first_publish_year":2024}]}`,
				"psychological thriller novel":    `{"docs":[{"title":"Never Lie","author_name":["Freida McFadden"],"cover_i":12,"first_publish_year":2022},{"title":"The Housemaid","author_name":["Freida McFadden"],"cover_i":13,"first_publish_year":2022}]}`,
				"crime thriller novel":            `{"docs":[{"title":"Listen for the Lie","author_name":["Amy Tintera"],"cover_i":30,"first_publish_year":2024},{"title":"The Boyfriend","author_name":["Freida McFadden"],"cover_i":31,"first_publish_year":2024}]}`,
				"suspense thriller novel":         `{"docs":[{"title":"The Crash","author_name":["Freida McFadden"],"cover_i":20,"first_publish_year":2025},{"title":"Ward D","author_name":["Freida McFadden"],"cover_i":21,"first_publish_year":2023}]}`,
				"domestic thriller novel":         `{"docs":[{"title":"The Teacher","author_name":["Freida McFadden"],"cover_i":32,"first_publish_year":2024},{"title":"What the Wife Knew","author_name":["Darby Kane"],"cover_i":33,"first_publish_year":2024}]}`,
				"romance novel":                   `{"docs":[{"title":"The Rule Book","author_name":["Sarah Adams"],"cover_i":36,"first_publish_year":2024},{"title":"Summer Romance","author_name":["Annabel Monaghan"],"cover_i":37,"first_publish_year":2024}]}`,
				"contemporary romance novel":      `{"docs":[{"title":"This Summer Will Be Different","author_name":["Carley Fortune"],"cover_i":34,"first_publish_year":2024},{"title":"Just for the Summer","author_name":["Abby Jimenez"],"cover_i":35,"first_publish_year":2024}]}`,
				"rom com romance novel":           `{"docs":[{"title":"Bride","author_name":["Ali Hazelwood"],"cover_i":22,"first_publish_year":2024},{"title":"Not in Love","author_name":["Ali Hazelwood"],"cover_i":23,"first_publish_year":2024}]}`,
				"love story novel":                `{"docs":[{"title":"Funny Story","author_name":["Emily Henry"],"cover_i":14,"first_publish_year":2024},{"title":"Happy Place","author_name":["Emily Henry"],"cover_i":15,"first_publish_year":2023}]}`,
				"science fiction novel":           `{"docs":[{"title":"System Collapse","author_name":["Martha Wells"],"cover_i":16,"first_publish_year":2023},{"title":"Network Effect","author_name":["Martha Wells"],"cover_i":17,"first_publish_year":2020}]}`,
				"space opera novel":               `{"docs":[{"title":"Some Desperate Glory","author_name":["Emily Tesh"],"cover_i":24,"first_publish_year":2023},{"title":"The Mercy of Gods","author_name":["James S. A. Corey"],"cover_i":25,"first_publish_year":2024}]}`,
				"dystopian science fiction novel": `{"docs":[{"title":"Red Rising","author_name":["Pierce Brown"],"cover_i":38,"first_publish_year":2014},{"title":"Starter Villain","author_name":["John Scalzi"],"cover_i":39,"first_publish_year":2023}]}`,
				"hard science fiction novel":      `{"docs":[{"title":"Service Model","author_name":["Adrian Tchaikovsky"],"cover_i":40,"first_publish_year":2024},{"title":"Alien Clay","author_name":["Adrian Tchaikovsky"],"cover_i":41,"first_publish_year":2024}]}`,
				"booktok books":                   `{"docs":[{"title":"Yellowface","author_name":["R. F. Kuang"],"cover_i":42,"first_publish_year":2023},{"title":"The Women","author_name":["Kristin Hannah"],"cover_i":43,"first_publish_year":2024}]}`,
				"new releases fiction":            `{"docs":[{"title":"All the Colors of the Dark","author_name":["Chris Whitaker"],"cover_i":44,"first_publish_year":2024},{"title":"James","author_name":["Percival Everett"],"cover_i":45,"first_publish_year":2024}]}`,
				"Fantasy Hits":                    `{"docs":[]}`,
				"Thriller Buzz":                   `{"docs":[]}`,
				"Romance Favorites":               `{"docs":[]}`,
				"Sci-Fi Series Hits":              `{"docs":[]}`,
			}
			queryBodies[fmt.Sprintf("thriller novel %d", nowYear)] = `{"docs":[{"title":"The Tenant","author_name":["Freida McFadden"],"cover_i":46,"first_publish_year":2025},{"title":"We Don't Talk About Carol","author_name":["Kristen L. Berry"],"cover_i":47,"first_publish_year":2025}]}`
			queryBodies[fmt.Sprintf("thriller novel %d", nowYear-1)] = `{"docs":[{"title":"Middle of the Night","author_name":["Riley Sager"],"cover_i":48,"first_publish_year":2024},{"title":"Listen for the Lie","author_name":["Amy Tintera"],"cover_i":30,"first_publish_year":2024}]}`
			queryBodies[fmt.Sprintf("thriller novel %d", nowYear-2)] = `{"docs":[{"title":"Everyone Here Is Lying","author_name":["Shari Lapena"],"cover_i":49,"first_publish_year":2023}]}`
			queryBodies[fmt.Sprintf("crime thriller novel %d", nowYear)] = `{"docs":[{"title":"Capture or Kill","author_name":["Vince Flynn"],"cover_i":50,"first_publish_year":2025}]}`
			queryBodies[fmt.Sprintf("crime thriller novel %d", nowYear-1)] = `{"docs":[{"title":"The Hunter","author_name":["Tana French"],"cover_i":51,"first_publish_year":2024}]}`
			queryBodies[fmt.Sprintf("crime thriller novel %d", nowYear-2)] = `{"docs":[{"title":"The Trap","author_name":["Catherine Ryan Howard"],"cover_i":52,"first_publish_year":2023}]}`
			queryBodies[fmt.Sprintf("suspense novel %d", nowYear)] = `{"docs":[{"title":"Don’t Let Him In","author_name":["Lisa Jewell"],"cover_i":62,"first_publish_year":2025}]}`
			queryBodies[fmt.Sprintf("suspense novel %d", nowYear-1)] = `{"docs":[{"title":"The God of the Woods","author_name":["Liz Moore"],"cover_i":63,"first_publish_year":2024}]}`
			queryBodies[fmt.Sprintf("suspense novel %d", nowYear-2)] = `{"docs":[{"title":"Bright Young Women","author_name":["Jessica Knoll"],"cover_i":64,"first_publish_year":2023}]}`
			queryBodies[fmt.Sprintf("domestic suspense novel %d", nowYear)] = `{"docs":[{"title":"Don’t Let Him In","author_name":["Lisa Jewell"],"cover_i":62,"first_publish_year":2025}]}`
			queryBodies[fmt.Sprintf("domestic suspense novel %d", nowYear-1)] = `{"docs":[{"title":"The God of the Woods","author_name":["Liz Moore"],"cover_i":63,"first_publish_year":2024}]}`
			queryBodies[fmt.Sprintf("domestic suspense novel %d", nowYear-2)] = `{"docs":[{"title":"Bright Young Women","author_name":["Jessica Knoll"],"cover_i":64,"first_publish_year":2023}]}`
			queryBodies[fmt.Sprintf("mystery thriller novel %d", nowYear)] = `{"docs":[{"title":"Capture or Kill","author_name":["Vince Flynn"],"cover_i":50,"first_publish_year":2025}]}`
			queryBodies[fmt.Sprintf("mystery thriller novel %d", nowYear-1)] = `{"docs":[{"title":"The Hunter","author_name":["Tana French"],"cover_i":51,"first_publish_year":2024}]}`
			queryBodies[fmt.Sprintf("mystery thriller novel %d", nowYear-2)] = `{"docs":[{"title":"The Trap","author_name":["Catherine Ryan Howard"],"cover_i":52,"first_publish_year":2023}]}`
			queryBodies[fmt.Sprintf("science fiction novel %d", nowYear)] = `{"docs":[{"title":"Death of the Author","author_name":["Nnedi Okorafor"],"cover_i":53,"first_publish_year":2025},{"title":"The Martian Contingency","author_name":["Mary Robinette Kowal"],"cover_i":54,"first_publish_year":2025}]}`
			queryBodies[fmt.Sprintf("science fiction novel %d", nowYear-1)] = `{"docs":[{"title":"Ghostdrift","author_name":["Suzanne Palmer"],"cover_i":55,"first_publish_year":2024},{"title":"The Stardust Grail","author_name":["Yume Kitasei"],"cover_i":56,"first_publish_year":2024}]}`
			queryBodies[fmt.Sprintf("science fiction novel %d", nowYear-2)] = `{"docs":[{"title":"The Terraformers","author_name":["Annalee Newitz"],"cover_i":57,"first_publish_year":2023}]}`
			queryBodies[fmt.Sprintf("space opera novel %d", nowYear)] = `{"docs":[{"title":"Shroud","author_name":["Adrian Tchaikovsky"],"cover_i":58,"first_publish_year":2025}]}`
			queryBodies[fmt.Sprintf("space opera novel %d", nowYear-1)] = `{"docs":[{"title":"The Mercy of Gods","author_name":["James S. A. Corey"],"cover_i":25,"first_publish_year":2024}]}`
			queryBodies[fmt.Sprintf("space opera novel %d", nowYear-2)] = `{"docs":[{"title":"Infinity Gate","author_name":["M. R. Carey"],"cover_i":59,"first_publish_year":2023}]}`
			queryBodies[fmt.Sprintf("fantasy novel %d", nowYear)] = `{"docs":[{"title":"A Drop of Corruption","author_name":["Robert Jackson Bennett"],"cover_i":65,"first_publish_year":2025}]}`
			queryBodies[fmt.Sprintf("fantasy novel %d", nowYear-1)] = `{"docs":[{"title":"The Bright Sword","author_name":["Lev Grossman"],"cover_i":66,"first_publish_year":2024}]}`
			queryBodies[fmt.Sprintf("fantasy novel %d", nowYear-2)] = `{"docs":[{"title":"The Adventures of Amina al-Sirafi","author_name":["Shannon Chakraborty"],"cover_i":67,"first_publish_year":2023}]}`
			queryBodies[fmt.Sprintf("fantasy fiction %d", nowYear)] = `{"docs":[{"title":"The Incandescent","author_name":["Emily Tesh"],"cover_i":68,"first_publish_year":2025}]}`
			queryBodies[fmt.Sprintf("fantasy fiction %d", nowYear-1)] = `{"docs":[{"title":"The Familiar","author_name":["Leigh Bardugo"],"cover_i":29,"first_publish_year":2024}]}`
			queryBodies[fmt.Sprintf("fantasy fiction %d", nowYear-2)] = `{"docs":[{"title":"The Will of the Many","author_name":["James Islington"],"cover_i":26,"first_publish_year":2023}]}`
			queryBodies[fmt.Sprintf("epic fantasy novel %d", nowYear)] = `{"docs":[{"title":"A Fate Forged in Fire","author_name":["Hazel McBride"],"cover_i":69,"first_publish_year":2025}]}`
			queryBodies[fmt.Sprintf("epic fantasy novel %d", nowYear-1)] = `{"docs":[{"title":"A Fate Inked in Blood","author_name":["Danielle L. Jensen"],"cover_i":19,"first_publish_year":2024}]}`
			queryBodies[fmt.Sprintf("epic fantasy novel %d", nowYear-2)] = `{"docs":[{"title":"Assistant to the Villain","author_name":["Hannah Nicole Maehrer"],"cover_i":10,"first_publish_year":2023}]}`
			queryBodies[fmt.Sprintf("rom com novel %d", nowYear)] = `{"docs":[{"title":"Problematic Summer Romance","author_name":["Ali Hazelwood"],"cover_i":70,"first_publish_year":2025}]}`
			queryBodies[fmt.Sprintf("rom com novel %d", nowYear-1)] = `{"docs":[{"title":"The Rule Book","author_name":["Sarah Adams"],"cover_i":36,"first_publish_year":2024}]}`
			queryBodies[fmt.Sprintf("rom com novel %d", nowYear-2)] = `{"docs":[{"title":"Happy Place","author_name":["Emily Henry"],"cover_i":15,"first_publish_year":2023}]}`
			queryBodies[fmt.Sprintf("romantic comedy novel %d", nowYear)] = `{"docs":[{"title":"Problematic Summer Romance","author_name":["Ali Hazelwood"],"cover_i":70,"first_publish_year":2025}]}`
			queryBodies[fmt.Sprintf("romantic comedy novel %d", nowYear-1)] = `{"docs":[{"title":"The Rule Book","author_name":["Sarah Adams"],"cover_i":36,"first_publish_year":2024}]}`
			queryBodies[fmt.Sprintf("romantic comedy novel %d", nowYear-2)] = `{"docs":[{"title":"Happy Place","author_name":["Emily Henry"],"cover_i":15,"first_publish_year":2023}]}`
			queryBodies[fmt.Sprintf("romance novel %d", nowYear)] = `{"docs":[{"title":"Great Big Beautiful Life","author_name":["Emily Henry"],"cover_i":71,"first_publish_year":2025}]}`
			queryBodies[fmt.Sprintf("romance novel %d", nowYear-1)] = `{"docs":[{"title":"This Summer Will Be Different","author_name":["Carley Fortune"],"cover_i":34,"first_publish_year":2024}]}`
			queryBodies[fmt.Sprintf("romance novel %d", nowYear-2)] = `{"docs":[{"title":"Forget Me Not","author_name":["Julie Soto"],"cover_i":72,"first_publish_year":2023}]}`
			queryBodies[fmt.Sprintf("contemporary romance novel %d", nowYear)] = `{"docs":[{"title":"Say You'll Remember Me","author_name":["Abby Jimenez"],"cover_i":73,"first_publish_year":2025}]}`
			queryBodies[fmt.Sprintf("contemporary romance novel %d", nowYear-1)] = `{"docs":[{"title":"Just for the Summer","author_name":["Abby Jimenez"],"cover_i":35,"first_publish_year":2024}]}`
			queryBodies[fmt.Sprintf("contemporary romance novel %d", nowYear-2)] = `{"docs":[{"title":"Out on a Limb","author_name":["Hannah Bonam-Young"],"cover_i":74,"first_publish_year":2023}]}`
			body, ok := queryBodies[r.URL.Query().Get("q")]
			if !ok {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"docs":[]}`)),
					Header:     make(http.Header),
				}, nil
			}
			body = withOpenLibrarySearchWorkKeys(body)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}
		t.Fatalf("unexpected Open Library request: %s", r.URL.String())
		return nil, nil
	})
	t.Cleanup(func() { http.DefaultTransport = prevTransport })
}

func TestSearchPageServerRendersDiscoveryOnFirstLoad(t *testing.T) {
	s := newServerForTest(t)
	req := httptest.NewRequest(http.MethodGet, "/search", nil)
	req.AddCookie(makeCookie(t, s, "user", false))
	rec := httptest.NewRecorder()

	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Your next bad bedtime decision",
		`hx-get="/ui/search"`,
		`hx-trigger="load"`,
		"Loading discovery shelves...",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in body: %s", want, body)
		}
	}
	if strings.Contains(body, "Trending This Week") {
		t.Fatalf("expected discovery content to load asynchronously: %s", body)
	}
}

func TestSearchUIShowsDiscoveryWhenQueryBlank(t *testing.T) {
	installOpenLibraryDiscoveryTransport(t)

	s := newServerForTest(t)
	req := httptest.NewRequest(http.MethodGet, "/ui/search", nil)
	req.AddCookie(makeCookie(t, s, "user", false))
	rec := httptest.NewRecorder()

	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Trending This Week",
		"Fantasy Hits",
		"Thriller Buzz",
		"Romance Favorites",
		"Sci-Fi Series Hits",
		"Project Hail Mary",
		"Assistant to the Villain",
		"The Tainted Cup",
		"Never Lie",
		"The Boyfriend",
		"Bride",
		"Some Desperate Glory",
		"Service Model",
		"Death of the Author",
		"View details",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("expected %q in body: %s", want, body)
		}
	}
	if strings.Contains(body, "Current picks you can open and request without typing first.") {
		t.Fatalf("expected extra current-picks copy to be removed: %s", body)
	}
	if count := strings.Count(body, `data-open-book="1"`); count < 35 {
		t.Fatalf("expected fuller category shelves, found %d cards in body: %s", count, body)
	}
	for _, unwanted := range []string{
		"Red Rising",
		"The Last Mrs. Parrish",
		"The Romantasy Cookbook",
		"The Romantasy Boxset",
	} {
		if strings.Contains(body, unwanted) {
			t.Fatalf("expected older fallback title %q to stay out of discovery shelves: %s", unwanted, body)
		}
	}
	if strings.Contains(body, `Results for "`) {
		t.Fatalf("expected discovery view, got search results wrapper: %s", body)
	}
	if strings.Contains(body, "Open details to request") {
		t.Fatalf("expected discovery label to be removed: %s", body)
	}
	if strings.Contains(body, "4 picks") {
		t.Fatalf("expected discovery count label to be removed: %s", body)
	}
}

func TestReadarrCoverSetsCacheHeaders(t *testing.T) {
	prevTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("cover-bytes")),
			Header: http.Header{
				"Content-Type":  []string{"image/jpeg"},
				"Etag":          []string{`"cover-123"`},
				"Last-Modified": []string{"Mon, 02 Jan 2006 15:04:05 GMT"},
			},
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = prevTransport })

	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = "https://readarr.example.internal"
	cfg.Readarr.Ebooks.APIKey = "test-key"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/ui/readarr-cover?u=https%3A%2F%2Freadarr.example.internal%2FMediaCover%2F12.jpg", nil)
	req.AddCookie(makeCookie(t, s, "user", false))
	rec := httptest.NewRecorder()

	s.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, max-age=3600" {
		t.Fatalf("unexpected Cache-Control header %q", got)
	}
	if got := rec.Header().Get("ETag"); got != `"cover-123"` {
		t.Fatalf("unexpected ETag header %q", got)
	}
	if got := rec.Header().Get("Last-Modified"); got != "Mon, 02 Jan 2006 15:04:05 GMT" {
		t.Fatalf("unexpected Last-Modified header %q", got)
	}
}
