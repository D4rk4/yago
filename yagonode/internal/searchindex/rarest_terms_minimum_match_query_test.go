package searchindex

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/mapping"
	"github.com/blevesearch/bleve/v2/search"
	blevequery "github.com/blevesearch/bleve/v2/search/query"
	bleveindex "github.com/blevesearch/bleve_index_api"
)

const rarestTermsTestField = "body"

func rarestTermsTestIndex(t *testing.T, documents map[string]string) bleve.Index {
	t.Helper()
	index, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })
	batch := index.NewBatch()
	for id, body := range documents {
		if err := batch.Index(id, map[string]any{rarestTermsTestField: body}); err != nil {
			t.Fatal(err)
		}
	}
	if err := index.Batch(batch); err != nil {
		t.Fatal(err)
	}

	return index
}

func rarestTermsTestClauses(terms []string) []blevequery.Query {
	clauses := make([]blevequery.Query, 0, len(terms))
	for _, term := range terms {
		clause := bleve.NewTermQuery(term)
		clause.SetField(rarestTermsTestField)
		clauses = append(clauses, clause)
	}

	return clauses
}

func rarestTermsTestScores(
	t *testing.T,
	index bleve.Index,
	query blevequery.Query,
) map[string]float64 {
	t.Helper()
	request := bleve.NewSearchRequestOptions(query, 10_000, 0, false)
	result, err := index.SearchInContext(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	scores := make(map[string]float64, len(result.Hits))
	for _, hit := range result.Hits {
		scores[hit.ID] = hit.Score
	}
	if uint64(len(scores)) != result.Total {
		t.Fatalf("hits = %d, total = %d", len(scores), result.Total)
	}

	return scores
}

func assertRarestTermsEquivalent(
	t *testing.T,
	label string,
	want map[string]float64,
	got map[string]float64,
) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d hits, want %d", label, len(got), len(want))
	}
	for id, score := range want {
		gotScore, found := got[id]
		if !found || math.Abs(gotScore-score) > 1e-12*math.Max(1, math.Abs(score)) {
			t.Fatalf("%s: %s score = %v/%t, want %v", label, id, gotScore, found, score)
		}
	}
}

func rarestTermsSkewedCorpus() map[string]string {
	documents := make(map[string]string, 600)
	for document := range 600 {
		words := make([]string, 0, 12)
		for word := range 12 {
			spread := (document*31 + word*17 + document*word*7) % 1600
			words = append(words, fmt.Sprintf("t%02d", 39-int(math.Sqrt(float64(spread)))))
		}
		documents[fmt.Sprintf("d%04d", document)] = strings.Join(words, " ")
	}

	return documents
}

func TestRarestTermsMinimumMatchEqualsDisjunctionMinimum(t *testing.T) {
	index := rarestTermsTestIndex(t, rarestTermsSkewedCorpus())
	filter := bleve.NewTermQuery("t00")
	filter.SetField(rarestTermsTestField)
	for _, terms := range [][]string{
		{"t00", "t01", "t02"},
		{"t00", "t01", "t03", "t07", "t15"},
		{"t00", "t01", "t02", "t04", "t06", "t09", "t12", "t20", "t30", "t35", "t38", "t39"},
		{"t05", "t00", "t25", "t01", "t02", "t31", "absent"},
	} {
		for minimum := 2; minimum <= len(terms); minimum++ {
			label := fmt.Sprintf("%v min %d", terms, minimum)
			stock := bleve.NewDisjunctionQuery(rarestTermsTestClauses(terms)...)
			stock.SetMin(float64(minimum))
			driven := minimumMatchQuery(rarestTermsTestClauses(terms), minimum)
			want := rarestTermsTestScores(t, index, stock)
			assertRarestTermsEquivalent(t, label, want, rarestTermsTestScores(t, index, driven))
			assertRarestTermsEquivalent(
				t,
				label+" under conjunction",
				rarestTermsTestScores(t, index, bleve.NewConjunctionQuery(filter, stock)),
				rarestTermsTestScores(t, index, bleve.NewConjunctionQuery(filter, driven)),
			)
		}
	}
}

