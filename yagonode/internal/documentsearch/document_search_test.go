package documentsearch

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
)

type fakeScanner struct {
	postings map[yagomodel.Hash][]yagomodel.RWIPosting
	err      error
}

func (s fakeScanner) RWICount(context.Context) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	return len(s.postings), nil
}

func (s fakeScanner) RWIURLCount(_ context.Context, word yagomodel.Hash) (int, error) {
	if s.err != nil {
		return 0, s.err
	}

	return len(s.postings[word]), nil
}

func (s fakeScanner) ScanWord(
	_ context.Context,
	word yagomodel.Hash,
	visit func(yagomodel.RWIPosting) (bool, error),
) error {
	if s.err != nil {
		return s.err
	}
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
	rows map[yagomodel.Hash]yagomodel.URIMetadataRow
	err  error
}

func (d fakeDirectory) RowsByHash(
	_ context.Context,
	hashes []yagomodel.Hash,
) ([]yagomodel.URIMetadataRow, error) {
	if d.err != nil {
		return nil, d.err
	}
	out := make([]yagomodel.URIMetadataRow, 0, len(hashes))
	for _, hash := range hashes {
		if row, ok := d.rows[hash]; ok {
			out = append(out, row)
		}
	}

	return out, nil
}

func (d fakeDirectory) MissingURLs(
	context.Context,
	[]yagomodel.Hash,
) ([]yagomodel.Hash, error) {
	return nil, nil
}

func (d fakeDirectory) Count(context.Context) (int, error) {
	if d.err != nil {
		return 0, d.err
	}
	return len(d.rows), nil
}

func hashFor(base string) yagomodel.Hash {
	const filler = "AAAAAAAAAAAA"
	if len(base) >= yagomodel.HashLength {
		return yagomodel.Hash(base[:yagomodel.HashLength])
	}

	return yagomodel.Hash(base + filler[len(base):])
}

func postingEntry(word yagomodel.Hash, url string, position, hits int) yagomodel.RWIPosting {
	return yagomodel.RWIPosting{
		WordHash: word,
		Properties: map[string]string{
			yagomodel.ColURLHash:      string(hashFor(url)),
			yagomodel.ColHitCount:     strconv.Itoa(hits),
			yagomodel.ColTextPosition: strconv.Itoa(position),
		},
	}
}

func indexerPosting(word yagomodel.Hash, url string, position int) yagomodel.RWIPosting {
	return yagomodel.RWIPosting{
		WordHash: word,
		Properties: map[string]string{
			yagomodel.ColURLHash:           string(hashFor(url)),
			yagomodel.ColLanguage:          "en",
			yagomodel.ColTextWordCount:     yagomodel.FormatRWICardinal(64),
			yagomodel.ColTitleWordCount:    yagomodel.FormatRWICardinal(4),
			yagomodel.ColLocalLinkCount:    yagomodel.FormatRWICardinal(2),
			yagomodel.ColExternalLinkCount: yagomodel.FormatRWICardinal(1),
			yagomodel.ColHitCount:          yagomodel.FormatRWICardinal(1),
			yagomodel.ColTextPosition:      strconv.Itoa(position),
		},
	}
}

func urlRows(ids ...string) map[yagomodel.Hash]yagomodel.URIMetadataRow {
	rows := make(map[yagomodel.Hash]yagomodel.URIMetadataRow, len(ids))
	for _, id := range ids {
		rows[hashFor(id)] = yagomodel.URIMetadataRow{
			Properties: map[string]string{yagomodel.URLMetaHash: string(hashFor(id))},
		}
	}

	return rows
}

