package providers

import (
	"context"
	"testing"
)

func TestSanitizeAuthorAddOptionsMonitor(t *testing.T) {
	r := NewReadarr(ReadarrInstance{})
	// Build a payload with an author but without addOptions
	pmap := map[string]any{
		"author": map[string]any{"name": "Test Author"},
	}
	out := r.sanitizeAndEnrichPayload(context.Background(), pmap, AddOpts{})
	a, ok := out["author"].(map[string]any)
	if !ok {
		t.Fatalf("expected author map, got %#v", out["author"])
	}
	ao, ok := a["addOptions"].(map[string]any)
	if !ok {
		t.Fatalf("expected author.addOptions map, got %#v", a["addOptions"])
	}
	if m, _ := ao["monitor"].(string); m != "none" {
		t.Fatalf("expected author.addOptions.monitor == 'none', got %q", m)
	}
}
