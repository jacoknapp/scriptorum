package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestOnlyISBNAndContains(t *testing.T) {
	if got := onlyISBN("ISBN-13:978-1-2345-6789-7"); got != "9781234567897" {
		t.Fatalf("unexpected 13-digit ISBN: %q", got)
	}
	if got := onlyISBN("ISBN-10:123-456-7890"); got != "1234567890" {
		t.Fatalf("unexpected 10-digit ISBN: %q", got)
	}
	if got := onlyISBN(" short "); got != "SHORT" {
		t.Fatalf("unexpected short ISBN normalization: %q", got)
	}

	if !contains([]string{"a", "b", "c"}, "b") {
		t.Fatal("expected contains to find present value")
	}
	if contains([]string{"a", "b", "c"}, "z") {
		t.Fatal("expected contains to reject missing value")
	}
}

func TestNormalizeReadarrInstance(t *testing.T) {
	got := normalize(ReadarrInstance{BaseURL: " readarr.internal:8787/ "})
	if got.BaseURL != "http://readarr.internal:8787" {
		t.Fatalf("unexpected normalized base url: %q", got.BaseURL)
	}

	kept := normalize(ReadarrInstance{BaseURL: "https://readarr.example/"})
	if kept.BaseURL != "https://readarr.example" {
		t.Fatalf("unexpected preserved https url: %q", kept.BaseURL)
	}
}

func TestGetValidQualityProfileIDFallbackAndRedactAPIKeyPassthrough(t *testing.T) {
	ra := NewReadarrWithDB(ReadarrInstance{BaseURL: "http://readarr", APIKey: "secret", DefaultQualityProfileID: 99}, nil)
	ra.cl.Transport = rtFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/api/v1/qualityprofile" {
			t.Fatalf("unexpected path: %s", req.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`[{"id":3,"name":"Any"},{"id":5,"name":"Lossless"}]`)),
			Header:     make(http.Header),
		}, nil
	})

	got := ra.getValidQualityProfileID(context.Background())
	if got != 3 && got != 5 {
		t.Fatalf("expected fallback quality profile id from server, got %d", got)
	}

	if passthrough := redactAPIKey("http://readarr/api/v1/book?term=test"); passthrough != "http://readarr/api/v1/book?term=test" {
		t.Fatalf("expected URL without apikey to pass through, got %q", passthrough)
	}
}