func TestRarestTermsMinimumMatchKeepsQualifiedAndRejectsUnderqualifiedDocuments(t *testing.T) {
	documents := map[string]string{
		"rare-and-common": "rare common shared",
		"common-only":     "common shared",
		"rare-only":       "rare",
		"all":             "rare common shared",
	}
	for document := range 40 {
		documents[fmt.Sprintf("filler-%02d", document)] = "common shared filler"
	}
	index := rarestTermsTestIndex(t, documents)
	scores := rarestTermsTestScores(
		t,
		index,
		minimumMatchQuery(rarestTermsTestClauses([]string{"common", "shared", "rare"}), 3),
	)
	if _, found := scores["rare-and-common"]; !found {
		t.Fatalf("document matching every term is missing: %v", scores)
	}
	for _, rejected := range []string{"common-only", "rare-only", "filler-00"} {
		if _, found := scores[rejected]; found {
			t.Fatalf("document %s below the minimum was returned: %v", rejected, scores)
		}
	}
	scores = rarestTermsTestScores(
		t,
		index,
		minimumMatchQuery(rarestTermsTestClauses([]string{"common", "shared", "rare"}), 2),
	)
	if _, found := scores["common-only"]; !found || len(scores) != 43 {
		t.Fatalf("two-term minimum dropped common-only matches: %v", scores)
	}
	if _, found := scores["rare-only"]; found {
		t.Fatalf("single-term document met a two-term minimum: %v", scores)
	}
}

type rarestTermsCountingQuery struct {
	blevequery.Query
	steps  *int
	closes *int
}

type rarestTermsCountingSearcher struct {
	search.Searcher
	steps  *int
	closes *int
}

func (query rarestTermsCountingQuery) Searcher(
	ctx context.Context,
	reader bleveindex.IndexReader,
	indexMapping mapping.IndexMapping,
	options search.SearcherOptions,
) (search.Searcher, error) {
	opened, err := query.Query.Searcher(ctx, reader, indexMapping, options)
	if err != nil {
		return nil, fmt.Errorf("open counted clause: %w", err)
	}

	return rarestTermsCountingSearcher{
		Searcher: opened,
		steps:    query.steps,
		closes:   query.closes,
	}, nil
}

func (s rarestTermsCountingSearcher) Next(
	ctx *search.SearchContext,
) (*search.DocumentMatch, error) {
	*s.steps++
	match, err := s.Searcher.Next(ctx)
	if err != nil {
		return nil, fmt.Errorf("next counted clause: %w", err)
	}

	return match, nil
}

func (s rarestTermsCountingSearcher) Advance(
	ctx *search.SearchContext,
	id bleveindex.IndexInternalID,
) (*search.DocumentMatch, error) {
	*s.steps++
	match, err := s.Searcher.Advance(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("advance counted clause: %w", err)
	}

	return match, nil
}

func (s rarestTermsCountingSearcher) Close() error {
	*s.closes++
	if err := s.Searcher.Close(); err != nil {
		return fmt.Errorf("close counted clause: %w", err)
	}

	return nil
}

func rarestTermsCountedClauses(terms []string, steps []int, closes []int) []blevequery.Query {
	clauses := rarestTermsTestClauses(terms)
	for clause := range clauses {
		clauses[clause] = rarestTermsCountingQuery{
			Query:  clauses[clause],
			steps:  &steps[clause],
			closes: &closes[clause],
		}
	}

	return clauses
}

func TestRarestTermsMinimumMatchVisitsCommonTermsOnlyAtRareCandidates(t *testing.T) {
	documents := make(map[string]string, 2_200)
	for document := range 2_000 {
		body := "common frequent"
		if document%400 == 0 {
			body += " rare scarce"
		}
		documents[fmt.Sprintf("d%04d", document)] = body
	}
	for document := range 200 {
		documents[fmt.Sprintf("r%04d", document)] = "rare frequent"
	}
	index := rarestTermsTestIndex(t, documents)
	terms := []string{"common", "frequent", "rare", "scarce"}
	oneTermSteps := make([]int, len(terms))
	oneTermScores := rarestTermsTestScores(
		t,
		index,
		minimumMatchQuery(
			rarestTermsCountedClauses(terms, oneTermSteps, make([]int, len(terms))),
			1,
		),
	)
	if len(oneTermScores) != len(documents) || oneTermSteps[1] < 2_000 {
		t.Fatalf("one-term minimum = %d hits, %v steps", len(oneTermScores), oneTermSteps)
	}
	steps := make([]int, len(terms))
	scores := rarestTermsTestScores(
		t,
		index,
		minimumMatchQuery(rarestTermsCountedClauses(terms, steps, make([]int, len(terms))), 3),
	)
	rareCandidates := 205
	if len(scores) != 5 || steps[0] > 2*rareCandidates+2 || steps[1] > 2*len(scores)+2 {
		t.Fatalf("three-term minimum = %d hits, %v steps", len(scores), steps)
	}
}

