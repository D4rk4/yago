package searchindex

import "testing"

func TestUnorderedProximityNeedsAPair(t *testing.T) {
	if got := unorderedProximity("alpha beta", []string{"alpha"}); got != 0 {
		t.Errorf("single term proximity = %v, want 0", got)
	}
	// Duplicates collapse, so a query of one distinct word carries no pair.
	if got := unorderedProximity("alpha alpha", []string{"alpha", "alpha"}); got != 0 {
		t.Errorf("duplicate-only proximity = %v, want 0", got)
	}
}

func TestUnorderedProximityRewardsCoOccurrence(t *testing.T) {
	// Adjacent within the window (a blank query term is skipped) scores the pair.
	if got := unorderedProximity("the alpha beta gamma", []string{"alpha", "", "beta"}); got != 1 {
		t.Errorf("adjacent proximity = %v, want 1", got)
	}
	// Order does not matter: the words co-occur regardless of which comes first.
	if got := unorderedProximity("beta filler alpha", []string{"alpha", "beta"}); got != 1 {
		t.Errorf("reversed proximity = %v, want 1", got)
	}
	// A repeated word contributes a second position; the closer one wins.
	repeated := "alpha x x x x x x x x x x x x beta x alpha"
	if got := unorderedProximity(repeated, []string{"alpha", "beta"}); got != 1 {
		t.Errorf("repeated-word proximity = %v, want 1 (second alpha is near beta)", got)
	}
}

func TestUnorderedProximityFallsOffOutsideWindow(t *testing.T) {
	far := "alpha x x x x x x x x x beta"
	if got := unorderedProximity(far, []string{"alpha", "beta"}); got != 0 {
		t.Errorf("far-apart proximity = %v, want 0", got)
	}
	// Same words, query reversed: the earlier text word is now the pair's second
	// element, exercising the mirror branch of the closest-pair walk.
	if got := unorderedProximity(far, []string{"beta", "alpha"}); got != 0 {
		t.Errorf("far-apart reversed proximity = %v, want 0", got)
	}
}

func TestUnorderedProximityIsFractionOfPairs(t *testing.T) {
	// alpha-beta are adjacent (satisfied) but beta-gamma straddle the window
	// (not), so one of the two adjacent pairs counts.
	text := "alpha beta x x x x x x x x gamma"
	if got := unorderedProximity(text, []string{"alpha", "beta", "gamma"}); got != 0.5 {
		t.Errorf("mixed proximity = %v, want 0.5", got)
	}
}
