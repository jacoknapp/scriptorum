package bookidentity

import "testing"

func TestTitleScoreAllowsSeriesSuffixButRejectsRenditions(t *testing.T) {
	if got := TitleScore("The Lost Metal", "The Lost Metal (Mistborn, #7)"); got != 2 {
		t.Fatalf("series title score=%d", got)
	}
	for _, title := range []string{
		"The Lost Metal (1 of 2) [Dramatized Adaptation]",
		"Summary of The Lost Metal",
		"The Lost Metal Workbook",
	} {
		if got := TitleScore("The Lost Metal", title); got != 0 {
			t.Fatalf("unsafe title %q scored %d", title, got)
		}
	}
}

func TestAuthorNamesMatchPresentationOnly(t *testing.T) {
	for _, candidate := range []string{"Tolkien, J. R. R.", "J R R Tolkien"} {
		if !AuthorNamesMatch("J. R. R. Tolkien", candidate) {
			t.Fatalf("expected presentation match for %q", candidate)
		}
	}
	if AuthorNamesMatch("Brandon Sanderson", "Dagg Forson") {
		t.Fatal("matched different authors")
	}
}
