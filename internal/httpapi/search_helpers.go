package httpapi

import (
	"strconv"
	"strings"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

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
	// If incoming is different and non-empty, prefer incoming
	if incoming == existing {
		return existing
	}
	// Append cache-busting param so browsers will refetch changed images.
	sep := "?"
	if strings.Contains(incoming, "?") {
		sep = "&"
	}
	incoming = incoming + sep + "v=" + strconv.FormatInt(time.Now().Unix(), 10)
	return incoming
}

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Join(strings.Fields(s), " ")
	return s
}
