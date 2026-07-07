package searchindex

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

func TestAdjacentPairsJoinsNeighbors(t *testing.T) {
	pairs := adjacentPairs([]string{"что", " такое ", "осень", ""})
	if len(pairs) != 2 || pairs[0] != "что такое" || pairs[1] != "такое осень" {
		t.Fatalf("pairs = %v", pairs)
	}
	if adjacentPairs([]string{"solo"}) != nil {
		t.Fatal("single term carries no dependency signal")
	}
	if adjacentPairs(nil) != nil {
		t.Fatal("no terms, no pairs")
	}
}

func TestSDMBigramBoostsBuildPerPair(t *testing.T) {
	boosts := sdmBigramBoosts(
		[]string{"alpha", "beta", "gamma"},
		DefaultRankingWeights(),
		"",
	)
	if len(boosts) != 2 {
		t.Fatalf("boosts = %d, want one per adjacent pair", len(boosts))
	}
}

// TestSDMBigramsRankPhraseDocumentFirst is the SEARCH-38 acceptance: two
// documents contain the same query words, but only one carries them as an
// adjacent pair — the ordered-window document must win (Metzler & Croft SDM).
func TestSDMBigramsRankPhraseDocumentFirst(t *testing.T) {
	scattered := documentstore.Document{
		NormalizedURL: "https://scattered.example/page",
		Title:         "beta report and alpha notes",
		ExtractedText: "beta appears early in this text while alpha appears much later apart",
	}
	phrase := documentstore.Document{
		NormalizedURL: "https://phrase.example/page",
		Title:         "alpha beta guide",
		ExtractedText: "the alpha beta sequence appears together in this document body",
	}
	index, err := NewBleveDiskIndex(
		t.Context(),
		t.TempDir(),
		newFakeDocumentDirectory(scattered, phrase),
		nil,
	)
	if err != nil {
		t.Fatalf("NewBleveDiskIndex: %v", err)
	}
	defer func() { _ = index.Close() }()
	if err := index.IndexBatch(
		t.Context(),
		[]documentstore.Document{scattered, phrase},
	); err != nil {
		t.Fatalf("IndexBatch: %v", err)
	}

	set, err := index.Search(t.Context(), SearchRequest{
		Query:      "alpha beta",
		MaxResults: 2,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(set.Results) != 2 {
		t.Fatalf("results = %d, want both documents", len(set.Results))
	}
	if set.Results[0].URL != "https://phrase.example/page" {
		t.Fatalf("adjacent-pair document must rank first, got %s", set.Results[0].URL)
	}
}
