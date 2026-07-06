package searchcore

import (
	"fmt"
	"testing"
)

func TestMarginalRelevanceDemotesRepetitiveResults(t *testing.T) {
	results := []Result{
		{
			URL:     "https://a.example/1",
			Title:   "Go concurrency patterns explained",
			Snippet: "goroutines channels select patterns",
			Score:   1.0,
		},
		{
			URL:     "https://a.example/2",
			Title:   "Go concurrency patterns explained again",
			Snippet: "goroutines channels select patterns",
			Score:   0.95,
		},
		{
			URL:     "https://b.example/3",
			Title:   "Postgres indexing deep dive",
			Snippet: "btree gin gist indexes tuning",
			Score:   0.9,
		},
		{
			URL:     "https://c.example/4",
			Title:   "Rust ownership model basics",
			Snippet: "borrow checker lifetimes memory",
			Score:   0.85,
		},
	}
	reranked := rerankMarginalRelevance(results)
	if reranked[0].URL != "https://a.example/1" {
		t.Fatalf("top must stay most relevant: %+v", reranked[0])
	}
	// The near-copy of the top result must fall behind the novel ones.
	position := map[string]int{}
	for i, result := range reranked {
		position[result.URL] = i
	}
	if position["https://a.example/2"] < position["https://b.example/3"] ||
		position["https://a.example/2"] < position["https://c.example/4"] {
		t.Fatalf("repetitive result not demoted: %+v", reranked)
	}
}

func TestMarginalRelevanceEdges(t *testing.T) {
	// Short lists pass through untouched.
	two := []Result{{URL: "a", Score: 1}, {URL: "b", Score: 0.5}}
	if got := rerankMarginalRelevance(two); got[0].URL != "a" || len(got) != 2 {
		t.Fatalf("short list = %+v", got)
	}

	// Beyond-window tail keeps its order.
	long := make([]Result, mmrWindow+5)
	for i := range long {
		long[i] = Result{
			URL:     fmt.Sprintf("https://h%d.example/", i),
			Title:   fmt.Sprintf("distinct topic %d alpha beta", i),
			Snippet: fmt.Sprintf("unique body %d gamma delta", i),
			Score:   float64(mmrWindow+5-i) * 0.01,
		}
	}
	reranked := rerankMarginalRelevance(long)
	if len(reranked) != len(long) {
		t.Fatalf("length = %d", len(reranked))
	}
	for i := mmrWindow; i < len(long); i++ {
		if reranked[i].URL != long[i].URL {
			t.Fatalf("tail reordered at %d", i)
		}
	}

	// Zero scores normalize safely.
	zeros := []Result{
		{URL: "a", Title: "one thing here", Score: 0},
		{URL: "b", Title: "two thing here", Score: 0},
		{URL: "c", Title: "three item there", Score: 0},
	}
	if got := rerankMarginalRelevance(zeros); len(got) != 3 {
		t.Fatalf("zero scores = %+v", got)
	}

	// Jaccard edge cases.
	if jaccard(nil, tokenSet("hello world")) != 0 {
		t.Fatal("empty set similarity must be zero")
	}
	if jaccard(tokenSet("alpha beta"), tokenSet("alpha beta")) != 1 {
		t.Fatal("identical sets must be fully similar")
	}
}
