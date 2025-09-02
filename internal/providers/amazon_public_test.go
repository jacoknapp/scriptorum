package providers

import (
	"context"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
)

type rtFunc func(*http.Request) (*http.Response, error)
func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestAmazonPublicGetByASIN(t *testing.T) {
	html := `
	<html><head><meta property="og:title" content="Test Book"/><meta property="og:image" content="http://img"/></head>
	<body>
	<div id="bylineInfo"><span class="author"><a>Jane Doe</a></span></div>
	<div id="detailBullets_feature_div"><li><span class="a-text-bold">ISBN-13:</span> 9781234567897</li></div>
	</body></html>`

	a := NewAmazonPublic("www.amazon.com")
	a.client.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{ StatusCode: 200, Body: ioutil.NopCloser(strings.NewReader(html)), Header: make(http.Header)}, nil
	})
	b, err := a.GetByASIN(context.Background(), "B012345678")
	if err != nil { t.Fatalf("err: %v", err) }
	if b.Title != "Test Book" { t.Fatalf("title: %s", b.Title) }
	if b.ISBN13 != "9781234567897" { t.Fatalf("isbn13: %s", b.ISBN13) }
	if len(b.Authors) != 1 || b.Authors[0] != "Jane Doe" { t.Fatalf("authors: %+v", b.Authors) }
}

func TestAmazonPublicSearchBooks(t *testing.T) {
	html := `
	<html><body>
	<div class="s-result-item" data-asin="B0ABC12345"><h2><a><span>One Book</span></a></h2><img class="s-image" src="http://i1"/></div>
	<div class="s-result-item" data-asin="B0DEF67890"><h2><a><span>Two Book</span></a></h2><img class="s-image" src="http://i2"/></div>
	</body></html>`
	a := NewAmazonPublic("www.amazon.com")
	a.client.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{ StatusCode: 200, Body: ioutil.NopCloser(strings.NewReader(html)), Header: make(http.Header)}, nil
	})
	items, err := a.SearchBooks(context.Background(), "keyword", 10)
	if err != nil { t.Fatalf("err: %v", err) }
	if len(items) != 2 { t.Fatalf("want 2 got %d", len(items)) }
	if items[0].ASIN == "" || items[0].Title == "" { t.Fatalf("missing fields") }
}
