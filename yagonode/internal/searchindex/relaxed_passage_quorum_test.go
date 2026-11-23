package searchindex

import (
	"strings"
	"testing"
)

func TestRelaxedPassageQuorumKeepsShortQueryMinimum(t *testing.T) {
	for requirements := range 6 {
		got := relaxedPassageQuorum(requirements, 0)
		if want := relaxedRequirementMinimum(requirements); got != want {
			t.Fatalf("relaxedPassageQuorum(%d, 0) = %d, want %d", requirements, got, want)
		}
	}
	for _, tc := range []struct {
		requirements int
		identifiers  int
		want         int
	}{
		{requirements: 8, identifiers: 0, want: 3},
		{requirements: 12, identifiers: 1, want: 3},
		{requirements: 10, identifiers: 5, want: 5},
		{requirements: 10, identifiers: 8, want: 6},
	} {
		if got := relaxedPassageQuorum(tc.requirements, tc.identifiers); got != tc.want {
			t.Fatalf(
				"relaxedPassageQuorum(%d, %d) = %d, want %d",
				tc.requirements, tc.identifiers, got, tc.want,
			)
		}
	}
}

func TestLongRelaxedQueryAdmitsThreeTermPassage(t *testing.T) {
	terms := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}
	req := SearchRequest{Query: strings.Join(terms, " "), Terms: terms, Relaxed: true}
	filler := strings.Repeat("filler ", 40)
	threeNearby := "alpha beta gamma " + filler + "delta " + filler + "epsilon"
	if !relaxedCandidateFound(t, req, threeNearby, SearchResult{RelaxedRank: 1}) {
		t.Fatal("three nearby terms of a long relaxed query were rejected")
	}
	twoNearby := "alpha beta " + filler + "gamma " + filler + "delta " + filler + "epsilon"
	if relaxedCandidateFound(t, req, twoNearby, SearchResult{RelaxedRank: 1}) {
		t.Fatal("two nearby terms of a long relaxed query were admitted")
	}
}

func TestLongRelaxedQueryKeepsEveryIdentifierInPassage(t *testing.T) {
	terms := []string{"ZX900Q", "QK220T", "MV310R", "LP450S", "alpha", "beta", "gamma", "delta"}
	req := SearchRequest{Query: strings.Join(terms, " "), Terms: terms, Relaxed: true}
	filler := strings.Repeat("filler ", 40)
	if !relaxedCandidateFound(
		t,
		req,
		"ZX900Q QK220T MV310R LP450S "+filler+"alpha",
		SearchResult{RelaxedRank: 1},
	) {
		t.Fatal("a passage holding every identifier was rejected")
	}
	if relaxedCandidateFound(
		t,
		req,
		"ZX900Q QK220T MV310R alpha "+filler+"LP450S",
		SearchResult{RelaxedRank: 1},
	) {
		t.Fatal("a passage missing one identifier was admitted")
	}
}
