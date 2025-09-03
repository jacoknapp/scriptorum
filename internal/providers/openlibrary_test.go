package providers

import (
	"context"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
)

func TestOpenLibrarySearch(t *testing.T) {
	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		body := `{"docs":[{"title":"Project Hail Mary","author_name":["Andy Weir"],"isbn":["1529157466"],"isbn13":["9781529157468"],"cover_i":12345}]}`
		return &http.Response{StatusCode: 200, Body: ioutil.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})
	items, err := ol.Search(context.Background(), "project hail mary", 10, 1)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("want 1 got %d", len(items))
	}
	if items[0].Title == "" || items[0].ISBN13 == "" {
		t.Fatalf("missing fields: %+v", items[0])
	}
}
