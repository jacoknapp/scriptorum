package util

import "strings"

// FirstNonEmpty returns the first non-empty string (after trimming).
func FirstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// ToTitleCase provides a simple ASCII title-casing without external deps.
func ToTitleCase(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return s
	}
	var out []rune
	capNext := true
	for _, r := range s {
		if capNext && r >= 'a' && r <= 'z' {
			out = append(out, r-('a'-'A'))
			capNext = false
			continue
		}
		out = append(out, r)
		if r == ' ' || r == '-' || r == '\'' {
			capNext = true
		} else {
			capNext = false
		}
	}
	return string(out)
}

// ParseAuthorNameFromTitle extracts an author name from a string like
// "lastname, firstname Rest Of Title" into "Firstname Lastname".
// Falls back to title-casing the whole string when not in that format.
func ParseAuthorNameFromTitle(title string) string {
	parts := strings.Split(strings.TrimSpace(title), " ")
	if len(parts) >= 2 {
		last := strings.Trim(parts[0], ",")
		first := parts[1]
		return ToTitleCase(first + " " + last)
	}
	return ToTitleCase(strings.TrimSpace(title))
}
