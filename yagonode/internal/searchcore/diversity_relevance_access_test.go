package searchcore

import "testing"

func TestDiversityRelevanceUsesCalibratedValueOrScore(t *testing.T) {
	if got := DiversityRelevance(Result{Score: 0.4}); got != 0.4 {
		t.Fatalf("score relevance = %v", got)
	}
	if got := DiversityRelevance(WithDiversityRelevance(Result{Score: 0.4}, 0.7)); got != 0.7 {
		t.Fatalf("calibrated relevance = %v", got)
	}
}
