package httpapi

import (
	"strings"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

// An OpenLibrary result matching a Readarr item by title/author should fill
// the item's missing identifiers and cover instead of duplicating the row.
func TestMergeOpenLibraryFillsGapsOnMatchingItem(t *testing.T) {
	readarrItem := searchItem{
		BookItem: providers.BookItem{
			Title:       "Project Hail Mary",
			Authors:     []string{"Andy Weir"},
			CoverMedium: "/ui/readarr-cover?u=https%3A%2F%2Freadarr.example.internal%2FMediaCoverProxy%2Fabc%2F1.jpg",
		},
		Provider:             "readarr-ebook",
		ProviderEbookPayload: `{"title":"Project Hail Mary"}`,
	}
	items := []searchItem{readarrItem}
	idx := map[string]int{dedupeKey(readarrItem.BookItem): 0}

	ol := providers.BookItem{
		Title:       "Project Hail Mary",
		Authors:     []string{"Andy Weir"},
		ISBN13:      "9780593135204",
		ISBN10:      "0593135202",
		CoverMedium: "https://covers.openlibrary.org/b/id/1-M.jpg",
	}
	merged := mergeOpenLibrarySearchItems(items, idx, []providers.BookItem{ol})

	if len(merged) != 1 {
		t.Fatalf("expected 1 merged item, got %d", len(merged))
	}
	got := merged[0]
	if got.ISBN13 != "9780593135204" || got.ISBN10 != "0593135202" {
		t.Fatalf("expected identifiers filled, got isbn13=%q isbn10=%q", got.ISBN13, got.ISBN10)
	}
	if got.ProviderEbookPayload == "" {
		t.Fatalf("expected readarr payload preserved")
	}
	// Existing Readarr cover is kept but gains the isbn fallback parameter.
	if !strings.HasPrefix(got.CoverMedium, "/ui/readarr-cover?u=") {
		t.Fatalf("expected readarr cover kept, got %q", got.CoverMedium)
	}
	if !strings.Contains(got.CoverMedium, "&isbn=9780593135204") {
		t.Fatalf("expected isbn fallback appended to cover, got %q", got.CoverMedium)
	}
}

// OpenLibrary books unknown to Readarr should be appended as new results and
// registered so later identifier-less duplicates still merge.
func TestMergeOpenLibraryAppendsUnknownBooks(t *testing.T) {
	items := []searchItem{}
	idx := map[string]int{}

	ol := providers.BookItem{
		Title:       "Obscure Tome",
		Authors:     []string{"Nobody Famous"},
		ISBN13:      "9780316274147",
		CoverMedium: "https://covers.openlibrary.org/b/id/2-M.jpg",
	}
	merged := mergeOpenLibrarySearchItems(items, idx, []providers.BookItem{ol})

	if len(merged) != 1 {
		t.Fatalf("expected appended OL item, got %d items", len(merged))
	}
	if merged[0].Title != "Obscure Tome" || merged[0].DetailsPayload == "" {
		t.Fatalf("expected OL search item with details payload, got %+v", merged[0])
	}
	if _, ok := idx["ISBN13:9780316274147"]; !ok {
		t.Fatalf("expected isbn13 key registered")
	}
	if _, ok := idx[titleAuthorKey(ol)]; !ok {
		t.Fatalf("expected title/author key registered")
	}

	// A second copy of the same book (no matter the key it matches by) merges
	// rather than duplicating.
	merged = mergeOpenLibrarySearchItems(merged, idx, []providers.BookItem{{
		Title:   "Obscure Tome",
		Authors: []string{"Nobody Famous"},
	}})
	if len(merged) != 1 {
		t.Fatalf("expected duplicate to merge, got %d items", len(merged))
	}
}

// Junk titles (compilations, study guides) are filtered from OL results too.
func TestMergeOpenLibrarySkipsNonRenderableTitles(t *testing.T) {
	merged := mergeOpenLibrarySearchItems(nil, map[string]int{}, []providers.BookItem{{
		Title:   "Project Hail Mary Study Guide",
		Authors: []string{"Test Author"},
		ISBN13:  "9780000000001",
	}})
	if len(merged) != 0 {
		t.Fatalf("expected non-renderable title skipped, got %d items", len(merged))
	}
}
