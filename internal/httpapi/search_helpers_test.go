package httpapi

import (
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

func TestDedupeKey(t *testing.T) {
	b := providers.BookItem{ASIN: "B012345678", Title: " The Book ", Authors: []string{"Alice"}}
	if k := dedupeKey(b); k != "ASIN:B012345678" {
		t.Fatalf("key=%s", k)
	}
	b.ASIN = ""
	b.ISBN13 = "9781234567897"
	if k := dedupeKey(b); k != "ISBN13:9781234567897" {
		t.Fatalf("key=%s", k)
	}
	b.ISBN13 = ""
	if k := dedupeKey(b); k != "TA:the book:alice" {
		t.Fatalf("key=%s", k)
	}
}

func TestAuthorsTextAndTruncateChars(t *testing.T) {
	if got := authorsText(nil); got != "Unknown Author" {
		t.Fatalf("expected fallback author text, got %q", got)
	}

	joined := authorsText([]string{"  First Author  ", "Second Author", ""})
	if joined != "First Author, Second Author" {
		t.Fatalf("unexpected joined authors: %q", joined)
	}

	short := truncateChars("Alice", 10)
	if short != "Alice" {
		t.Fatalf("unexpected short truncate result: %q", short)
	}

	long := truncateChars("ABCDEFGHIJKLMNOPQRSTUVWXYZ", 12)
	if long != "ABCDEFGHI..." {
		t.Fatalf("expected ellipsis truncate result, got %q", long)
	}
}

func TestBestLookupBookMatchRejectsSummaryAndAcceptsSeriesSuffix(t *testing.T) {
	list := []providers.LookupBook{
		{
			Title:         "Summary Of The Lost Metal By Brandon Sanderson",
			Author:        map[string]any{"authorName": "Dagg Forson"},
			ForeignBookId: "gr:spam",
		},
		{
			Title:            "The Lost Metal (Mistborn, #7)",
			Author:           map[string]any{"authorName": "Brandon Sanderson"},
			ForeignBookId:    "gr:work",
			ForeignEditionId: "gr:edition",
			Editions: []any{map[string]any{
				"title": "The Lost Metal (Mistborn, #7)", "foreignEditionId": "gr:edition",
			}},
		},
	}

	got, ok := bestLookupBookMatch(list, "The Lost Metal", []string{"Brandon Sanderson"})
	if !ok || got.ForeignBookId != "gr:work" {
		t.Fatalf("picked unsafe lookup result: ok=%v book=%+v", ok, got)
	}
	candidate := lookupBookCandidate(got)
	editions, _ := candidate["editions"].([]any)
	if len(editions) != 1 {
		t.Fatalf("hydrated editions were not preserved: %#v", candidate["editions"])
	}
	edition, _ := editions[0].(map[string]any)
	if edition["title"] != "The Lost Metal (Mistborn, #7)" {
		t.Fatalf("edition title was lost: %#v", edition)
	}
}

func TestBestLookupBookMatchHasNoArbitraryFirstFallback(t *testing.T) {
	list := []providers.LookupBook{{
		Title:  "Summary Of The Lost Metal By Brandon Sanderson",
		Author: map[string]any{"authorName": "Dagg Forson"},
	}}
	if got, ok := bestLookupBookMatch(list, "The Lost Metal", []string{"Brandon Sanderson"}); ok {
		t.Fatalf("unexpected unsafe match: %+v", got)
	}
}

func TestBestLookupBookMatchRejectsDramatizedOnlyResult(t *testing.T) {
	list := []providers.LookupBook{{
		Title:  "The Lost Metal (1 of 2) [Dramatized Adaptation]",
		Author: map[string]any{"authorName": "Brandon Sanderson"},
	}}
	if got, ok := bestLookupBookMatch(list, "The Lost Metal", []string{"Brandon Sanderson"}); ok {
		t.Fatalf("matched a different rendition: %+v", got)
	}
}

func TestBestLookupBookMatchUsesIdentifiersAndRejectsAmbiguity(t *testing.T) {
	list := []providers.LookupBook{
		{Title: "Shared Title", Author: map[string]any{"name": "Same Author"}, ForeignBookId: "work-1", Identifiers: []map[string]any{{"type": "isbn13", "value": "9780000000001"}}},
		{Title: "Shared Title", Author: map[string]any{"name": "Same Author"}, ForeignBookId: "work-2", Identifiers: []map[string]any{{"type": "isbn13", "value": "9780000000002"}}},
	}
	if got, ok := bestLookupBookMatch(list, "Shared Title", []string{"Same Author"}); ok {
		t.Fatalf("picked one of two ambiguous works: %+v", got)
	}
	got, ok := bestLookupBookMatchWithIdentifiers(list, "Shared Title", []string{"Same Author"}, "", "9780000000002", "", "", "")
	if !ok || got.ForeignBookId != "work-2" {
		t.Fatalf("identifier did not resolve the unique work: ok=%v got=%+v", ok, got)
	}
}

func TestSelectionPayloadUsesRequestedFormatAndValidatesIdentity(t *testing.T) {
	raw := []byte(`{"ebook":{"title":"Wrong Book","author":{"name":"Wrong Author"}},"audiobook":{"title":"The Lost Metal (Mistborn, #7)","author":{"authorName":"Brandon Sanderson"}}}`)
	candidate, normalized, ok := selectionPayloadForFormat(raw, "audiobook")
	if !ok || len(normalized) == 0 || !selectionPayloadMatchesRequest(candidate, "The Lost Metal", []string{"Brandon Sanderson"}) {
		t.Fatalf("did not select and validate audiobook payload: ok=%v candidate=%+v", ok, candidate)
	}
	if selectionPayloadMatchesRequest(candidate, "The Lost Metal", []string{"Dagg Forson"}) {
		t.Fatal("accepted a payload for the wrong requested author")
	}
}

func TestPickDiscoveryBooksEnforcesMinYearStrictly(t *testing.T) {
	books := []providers.BookItem{
		{Title: "Recent", Authors: []string{"A"}, FirstPublishYear: 2024},
		{Title: "Old", Authors: []string{"B"}, FirstPublishYear: 2010},
	}

	filtered := pickDiscoveryBooks(books, 2020, 10)
	if len(filtered) != 1 || filtered[0].Title != "Recent" {
		t.Fatalf("expected only recent books, got %+v", filtered)
	}

	strictEmpty := pickDiscoveryBooks([]providers.BookItem{{Title: "Too Old", Authors: []string{"C"}, FirstPublishYear: 2012}}, 2020, 10)
	if len(strictEmpty) != 0 {
		t.Fatalf("expected strict min-year filter to return empty, got %+v", strictEmpty)
	}
}