func TestSearchJoinsAndCountsAndReports(t *testing.T) {
	word1, word2 := hashFor("w1"), hashFor("w2")
	index := fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
		word1: {postingEntry(word1, "u1", 0, 1), postingEntry(word1, "u2", 0, 1)},
		word2: {postingEntry(word2, "u2", 0, 1), postingEntry(word2, "u3", 0, 1)},
	}}
	s := searcher{
		index:          index,
		documents:      fakeDirectory{rows: urlRows("u1", "u2", "u3")},
		matchesPerTerm: 100,
	}

	result, err := s.search(context.Background(), searchCriteria{
		terms:     []yagomodel.Hash{word1, word2},
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
	if result.resources[0].Properties[yagomodel.URLMetaHash] != string(hashFor("u2")) {
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
	index := fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
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
		searchCriteria{terms: []yagomodel.Hash{word}, maxResults: 2},
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
	index := fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
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
		searchCriteria{terms: []yagomodel.Hash{word1, word2}},
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if got := result.resources[0].Properties[yagomodel.URLMetaHash]; got != string(hashFor("u2")) {
		t.Errorf("first resource = %q, want u2", got)
	}
}

func TestSearchReturnsPostingsBuiltLikeTheIndexer(t *testing.T) {
	word1, word2 := hashFor("w1"), hashFor("w2")
	postings := map[yagomodel.Hash][]yagomodel.RWIPosting{
		word1: {indexerPosting(word1, "u1", 3), indexerPosting(word1, "u2", 10)},
		word2: {indexerPosting(word2, "u1", 5), indexerPosting(word2, "u2", 40)},
	}
	for _, entries := range postings {
		for _, entry := range entries {
			if _, injected := entry.Properties[yagomodel.ColWordDistance]; injected {
				t.Fatalf("fixture must not inject ColWordDistance: %v", entry.Properties)
			}
			if _, ok := entry.Properties[yagomodel.ColTextPosition]; !ok {
				t.Fatalf("fixture must carry ColTextPosition: %v", entry.Properties)
			}
		}
	}

	s := searcher{
		index:          fakeScanner{postings: postings},
		documents:      fakeDirectory{rows: urlRows("u1", "u2")},
		matchesPerTerm: 100,
	}

	result, err := s.search(
		context.Background(),
		searchCriteria{terms: []yagomodel.Hash{word1, word2}},
	)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if result.totalDocumentsMatchingEveryTerm != 2 {
		t.Fatalf(
			"totalDocumentsMatchingEveryTerm = %d, want 2: real crawled postings must not be discarded",
			result.totalDocumentsMatchingEveryTerm,
		)
	}
	if len(result.resources) != 2 {
		t.Fatalf("resources = %d, want 2", len(result.resources))
	}
	if got := result.resources[0].Properties[yagomodel.URLMetaHash]; got != string(hashFor("u1")) {
		t.Errorf("first resource = %q, want u1 with the smaller term spread", got)
	}
}

func TestSearchExcludesTerms(t *testing.T) {
	word, ban := hashFor("w1"), hashFor("ban")
	index := fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
		word: {postingEntry(word, "u1", 0, 1), postingEntry(word, "u2", 0, 1)},
		ban:  {postingEntry(ban, "u2", 0, 1)},
	}}
	s := searcher{
		index:          index,
		documents:      fakeDirectory{rows: urlRows("u1", "u2")},
		matchesPerTerm: 100,
	}

	result, err := s.search(context.Background(), searchCriteria{
		terms:         []yagomodel.Hash{word},
		excludedTerms: []yagomodel.Hash{ban},
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(result.resources) != 1 ||
		result.resources[0].Properties[yagomodel.URLMetaHash] != string(hashFor("u1")) {
		t.Fatalf("resources = %v, want only u1", result.resources)
	}
}

func TestSearchReportsRequestedTermsWithoutWantedTerms(t *testing.T) {
	word := hashFor("w1")
	index := fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
		word: {postingEntry(word, "u1", 1, 1), postingEntry(word, "u2", 1, 1)},
	}}
	s := searcher{index: index, documents: fakeDirectory{}, matchesPerTerm: 100}

	result, err := s.search(context.Background(), searchCriteria{
		reporting: matchReporting{mode: reportRequestedTerms, terms: []yagomodel.Hash{word}},
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
	english := func(url string, position int) yagomodel.RWIPosting {
		posting := postingEntry(word1, url, position, 1)
		posting.Properties[yagomodel.ColLanguage] = "en"

		return posting
	}
	inLanguage := func(word yagomodel.Hash, url, language string, position int) yagomodel.RWIPosting {
		posting := postingEntry(word, url, position, 1)
		posting.Properties[yagomodel.ColLanguage] = language

		return posting
	}

	near := english("u1", 1)
	nearOther := inLanguage(word2, "u1", "en", 2)
	german := inLanguage(word1, "u2", "de", 1)
	germanOther := inLanguage(word2, "u2", "de", 2)
	far := english("u3", 1)
	farOther := inLanguage(word2, "u3", "en", 9)

	index := fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
		word1: {near, german, far},
		word2: {nearOther, germanOther, farOther},
	}}
	s := searcher{
		index:          index,
		documents:      fakeDirectory{rows: urlRows("u1", "u2", "u3")},
		matchesPerTerm: 100,
	}

	result, err := s.search(context.Background(), searchCriteria{
		terms:         []yagomodel.Hash{word1, word2},
		maxTermSpread: 5,
		language:      "en",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(result.resources) != 1 ||
		result.resources[0].Properties[yagomodel.URLMetaHash] != string(hashFor("u1")) {
		t.Fatalf("resources = %v, want only u1", result.resources)
	}
}

func TestSearchReturnsPipelineErrors(t *testing.T) {
	sentinel := errors.New("scan failed")
	word := hashFor("w1")
	if _, err := (searcher{index: fakeScanner{err: sentinel}}).search(
		context.Background(),
		searchCriteria{excludedTerms: []yagomodel.Hash{word}},
	); !errors.Is(err, sentinel) {
		t.Fatalf("excluded term error = %v, want %v", err, sentinel)
	}

	if _, err := (searcher{index: fakeScanner{err: sentinel}}).search(
		context.Background(),
		searchCriteria{terms: []yagomodel.Hash{word}},
	); !errors.Is(err, sentinel) {
		t.Fatalf("wanted term error = %v, want %v", err, sentinel)
	}

	if _, err := (searcher{
		index: fakeScanner{postings: map[yagomodel.Hash][]yagomodel.RWIPosting{
			word: {postingEntry(word, "u1", 0, 1)},
		}},
		documents: fakeDirectory{err: sentinel},
	}).search(context.Background(), searchCriteria{terms: []yagomodel.Hash{word}}); !errors.Is(err, sentinel) {
		t.Fatalf("rows error = %v, want %v", err, sentinel)
	}

	if _, err := (searcher{index: fakeScanner{err: sentinel}, documents: fakeDirectory{rows: map[yagomodel.Hash]yagomodel.URIMetadataRow{}}}).search(
		context.Background(),
		searchCriteria{
			reporting: matchReporting{mode: reportRequestedTerms, terms: []yagomodel.Hash{word}},
		},
	); !errors.Is(
		err,
		sentinel,
	) {
		t.Fatalf("report error = %v, want %v", err, sentinel)
	}
}
