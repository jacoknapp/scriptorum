package providers

import (
	"context"
	"fmt"
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

func TestReadarrPingLookupRedactsAPIKeyOnTransportError(t *testing.T) {
	r := NewReadarrWithDB(ReadarrInstance{BaseURL: "http://r", APIKey: "supersecret"}, nil)
	r.cl.Transport = rtFunc(func(req *http.Request) (*http.Response, error) {
		return nil, fmt.Errorf(`Get "http://r/api/v1/book/lookup?term=test&apikey=supersecret": tls: failed`)
	})
	err := r.PingLookup(context.Background())
	if err == nil {
		t.Fatal("expected transport error")
	}
	if strings.Contains(err.Error(), "supersecret") {
		t.Fatalf("expected api key to be redacted, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "apikey=***") {
		t.Fatalf("expected redacted url in error, got %q", err.Error())
	}
}
