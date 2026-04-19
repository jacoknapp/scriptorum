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
	t := norm(b.Title)
	a := ""
	if len(b.Authors) > 0 {
		a = norm(b.Authors[0])
	}
	if t == "" {
		return ""
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
