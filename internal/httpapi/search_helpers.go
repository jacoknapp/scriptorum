package httpapi

import (
	"encoding/json"
	"regexp"
	"strings"

	"gitea.knapp/jacoknapp/scriptorum/internal/bookidentity"
	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

var (
	searchBookRangePattern      = regexp.MustCompile(`\b(?:books?|vol(?:ume)?s?)\.?\s*\d+\s*[-–]\s*\d+\b`)
	searchBookListPattern       = regexp.MustCompile(`\b(?:books?|vol(?:ume)?s?)\.?\s*\d+(?:\s*,\s*\d+)+(?:\s*(?:and|&)\s*\d+)?\b`)
	searchInOnePattern          = regexp.MustCompile(`\b\d+\s*(?:-| )?in(?:-| )one\b`)
	searchBookCollectionPattern = regexp.MustCompile(`\b\d+\s*(?:-| )?book collection\b`)
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
	return bookidentity.TitleScore(want, candidate)
}

// bestLookupBookMatch returns only a unique safe title-and-author identity;
// never fall back to the first text-search hit, which can be a summary or
// study guide. The identifier-aware variant may also accept a strong ID match.
func bestLookupBookMatch(list []providers.LookupBook, title string, authors []string) (providers.LookupBook, bool) {
	return bestLookupBookMatchWithIdentifiers(list, title, authors, "", "", "", "", "")
}

func lookupBookAuthorNames(book providers.LookupBook) []string {
	var names []string
	if name := lookupBookAuthorName(book); name != "" {
		names = append(names, name)
	}
	for _, author := range book.Authors {
		for _, key := range []string{"name", "authorName"} {
			if name, _ := author[key].(string); strings.TrimSpace(name) != "" {
				names = append(names, strings.TrimSpace(name))
			}
		}
	}
	return names
}

func lookupBookIdentifiers(book providers.LookupBook) map[string][]string {
	out := map[string][]string{
		"foreignBookId":    {strings.TrimSpace(book.ForeignBookId)},
		"foreignEditionId": {strings.TrimSpace(book.ForeignEditionId)},
	}
	add := func(kind, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			out[kind] = append(out[kind], value)
		}
	}
	for _, identifier := range book.Identifiers {
		typ, _ := identifier["type"].(string)
		value, _ := identifier["value"].(string)
		switch strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(typ), "_", ""), "-", "")) {
		case "isbn10":
			add("isbn10", value)
		case "isbn13":
			add("isbn13", value)
		case "asin":
			add("asin", value)
		}
		for _, key := range []string{"isbn10", "isbn13", "asin"} {
			if direct, _ := identifier[key].(string); direct != "" {
				add(key, direct)
			}
		}
	}
	for _, rawEdition := range book.Editions {
		edition, ok := rawEdition.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"foreignEditionId", "isbn10", "isbn13", "asin"} {
			if value, _ := edition[key].(string); value != "" {
				add(key, value)
			}
		}
	}
	return out
}

func identifierMatchCount(book providers.LookupBook, isbn10, isbn13, asin, foreignBookID, foreignEditionID string) int {
	want := map[string]string{
		"isbn10": strings.TrimSpace(isbn10), "isbn13": strings.TrimSpace(isbn13),
		"asin": strings.TrimSpace(asin), "foreignBookId": strings.TrimSpace(foreignBookID),
		"foreignEditionId": strings.TrimSpace(foreignEditionID),
	}
	got := lookupBookIdentifiers(book)
	matches := 0
	for kind, value := range want {
		if value == "" {
			continue
		}
		for _, candidate := range got[kind] {
			if strings.EqualFold(strings.TrimSpace(candidate), value) {
				matches++
				break
			}
		}
	}
	return matches
}

func sameLookupWork(left, right providers.LookupBook) bool {
	if left.ForeignBookId != "" && right.ForeignBookId != "" {
		return strings.EqualFold(strings.TrimSpace(left.ForeignBookId), strings.TrimSpace(right.ForeignBookId))
	}
	return false
}

// bestLookupBookMatchWithIdentifiers accepts a strong provider/edition/ISBN
// identity, or a unique safe title-and-author identity. Equal-confidence
// results for different works are ambiguous and are rejected.
func bestLookupBookMatchWithIdentifiers(list []providers.LookupBook, title string, authors []string, isbn10, isbn13, asin, foreignBookID, foreignEditionID string) (providers.LookupBook, bool) {
	bestScore := 0
	var best providers.LookupBook
	ambiguous := false
	for _, book := range list {
		identifierMatches := identifierMatchCount(book, isbn10, isbn13, asin, foreignBookID, foreignEditionID)
		titleScore := lookupTitleScore(title, book.Title)
		authorMatches := len(authors) == 0 || bookidentity.AuthorsMatch(authors, lookupBookAuthorNames(book))

		// A strong identifier is sufficient. Without one, require title and
		// every supplied author constraint; never infer identity from rank.
		if identifierMatches == 0 && (titleScore == 0 || !authorMatches) {
			continue
		}
		score := identifierMatches*100 + titleScore*10
		if authorMatches && len(authors) > 0 {
			score += 1
		}
		if score > bestScore {
			best, bestScore, ambiguous = book, score, false
		} else if score == bestScore && score > 0 {
			if sameLookupWork(best, book) {
				if len(book.Editions) > len(best.Editions) {
					best = book
				}
			} else {
				ambiguous = true
			}
		}
	}
	return best, bestScore > 0 && !ambiguous
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

func selectionPayloadForFormat(raw []byte, format string) (map[string]any, []byte, bool) {
	var candidate map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &candidate) != nil || candidate == nil {
		return nil, nil, false
	}
	if nested, ok := candidate[normalizeSyncKind(format)].(map[string]any); ok && nested != nil {
		candidate = nested
		normalized, err := json.Marshal(candidate)
		if err != nil {
			return nil, nil, false
		}
		return candidate, normalized, true
	}
	return candidate, raw, true
}

func selectionAuthorNames(candidate map[string]any) []string {
	var names []string
	appendName := func(author map[string]any) {
		for _, key := range []string{"name", "authorName"} {
			if name, _ := author[key].(string); strings.TrimSpace(name) != "" {
				names = append(names, strings.TrimSpace(name))
			}
		}
	}
	if author, ok := candidate["author"].(map[string]any); ok {
		appendName(author)
	}
	if authors, ok := candidate["authors"].([]any); ok {
		for _, rawAuthor := range authors {
			if author, ok := rawAuthor.(map[string]any); ok {
				appendName(author)
			}
		}
	}
	return names
}

// selectionPayloadMatchesRequest is the trust boundary for stored provider
// payloads. A payload may be stale, client-supplied, or left over from an old
// matching bug; it is never allowed to override the request's book identity.
func selectionPayloadMatchesRequest(candidate map[string]any, title string, authors []string) bool {
	candidateTitle, _ := candidate["title"].(string)
	if strings.TrimSpace(candidateTitle) == "" {
		return false
	}
	if strings.TrimSpace(title) != "" && lookupTitleScore(title, candidateTitle) == 0 {
		return false
	}
	if len(authors) > 0 && !bookidentity.AuthorsMatch(authors, selectionAuthorNames(candidate)) {
		return false
	}
	return true
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
