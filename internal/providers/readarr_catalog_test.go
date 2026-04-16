package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListBooks(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/book" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[{"id":42,"title":"Burn for Me","foreignBookId":"fb-1","foreignEditionId":"fe-1","monitored":true,"grabbed":false,"statistics":{"bookFileCount":1},"author":{"name":"Ilona Andrews"},"identifiers":[{"type":"isbn13","value":"9780000000001"}]}]`))
	}))
	defer ts.Close()

	ra := NewReadarrWithDB(ReadarrInstance{BaseURL: ts.URL, APIKey: "test-key"}, nil)
	books, err := ra.ListBooks(context.Background())
	if err != nil {
		t.Fatalf("ListBooks: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("expected 1 book, got %d", len(books))
	}
	if books[0].ID != 42 {
		t.Fatalf("expected book id 42, got %d", books[0].ID)
	}
	if books[0].Statistics.BookFileCount != 1 {
		t.Fatalf("expected book file count 1, got %d", books[0].Statistics.BookFileCount)
	}
}
