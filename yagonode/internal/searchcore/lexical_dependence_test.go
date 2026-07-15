package searchcore

import "testing"

func TestLexicalDependenceLiftsCoherentStrictCoverage(t *testing.T) {
	results := []Result{
		{
			URL:   "distributed",
			Score: 1,
			FieldTermPositions: bodyPositions(map[string][]int{
				"alpha": {1},
				"beta":  {50},
				"gamma": {100},
			}),
		},
		{
			URL:   "coherent",
			Score: 0.98,
			FieldTermPositions: bodyPositions(map[string][]int{
				"alpha": {8},
				"beta":  {9},
				"gamma": {10},
			}),
		},
		{
			URL:   "partial",
			Score: 0.97,
			FieldTermPositions: bodyPositions(map[string][]int{
				"beta":  {2, 4, 6},
				"gamma": {3, 5, 7},
			}),
		},
	}
	got := rerankLexicalProximity(results, Request{Terms: []string{"alpha", "beta", "gamma"}})
	if got[0].URL != "coherent" || got[1].URL != "partial" ||
		got[2].URL != "distributed" {
		t.Fatalf("dependence order = %#v", urls(got))
	}
}

func TestOrderedPositionFractionDoesNotCrossFields(t *testing.T) {
	requirements := rerankQueryRequirements(Request{Terms: []string{"alpha", "beta"}})
	fields := map[string]map[string][]int{
		"title": {"alpha": {1}},
		"body":  {"beta": {2}},
	}
	if got := orderedPositionFraction(fields, requirements); got != 0 {
		t.Fatalf("cross-field ordered fraction = %v", got)
	}
	fields["body"]["alpha"] = []int{1}
	if got := orderedPositionFraction(fields, requirements); got != 1 {
		t.Fatalf("same-field ordered fraction = %v", got)
	}
	if got := orderedPositionFraction(
		fields,
		rerankQueryRequirements(Request{Terms: []string{"alpha"}}),
	); got != 0 {
		t.Fatalf("single-term ordered fraction = %v", got)
	}
}

func TestPositionsAtQueryDistanceHandlesEarlierAndMissingPositions(t *testing.T) {
	if !positionsAtQueryDistance([]int{2}, []int{1, 3}, 1) {
		t.Fatal("adjacency after an earlier right position was missed")
	}
	if positionsAtQueryDistance([]int{1, 5}, []int{3}, 1) {
		t.Fatal("non-adjacent positions matched")
	}
	if positionsAtQueryDistance(nil, []int{1}, 1) {
		t.Fatal("empty left positions matched")
	}
}

func TestOrderedTextFractionRequiresConsecutiveQueryTerms(t *testing.T) {
	requirements := rerankQueryRequirements(
		Request{Terms: []string{"alpha", "beta", "gamma"}},
	)
	if got := orderedTextFraction("alpha beta filler gamma", requirements); got != 0.5 {
		t.Fatalf("ordered text fraction = %v", got)
	}
	if got := orderedTextFraction("beta alpha", requirements); got != 0 {
		t.Fatalf("reversed ordered text fraction = %v", got)
	}
	if got := orderedTextFraction(
		"alpha",
		rerankQueryRequirements(Request{Terms: []string{"alpha"}}),
	); got != 0 {
		t.Fatalf("single-term ordered text fraction = %v", got)
	}
}

func TestOrderedDependencePreservesFilteredQueryDistance(t *testing.T) {
	requirements := rerankQueryRequirements(
		Request{Terms: []string{"alpha", "and", "beta"}},
	)
	if len(requirements) != 2 || requirements[0].ordinal != 0 ||
		requirements[1].ordinal != 2 {
		t.Fatalf("requirements = %#v", requirements)
	}
	spaced := map[string]map[string][]int{
		"body": {"alpha": {4}, "beta": {6}},
	}
	if got := orderedPositionFraction(spaced, requirements); got != 1 {
		t.Fatalf("spaced ordered fraction = %v", got)
	}
	spaced["body"]["beta"] = []int{5}
	if got := orderedPositionFraction(spaced, requirements); got != 0 {
		t.Fatalf("collapsed ordered fraction = %v", got)
	}
	if got := orderedTextFraction("alpha and beta", requirements); got != 1 {
		t.Fatalf("text ordered fraction = %v", got)
	}
	if got := orderedTextFraction("alpha beta", requirements); got != 0 {
		t.Fatalf("collapsed text ordered fraction = %v", got)
	}
}