func TestMinimumMatchQueryKeepsDisjunctionBelowTwoTerms(t *testing.T) {
	clauses := rarestTermsTestClauses([]string{"alpha", "beta", "gamma"})
	for _, minimum := range []int{0, 1} {
		query, ok := minimumMatchQuery(clauses, minimum).(*blevequery.DisjunctionQuery)
		if !ok || query.Min != float64(minimum) || len(query.Disjuncts) != len(clauses) {
			t.Fatalf("minimum %d query = %#v", minimum, query)
		}
	}
	driven, ok := minimumMatchQuery(clauses, 2).(rarestTermsMinimumMatchQuery)
	if !ok || driven.minimum != 2 || len(driven.clauses) != len(clauses) {
		t.Fatalf("two-term minimum query = %#v", driven)
	}
}

type rarestTermsFailingQuery struct {
	blevequery.Query
	failure error
}

func (query rarestTermsFailingQuery) Searcher(
	context.Context,
	bleveindex.IndexReader,
	mapping.IndexMapping,
	search.SearcherOptions,
) (search.Searcher, error) {
	return nil, query.failure
}

type rarestTermsFaultSearcher struct {
	search.Searcher
	nextFailure    error
	advanceFailure error
	closeFailure   error
}

func (s rarestTermsFaultSearcher) Next(ctx *search.SearchContext) (*search.DocumentMatch, error) {
	if s.nextFailure != nil {
		return nil, s.nextFailure
	}
	match, err := s.Searcher.Next(ctx)
	if err != nil {
		return nil, fmt.Errorf("next fault clause: %w", err)
	}

	return match, nil
}

func (s rarestTermsFaultSearcher) Advance(
	ctx *search.SearchContext,
	id bleveindex.IndexInternalID,
) (*search.DocumentMatch, error) {
	if s.advanceFailure != nil {
		return nil, s.advanceFailure
	}
	match, err := s.Searcher.Advance(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("advance fault clause: %w", err)
	}

	return match, nil
}

func (s rarestTermsFaultSearcher) Close() error {
	if err := s.Searcher.Close(); err != nil {
		return fmt.Errorf("close fault clause: %w", err)
	}

	return s.closeFailure
}

func TestRarestTermsMinimumMatchClosesOpenedClausesWhenOpeningFails(t *testing.T) {
	index := rarestTermsTestIndex(t, map[string]string{"one": "alpha beta"})
	wantFailure := errors.New("clause unavailable")
	closes := make([]int, 2)
	clauses := rarestTermsCountedClauses([]string{"alpha", "beta"}, make([]int, 2), closes)
	clauses = append(clauses, rarestTermsFailingQuery{failure: wantFailure})
	request := bleve.NewSearchRequest(minimumMatchQuery(clauses, 2))
	if _, err := index.SearchInContext(t.Context(), request); !errors.Is(err, wantFailure) {
		t.Fatalf("opening failure = %v", err)
	}
	if !slices.Equal(closes, []int{1, 1}) {
		t.Fatalf("opened clauses closed = %v, want each once", closes)
	}
}

type rarestTermsFault struct {
	clause         int
	nextFailure    bool
	advanceFailure bool
	advance        bool
}

func openRarestTermsFaultSearcher(
	t *testing.T,
	index bleve.Index,
	fault rarestTermsFault,
	failure error,
) *rarestTermsMinimumMatchSearcher {
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
	clauses := make([]search.Searcher, 0, 2)
	for position, clause := range rarestTermsTestClauses([]string{"alpha", "beta"}) {
		opened, err := clause.Searcher(
			t.Context(),
			reader,
			index.Mapping(),
			search.SearcherOptions{},
		)
		if err != nil {
			t.Fatal(err)
		}
		if position == fault.clause {
			faulty := rarestTermsFaultSearcher{Searcher: opened, closeFailure: failure}
			if fault.nextFailure {
				faulty.nextFailure = failure
			}
			if fault.advanceFailure {
				faulty.advanceFailure = failure
			}
			opened = faulty
		}
		clauses = append(clauses, opened)
	}

	return newRarestTermsMinimumMatchSearcher(clauses, 2, search.SearcherOptions{})
}

