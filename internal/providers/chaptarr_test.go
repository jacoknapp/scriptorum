package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChaptarrRequestsEbookAndAudiobookWithTypedProfiles(t *testing.T) {
	for _, format := range []string{"ebook", "audiobook"} {
		format := format
		t.Run(format, func(t *testing.T) {
			authorEnabled := false
			monitored := false
			editionSelected := false
			var addPayload map[string]any
			var selectedEditions []any
			searchQueued := false

			book := func() map[string]any {
				return map[string]any{
					"id": 41, "authorId": 7, "title": "The Test Book", "mediaType": format,
					"foreignBookId": "gr:work-1", "foreignEditionId": "gr:edition-1",
					"releaseDate": "2026-01-01T00:00:00Z", "images": []any{map[string]any{"url": "/cover.jpg"}},
					"monitored": monitored, "ebookMonitored": monitored && format == "ebook",
					"audiobookMonitored": monitored && format == "audiobook",
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Api-Key") != "secret" {
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/system/status":
					_, _ = w.Write([]byte(`{"appName":"Chaptarr","version":"0.9.911.0"}`))
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/qualityprofile":
					_, _ = w.Write([]byte(`[{"id":11,"name":"Ebook","profileType":"ebook"},{"id":12,"name":"Audio","profileType":"audiobook"}]`))
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/metadataprofile":
					_, _ = w.Write([]byte(`[{"id":21,"name":"Ebook metadata","profileType":2},{"id":22,"name":"Audio metadata","profileType":1}]`))
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/rootfolder":
					_, _ = w.Write([]byte(`[{"id":1,"path":"/ebooks","accessible":true,"isEffectiveDefaultEbook":true},{"id":2,"path":"/audio","accessible":true,"isEffectiveDefaultAudiobook":true}]`))
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/author":
					_, _ = w.Write([]byte(`[]`))
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/book":
					_ = json.NewDecoder(r.Body).Decode(&addPayload)
					_, _ = w.Write([]byte(`{"authorId":7}`))
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/book" && r.URL.Query().Get("authorId") == "7":
					_ = json.NewEncoder(w).Encode([]map[string]any{book()})
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/book/41":
					_ = json.NewEncoder(w).Encode(book())
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/author/7":
					_ = json.NewEncoder(w).Encode(map[string]any{"id": 7, "authorName": "Test Author", "monitored": true, "ebookMonitorFuture": authorEnabled && format == "ebook", "audiobookMonitorFuture": authorEnabled && format == "audiobook"})
				case r.Method == http.MethodPut && r.URL.Path == "/api/v1/author/7":
					authorEnabled = true
					_, _ = w.Write([]byte(`{}`))
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/edition":
					otherFormat := "audiobook"
					if format == "audiobook" {
						otherFormat = "ebook"
					}
					_ = json.NewEncoder(w).Encode([]map[string]any{
						{"id": 51, "bookId": 41, "title": "The Test Book", "foreignEditionId": "gr:edition-1", "format": format, "monitored": editionSelected},
						{"id": 52, "bookId": 41, "title": "The Other Edition", "foreignEditionId": "gr:edition-2", "format": otherFormat, "monitored": false},
					})
				case r.Method == http.MethodPut && r.URL.Path == "/api/v1/book/41":
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					selectedEditions, _ = body["editions"].([]any)
					editionSelected = true
					_, _ = w.Write([]byte(`{}`))
				case r.Method == http.MethodPut && r.URL.Path == "/api/v1/book/monitor":
					monitored = true
					w.WriteHeader(http.StatusAccepted)
					_, _ = w.Write([]byte(`{}`))
				case r.Method == http.MethodPost && r.URL.Path == "/api/v1/command":
					var command map[string]any
					_ = json.NewDecoder(r.Body).Decode(&command)
					searchQueued = command["name"] == "BookSearch"
					_, _ = w.Write([]byte(`{"id":99,"name":"BookSearch","status":"queued"}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			instance := ReadarrInstance{
				BaseURL: server.URL, APIKey: "secret", Backend: "chaptarr", MediaType: format,
				EbookQualityProfileID: 11, AudiobookQualityProfileID: 12,
				EbookMetadataProfileID: 21, AudiobookMetadataProfileID: 22,
				EbookRootFolderPath: "/ebooks", AudiobookRootFolderPath: "/audio",
			}
			client := NewReadarrWithDB(instance, nil)
			raw := json.RawMessage(`{"title":"The Test Book","foreignBookId":"gr:work-1","author":{"authorName":"Test Author","foreignAuthorId":"gr:author-1"}}`)
			_, response, err := client.AddBookRaw(context.Background(), raw)
			if err != nil {
				t.Fatalf("request %s: %v", format, err)
			}
			if len(response) == 0 || !searchQueued || !authorEnabled || !monitored {
				t.Fatalf("request sequence incomplete: response=%s search=%v author=%v monitored=%v", response, searchQueued, authorEnabled, monitored)
			}
			if got := addPayload["mediaType"]; got != format {
				t.Fatalf("mediaType=%v, want %s", got, format)
			}
			if addPayload["ebookQualityProfileId"] != float64(11) || addPayload["audiobookQualityProfileId"] != float64(12) || addPayload["ebookMetadataProfileId"] != float64(21) || addPayload["audiobookMetadataProfileId"] != float64(22) {
				t.Fatalf("missing typed Chaptarr profiles: %+v", addPayload)
			}
			wantRoot := "/ebooks"
			if format == "audiobook" {
				wantRoot = "/audio"
			}
			if addPayload["rootFolderPath"] != wantRoot {
				t.Fatalf("rootFolderPath=%v, want %s", addPayload["rootFolderPath"], wantRoot)
			}
			if len(selectedEditions) != 2 {
				t.Fatalf("selected editions=%+v", selectedEditions)
			}
			edition, _ := selectedEditions[0].(map[string]any)
			if edition["monitored"] != true || edition["manualAdd"] != true {
				t.Fatalf("edition was not explicitly selected: %+v", edition)
			}
			otherEdition, _ := selectedEditions[1].(map[string]any)
			if otherEdition["monitored"] != false || otherEdition["manualAdd"] != false {
				t.Fatalf("other format edition was not disabled: %+v", otherEdition)
			}
		})
	}
}

func TestChaptarrCapabilitiesRejectWrongProfileType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/system/status":
			_, _ = w.Write([]byte(`{"appName":"Chaptarr","version":"0.9.911.0"}`))
		case "/api/v1/qualityprofile":
			_, _ = w.Write([]byte(`[{"id":11,"name":"Audio only","profileType":"audiobook"}]`))
		case "/api/v1/metadataprofile":
			_, _ = w.Write([]byte(`[{"id":21,"name":"Ebook","profileType":2},{"id":22,"name":"Audio","profileType":1}]`))
		case "/api/v1/rootfolder":
			_, _ = w.Write([]byte(`[{"path":"/ebooks","accessible":true},{"path":"/audio","accessible":true}]`))
		}
	}))
	defer server.Close()
	client := NewReadarrWithDB(ReadarrInstance{BaseURL: server.URL, Backend: "chaptarr", EbookQualityProfileID: 11}, nil)
	_, err := client.ChaptarrCapabilities(context.Background())
	if err == nil || !strings.Contains(err.Error(), "wrong media type") {
		t.Fatalf("expected typed profile validation error, got %v", err)
	}
}

func TestPositiveInt(t *testing.T) {
	tests := []struct {
		name string
		v    any
		want int
	}{
		{"int", 42, 42},
		{"int64", int64(100), 100},
		{"float64", float64(3.14), 3},
		{"json.Number", json.Number("99"), 99},
		{"string", "77", 77},
		{"string trim", " 88 ", 88},
		{"invalid string", "abc", 0},
		{"nil bool", true, 0},
	}
	for _, tt := range tests {
		got := positiveInt(tt.v)
		if got != tt.want {
			t.Errorf("%s: positiveInt(%v) = %d, want %d", tt.name, tt.v, got, tt.want)
		}
	}
}

func TestFindChaptarrAuthorUsesSelectedLocalID(t *testing.T) {
	listedAll := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/author/273":
			// Chaptarr may canonicalize the selected Goodreads identity to a
			// different provider while retaining the same local id and name.
			_, _ = w.Write([]byte(`{"id":273,"authorName":"Brandon Sanderson","foreignAuthorId":"hc:6360"}`))
		case "/api/v1/author":
			listedAll = true
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewReadarrWithDB(ReadarrInstance{BaseURL: server.URL, APIKey: "secret", Backend: "chaptarr"}, nil)
	got, err := client.findChaptarrAuthor(context.Background(), map[string]any{
		"author": map[string]any{"id": 273, "authorName": "Brandon Sanderson", "foreignAuthorId": "gr:38550"},
	})
	if err != nil {
		t.Fatalf("find author by id: %v", err)
	}
	if listedAll {
		t.Fatal("downloaded the full author catalog despite a selected local id")
	}
	if positiveInt(got["id"]) != 273 {
		t.Fatalf("author=%+v", got)
	}
}

func TestNormalizeBookTitle(t *testing.T) {
	tests := []struct{ in, want string }{
		{"The Hobbit", "the hobbit"},
		{"  THE FELLOWSHIP OF THE RING  ", "the fellowship of the ring"},
		{"A\tStorm of\nSwords", "a storm of swords"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizeBookTitle(tt.in)
		if got != tt.want {
			t.Errorf("normalizeBookTitle(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFindChaptarrBookMatchesCanonicalSeriesSuffix(t *testing.T) {
	client := &Readarr{}
	books := []map[string]any{
		{"id": 26992, "title": "The Lost Metal (1 of 2) [Dramatized Adaptation]", "mediaType": "audiobook"},
		{"id": 26998, "title": "The Lost Metal (2 of 2) [Dramatized Adaptation]", "mediaType": "audiobook"},
		{"id": 26812, "title": "The Lost Metal", "mediaType": "audiobook"},
		{"id": 32442, "title": "The Lost Metal", "mediaType": "ebook"},
	}
	got := client.findChaptarrBook(books, "The Lost Metal (Mistborn, #7)", "gr:43551632", "audiobook")
	if positiveInt(got["id"]) != 26812 {
		t.Fatalf("matched wrong Lost Metal rendition: %+v", got)
	}
}

func TestFindChaptarrBookRejectsEqualConfidenceDuplicates(t *testing.T) {
	client := &Readarr{}
	books := []map[string]any{
		{"id": 10, "title": "Shared Title", "mediaType": "audiobook"},
		{"id": 11, "title": "Shared Title", "mediaType": "audiobook"},
	}
	if got := client.findChaptarrBook(books, "Shared Title", "provider-id-not-retained", "audiobook"); got != nil {
		t.Fatalf("picked an ambiguous local row: %+v", got)
	}
}
