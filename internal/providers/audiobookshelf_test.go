package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestABSPingOK(t *testing.T) {
	a := NewABS("http://abs", "", "")
	a.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"version":"1"}`)), Header: make(http.Header)}, nil
	})
	if err := a.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestABSPresence(t *testing.T) {
	a := NewABS("http://abs", "", "")
	a.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"results":[{"title":"X"}]}`)), Header: make(http.Header)}, nil
	})
	ok, err := a.HasTitle(context.Background(), "Anything")
	if err != nil || !ok {
		t.Fatalf("presence: %v %v", ok, err)
	}
}