func TestRarestTermsMinimumMatchSearcherReportsClauseFailures(t *testing.T) {
	index := rarestTermsTestIndex(t, map[string]string{"one": "alpha beta", "two": "beta"})
	wantFailure := errors.New("clause failed")
	beyondEveryDocument := bleveindex.IndexInternalID{0xff}
	for _, fault := range []rarestTermsFault{
		{clause: 0, nextFailure: true},
		{clause: 0, nextFailure: true, advance: true},
		{clause: 0, advanceFailure: true, advance: true},
		{clause: 1, advanceFailure: true},
		{clause: 1, nextFailure: true},
	} {
		searcher := openRarestTermsFaultSearcher(t, index, fault, wantFailure)
		searchContext := &search.SearchContext{DocumentMatchPool: search.NewDocumentMatchPool(8, 0)}
		var err error
		if fault.advance {
			_, err = searcher.Advance(searchContext, beyondEveryDocument)
		} else {
			_, err = searcher.Next(searchContext)
		}
		if !errors.Is(err, wantFailure) {
			t.Fatalf("fault %#v = %v", fault, err)
		}
		if err := searcher.Close(); !errors.Is(err, wantFailure) {
			t.Fatalf("fault %#v close = %v", fault, err)
		}
	}
	searcher := openRarestTermsFaultSearcher(t, index, rarestTermsFault{clause: -1}, wantFailure)
	if searcher.Min() != 2 || searcher.Count() != 3 || searcher.Size() <= 0 ||
		searcher.DocumentMatchPoolSize() < 2 {
		t.Fatalf(
			"searcher shape = min %d, count %d, size %d, pool %d",
			searcher.Min(),
			searcher.Count(),
			searcher.Size(),
			searcher.DocumentMatchPoolSize(),
		)
	}
	if err := searcher.Close(); err != nil {
		t.Fatal(err)
	}
}

type rarestTermsFrequencyProbe struct {
	search.Searcher
	frequencyReads *int
}

func (probe rarestTermsFrequencyProbe) Count() uint64 {
	*probe.frequencyReads++

	return probe.Searcher.Count()
}

func TestRarestTermsMinimumMatchReadsEachClauseFrequencyOnceInRarityOrder(t *testing.T) {
	index := rarestTermsTestIndex(t, map[string]string{
		"one":   "common tiea tieb rare",
		"two":   "common tiea tieb",
		"three": "common",
	})
	advanced, err := index.Advanced()
	if err != nil {
		t.Fatal(err)
	}
	reader, err := advanced.Reader()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reader.Close() })
	frequencyReads := make([]int, 4)
	clauses := make([]search.Searcher, 0, 4)
	for position, clause := range rarestTermsTestClauses([]string{"common", "tiea", "tieb", "rare"}) {
		opened, err := clause.Searcher(
			t.Context(),
			reader,
			index.Mapping(),
			search.SearcherOptions{},
		)
		if err != nil {
			t.Fatal(err)
		}
		clauses = append(clauses, rarestTermsFrequencyProbe{
			Searcher:       opened,
			frequencyReads: &frequencyReads[position],
		})
	}
	searcher := newRarestTermsMinimumMatchSearcher(clauses, 2, search.SearcherOptions{})
	if !slices.Equal(searcher.rarity, []int{3, 1, 2, 0}) {
		t.Fatalf("clauses by rarity = %v", searcher.rarity)
	}
	for range 3 {
		if count := searcher.Count(); count != 8 {
			t.Fatalf("searcher count = %d, want 8", count)
		}
	}
	if !slices.Equal(frequencyReads, []int{1, 1, 1, 1}) {
		t.Fatalf("clause frequency reads = %v, want each once", frequencyReads)
	}
	if err := searcher.Close(); err != nil {
		t.Fatal(err)
	}
}
