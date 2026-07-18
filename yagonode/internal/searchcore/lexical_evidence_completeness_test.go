package searchcore

import "testing"

func TestAnalyzerEvidenceKeepsAbsentRequirementsInCoverageDenominator(t *testing.T) {
	req := Request{Terms: []string{"alpha", "beta", "gamma"}}
	result := Result{
		EvidenceReady:               true,
		EvidenceRequirementOrdinals: []int{0, 1, 2},
		FieldTermPositions: bodyPositions(map[string][]int{
			"alpha": {1},
		}),
	}
	requirements := rerankResultRequirements(req, result)
	terms := rerankRequirementTerms(requirements)
	coverage, _, _, _ := lexicalDependenceComponents(result, terms, requirements)
	if len(requirements) != 3 || coverage != 1.0/3.0 {
		t.Fatalf("requirements=%#v coverage=%v", requirements, coverage)
	}
}

func TestMalformedEvidenceCompletenessFallsBackToFullRequirements(t *testing.T) {
	req := Request{Terms: []string{"alpha", "beta", "gamma"}}
	result := Result{
		EvidenceReady:               true,
		EvidenceRequirementOrdinals: []int{0, 0},
		FieldTermPositions: bodyPositions(map[string][]int{
			"alpha": {1},
		}),
	}
	requirements := rerankResultRequirements(req, result)
	terms := rerankRequirementTerms(requirements)
	coverage, _, _, _ := lexicalDependenceComponents(result, terms, requirements)
	if len(requirements) != 3 || coverage != 1.0/3.0 {
		t.Fatalf("requirements=%#v coverage=%v", requirements, coverage)
	}
}
