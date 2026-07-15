package searchcore

import "testing"

func TestTokenMatchesTermUsesWholeTokenMorphology(t *testing.T) {
	for _, test := range []struct {
		observed string
		term     string
		want     bool
	}{
		{observed: "API", term: "api", want: true},
		{observed: "черногории", term: "черногория", want: true},
		{observed: "games", term: "game", want: true},
		{observed: "capital", term: "api", want: false},
		{observed: "rapid", term: "api", want: false},
		{observed: "", term: "api", want: false},
		{observed: "api", term: " ", want: false},
	} {
		if got := TokenMatchesTerm(test.observed, test.term); got != test.want {
			t.Errorf(
				"TokenMatchesTerm(%q, %q) = %v, want %v",
				test.observed,
				test.term,
				got,
				test.want,
			)
		}
	}
}
