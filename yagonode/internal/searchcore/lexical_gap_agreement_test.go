package searchcore

import (
	"math"
	"testing"
)

func TestPositionGapEvidence(t *testing.T) {
	cases := []struct {
		name      string
		left      []int
		right     []int
		expected  int
		exact     bool
		agreement float64
	}{
		{
			name: "exact after earlier right", left: []int{2}, right: []int{1, 3},
			expected: 1, exact: true, agreement: 1,
		},
		{name: "near forward", left: []int{1}, right: []int{4}, expected: 2, agreement: 0.5},
		{name: "reverse", left: []int{4}, right: []int{1}, expected: 2},
		{name: "scattered", left: []int{1}, right: []int{11}, expected: 2, agreement: 1.0 / 9.0},
		{
			name: "repeated best", left: []int{1, 100}, right: []int{4, 102},
			expected: 2, exact: true, agreement: 1,
		},
		{name: "invalid expected gap", left: []int{1}, right: []int{2}, expected: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exact, agreement := positionGapEvidence(tc.left, tc.right, tc.expected)
			if exact != tc.exact || math.Abs(agreement-tc.agreement) > 1e-12 {
				t.Fatalf("evidence = %v/%v, want %v/%v", exact, agreement, tc.exact, tc.agreement)
			}
			if agreement < 0 || agreement > 1 {
				t.Fatalf("agreement out of bounds: %v", agreement)
			}
		})
	}
}

func TestPositionGapEvidenceMatchesAllOrderedPairs(t *testing.T) {
	for leftMask := 1; leftMask < 1<<5; leftMask++ {
		for rightMask := 1; rightMask < 1<<5; rightMask++ {
			left := positionsFromMask(leftMask)
			right := positionsFromMask(rightMask)
			for expected := 1; expected <= 4; expected++ {
				wantExact, wantAgreement := brutePositionGapEvidence(left, right, expected)
				exact, agreement := positionGapEvidence(left, right, expected)
				if exact != wantExact || math.Abs(agreement-wantAgreement) > 1e-12 {
					t.Fatalf(
						"left=%v right=%v expected=%d evidence=%v/%v want=%v/%v",
						left,
						right,
						expected,
						exact,
						agreement,
						wantExact,
						wantAgreement,
					)
				}
			}
		}
	}
}

func positionsFromMask(mask int) []int {
	positions := make([]int, 0, 5)
	for position := 1; position <= 5; position++ {
		if mask&(1<<(position-1)) != 0 {
			positions = append(positions, position)
		}
	}

	return positions
}

func brutePositionGapEvidence(left []int, right []int, expected int) (bool, float64) {
	bestDeviation := -1
	for _, leftPosition := range left {
		for _, rightPosition := range right {
			difference := rightPosition - leftPosition
			if difference <= 0 {
				continue
			}
			deviation := difference - expected
			if deviation < 0 {
				deviation = -deviation
			}
			if deviation == 0 {
				return true, 1
			}
			if bestDeviation < 0 || deviation < bestDeviation {
				bestDeviation = deviation
			}
		}
	}
	if bestDeviation < 0 {
		return false, 0
	}

	return false, 1 / (1 + float64(bestDeviation))
}

func TestOrderedPositionEvidenceAveragesPairsWithinFields(t *testing.T) {
	requirements := rerankQueryRequirements(
		Request{Terms: []string{"alpha", "and", "beta", "gamma"}},
	)
	fields := map[string]map[string][]int{
		"title": {"alpha": {1}},
		"body":  {"alpha": {1}, "beta": {4}, "gamma": {5}},
	}
	exact, agreement := orderedPositionEvidence(fields, requirements)
	if exact != 0.5 || agreement != 0.75 {
		t.Fatalf("position evidence = %v/%v", exact, agreement)
	}
	delete(fields["body"], "alpha")
	exact, agreement = orderedPositionEvidence(fields, requirements)
	if exact != 0.5 || agreement != 0.5 {
		t.Fatalf("cross-field evidence = %v/%v", exact, agreement)
	}
}

func TestOrderedPositionEvidenceRequiresExactSurfaceKeys(t *testing.T) {
	requirements := rerankQueryRequirements(
		Request{Terms: []string{"rare-identifier", "manual"}},
	)
	exact, agreement := orderedPositionEvidence(
		bodyPositions(map[string][]int{"rare": {1}, "manual": {2}}),
		requirements,
	)
	if exact != 0 || agreement != 0 {
		t.Fatalf("analyzer variant received exact evidence: %v/%v", exact, agreement)
	}
	exact, agreement = orderedPositionEvidence(
		bodyPositions(map[string][]int{"rare-identifier": {1}, "manual": {2}}),
		requirements,
	)
	if exact != 1 || agreement != 1 {
		t.Fatalf("exact identifier evidence = %v/%v", exact, agreement)
	}
}

func TestRepeatedPositionsDoNotAccumulateGapAgreement(t *testing.T) {
	_, single := positionGapEvidence([]int{1}, []int{4}, 2)
	_, repeated := positionGapEvidence([]int{1, 20}, []int{4, 23}, 2)
	if single != 0.5 || repeated != single {
		t.Fatalf("single/repeated agreement = %v/%v", single, repeated)
	}
}

func TestLexicalGapAgreementPrefersOriginalGapDirection(t *testing.T) {
	results := []Result{
		positionedResult("reverse", 4, 1),
		positionedResult("near-forward", 1, 4),
		positionedResult("scattered", 1, 20),
		positionedResult("original-gap", 1, 3),
	}
	got := rerankLexicalProximity(
		results,
		Request{Terms: []string{"alpha", "and", "beta"}},
	)
	if order := urls(got); order[0] != "original-gap" || order[1] != "near-forward" ||
		order[2] != "reverse" || order[3] != "scattered" {
		t.Fatalf("gap order = %v", order)
	}
	if ordered, known := got[1].Evidence.Value(SignalOrderedProximity); !known || ordered != 0 {
		t.Fatalf("smooth gap changed binary evidence = %v/%v", ordered, known)
	}
}

func positionedResult(identifier string, alphaPosition, betaPosition int) Result {
	return Result{
		URL:   identifier,
		Score: 1,
		FieldTermPositions: bodyPositions(map[string][]int{
			"alpha": {alphaPosition},
			"beta":  {betaPosition},
		}),
	}
}

func TestLexicalGapAgreementFallsBackToVisibleText(t *testing.T) {
	results := []Result{
		{URL: "reverse", Score: 1, Title: "beta x x alpha"},
		{URL: "near-forward", Score: 1, Title: "alpha x x beta"},
		{URL: "unrelated", Score: 1, Title: "other text"},
	}
	got := rerankLexicalProximity(
		results,
		Request{Terms: []string{"alpha", "and", "beta"}},
	)
	if order := urls(got); order[0] != "near-forward" || order[1] != "reverse" {
		t.Fatalf("fallback gap order = %v", order)
	}
}
