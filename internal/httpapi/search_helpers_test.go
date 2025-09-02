package httpapi

import (
	"testing"

	"gitea.knapp/jacoknapp/scriptoruminternal/providers"
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
