package providers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestReadarrSearchBooksQueuesCommand(t *testing.T) {
	var gotPath, gotMethod, gotBody, gotAPIKey string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAPIKey = r.Header.Get("X-Api-Key")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":1}`))
	}))
	t.Cleanup(ts.Close)

	r := NewReadarrWithDB(ReadarrInstance{BaseURL: ts.URL, APIKey: "secret"}, nil)
	if _, err := r.SearchBooks(context.Background(), []int{10, 20}); err != nil {
		t.Fatalf("SearchBooks: %v", err)
	}

	if gotMethod != http.MethodPost || gotPath != "/api/v1/command" {
		t.Fatalf("unexpected request %s %s", gotMethod, gotPath)
	}
	if gotAPIKey != "secret" {
		t.Fatalf("expected API key header, got %q", gotAPIKey)
	}
	if !strings.Contains(gotBody, `"name":"BookSearch"`) || !strings.Contains(gotBody, `"bookIds":[10,20]`) {
		t.Fatalf("unexpected body: %s", gotBody)
	}
}
