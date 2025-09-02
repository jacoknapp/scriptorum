package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSelectCandidateIdentifierPriority(t *testing.T) {
	ra := NewReadarr(ReadarrInstance{BaseURL: "http://x"})
	list := []LookupBook{
		{Title: "A", Identifiers: []map[string]any{{"identifierType": "ISBN10", "value": "1234567890"}}},
		{Title: "B", Identifiers: []map[string]any{{"identifierType": "ISBN13", "value": "9781234567897"}}},
		{Title: "C", Identifiers: []map[string]any{{"identifierType": "ASIN", "value": "B012345678"}}},
	}
	if cand, ok := ra.SelectCandidate(list, "9781234567897", "", ""); !ok || cand["title"].(string) != "B" {
		t.Fatalf("isbn13 should win: %+v", cand)
	}
	if cand, ok := ra.SelectCandidate(list, "", "1234567890", ""); !ok || cand["title"].(string) != "A" {
		t.Fatalf("isbn10 should win")
	}
	if cand, ok := ra.SelectCandidate(list, "", "", "B012345678"); !ok || cand["title"].(string) != "C" {
		t.Fatalf("asin should win")
	}
}

func TestReadarrAddBookRoundTrip(t *testing.T) {
	lookupJSON := `[{"title":"Book","titleSlug":"book","author":{"id":1},"identifiers":[{"identifierType":"ISBN13","value":"9781111111111"}],"editions":[]}]`
	ra := NewReadarr(ReadarrInstance{BaseURL: "http://mock", APIKey: "k", AddPayloadTemplate: `{"title":{{ toJSON (index .Candidate "title") }}}`})
	ra.cl.Transport = rtFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/lookup") {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(lookupJSON)), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"ok":true}`)), Header: make(http.Header)}, nil
	})
	res, err := ra.LookupByTerm(context.Background(), "9781111111111")
	if err != nil || len(res) == 0 {
		t.Fatalf("lookup: %v %v", len(res), err)
	}
	cand, ok := ra.SelectCandidate(res, "9781111111111", "", "")
	if !ok {
		t.Fatalf("no cand")
	}
	pay, resp, err := ra.AddBook(context.Background(), cand, AddOpts{})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(pay, &m)
	if m["title"] != "Book" {
		t.Fatalf("payload mismatch: %s", string(pay))
	}
	if len(resp) == 0 {
		t.Fatalf("empty resp")
	}
}
