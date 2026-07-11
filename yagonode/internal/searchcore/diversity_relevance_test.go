package searchcore

import "testing"

func TestDiversifyResultsUsesLearnedRelevance(t *testing.T) {
	results := []Result{
		WithDiversityRelevance(Result{
			URL: "https://low.example/", Title: "low distinct document",
		}, 0.1),
		WithDiversityRelevance(Result{
			URL: "https://high.example/", Title: "high separate reference",
		}, 3),
		WithDiversityRelevance(Result{
			URL: "https://middle.example/", Title: "middle independent guide",
		}, 1),
	}
	diversified := DiversifyResults(results, Request{})
	if diversified[0].URL != "https://high.example/" {
		t.Fatalf("first result = %q", diversified[0].URL)
	}
}
