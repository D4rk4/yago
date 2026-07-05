package searchlocal

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

func resultTitles(results []searchcore.Result) []string {
	titles := make([]string, len(results))
	for i, result := range results {
		titles[i] = result.Title
	}

	return titles
}

func TestSearcherAppliesHostRankBoost(t *testing.T) {
	// "Low" outranks "High" on raw relevance; a strong host-authority score for
	// High's host must lift it to the top after rescoring.
	lowURL := "https://low.example/a"
	highURL := "https://high.example/b"
	index := &fakeIndex{response: searchindex.SearchResultSet{
		Total: 2,
		Results: []searchindex.SearchResult{
			{Title: "Low", URL: lowURL, Score: 1.0},
			{Title: "High", URL: highURL, Score: 0.9},
		},
	}}
	table := hostrank.Table{hostHashOf(highURL): 1.0}
	searcher := NewSearcherWithRanking(
		index,
		func() searchindex.RankingWeights {
			return searchindex.RankingWeights{Title: 1, HostRank: 1}
		},
		func() hostrank.Table { return table },
	)

	resp, err := searcher.Search(t.Context(), searchcore.Request{Query: "q"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := resultTitles(resp.Results); len(got) != 2 || got[0] != "High" || got[1] != "Low" {
		t.Fatalf("host-rank order = %v, want [High Low]", got)
	}
}

func TestSearcherSkipsHostRankWhenWeightZero(t *testing.T) {
	index := &fakeIndex{response: searchindex.SearchResultSet{
		Total: 2,
		Results: []searchindex.SearchResult{
			{Title: "First", URL: "https://a.example/1", Score: 1.0},
			{Title: "Second", URL: "https://b.example/2", Score: 0.9},
		},
	}}
	consulted := false
	searcher := NewSearcherWithRanking(
		index,
		func() searchindex.RankingWeights { return searchindex.RankingWeights{Title: 1} },
		func() hostrank.Table { consulted = true; return hostrank.Table{} },
	)

	resp, err := searcher.Search(t.Context(), searchcore.Request{Query: "q"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if consulted {
		t.Fatal("host-rank table consulted despite zero HostRank weight")
	}
	if got := resultTitles(resp.Results); got[0] != "First" {
		t.Fatalf("order changed with host-rank disabled: %v", got)
	}
}
