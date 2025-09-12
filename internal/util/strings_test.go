package util

import "testing"

func TestFirstNonEmpty(t *testing.T) {
	if got := FirstNonEmpty("", " ", "a", "b"); got != "a" {
		t.Fatalf("want a got %q", got)
	}
	if got := FirstNonEmpty("", "  "); got != "" {
		t.Fatalf("want empty got %q", got)
	}
}

func TestToTitleCase(t *testing.T) {
	cases := map[string]string{
		"john doe":  "John Doe",
		"Mc-donald": "Mc-Donald",
		"o'reilly":  "O'Reilly",
		"  ":        "",
	}
	for in, want := range cases {
		if got := ToTitleCase(in); got != want {
			t.Fatalf("%q => %q (want %q)", in, got, want)
		}
	}
}
