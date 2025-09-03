package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestReadarrNormalizeDefaults(t *testing.T) {
	r := NewReadarr(ReadarrInstance{BaseURL: "http://r"})
	if !strings.Contains(r.inst.LookupEndpoint, "/api/v1/") || !strings.Contains(r.inst.AddEndpoint, "/api/v1/") {
		t.Fatalf("endpoints not normalized: %s %s", r.inst.LookupEndpoint, r.inst.AddEndpoint)
	}
}

func TestReadarrPingLookupStatusError(t *testing.T) {
	r := NewReadarr(ReadarrInstance{BaseURL: "http://r"})
	r.cl.Transport = rtFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})
	if err := r.PingLookup(context.Background()); err == nil {
		t.Fatalf("expected status error")
	}
}