func TestLexicalDependenceUsesMouseForGamingQueryDistance(t *testing.T) {
	results := []Result{
		{
			URL:   "collapsed",
			Score: 1,
			FieldTermPositions: bodyPositions(map[string][]int{
				"mouse":  {5},
				"gaming": {6},
			}),
		},
		{
			URL:   "query-distance",
			Score: 1,
			FieldTermPositions: bodyPositions(map[string][]int{
				"mouse":  {5},
				"gaming": {7},
			}),
		},
		{
			URL:   "reversed",
			Score: 1,
			FieldTermPositions: bodyPositions(map[string][]int{
				"mouse":  {7},
				"gaming": {5},
			}),
		},
	}
	got := rerankLexicalProximity(
		results,
		Request{Terms: []string{"mouse", "for", "gaming"}},
	)
	if got[0].URL != "query-distance" {
		t.Fatalf("dependence order = %#v", urls(got))
	}
	if ordered, known := got[0].Evidence.Value(SignalOrderedProximity); !known || ordered != 1 {
		t.Fatalf("ordered evidence = %v/%v", ordered, known)
	}
}

func TestResultAnalyzerRequirementsPreserveDroppedTermDistance(t *testing.T) {
	req := Request{Terms: []string{"can", "am", "rover"}}
	result := Result{
		EvidenceReady: true,
		FieldTermPositions: bodyPositions(map[string][]int{
			"can":   {1},
			"rover": {3},
		}),
	}
	requirements := rerankResultRequirements(req, result)
	if len(requirements) != 2 || requirements[0].term != "can" ||
		requirements[0].ordinal != 0 || requirements[1].term != "rover" ||
		requirements[1].ordinal != 2 {
		t.Fatalf("requirements = %#v", requirements)
	}
	if ordered := orderedPositionFraction(result.FieldTermPositions, requirements); ordered != 1 {
		t.Fatalf("ordered fraction = %v", ordered)
	}
}

func TestAnalyzerAlignedEvidenceDemotesHigherRetrievalRank(t *testing.T) {
	results := []Result{
		{
			URL:           "far",
			Score:         1,
			EvidenceReady: true,
			FieldTermPositions: bodyPositions(map[string][]int{
				"can":   {1},
				"rover": {30},
			}),
		},
		{
			URL:           "tight",
			Score:         0.99,
			EvidenceReady: true,
			FieldTermPositions: bodyPositions(map[string][]int{
				"can":   {1},
				"rover": {3},
			}),
		},
		{
			URL:           "reversed",
			Score:         0.98,
			EvidenceReady: true,
			FieldTermPositions: bodyPositions(map[string][]int{
				"can":   {3},
				"rover": {1},
			}),
		},
	}
	got := rerankLexicalProximity(results, Request{
		Terms: []string{"can", "am", "rover"},
	})
	if got[0].URL != "tight" {
		t.Fatalf("dependence order = %#v", urls(got))
	}
}

func TestOneAnalyzedOccurrenceDoesNotDoubleCountCoverage(t *testing.T) {
	coverage, proximity, positioned := lexicalComponentsFromPositions(
		bodyPositions(map[string][]int{
			"gaming": {4},
			"games":  nil,
		}),
		[]string{"gaming", "games"},
	)
	if !positioned || coverage != 0.5 || proximity != 0 {
		t.Fatalf("components = %v/%v/%v", coverage, proximity, positioned)
	}
}
