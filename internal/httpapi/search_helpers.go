package httpapi

import (
	"strings"

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

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Join(strings.Fields(s), " ")
	return s
}
