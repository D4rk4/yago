package searchcore

import (
	"slices"
	"testing"
)

func TestYaCyQueryGoalIncludeStringFixtures(t *testing.T) {
	cases := []struct {
		query   string
		include []string
	}{
		{"O'Reily's book", []string{"o'reily's", "book"}},
		{`"O'Reily's book"`, []string{"o'reily's book"}},
		{`"O'Reily's" +book`, []string{"o'reily's", "book"}},
		{"Umphrey's + McGee", []string{"umphrey's", " mcgee"}},
		{"'The Book' library", []string{"the book", "library"}},
		{"book -", []string{"book"}},
	}
	for _, c := range cases {
		got := ParseTextQuery(c.query)
		if !slices.Equal(got.IncludePhrases, c.include) {
			t.Errorf(
				"IncludePhrases(%q) = %q, want %q",
				c.query,
				got.IncludePhrases,
				c.include,
			)
		}
	}
}

func TestYaCyQueryGoalWordFixtures(t *testing.T) {
	got := ParseTextQuery(`"O'Reily's book" -"the library"`)

	if want := []string{"o'reily's", "book"}; !slices.Equal(got.Terms, want) {
		t.Errorf("Terms = %q, want %q", got.Terms, want)
	}
	if want := []string{"the", "library"}; !slices.Equal(got.ExcludedTerms, want) {
		t.Errorf("ExcludedTerms = %q, want %q", got.ExcludedTerms, want)
	}
	if want := []string{"the library"}; !slices.Equal(got.ExcludePhrases, want) {
		t.Errorf("ExcludePhrases = %q, want %q", got.ExcludePhrases, want)
	}
}

func TestYaCyQueryGoalPrunesSingleCharacterTokens(t *testing.T) {
	got := ParseTextQuery("a book")

	if want := []string{"book"}; !slices.Equal(got.IncludePhrases, want) {
		t.Errorf("IncludePhrases = %q, want %q", got.IncludePhrases, want)
	}
}
