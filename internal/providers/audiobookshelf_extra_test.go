package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestABSAuthHeaderFormatting(t *testing.T) {
	a := NewABS("http://abs", "mytoken", "")
	a.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer mytoken" {
			t.Fatalf("auth header = %q", got)
		}
		// respond with empty results for presence
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"results":[]}`)), Header: make(http.Header)}, nil
	})
	ok, err := a.HasTitle(context.Background(), "anything")
	if err != nil || ok {
		t.Fatalf("hasTitle err=%v ok=%v", err, ok)
	}
}

func TestABSEmptyBasePingError(t *testing.T) {
	a := NewABS("", "", "")
	if err := a.Ping(context.Background()); err == nil {
		t.Fatalf("expected error for empty base")
	}
}

func TestABSRenderSearchURLTemplate(t *testing.T) {
	a := NewABS("http://abs", "", "/api/search?query={{urlquery .Term}}&limit=5")
	a.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if u := r.URL.String(); u != "http://abs/api/search?query=C%2B%2B+Primer&limit=5" {
			t.Fatalf("url: %s", u)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"results":[]}`)), Header: make(http.Header)}, nil
	})
	_, _ = a.HasTitle(context.Background(), "C++ Primer")
}
