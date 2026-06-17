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
		Words:   []yacymodel.Hash{word1, word2},
		Exclude: []yacymodel.Hash{stop},
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
	if result.WordCounts[word1] != 3 {
		t.Errorf("word count w1: got %d, want 3", result.WordCounts[word1])
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
