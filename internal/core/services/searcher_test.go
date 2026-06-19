package services

import (
	"context"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/contracts"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func urlRows(ids ...string) map[yacymodel.Hash]yacymodel.URIMetadataRow {
	rows := make(map[yacymodel.Hash]yacymodel.URIMetadataRow, len(ids))
	for _, id := range ids {
		rows[hashFor(id)] = yacymodel.URIMetadataRow{
			Properties: map[string]string{yacymodel.URLMetaHash: string(hashFor(id))},
		}
	}

	return rows
}

func TestSearchJoinsAndExcludes(t *testing.T) {
	word1, word2, stop := hashFor("w1"), hashFor("w2"), hashFor("ex")
	rwi := &fakeRWIStore{postings: map[yacymodel.Hash][]yacymodel.RWIEntry{
		word1: {
			postingEntry(word1, "u1", 0),
			postingEntry(word1, "u2", 0),
			postingEntry(word1, "u3", 0),
		},
		word2: {postingEntry(word2, "u2", 0), postingEntry(word2, "u3", 0)},
		stop:  {postingEntry(stop, "u3", 0)},
	}}
	urls := &fakeURLStore{rows: urlRows("u1", "u2", "u3")}
	searcher := NewSearcher(rwi, urls, 100)

	result, err := searcher.Search(context.Background(), contracts.SearchQuery{
		Words:     []yacymodel.Hash{word1, word2},
		Exclude:   []yacymodel.Hash{stop},
		Abstracts: contracts.SearchAbstractRequest{Mode: contracts.SearchAbstractAuto},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.JoinCount != 1 {
		t.Errorf("join count: got %d, want 1", result.JoinCount)
	}
	if len(result.Resources) != 1 {
		t.Fatalf("resources: got %d, want 1", len(result.Resources))
	}
	if result.Resources[0].Properties[yacymodel.URLMetaHash] != string(hashFor("u2")) {
		t.Errorf("unexpected resource %v", result.Resources[0])
	}
	if result.WordCounts[word1] != 2 {
		t.Errorf("word count w1: got %d, want 2", result.WordCounts[word1])
	}
	if got := result.Abstracts[word1]; got != "{AAAAAA:u1AAAAu2AAAA}" {
		t.Errorf("abstract = %q, want compressed w1 abstract", got)
	}
}

func TestSearchMaxDistance(t *testing.T) {
	word := hashFor("w1")
	rwi := &fakeRWIStore{postings: map[yacymodel.Hash][]yacymodel.RWIEntry{
		word: {postingEntry(word, "u1", 1), postingEntry(word, "u2", 9)},
	}}
	urls := &fakeURLStore{rows: urlRows("u1", "u2")}
	searcher := NewSearcher(rwi, urls, 100)

	result, err := searcher.Search(context.Background(), contracts.SearchQuery{
		Words:       []yacymodel.Hash{word},
		MaxDistance: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.JoinCount != 1 {
		t.Errorf("join count: got %d, want 1", result.JoinCount)
	}
}

func TestSearchBoundsPostingsPerWord(t *testing.T) {
	word := hashFor("w1")
	rwi := &fakeRWIStore{postings: map[yacymodel.Hash][]yacymodel.RWIEntry{
		word: {
			postingEntry(word, "u1", 0),
			postingEntry(word, "u2", 0),
			postingEntry(word, "u3", 0),
		},
	}}
	urls := &fakeURLStore{rows: urlRows("u1", "u2", "u3")}
	searcher := NewSearcher(rwi, urls, 1)

	result, err := searcher.Search(context.Background(), contracts.SearchQuery{
		Words: []yacymodel.Hash{word},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rwi.postingsLimit != 1 {
		t.Errorf("per-word limit: got %d, want 1", rwi.postingsLimit)
	}
	if result.JoinCount != 1 {
		t.Errorf("join count: got %d, want 1 (bounded read)", result.JoinCount)
	}
}

func TestSearchTruncatesToMaxResults(t *testing.T) {
	word := hashFor("w1")
	rwi := &fakeRWIStore{postings: map[yacymodel.Hash][]yacymodel.RWIEntry{
		word: {
			postingEntry(word, "u1", 0),
			postingEntry(word, "u2", 0),
			postingEntry(word, "u3", 0),
		},
	}}
	urls := &fakeURLStore{rows: urlRows("u1", "u2", "u3")}
	searcher := NewSearcher(rwi, urls, 100)

	result, err := searcher.Search(context.Background(), contracts.SearchQuery{
		Words:      []yacymodel.Hash{word},
		MaxResults: 2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.JoinCount != 3 {
		t.Errorf("join count: got %d, want 3", result.JoinCount)
	}
	if len(result.Resources) != 2 {
		t.Errorf("resources: got %d, want 2", len(result.Resources))
	}
}

func TestSearchRanksByHitsDistanceAndHash(t *testing.T) {
	word := hashFor("w1")
	rwi := &fakeRWIStore{postings: map[yacymodel.Hash][]yacymodel.RWIEntry{
		word: {
			postingEntryWith(word, "u1", 9, 1, ""),
			postingEntryWith(word, "u2", 1, 3, ""),
			postingEntryWith(word, "u3", 2, 3, ""),
		},
	}}
	urls := &fakeURLStore{rows: urlRows("u1", "u2", "u3")}
	searcher := NewSearcher(rwi, urls, 100)

	result, err := searcher.Search(
		context.Background(),
		contracts.SearchQuery{Words: []yacymodel.Hash{word}},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := result.Resources[0].Properties[yacymodel.URLMetaHash]; got != string(hashFor("u2")) {
		t.Errorf("first resource = %q, want u2 hash", got)
	}
}

func TestSearchFiltersLanguage(t *testing.T) {
	word := hashFor("w1")
	rwi := &fakeRWIStore{postings: map[yacymodel.Hash][]yacymodel.RWIEntry{
		word: {
			postingEntryWith(word, "u1", 1, 1, "en"),
			postingEntryWith(word, "u2", 1, 1, "de"),
		},
	}}
	urls := &fakeURLStore{rows: urlRows("u1", "u2")}
	searcher := NewSearcher(rwi, urls, 100)

	result, err := searcher.Search(context.Background(), contracts.SearchQuery{
		Words:   []yacymodel.Hash{word},
		Filters: contracts.SearchFilters{Language: "en"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.JoinCount != 1 {
		t.Fatalf("join count: got %d, want 1", result.JoinCount)
	}
	if got := result.Resources[0].Properties[yacymodel.URLMetaHash]; got != string(hashFor("u1")) {
		t.Errorf("resource = %q, want u1 hash", got)
	}
}

func TestSearchExplicitAbstractOnlyCounts(t *testing.T) {
	word := hashFor("w1")
	rwi := &fakeRWIStore{postings: map[yacymodel.Hash][]yacymodel.RWIEntry{
		word: {
			postingEntry(word, "u1", 1),
			postingEntry(word, "u2", 1),
		},
	}}
	searcher := NewSearcher(rwi, &fakeURLStore{}, 100)

	result, err := searcher.Search(context.Background(), contracts.SearchQuery{
		Abstracts: contracts.SearchAbstractRequest{
			Mode:  contracts.SearchAbstractExplicit,
			Words: []yacymodel.Hash{word},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.JoinCount != 0 || len(result.Resources) != 0 {
		t.Fatalf("result = %+v, want counts only", result)
	}
	if result.WordCounts[word] != 2 {
		t.Errorf("word count = %d, want 2", result.WordCounts[word])
	}
	if got := result.Abstracts[word]; got != "{AAAAAA:u1AAAAu2AAAA}" {
		t.Errorf("abstract = %q, want compressed abstract", got)
	}
}

func TestSearchExplicitAbstractWithQuery(t *testing.T) {
	word, related := hashFor("w1"), hashFor("w2")
	rwi := &fakeRWIStore{postings: map[yacymodel.Hash][]yacymodel.RWIEntry{
		word:    {postingEntry(word, "u1", 0), postingEntry(word, "u2", 0)},
		related: {postingEntry(related, "u2", 0), postingEntry(related, "u3", 0)},
	}}
	urls := &fakeURLStore{rows: urlRows("u1", "u2")}
	searcher := NewSearcher(rwi, urls, 100)

	result, err := searcher.Search(context.Background(), contracts.SearchQuery{
		Words: []yacymodel.Hash{word},
		Abstracts: contracts.SearchAbstractRequest{
			Mode:  contracts.SearchAbstractExplicit,
			Words: []yacymodel.Hash{related},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := result.Abstracts[related]; got != "{AAAAAA:u2AAAAu3AAAA}" {
		t.Errorf("abstract = %q, want compressed related abstract", got)
	}
}

func postingEntryWith(
	word yacymodel.Hash,
	url string,
	distance byte,
	hits byte,
	language string,
) yacymodel.RWIEntry {
	entry := postingEntry(word, url, distance)
	entry.Properties[yacymodel.ColHitCount] = decimalForTest(hits)
	if language != "" {
		entry.Properties[yacymodel.ColLanguage] = language
	}
	return entry
}
