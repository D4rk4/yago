package searchindex

import (
	"context"
	"slices"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	"github.com/blevesearch/bleve/v2/search/searcher"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

func candidateMinimumMatchReader(t *testing.T, index bleve.Index) bleveindex.IndexReader {
	t.Helper()
	advanced, err := index.Advanced()
	if err != nil {
		t.Fatal(err)
	}
	reader, err := advanced.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })

	return reader
}

func openCandidateMinimumMatch(
	t *testing.T,
	ctx context.Context,
	reader bleveindex.IndexReader,
	query blevequery.Query,
) search.Searcher {
	t.Helper()
	opened, err := query.Searcher(ctx, reader, bleve.NewIndexMapping(), search.SearcherOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = opened.Close() })

	return opened
}

func drainCandidateMinimumMatch(
	t *testing.T,
	reader bleveindex.IndexReader,
	opened search.Searcher,
) (map[string]float64, []bleveindex.IndexInternalID) {
	t.Helper()
	ctx := &search.SearchContext{
		DocumentMatchPool: search.NewDocumentMatchPool(opened.DocumentMatchPoolSize(), 0),
	}
	scores := map[string]float64{}
	var identities []bleveindex.IndexInternalID
	for {
		match, err := opened.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if match == nil {
			return scores, identities
		}
		external, err := reader.ExternalID(match.IndexInternalID)
		if err != nil {
			t.Fatal(err)
		}
		scores[external] = match.Score
		identities = append(identities, slices.Clone(match.IndexInternalID))
		ctx.DocumentMatchPool.Put(match)
	}
}

func lexicalCandidateContext(
	identities []bleveindex.IndexInternalID,
) context.Context {
	return withLexicalCandidateIdentities(
		context.Background(),
		newBleveLexicalCandidateInternalSearcher(identities, search.SearcherOptions{}),
	)
}

func TestCandidateMinimumMatchScoresOnlyCandidatesExactly(t *testing.T) {
	documents := rarestTermsSkewedCorpus()
	index := rarestTermsTestIndex(t, documents)
	reader := candidateMinimumMatchReader(t, index)
	terms := []string{"t00", "t01", "t03", "t07", "t15"}
	driverSteps := make([]int, len(terms))
	driver, matches := drainCandidateMinimumMatch(t, reader, openCandidateMinimumMatch(
		t,
		context.Background(),
		reader,
		minimumMatchQuery(
			rarestTermsCountedClauses(terms, driverSteps, make([]int, len(terms))),
			3,
		),
	))
	var candidates, admitted, refused []bleveindex.IndexInternalID
	for position, match := range matches {
		if position%2 == 0 {
			admitted = append(admitted, match)
		}
	}
	for document := range documents {
		identity, err := reader.InternalID(document)
		if err != nil {
			t.Fatal(err)
		}
		if _, matched := driver[document]; !matched && len(refused) < 20 {
			refused = append(refused, identity)
		}
	}
	candidates = append(append(candidates, admitted...), refused...)
	slices.SortFunc(candidates, func(left, right bleveindex.IndexInternalID) int {
		return left.Compare(right)
	})
	if len(admitted) == 0 || len(admitted) == len(matches) || len(refused) == 0 {
		t.Fatalf(
			"fixture admitted=%d matches=%d refused=%d",
			len(admitted),
			len(matches),
			len(refused),
		)
	}

	candidateSteps := make([]int, len(terms))
	got, _ := drainCandidateMinimumMatch(t, reader, openCandidateMinimumMatch(
		t,
		lexicalCandidateContext(candidates),
		reader,
		minimumMatchQuery(
			rarestTermsCountedClauses(terms, candidateSteps, make([]int, len(terms))),
			3,
		),
	))

	want := map[string]float64{}
	for _, identity := range admitted {
		external, err := reader.ExternalID(identity)
		if err != nil {
			t.Fatal(err)
		}
		want[external] = driver[external]
	}
	assertRarestTermsEquivalent(t, "candidate-led minimum match", want, got)
	for clause, steps := range candidateSteps {
		if steps > 2*len(candidates) {
			t.Fatalf("clause %d moved %d times for %d candidates", clause, steps, len(candidates))
		}
	}
}

