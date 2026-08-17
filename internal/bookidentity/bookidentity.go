// Package bookidentity contains the conservative normalization rules shared by
// provider lookup, request hydration, and existing-library reconciliation.
package bookidentity

import (
	"regexp"
	"strings"
	"unicode"
)

var trailingTitleGroup = regexp.MustCompile(`\s*(\(([^()]*)\)|\[([^\[\]]*)\])\s*$`)

// NormalizeText removes punctuation differences without doing fuzzy or
// substring matching. It is suitable for comparing names and titles, but not
// for deciding on its own that two books are the same work.
func NormalizeText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			return r
		}
		return ' '
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

// CanonicalTitle strips only metadata-style series suffixes. A comma or '#'
// is required so rendition markers such as "(1 of 2)" and "[Dramatized
// Adaptation]" remain part of the title and cannot collapse onto the novel.
func CanonicalTitle(value string) string {
	value = strings.TrimSpace(value)
	for {
		match := trailingTitleGroup.FindStringSubmatch(value)
		if len(match) == 0 {
			break
		}
		contents := match[2]
		if contents == "" {
			contents = match[3]
		}
		if !strings.Contains(contents, ",") && !strings.Contains(contents, "#") {
			break
		}
		value = strings.TrimSpace(value[:len(value)-len(match[0])])
	}
	return NormalizeText(value)
}

// TitleScore returns 3 for an exact normalized title and 2 for a title that
// differs only by a safe series suffix. Zero means the titles are not a safe
// identity match.
func TitleScore(want, candidate string) int {
	wantExact := NormalizeText(want)
	candidateExact := NormalizeText(candidate)
	if wantExact == "" || candidateExact == "" {
		return 0
	}
	if wantExact == candidateExact {
		return 3
	}
	if CanonicalTitle(want) == CanonicalTitle(candidate) {
		return 2
	}
	return 0
}

func authorKeys(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	keys := []string{NormalizeText(value)}
	if parts := strings.Split(value, ","); len(parts) == 2 {
		reordered := NormalizeText(parts[1] + " " + parts[0])
		if reordered != "" && reordered != keys[0] {
			keys = append(keys, reordered)
		}
	}
	return keys
}

// AuthorNamesMatch accepts punctuation and "last, first" presentation
// differences, but deliberately does not guess at pen names or fuzzy aliases.
func AuthorNamesMatch(want, candidate string) bool {
	for _, left := range authorKeys(want) {
		for _, right := range authorKeys(candidate) {
			if left != "" && left == right {
				return true
			}
		}
	}
	return false
}

// AuthorsMatch reports whether any requested author matches any candidate
// author. Multi-author works therefore do not depend on array ordering.
func AuthorsMatch(want, candidates []string) bool {
	for _, left := range want {
		for _, right := range candidates {
			if AuthorNamesMatch(left, right) {
				return true
			}
		}
	}
	return false
}
