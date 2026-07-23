package searchcore

import "testing"

func TestPseudoRelevanceActivationLimit(t *testing.T) {
	cases := []struct {
		limit int
		want  int
	}{
		{limit: 0, want: DefaultPublicLimit},
		{limit: 5, want: 5},
		{limit: 100, want: prfActivateBelow},
	}
	for _, test := range cases {
		if got := pseudoRelevanceActivationLimit(test.limit); got != test.want {
			t.Fatalf("pseudoRelevanceActivationLimit(%d) = %d, want %d",
				test.limit, got, test.want)
		}
	}
}