func TestCandidateMinimumMatchAdvancesWithinCandidates(t *testing.T) {
	index := rarestTermsTestIndex(t, rarestTermsSkewedCorpus())
	reader := candidateMinimumMatchReader(t, index)
	query := minimumMatchQuery(rarestTermsTestClauses([]string{"t00", "t01", "t03"}), 2)
	_, matches := drainCandidateMinimumMatch(
		t, reader, openCandidateMinimumMatch(t, context.Background(), reader, query),
	)
	if len(matches) < 3 {
		t.Fatalf("fixture has %d matches", len(matches))
	}
	candidates := []bleveindex.IndexInternalID{matches[0], matches[2]}
	opened := openCandidateMinimumMatch(t, lexicalCandidateContext(candidates), reader, query)
	ctx := &search.SearchContext{
		DocumentMatchPool: search.NewDocumentMatchPool(opened.DocumentMatchPoolSize(), 0),
	}
	match, err := opened.Advance(ctx, matches[1])
	if err != nil || match == nil || !match.IndexInternalID.Equals(matches[2]) {
		t.Fatalf("advance = %v, %v, want third match", match, err)
	}
	ctx.DocumentMatchPool.Put(match)
	if match, err := opened.Next(ctx); match != nil || err != nil {
		t.Fatalf("next after last candidate = %v, %v", match, err)
	}
}

func TestCandidateMinimumMatchRequiresCompleteSnapshot(t *testing.T) {
	index := rarestTermsTestIndex(t, rarestTermsSkewedCorpus())
	reader := candidateMinimumMatchReader(t, index)
	query := minimumMatchQuery(rarestTermsTestClauses([]string{"t00", "t01", "t03"}), 2)
	if _, driver := openCandidateMinimumMatch(t, context.Background(), reader, query).(*rarestTermsMinimumMatchSearcher); !driver {
		t.Fatal("search without a candidate snapshot left the rarest-term driver")
	}
	allDocuments, err := searcher.NewMatchAllSearcher(
		t.Context(),
		reader,
		0,
		search.SearcherOptions{},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = allDocuments.Close() })
	incomplete := withLexicalCandidateIdentities(context.Background(), allDocuments)
	if _, driver := openCandidateMinimumMatch(t, incomplete, reader, query).(*rarestTermsMinimumMatchSearcher); !driver {
		t.Fatal("incomplete candidate snapshot bounded the minimum match")
	}
	complete := lexicalCandidateContext(nil)
	if _, led := openCandidateMinimumMatch(t, complete, reader, query).(*candidateMinimumMatchSearcher); !led {
		t.Fatal("complete empty candidate snapshot did not bound the minimum match")
	}
}

type lexicalCandidateContextProbe struct {
	bleveLexicalCandidateQueryProbe
	identities []bleveindex.IndexInternalID
	bounded    bool
}

func (probe *lexicalCandidateContextProbe) Searcher(
	ctx context.Context,
	reader bleveindex.IndexReader,
	indexMapping mapping.IndexMapping,
	options search.SearcherOptions,
) (search.Searcher, error) {
	probe.identities, probe.bounded = ctx.Value(lexicalCandidateIdentitiesKey{}).([]bleveindex.IndexInternalID)

	return probe.bleveLexicalCandidateQueryProbe.Searcher(ctx, reader, indexMapping, options)
}

func TestBleveLexicalScoringBoundsRankingOnlyByCompleteSnapshot(t *testing.T) {
	identities := lexicalScoringIdentities(2, 5)
	for name, candidates := range map[string]search.Searcher{
		"complete":   newBleveLexicalCandidateInternalSearcher(identities, search.SearcherOptions{}),
		"incomplete": newBleveLexicalCandidateSearcherProbe(identities...),
	} {
		t.Run(name, func(t *testing.T) {
			ranking := &lexicalCandidateContextProbe{}
			ranking.searcher = newBleveLexicalCandidateSearcherProbe(identities...)
			query := bleveLexicalCandidateScoringQuery{bleve.NewConjunctionQuery(
				ranking, &bleveLexicalCandidateQueryProbe{searcher: candidates},
			)}
			opened, err := query.Searcher(
				t.Context(),
				nil,
				bleve.NewIndexMapping(),
				search.SearcherOptions{},
			)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = opened.Close() })
			if ranking.bounded != (name == "complete") ||
				ranking.bounded &&
					!slices.EqualFunc(
						ranking.identities,
						identities,
						bleveindex.IndexInternalID.Equals,
					) {
				t.Fatalf("ranking bounded=%t identities=%v", ranking.bounded, ranking.identities)
			}
		})
	}
}

func lexicalScoringIdentities(documents ...uint64) []bleveindex.IndexInternalID {
	identities := make([]bleveindex.IndexInternalID, len(documents))
	for position, document := range documents {
		identities[position] = bleveindex.NewIndexInternalID(nil, document)
	}

	return identities
}
