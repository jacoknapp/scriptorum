package httpapi

import (
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

func TestEnsureDiscoveryCategoriesUsesExisting(t *testing.T) {
	existing := []discoveryCategory{{Name: "Existing", Items: []searchItem{{BookItem: providers.BookItem{Title: "Existing Title"}}}}}
	got := ensureDiscoveryCategories(existing, []searchItem{{}, {}})
	if len(got) != 1 || got[0].Name != "Existing" {
		t.Fatalf("expected existing categories to be preserved, got %+v", got)
	}
}

func TestEnsureDiscoveryCategoriesBuildsFromTrending(t *testing.T) {
	trending := []searchItem{{BookItem: providers.BookItem{Title: "One"}}, {BookItem: providers.BookItem{Title: "Two"}}}

	got := ensureDiscoveryCategories(nil, trending)
	if len(got) != 1 {
		t.Fatalf("expected one synthesized category, got %+v", got)
	}
	if got[0].Name != "More to Explore" {
		t.Fatalf("unexpected synthesized category name: %+v", got[0])
	}
	if len(got[0].Items) != 2 || got[0].Items[0].Title != "One" || got[0].Items[1].Title != "Two" {
		t.Fatalf("unexpected synthesized category items: %+v", got[0].Items)
	}

	// Ensure returned items are copied and not aliasing original slice.
	trending[0].Title = "Changed"
	if got[0].Items[0].Title != "One" {
		t.Fatalf("expected synthesized category copy to remain stable, got %+v", got[0].Items)
	}
}
