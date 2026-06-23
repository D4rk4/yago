package search

import (
	"context"
	"strconv"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type fakeScanner struct {
	postings map[yacymodel.Hash][]yacymodel.RWIPosting
}

func (s fakeScanner) ScanWord(
	_ context.Context,
	word yacymodel.Hash,
	visit func(yacymodel.RWIPosting) (bool, error),
) error {
	for _, entry := range s.postings[word] {
		entry.WordHash = word
		keepGoing, err := visit(entry)
		if err != nil {
			return err
		}
		if !keepGoing {
			return nil
		}
	}

	return nil
}

type fakeDirectory struct {
	rows map[yacymodel.Hash]yacymodel.URIMetadataRow
}

func (d fakeDirectory) RowsByHash(
	_ context.Context,
	hashes []yacymodel.Hash,
) ([]yacymodel.URIMetadataRow, error) {
	out := make([]yacymodel.URIMetadataRow, 0, len(hashes))
	for _, hash := range hashes {
		if row, ok := d.rows[hash]; ok {
			out = append(out, row)
		}
	}

	return out, nil
}

func (d fakeDirectory) MissingURLs(
	context.Context,
	[]yacymodel.Hash,
) ([]yacymodel.Hash, error) {
	return nil, nil
}

func (d fakeDirectory) Count(context.Context) (int, error) {
	return len(d.rows), nil
}

func hashFor(base string) yacymodel.Hash {
	const filler = "AAAAAAAAAAAA"
	if len(base) >= yacymodel.HashLength {
		return yacymodel.Hash(base[:yacymodel.HashLength])
	}

	return yacymodel.Hash(base + filler[len(base):])
}

func postingEntry(word yacymodel.Hash, url string, distance, hits int) yacymodel.RWIPosting {
	return yacymodel.RWIPosting{
		WordHash: word,
		Properties: map[string]string{
			yacymodel.ColURLHash:      string(hashFor(url)),
			yacymodel.ColHitCount:     strconv.Itoa(hits),
			yacymodel.ColWordDistance: strconv.Itoa(distance),
		},
	}
}

func urlRows(ids ...string) map[yacymodel.Hash]yacymodel.URIMetadataRow {
	rows := make(map[yacymodel.Hash]yacymodel.URIMetadataRow, len(ids))
	for _, id := range ids {
		rows[hashFor(id)] = yacymodel.URIMetadataRow{
			Properties: map[string]string{yacymodel.URLMetaHash: string(hashFor(id))},
		}
	}

	return rows
}

func TestSearchJoinsAndCountsAndAbstracts(t *testing.T) {
	word1, word2 := hashFor("w1"), hashFor("w2")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word1: {postingEntry(word1, "u1", 0, 1), postingEntry(word1, "u2", 0, 1)},
		word2: {postingEntry(word2, "u2", 0, 1), postingEntry(word2, "u3", 0, 1)},
	}}
	s := searcher{
		index:           index,
		urls:            fakeDirectory{rows: urlRows("u1", "u2", "u3")},
		postingsPerWord: 100,
	}

	result, err := s.Search(context.Background(), searchQuery{
		Words:     []yacymodel.Hash{word1, word2},
		Abstracts: abstractSpec{Mode: abstractAuto},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.JoinCount != 1 {
		t.Errorf("JoinCount = %d, want 1", result.JoinCount)
	}
	if len(result.Resources) != 1 {
		t.Fatalf("Resources = %d, want 1", len(result.Resources))
	}
	if result.Resources[0].Properties[yacymodel.URLMetaHash] != string(hashFor("u2")) {
		t.Errorf("resource = %v, want u2", result.Resources[0])
	}
	if result.WordCounts[word1] != 2 {
		t.Errorf("WordCounts[w1] = %d, want 2", result.WordCounts[word1])
	}
	if got := result.Abstracts[word1]; got != "{AAAAAA:u1AAAAu2AAAA}" {
		t.Errorf("Abstracts[w1] = %q", got)
	}
}

func TestSearchTruncatesToMaxResults(t *testing.T) {
	word := hashFor("w1")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word: {
			postingEntry(word, "u1", 0, 1),
			postingEntry(word, "u2", 0, 1),
			postingEntry(word, "u3", 0, 1),
		},
	}}
	s := searcher{
		index:           index,
		urls:            fakeDirectory{rows: urlRows("u1", "u2", "u3")},
		postingsPerWord: 100,
	}

	result, err := s.Search(
		context.Background(),
		searchQuery{Words: []yacymodel.Hash{word}, MaxResults: 2},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.JoinCount != 3 {
		t.Errorf("JoinCount = %d, want 3", result.JoinCount)
	}
	if len(result.Resources) != 2 {
		t.Errorf("Resources = %d, want 2", len(result.Resources))
	}
}

func TestSearchRanksByHitsThenDistance(t *testing.T) {
	word := hashFor("w1")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word: {
			postingEntry(word, "u1", 9, 1),
			postingEntry(word, "u2", 1, 3),
			postingEntry(word, "u3", 2, 3),
		},
	}}
	s := searcher{
		index:           index,
		urls:            fakeDirectory{rows: urlRows("u1", "u2", "u3")},
		postingsPerWord: 100,
	}

	result, err := s.Search(context.Background(), searchQuery{Words: []yacymodel.Hash{word}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if got := result.Resources[0].Properties[yacymodel.URLMetaHash]; got != string(hashFor("u2")) {
		t.Errorf("first resource = %q, want u2", got)
	}
}

func TestSearchExcludesWords(t *testing.T) {
	word, ban := hashFor("w1"), hashFor("ban")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word: {postingEntry(word, "u1", 0, 1), postingEntry(word, "u2", 0, 1)},
		ban:  {postingEntry(ban, "u2", 0, 1)},
	}}
	s := searcher{
		index:           index,
		urls:            fakeDirectory{rows: urlRows("u1", "u2")},
		postingsPerWord: 100,
	}

	result, err := s.Search(context.Background(), searchQuery{
		Words:   []yacymodel.Hash{word},
		Exclude: []yacymodel.Hash{ban},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(result.Resources) != 1 ||
		result.Resources[0].Properties[yacymodel.URLMetaHash] != string(hashFor("u1")) {
		t.Fatalf("Resources = %v, want only u1", result.Resources)
	}
}

func TestSearchExplicitAbstractOnlyCounts(t *testing.T) {
	word := hashFor("w1")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word: {postingEntry(word, "u1", 1, 1), postingEntry(word, "u2", 1, 1)},
	}}
	s := searcher{index: index, urls: fakeDirectory{}, postingsPerWord: 100}

	result, err := s.Search(context.Background(), searchQuery{
		Abstracts: abstractSpec{Mode: abstractExplicit, Words: []yacymodel.Hash{word}},
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if result.JoinCount != 0 || len(result.Resources) != 0 {
		t.Fatalf("result = %+v, want counts only", result)
	}
	if result.WordCounts[word] != 2 {
		t.Errorf("WordCounts = %d, want 2", result.WordCounts[word])
	}
	if got := result.Abstracts[word]; got != "{AAAAAA:u1AAAAu2AAAA}" {
		t.Errorf("Abstracts = %q", got)
	}
}
