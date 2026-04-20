package httpapi

import (
	"context"
	"strings"
	"testing"
)

func TestDiscoveryErrorSearchData(t *testing.T) {
	data := discoveryErrorSearchData("  timeout from provider  ")
	if isDiscovery, _ := data["IsDiscovery"].(bool); !isDiscovery {
		t.Fatalf("expected discovery marker, got %+v", data)
	}
	if got, _ := data["DiscoveryError"].(string); got != "timeout from provider" {
		t.Fatalf("unexpected discovery error text: %q", got)
	}
}

func TestCachedDiscoverySearchDataReturnsErrorOnProbeFailure(t *testing.T) {
	s := newServerForTest(t)

	originalBuilder := buildDiscoverySearchDataFn
	originalProbe := discoveryProbeErrorFn
	buildDiscoverySearchDataFn = func(ctx context.Context, _ *Server, _ *searchUI) map[string]any {
		return map[string]any{"IsDiscovery": true}
	}
	discoveryProbeErrorFn = func(ctx context.Context) string {
		return "openlibrary timeout"
	}
	t.Cleanup(func() {
		buildDiscoverySearchDataFn = originalBuilder
		discoveryProbeErrorFn = originalProbe
	})

	data := s.cachedDiscoverySearchData(context.Background(), &searchUI{})
	if loading, _ := data["DiscoveryLoading"].(bool); loading {
		t.Fatalf("expected error payload, got loading state: %+v", data)
	}
	if got, _ := data["DiscoveryError"].(string); !strings.Contains(got, "openlibrary timeout") {
		t.Fatalf("expected probe error to be surfaced, got %+v", data)
	}
}
