package httpapi

import (
	"context"
	"encoding/json"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/db"
)

func TestInputMapValue(t *testing.T) {
	in := map[string]any{
		"direct": map[string]any{"k": "v"},
		"json":   `{"title":"Book","year":2024}`,
		"bad":    "not-json",
		"blank":  "   ",
	}

	if got := inputMapValue(nil, "direct"); got != nil {
		t.Fatalf("expected nil for nil input, got %#v", got)
	}
	if got := inputMapValue(in, "missing"); got != nil {
		t.Fatalf("expected nil for missing key, got %#v", got)
	}

	direct := inputMapValue(in, "direct")
	if direct == nil || direct["k"] != "v" {
		t.Fatalf("expected direct map payload, got %#v", direct)
	}

	decoded := inputMapValue(in, "json")
	if decoded == nil || decoded["title"] != "Book" {
		t.Fatalf("expected decoded json map, got %#v", decoded)
	}

	if got := inputMapValue(in, "bad"); got != nil {
		t.Fatalf("expected nil for invalid json, got %#v", got)
	}
	if got := inputMapValue(in, "blank"); got != nil {
		t.Fatalf("expected nil for blank string, got %#v", got)
	}
}

func TestInputStringValue(t *testing.T) {
	in := map[string]any{"name": "  Alice  ", "count": 3}

	if got := inputStringValue(nil, "name"); got != "" {
		t.Fatalf("expected empty for nil input, got %q", got)
	}
	if got := inputStringValue(in, "missing"); got != "" {
		t.Fatalf("expected empty for missing key, got %q", got)
	}
	if got := inputStringValue(in, "count"); got != "" {
		t.Fatalf("expected empty for non-string key, got %q", got)
	}
	if got := inputStringValue(in, "name"); got != "Alice" {
		t.Fatalf("expected trimmed value, got %q", got)
	}
}

func TestInputStringSlice(t *testing.T) {
	if got := inputStringSlice(nil, "authors"); got != nil {
		t.Fatalf("expected nil for nil input, got %#v", got)
	}

	inStrings := map[string]any{"authors": []string{" Alice ", "", "Bob"}}
	gotStrings := inputStringSlice(inStrings, "authors")
	if len(gotStrings) != 2 || gotStrings[0] != "Alice" || gotStrings[1] != "Bob" {
		t.Fatalf("unexpected []string parsing result: %#v", gotStrings)
	}

	inAny := map[string]any{"authors": []any{" Carol ", 99, "", "Dan"}}
	gotAny := inputStringSlice(inAny, "authors")
	if len(gotAny) != 2 || gotAny[0] != "Carol" || gotAny[1] != "Dan" {
		t.Fatalf("unexpected []any parsing result: %#v", gotAny)
	}

	inSingle := map[string]any{"authors": "  Erin  "}
	gotSingle := inputStringSlice(inSingle, "authors")
	if len(gotSingle) != 1 || gotSingle[0] != "Erin" {
		t.Fatalf("unexpected single string parsing result: %#v", gotSingle)
	}

	inBlank := map[string]any{"authors": "   "}
	if got := inputStringSlice(inBlank, "authors"); got != nil {
		t.Fatalf("expected nil for blank string input, got %#v", got)
	}
}

func TestInputIntValue(t *testing.T) {
	in := map[string]any{
		"float": float64(7),
		"int":   int(8),
		"int64": int64(9),
		"str":   "10",
	}

	if got := inputIntValue(nil, "float"); got != 0 {
		t.Fatalf("expected 0 for nil input, got %d", got)
	}
	if got := inputIntValue(in, "missing"); got != 0 {
		t.Fatalf("expected 0 for missing key, got %d", got)
	}
	if got := inputIntValue(in, "float"); got != 7 {
		t.Fatalf("expected 7 from float64, got %d", got)
	}
	if got := inputIntValue(in, "int"); got != 8 {
		t.Fatalf("expected 8 from int, got %d", got)
	}
	if got := inputIntValue(in, "int64"); got != 9 {
		t.Fatalf("expected 9 from int64, got %d", got)
	}
	if got := inputIntValue(in, "str"); got != 0 {
		t.Fatalf("expected 0 for unsupported type, got %d", got)
	}
}

func TestRequestListMatchedBookKey(t *testing.T) {
	if got := requestListMatchedBookKey("ebook", 0); got != "" {
		t.Fatalf("expected empty key for invalid id, got %q", got)
	}
	if got := requestListMatchedBookKey("  AUDIO  ", 42); got != "ebook|42" {
		t.Fatalf("expected normalized key, got %q", got)
	}
}

func TestPayloadImageURLAndMapStringValue(t *testing.T) {
	if got := mapStringValue(nil, "cover"); got != "" {
		t.Fatalf("expected empty from nil payload, got %q", got)
	}

	payload := map[string]any{
		"cover": "  https://cover.example/image.jpg  ",
	}
	if got := mapStringValue(payload, "cover"); got != "https://cover.example/image.jpg" {
		t.Fatalf("expected trimmed cover value, got %q", got)
	}

	withImages := map[string]any{
		"images": []any{
			map[string]any{"coverType": "fanart", "url": "https://ignored.example/fanart.jpg"},
			map[string]any{"coverType": "cover", "remoteUrl": "https://covers.example/cover.jpg"},
		},
	}
	if got := payloadImageURL(withImages); got != "https://covers.example/cover.jpg" {
		t.Fatalf("expected cover image URL, got %q", got)
	}

	if got := payloadImageURL(map[string]any{"images": []any{"bad"}}); got != "" {
		t.Fatalf("expected empty for invalid image payload entries, got %q", got)
	}
}

func TestRequestListMatchedBooks(t *testing.T) {
	s := makeTestServer(t)

	bookPayload, err := json.Marshal(map[string]any{"images": []map[string]any{{"coverType": "cover", "url": "https://example/cover.jpg"}}})
	if err != nil {
		t.Fatalf("marshal readarr payload: %v", err)
	}

	err = s.db.ReplaceReadarrBooks(context.Background(), "ebook", []db.ReadarrBook{{
		SourceKind:  "ebook",
		ReadarrID:   101,
		Title:       "Book 101",
		ReadarrData: bookPayload,
	}})
	if err != nil {
		t.Fatalf("replace readarr books: %v", err)
	}

	matched := s.requestListMatchedBooks(context.Background(), []db.Request{
		{Format: "ebook", MatchedReadarrID: 101},
		{Format: "ebook", MatchedReadarrID: 0},
		{Format: "audiobook", MatchedReadarrID: 999},
	})

	book, ok := matched["ebook|101"]
	if !ok {
		t.Fatalf("expected ebook match for readarr id 101, got %#v", matched)
	}
	if book.ReadarrID != 101 {
		t.Fatalf("expected matched book id 101, got %d", book.ReadarrID)
	}
}
