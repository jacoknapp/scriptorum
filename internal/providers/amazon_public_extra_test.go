package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestAmazonPublicDefaultsMarketplace(t *testing.T) {
	a := NewAmazonPublic("")
	// ensure it tries to hit www.amazon.com and sends UA
	a.client.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if !strings.Contains(r.URL.Host, "amazon.com") {
			t.Fatalf("host: %s", r.URL.Host)
		}
		if r.Header.Get("User-Agent") == "" {
			t.Fatalf("missing UA header")
		}
		body := `<html><body><div class="s-result-item" data-asin="B0X"><h2><a><span>T</span></a></h2><img class="s-image" src="http://i"/></div></body></html>`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	items, err := a.SearchBooks(context.Background(), "x", 1, 1)
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
}
