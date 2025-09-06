package httpapi

import (
	"encoding/json"
	"strings"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

func TestMergeProviderPayloads_BothDifferentCreatesWrapper(t *testing.T) {
	e := `{"title":"A"}`
	a := `{"title":"A","format":"audio"}`
	m := mergeProviderPayloads(e, a)
	if m == "" {
		t.Fatalf("expected non-empty merged payload")
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal([]byte(m), &out); err != nil {
		t.Fatalf("merged payload must be valid JSON object: %v", err)
	}
	if _, ok := out["ebook"]; !ok {
		t.Fatalf("merged payload missing ebook field")
	}
	if _, ok := out["audiobook"]; !ok {
		t.Fatalf("merged payload missing audiobook field")
	}
}

func TestCoverOverwriteWhenMergingItems(t *testing.T) {
	// simulate two search items that will be deduped into one
	// use helper logic from search.go upsert to simulate merge
	base := providers.BookItem{Title: "The Book", Authors: []string{"Alice"}, CoverMedium: "https://one.example/c.jpg"}
	inc := providers.BookItem{Title: "The Book", Authors: []string{"Alice"}, CoverMedium: "https://two.example/c.jpg"}
	// After merge, expect CoverMedium to be replaced by incoming value
	if base.CoverMedium == inc.CoverMedium {
		t.Skip("test inconclusive: same URL")
	}
	// Simulate merge behavior: prefer non-empty incoming cover and overwrite
	merged := mergeCover(base.CoverMedium, inc.CoverMedium)
	if !strings.HasPrefix(merged, "https://two.example/") && !strings.Contains(merged, "two.example") {
		t.Fatalf("expected merged cover to include incoming host, got %s", merged)
	}
	if !strings.Contains(merged, "v=") {
		t.Fatalf("expected cache-buster param in merged cover, got %s", merged)
	}
}
