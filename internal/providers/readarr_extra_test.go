package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestReadarrNormalizeDefaults(t *testing.T) {
	r := NewReadarrWithDB(ReadarrInstance{BaseURL: "http://r"}, nil)
	// Test that the base URL is normalized correctly
	if r.inst.BaseURL != "http://r" {
		t.Fatalf("base URL not normalized correctly: %s", r.inst.BaseURL)
	}
}

func TestReadarrPingLookupStatusError(t *testing.T) {
	r := NewReadarrWithDB(ReadarrInstance{BaseURL: "http://r"}, nil)
	r.cl.Transport = rtFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})
	if err := r.PingLookup(context.Background()); err == nil {
		t.Fatalf("expected status error")
	}
}
