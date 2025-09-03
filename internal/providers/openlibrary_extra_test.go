package providers

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestOpenLibraryEmptyQuery(t *testing.T) {
	ol := NewOpenLibrary()
	items, err := ol.Search(context.Background(), "", 10, 1)
	if err != nil || items != nil {
		t.Fatalf("expected nil,nil got %v,%v", items, err)
	}
}

func TestOpenLibraryHTTPError(t *testing.T) {
	ol := NewOpenLibrary()
	ol.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		return nil, errors.New("boom")
	})
	_, err := ol.Search(context.Background(), "x", 10, 1)
	if err == nil {
		t.Fatalf("expected error")
	}
}
