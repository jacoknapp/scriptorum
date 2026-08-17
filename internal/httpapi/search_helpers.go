package httpapi

import (
	"regexp"
	"strings"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

var (
	searchBookRangePattern      = regexp.MustCompile(`\b(?:books?|vol(?:ume)?s?)\.?\s*\d+\s*[-–]\s*\d+\b`)
	searchBookListPattern       = regexp.MustCompile(`\b(?:books?|vol(?:ume)?s?)\.?\s*\d+(?:\s*,\s*\d+)+(?:\s*(?:and|&)\s*\d+)?\b`)
	searchInOnePattern          = regexp.MustCompile(`\b\d+\s*(?:-| )?in(?:-| )one\b`)
	searchBookCollectionPattern = regexp.MustCompile(`\b\d+\s*(?:-| )?book collection\b`)
	lookupTitleSuffixPattern    = regexp.MustCompile(`\s*(\([^)]*\)|\[[^]]*\])\s*$`)
)

var blockedSearchTitleSnippets = []string{
	"anthology",
	"omnibus",
	"boxed set",
	"boxed sets",
	"box set",
	"boxset",
	"bundle",
	"companion guide",
	"study guide",
	"teacher's guide",
	"teachers guide",
	"workbook",
	"planner",
	"calendar",
	"coloring book",
	"poster book",
	"short story collection",
	"collected stories",
	"complete works",
	"complete series",
	"collection set",
	"activity book",
	"guided journal",
	"prompt journal",
	"crossword",
	"word search",
	"notebook",
	"2-in-1",
	"3-in-1",
	"4-in-1",
	"two-in-one",
	"three-in-one",
	"four-in-one",
	"all-in-one",
}

func dedupeKey(b providers.BookItem) string {
	if s := strings.TrimSpace(strings.ToUpper(b.ASIN)); s != "" {
		return "ASIN:" + s
	}
	if s := strings.TrimSpace(strings.ToUpper(b.ISBN13)); s != "" {
		return "ISBN13:" + s
	}
	return titleAuthorKey(b)
}

// titleAuthorKey returns the title/author dedupe key for a book, or "" when
// the title is empty. It is used to match items across sources that carry
// different identifiers (e.g. Readarr lookups without ISBNs vs OpenLibrary).
func titleAuthorKey(b providers.BookItem) string {
	t := norm(b.Title)
	if t == "" {
		return ""
	}
	a := ""
	if len(b.Authors) > 0 {
		a = norm(b.Authors[0])
	}
	return "TA:" + t + ":" + a
}

// mergeProviderPayloads returns a single provider payload string by preferring
// the ebook rendition, then the audiobook rendition, then an empty string.
// If both exist and are different, prefer the ebook payload but include the
// audiobook payload inside a wrapper object for server-side convenience.
func mergeProviderPayloads(ebook, audio string) string {
	ebook = strings.TrimSpace(ebook)
	audio = strings.TrimSpace(audio)
	if ebook == "" && audio == "" {
		return ""
	}
	if ebook != "" && audio == "" {
		return ebook
	}
	if ebook == "" && audio != "" {
		return audio
	}
	// Both present and different: include both under an outer object so client
	// or server can pick the correct rendition at create-time.
	if ebook == audio {
		return ebook
	}
	// Build a minimal wrapper JSON object: { "ebook": <ebook>, "audiobook": <audio> }
	return `{"ebook":` + ebook + `,"audiobook":` + audio + `}`
}

// mergeCover chooses the incoming cover when non-empty, otherwise keeps the
// existing cover. If incoming equals existing, returns existing.
func mergeCover(existing, incoming string) string {
	existing = strings.TrimSpace(existing)
	incoming = strings.TrimSpace(incoming)
	if incoming == "" {
		return existing
	}
	if incoming == existing {
		return existing
	}
	return incoming
}

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func lookupBookAuthor(book providers.LookupBook) map[string]any {
	if book.Author != nil {
		return book.Author
	}
	if len(book.Authors) > 0 {
		return book.Authors[0]
	}
	if book.AuthorId > 0 {
		return map[string]any{"id": book.AuthorId}
	}
	if book.AuthorTitle != "" {
		return map[string]any{"name": parseAuthorNameFromTitle(book.AuthorTitle)}
	}
	return nil
}

func lookupBookAuthorName(book providers.LookupBook) string {
	author := lookupBookAuthor(book)
	if author == nil {
		return ""
	}
	for _, key := range []string{"name", "authorName"} {
		if value, ok := author[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// lookupTitleScore recognizes the common metadata-server shape where a work's
// canonical title has a trailing series marker (for example, "The Lost Metal
// (Mistborn, #7)"). It deliberately does not use arbitrary substring matching:
// that would make summaries, study guides, and collections look like the book.
func lookupTitleScore(want, candidate string) int {
	want = norm(want)
	candidate = norm(candidate)
	if want == "" || candidate == "" {
		return 0
	}
	if want == candidate {
		return 3
	}
	for previous := ""; candidate != previous; {
		previous = candidate
		candidate = norm(lookupTitleSuffixPattern.ReplaceAllString(candidate, ""))
		if candidate == want {
			return 2
		}
	}
	return 0
}

// bestLookupBookMatch returns only a safe identity match. When the request has
// an author, the backend result must have the same author; never fall back to
// the first text-search hit, which can be a summary or study guide.
func bestLookupBookMatch(list []providers.LookupBook, title string, authors []string) (providers.LookupBook, bool) {
	wantAuthor := ""
	if len(authors) > 0 {
		wantAuthor = norm(authors[0])
	}
	bestScore := 0
	var best providers.LookupBook
	for _, book := range list {
		if wantAuthor != "" && norm(lookupBookAuthorName(book)) != wantAuthor {
			continue
		}
		score := lookupTitleScore(title, book.Title)
		if score > bestScore {
			best, bestScore = book, score
		}
	}
	return best, bestScore > 0
}

// lookupBookCandidate keeps the metadata server's complete edition objects.
// Chaptarr rejects ID-only edition stubs, but it accepts hydrated editions and
// needs them when it cannot hydrate a work from foreignBookId by itself.
func lookupBookCandidate(book providers.LookupBook) map[string]any {
	editions := book.Editions
	if editions == nil {
		editions = []any{}
	}
	return map[string]any{
		"title":             book.Title,
		"titleSlug":         book.TitleSlug,
		"author":            lookupBookAuthor(book),
		"editions":          editions,
		"foreignBookId":     book.ForeignBookId,
		"foreignEditionId":  book.ForeignEditionId,
		"monitored":         true,
		"metadataProfileId": 1,
	}
}

func isRenderableSearchBook(title string, extras ...string) bool {
	parts := []string{strings.TrimSpace(title)}
	for _, extra := range extras {
		if trimmed := strings.TrimSpace(extra); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	joined := norm(strings.Join(parts, " "))
	if joined == "" {
		return false
	}

	for _, snippet := range blockedSearchTitleSnippets {
		if strings.Contains(joined, snippet) {
			return false
		}
	}
	if searchBookRangePattern.MatchString(joined) ||
		searchBookListPattern.MatchString(joined) ||
		searchInOnePattern.MatchString(joined) ||
		searchBookCollectionPattern.MatchString(joined) {
		return false
	}
	return true
}
