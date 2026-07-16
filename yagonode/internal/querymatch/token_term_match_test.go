package querymatch

import "testing"

func TestTokenMatchesTerm(t *testing.T) {
	for _, test := range []struct {
		surface   string
		queryTerm string
		want      bool
	}{
		{surface: " API ", queryTerm: "api", want: true},
		{surface: "полномочий", queryTerm: "полномочия", want: true},
		{surface: "Путину", queryTerm: "Путина", want: true},
		{surface: "crawling", queryTerm: "crawl", want: true},
		{surface: "javascript", queryTerm: "java", want: false},
		{surface: "spaceship", queryTerm: "space", want: false},
		{surface: "capital", queryTerm: "api", want: false},
		{surface: "", queryTerm: "api", want: false},
		{surface: "api", queryTerm: " ", want: false},
	} {
		if got := TokenMatchesTerm(test.surface, test.queryTerm); got != test.want {
			t.Errorf(
				"TokenMatchesTerm(%q, %q) = %v, want %v",
				test.surface,
				test.queryTerm,
				got,
				test.want,
			)
		}
	}
}

func TestSharedPrefixRunes(t *testing.T) {
	for _, test := range []struct {
		left  string
		right string
		want  int
	}{
		{left: "полномочия", right: "полномочий", want: 9},
		{left: "abc", right: "abd", want: 2},
		{left: "аб", right: "абв", want: 2},
		{left: "", right: "abc", want: 0},
	} {
		if got := sharedPrefixRunes(test.left, test.right); got != test.want {
			t.Errorf(
				"sharedPrefixRunes(%q, %q) = %d, want %d",
				test.left,
				test.right,
				got,
				test.want,
			)
		}
	}
}
