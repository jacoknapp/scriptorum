package httpapi

import (
	"context"
	"testing"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/db"
	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

func TestReadarrStateFromResponseVariants(t *testing.T) {
	tests := []struct {
		name   string
		body   []byte
		wantID int64
		want   string
	}{
		{name: "empty", body: nil, wantID: 0, want: ""},
		{name: "invalid json", body: []byte("{"), wantID: 0, want: ""},
		{name: "available from stats", body: []byte(`{"id":12,"statistics":{"bookFileCount":1}}`), wantID: 12, want: "available"},
		{name: "grabbed", body: []byte(`{"id":13,"grabbed":true}`), wantID: 13, want: "grabbed"},
		{name: "monitored", body: []byte(`{"id":14,"monitored":true}`), wantID: 14, want: "monitored"},
		{name: "array first usable", body: []byte(`[{"id":0},{"id":15,"monitored":true}]`), wantID: 15, want: "monitored"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotID, got := readarrStateFromResponse(tc.body)
			if gotID != tc.wantID || got != tc.want {
				t.Fatalf("got (%d,%q) want (%d,%q)", gotID, got, tc.wantID, tc.want)
			}
		})
	}
}

func TestCtxOrBackground(t *testing.T) {
	if got := ctxOrBackground(nil); got == nil {
		t.Fatal("expected background context for nil input")
	}

	ctx := context.WithValue(context.Background(), "k", "v")
	if got := ctxOrBackground(ctx); got != ctx {
		t.Fatal("expected same non-nil context instance back")
	}
}

func TestBuildCatalogMatchQueryFromPayload(t *testing.T) {
	query := buildCatalogMatchQuery(" audiobooks ", "", nil, "", "", "", []byte(`{
		"title":"Payload Title",
		"foreignBookId":"fb-1",
		"foreignEditionId":"fe-1",
		"author":{"name":"Payload Author"}
	}`))

	if query.SourceKind != "audiobook" {
		t.Fatalf("unexpected source kind: %q", query.SourceKind)
	}
	if query.Title != "Payload Title" || query.ForeignBookID != "fb-1" || query.ForeignEditionID != "fe-1" {
		t.Fatalf("unexpected query fields: %+v", query)
	}
	if len(query.Authors) != 1 || query.Authors[0] != "Payload Author" {
		t.Fatalf("unexpected authors: %+v", query.Authors)
	}

	invalid := buildCatalogMatchQuery("ebook", "Title", []string{"Author"}, "10", "13", "asin", []byte("{"))
	if invalid.Title != "Title" || invalid.ISBN10 != "10" || invalid.ISBN13 != "13" || invalid.ASIN != "asin" {
		t.Fatalf("invalid payload should preserve explicit values: %+v", invalid)
	}
}

func TestCatalogMatchCacheLookup(t *testing.T) {
	s := makeTestServer(t)
	book := &db.ReadarrBook{ReadarrID: 88, Title: "Cached"}
	s.catalogMatchCache["match"] = catalogMatchCacheEntry{match: book, exp: time.Now().Add(time.Minute)}
	s.catalogMatchCache["notfound"] = catalogMatchCacheEntry{notFound: true, exp: time.Now().Add(time.Minute)}
	s.catalogMatchCache["expired"] = catalogMatchCacheEntry{match: &db.ReadarrBook{ReadarrID: 99}, exp: time.Now().Add(-time.Minute)}

	matched, ok, found := s.catalogMatchCacheLookup("match")
	if !ok || !found || matched == nil || matched.ReadarrID != 88 {
		t.Fatalf("unexpected match cache lookup: match=%+v ok=%v found=%v", matched, ok, found)
	}
	matched.ReadarrID = 77
	if s.catalogMatchCache["match"].match.ReadarrID != 88 {
		t.Fatal("expected returned cache match to be a copy")
	}

	matched, ok, found = s.catalogMatchCacheLookup("notfound")
	if !ok || found || matched != nil {
		t.Fatalf("unexpected notfound lookup: match=%+v ok=%v found=%v", matched, ok, found)
	}

	matched, ok, found = s.catalogMatchCacheLookup("expired")
	if ok || found || matched != nil {
		t.Fatalf("unexpected expired lookup: match=%+v ok=%v found=%v", matched, ok, found)
	}
	if _, exists := s.catalogMatchCache["expired"]; exists {
		t.Fatal("expected expired cache entry to be evicted")
	}
}

func TestDecorateSearchItemsAndURLHost(t *testing.T) {
	s := makeTestServer(t)
	if err := s.db.ReplaceReadarrBooks(context.Background(), "ebook", []db.ReadarrBook{{
		SourceKind:    "ebook",
		ReadarrID:     1,
		Title:         "Decorated",
		AuthorName:    "Author One",
		BookFileCount: 1,
	}}); err != nil {
		t.Fatalf("replace ebook books: %v", err)
	}
	if err := s.db.ReplaceReadarrBooks(context.Background(), "audiobook", []db.ReadarrBook{{
		SourceKind: "audiobook",
		ReadarrID:  2,
		Title:      "Decorated",
		AuthorName: "Author One",
		Monitored:  true,
	}}); err != nil {
		t.Fatalf("replace audiobook books: %v", err)
	}

	items := []searchItem{{
		BookItem:                 providers.BookItem{Title: "Decorated", Authors: []string{"Author One"}},
		ProviderEbookPayload:     `{"foreignBookId":"ebook-fb"}`,
		ProviderAudiobookPayload: `{"foreignBookId":"audio-fb"}`,
	}}
	decorateSearchItems(s, items)

	if items[0].ProviderPayload == "" {
		t.Fatal("expected merged provider payload")
	}
	if items[0].EbookState != "available" || items[0].AudiobookState != "monitored" {
		t.Fatalf("unexpected decorated states: %+v", items[0])
	}

	if got := urlHost(""); got != "" {
		t.Fatalf("expected empty host for empty base, got %q", got)
	}
	if got := urlHost("https://readarr.example:8787/"); got != "readarr.example:8787" {
		t.Fatalf("unexpected parsed host: %q", got)
	}
	if got := urlHost("readarr.internal:8787/"); got != "" {
		t.Fatalf("unexpected schemeless host parsing result: %q", got)
	}
	if got := urlHost("https://readarr.internal/%zz"); got != "readarr.internal/%zz" {
		t.Fatalf("unexpected fallback host: %q", got)
	}
}
