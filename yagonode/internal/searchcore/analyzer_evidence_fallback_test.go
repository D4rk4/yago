package searchcore

import "testing"

func TestLexicalComponentsUseStructuralTextOnlyWithoutAnalyzerEvidence(t *testing.T) {
	terms := []string{"alpha", "beta"}
	structural := Result{Title: "alpha beta"}
	coverage, proximity := lexicalComponents(structural, terms)
	if coverage != 1 || proximity != 1 {
		t.Fatalf("structural coverage/proximity = %v/%v", coverage, proximity)
	}

	analyzed := structural
	analyzed.EvidenceReady = true
	analyzed.FieldTermPositions = map[string]map[string][]int{
		"title": {"alpha": nil, "beta": nil},
	}
	coverage, proximity = lexicalComponents(analyzed, terms)
	if coverage != 0 || proximity != 0 {
		t.Fatalf("analyzed coverage/proximity = %v/%v", coverage, proximity)
	}
	requirements := []rerankQueryRequirement{
		{term: "alpha", ordinal: 0},
		{term: "beta", ordinal: 1},
	}
	coverage, proximity, ordered, gap := lexicalDependenceComponents(
		analyzed,
		terms,
		requirements,
	)
	if coverage != 0 || proximity != 0 || ordered != 0 || gap != 0 {
		t.Fatalf(
			"analyzed dependence = %v/%v/%v/%v",
			coverage,
			proximity,
			ordered,
			gap,
		)
	}
}
