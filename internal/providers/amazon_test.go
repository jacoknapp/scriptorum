package providers

import "testing"

func TestExtractASINFromInput(t *testing.T) {
	cases := []struct{ in, want string }{
		{"B08N5WRWNW", "B08N5WRWNW"},
		{"https://www.amazon.com/dp/B08N5WRWNW", "B08N5WRWNW"},
		{"https://www.amazon.com/gp/product/B08N5WRWNW?th=1", "B08N5WRWNW"},
		{"not-an-asin", ""},
	}
	for _, c := range cases {
		got := ExtractASINFromInput(c.in)
		if got != c.want { t.Fatalf("in=%q got=%q want=%q", c.in, got, c.want) }
	}
}
