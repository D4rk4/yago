package searchindex

import "testing"

func TestOrderedProximityMeasuresAdjacentQueryPairs(t *testing.T) {
	if got := orderedProximity(
		"alpha beta gap gamma",
		[]string{"alpha", "beta", "gamma"},
	); got != 0.5 {
		t.Fatalf("ordered proximity = %v", got)
	}
	if got := orderedProximity("beta alpha", []string{"alpha", "beta"}); got != 0 {
		t.Fatalf("reverse proximity = %v", got)
	}
	if got := orderedProximity("alpha beta", []string{"alpha"}); got != 0 {
		t.Fatalf("single-term proximity = %v", got)
	}
}

func TestOrderedAdjacentCoversPositionRelationships(t *testing.T) {
	cases := []struct {
		left  []int
		right []int
		want  bool
	}{
		{[]int{1}, []int{2}, true},
		{[]int{4}, []int{1, 5}, true},
		{[]int{1, 7}, []int{5, 9}, false},
		{nil, []int{1}, false},
	}
	for _, test := range cases {
		if got := orderedAdjacent(test.left, test.right); got != test.want {
			t.Errorf("orderedAdjacent(%v, %v) = %v", test.left, test.right, got)
		}
	}
}
