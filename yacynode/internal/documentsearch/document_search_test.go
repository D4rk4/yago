package documentsearch

import (
	"context"
	"strconv"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type fakeScanner struct {
	postings map[yacymodel.Hash][]yacymodel.RWIPosting
}

func (s fakeScanner) RWICount(context.Context) (int, error) {
	return len(s.postings), nil
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

func postingEntry(word yacymodel.Hash, url string, position, hits int) yacymodel.RWIPosting {
	return yacymodel.RWIPosting{
		WordHash: word,
		Properties: map[string]string{
			yacymodel.ColURLHash:      string(hashFor(url)),
			yacymodel.ColHitCount:     strconv.Itoa(hits),
			yacymodel.ColTextPosition: strconv.Itoa(position),
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

func TestSearchJoinsAndCountsAndReports(t *testing.T) {
	word1, word2 := hashFor("w1"), hashFor("w2")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word1: {postingEntry(word1, "u1", 0, 1), postingEntry(word1, "u2", 0, 1)},
		word2: {postingEntry(word2, "u2", 0, 1), postingEntry(word2, "u3", 0, 1)},
	}}
	s := searcher{
		index:          index,
		documents:      fakeDirectory{rows: urlRows("u1", "u2", "u3")},
		matchesPerTerm: 100,
	}

	result, err := s.search(context.Background(), searchCriteria{
		terms:     []yacymodel.Hash{word1, word2},
		reporting: matchReporting{mode: reportTermWithMostMatches},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result.totalDocumentsMatchingEveryTerm != 1 {
		t.Errorf(
			"totalDocumentsMatchingEveryTerm = %d, want 1",
			result.totalDocumentsMatchingEveryTerm,
		)
	}
	if len(result.resources) != 1 {
		t.Fatalf("resources = %d, want 1", len(result.resources))
	}
	if result.resources[0].Properties[yacymodel.URLMetaHash] != string(hashFor("u2")) {
		t.Errorf("resource = %v, want u2", result.resources[0])
	}
	if result.totalMatchesPerTerm[word1] != 2 {
		t.Errorf("totalMatchesPerTerm[w1] = %d, want 2", result.totalMatchesPerTerm[word1])
	}
	if got := result.documentsMatchingEachReportedTerm[word1]; got != "{AAAAAA:u1AAAAu2AAAA}" {
		t.Errorf("documentsMatchingEachReportedTerm[w1] = %q", got)
	}
}

func TestSearchTakesMostRelevantUpToLimit(t *testing.T) {
	word := hashFor("w1")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word: {
			postingEntry(word, "u1", 0, 1),
			postingEntry(word, "u2", 0, 1),
			postingEntry(word, "u3", 0, 1),
		},
	}}
	s := searcher{
		index:          index,
		documents:      fakeDirectory{rows: urlRows("u1", "u2", "u3")},
		matchesPerTerm: 100,
	}

	result, err := s.search(
		context.Background(),
		searchCriteria{terms: []yacymodel.Hash{word}, maxResults: 2},
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result.totalDocumentsMatchingEveryTerm != 3 {
		t.Errorf(
			"totalDocumentsMatchingEveryTerm = %d, want 3",
			result.totalDocumentsMatchingEveryTerm,
		)
	}
	if len(result.resources) != 2 {
		t.Errorf("resources = %d, want 2", len(result.resources))
	}
}

func TestSearchOrdersByOccurrencesThenTermSpread(t *testing.T) {
	word1, word2 := hashFor("w1"), hashFor("w2")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word1: {postingEntry(word1, "u2", 1, 1), postingEntry(word1, "u3", 1, 1)},
		word2: {postingEntry(word2, "u2", 2, 2), postingEntry(word2, "u3", 5, 2)},
	}}
	s := searcher{
		index:          index,
		documents:      fakeDirectory{rows: urlRows("u2", "u3")},
		matchesPerTerm: 100,
	}

	result, err := s.search(
		context.Background(),
		searchCriteria{terms: []yacymodel.Hash{word1, word2}},
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got := result.resources[0].Properties[yacymodel.URLMetaHash]; got != string(hashFor("u2")) {
		t.Errorf("first resource = %q, want u2", got)
	}
}

func TestSearchExcludesTerms(t *testing.T) {
	word, ban := hashFor("w1"), hashFor("ban")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word: {postingEntry(word, "u1", 0, 1), postingEntry(word, "u2", 0, 1)},
		ban:  {postingEntry(ban, "u2", 0, 1)},
	}}
	s := searcher{
		index:          index,
		documents:      fakeDirectory{rows: urlRows("u1", "u2")},
		matchesPerTerm: 100,
	}

	result, err := s.search(context.Background(), searchCriteria{
		terms:         []yacymodel.Hash{word},
		excludedTerms: []yacymodel.Hash{ban},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(result.resources) != 1 ||
		result.resources[0].Properties[yacymodel.URLMetaHash] != string(hashFor("u1")) {
		t.Fatalf("resources = %v, want only u1", result.resources)
	}
}

func TestSearchReportsRequestedTermsWithoutWantedTerms(t *testing.T) {
	word := hashFor("w1")
	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word: {postingEntry(word, "u1", 1, 1), postingEntry(word, "u2", 1, 1)},
	}}
	s := searcher{index: index, documents: fakeDirectory{}, matchesPerTerm: 100}

	result, err := s.search(context.Background(), searchCriteria{
		reporting: matchReporting{mode: reportRequestedTerms, terms: []yacymodel.Hash{word}},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result.totalDocumentsMatchingEveryTerm != 0 || len(result.resources) != 0 {
		t.Fatalf("result = %+v, want report only", result)
	}
	if result.totalMatchesPerTerm[word] != 2 {
		t.Errorf("totalMatchesPerTerm = %d, want 2", result.totalMatchesPerTerm[word])
	}
	if got := result.documentsMatchingEachReportedTerm[word]; got != "{AAAAAA:u1AAAAu2AAAA}" {
		t.Errorf("documentsMatchingEachReportedTerm = %q", got)
	}
}

func TestSearchQualifiesByLanguageAndTermSpread(t *testing.T) {
	word1, word2 := hashFor("w1"), hashFor("w2")
	english := func(url string, position int) yacymodel.RWIPosting {
		posting := postingEntry(word1, url, position, 1)
		posting.Properties[yacymodel.ColLanguage] = "en"

		return posting
	}
	inLanguage := func(word yacymodel.Hash, url, language string, position int) yacymodel.RWIPosting {
		posting := postingEntry(word, url, position, 1)
		posting.Properties[yacymodel.ColLanguage] = language

		return posting
	}

	near := english("u1", 1)
	nearOther := inLanguage(word2, "u1", "en", 2)
	german := inLanguage(word1, "u2", "de", 1)
	germanOther := inLanguage(word2, "u2", "de", 2)
	far := english("u3", 1)
	farOther := inLanguage(word2, "u3", "en", 9)

	index := fakeScanner{postings: map[yacymodel.Hash][]yacymodel.RWIPosting{
		word1: {near, german, far},
		word2: {nearOther, germanOther, farOther},
	}}
	s := searcher{
		index:          index,
		documents:      fakeDirectory{rows: urlRows("u1", "u2", "u3")},
		matchesPerTerm: 100,
	}

	result, err := s.search(context.Background(), searchCriteria{
		terms:         []yacymodel.Hash{word1, word2},
		maxTermSpread: 5,
		language:      "en",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(result.resources) != 1 ||
		result.resources[0].Properties[yacymodel.URLMetaHash] != string(hashFor("u1")) {
		t.Fatalf("resources = %v, want only u1", result.resources)
	}
}
