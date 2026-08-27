package searchindex

import (
	"context"
	"errors"
	stdmaps "maps"
	"math"
	"slices"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type lexicalCandidateSearchProbe struct {
	bleveIndexContract
	results  []*bleve.SearchResult
	errors   []error
	requests []*bleve.SearchRequest
}

func (probe *lexicalCandidateSearchProbe) SearchInContext(
	_ context.Context,
	request *bleve.SearchRequest,
) (*bleve.SearchResult, error) {
	position := len(probe.requests)
	probe.requests = append(probe.requests, request)
	if position < len(probe.errors) && probe.errors[position] != nil {
		return nil, probe.errors[position]
	}

	return probe.results[position], nil
}

func TestCompleteLexicalCandidateSetAcceptsOnlyExhaustedResults(t *testing.T) {
	query := bleve.NewMatchAllQuery()
	acceptedProbe := &lexicalCandidateSearchProbe{results: []*bleve.SearchResult{
		lexicalCandidateProbeResult(2, "one", "two"),
	}}
	accepted, complete, err := completeLexicalCandidateSetWithin(
		t.Context(), acceptedProbe, query, 2,
	)
	if err != nil || !complete || len(accepted.Hits) != 2 {
		t.Fatalf("accepted=%#v complete=%t error=%v", accepted, complete, err)
	}
	if len(acceptedProbe.requests) != 1 || acceptedProbe.requests[0].Size != 3 ||
		acceptedProbe.requests[0].Score != bleve.ScoreNone {
		t.Fatalf("candidate request=%#v", acceptedProbe.requests)
	}

	for name, result := range map[string]*bleve.SearchResult{
		"sentinel":     lexicalCandidateProbeResult(3, "one", "two", "three"),
		"unknown tail": lexicalCandidateProbeResult(3, "one", "two"),
	} {
		t.Run(name, func(t *testing.T) {
			probe := &lexicalCandidateSearchProbe{results: []*bleve.SearchResult{result}}
			candidates, complete, err := completeLexicalCandidateSetWithin(
				t.Context(), probe, query, 2,
			)
			if err != nil || complete || candidates != nil {
				t.Fatalf("candidates=%#v complete=%t error=%v", candidates, complete, err)
			}
		})
	}
}

func TestCompleteLexicalCandidateSetReturnsSearchFailures(t *testing.T) {
	sentinel := errors.New("candidate read failed")
	probe := &lexicalCandidateSearchProbe{errors: []error{sentinel}}
	_, _, err := completeLexicalCandidateSetWithin(
		t.Context(), probe, bleve.NewMatchAllQuery(), 2,
	)
	if !errors.Is(err, sentinel) {
		t.Fatalf("candidate error=%v, want=%v", err, sentinel)
	}

	canceled, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("request budget elapsed")
	cancel(cause)
	probe = &lexicalCandidateSearchProbe{errors: []error{context.Canceled}}
	_, _, err = completeLexicalCandidateSetWithin(
		canceled, probe, bleve.NewMatchAllQuery(), 2,
	)
	if !errors.Is(err, cause) {
		t.Fatalf("canceled candidate error=%v, want=%v", err, cause)
	}

	probe = &lexicalCandidateSearchProbe{results: []*bleve.SearchResult{{
		Status: &bleve.SearchStatus{Total: 2, Failed: 1},
	}}}
	_, _, err = completeLexicalCandidateSetWithin(
		t.Context(), probe, bleve.NewMatchAllQuery(), 2,
	)
	if !errors.Is(err, errIncompleteBleveSearch) {
		t.Fatalf("incomplete candidate error=%v", err)
	}
}

func TestBleveDiskLexicalCandidatePageSelectsBoundedWorkflow(t *testing.T) {
	request := SearchRequest{Query: "needle", Terms: []string{"needle"}, MaxResults: 2}
	accepted := &lexicalCandidateSearchProbe{results: []*bleve.SearchResult{
		lexicalCandidateProbeResult(2, "one", "two"),
		lexicalCandidateProbeResult(1, "one"),
	}}
	index := &BleveDiskIndex{alias: accepted, analyzerScope: true}
	result, err := index.searchLexicalCandidateHitPage(t.Context(), request, 2)
	if err != nil || result.Total != 1 || len(accepted.requests) != 2 {
		t.Fatalf("result=%#v requests=%d error=%v", result, len(accepted.requests), err)
	}
	if accepted.requests[0].Score != bleve.ScoreNone ||
		accepted.requests[1].Score == bleve.ScoreNone || accepted.requests[1].Size != 2 {
		t.Fatalf("accepted requests=%#v", accepted.requests)
	}

	refused := &lexicalCandidateSearchProbe{results: []*bleve.SearchResult{
		lexicalCandidateProbeResult(2, "one"),
		lexicalCandidateProbeResult(1, "two"),
	}}
	index.alias = refused
	result, err = index.searchLexicalCandidateHitPage(t.Context(), request, 2)
	if err != nil || result.Total != 1 || len(refused.requests) != 2 ||
		refused.requests[1].Size != 2 {
		t.Fatalf("fallback result=%#v requests=%#v error=%v", result, refused.requests, err)
	}

	empty := &lexicalCandidateSearchProbe{results: []*bleve.SearchResult{
		lexicalCandidateProbeResult(0),
	}}
	index.alias = empty
	result, err = index.searchLexicalCandidateHitPage(t.Context(), request, 2)
	if err != nil || result.Total != 0 || len(empty.requests) != 1 {
		t.Fatalf("empty result=%#v requests=%d error=%v", result, len(empty.requests), err)
	}

	explained := &lexicalCandidateSearchProbe{results: []*bleve.SearchResult{
		lexicalCandidateProbeResult(1, "one"),
	}}
	index.alias = explained
	request.Explain = true
	result, err = index.searchLexicalCandidateHitPage(t.Context(), request, 2)
	if err != nil || result.Total != 1 || len(explained.requests) != 1 ||
		!explained.requests[0].Explain || explained.requests[0].Score == bleve.ScoreNone {
		t.Fatalf("explained result=%#v requests=%#v error=%v", result, explained.requests, err)
	}
}

func TestBleveDiskLexicalCandidatePageRejectsIncompleteRerank(t *testing.T) {
	probe := &lexicalCandidateSearchProbe{results: []*bleve.SearchResult{
		lexicalCandidateProbeResult(1, "one"),
		{Status: &bleve.SearchStatus{Total: 2, Failed: 1}},
	}}
	index := &BleveDiskIndex{alias: probe, analyzerScope: true}
	_, err := index.searchLexicalCandidateHitPage(
		t.Context(),
		SearchRequest{Query: "needle", Terms: []string{"needle"}, MaxResults: 1},
		1,
	)
	if !errors.Is(err, errIncompleteBleveSearch) {
		t.Fatalf("incomplete rerank error=%v", err)
	}
}

func TestBleveDiskLexicalCandidatesPreserveScopedSearch(t *testing.T) {
	documents := lexicalCandidateDocuments()
	index, err := NewBleveDiskIndex(
		t.Context(),
		t.TempDir(),
		newFakeDocumentDirectory(documents...),
		&fakeStoredDocuments{documents: documents},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = index.Close() })

	requests := []SearchRequest{
		{Query: "the cat", Terms: []string{"the", "cat"}, MaxResults: 10},
		{Query: "die hard", Terms: []string{"die", "hard"}, MaxResults: 10},
		{Query: "economics", Terms: []string{"economics"}, MaxResults: 10},
		{
			Query:         "economics",
			Terms:         []string{"economics"},
			IncludeDomain: []string{"example.org"},
			MaxResults:    10,
		},
		{
			Query:      "economics archaeology meteorology",
			Terms:      []string{"economics", "archaeology", "meteorology"},
			MaxResults: 10,
			Relaxed:    true,
		},
		{
			Query:              "economics",
			Terms:              []string{"economics"},
			ExpansionTerms:     []string{"markets"},
			ExcludeTerms:       []string{"obsolete"},
			MaxResults:         10,
			IncludeFieldScores: true,
		},
		{
			Query:      "economics markets",
			Terms:      []string{"economics", "markets"},
			MaxResults: 10,
			Explain:    true,
		},
		{Query: "golnag", Terms: []string{"golnag"}, MaxResults: 10, Fuzzy: true},
		{Query: "honest-empty", Terms: []string{"honest-empty"}, MaxResults: 10},
	}
	for _, request := range requests {
		t.Run(request.Query, func(t *testing.T) {
			optimized, err := index.searchLexicalCandidateHitPage(
				t.Context(), request, request.MaxResults,
			)
			if err != nil {
				t.Fatal(err)
			}
			exhaustive, err := index.executeRequestedHitPage(
				t.Context(),
				request,
				bleveSearchQuery(request, index.multilingual, index.analyzerScope),
				request.MaxResults,
			)
			if err != nil {
				t.Fatal(err)
			}
			assertEquivalentBleveResults(t, request, optimized, exhaustive)
		})
	}
}

func TestDistinctLexicalCandidateTermsPreserveFirstSurface(t *testing.T) {
	request := SearchRequest{Query: "fallback query", Terms: []string{" alpha ", "alpha", ""}}
	if got := distinctLexicalCandidateTerms(request); !slices.Equal(got, []string{"alpha"}) {
		t.Fatalf("distinct terms=%v", got)
	}
	request.Terms = []string{"", " "}
	got := distinctLexicalCandidateTerms(request)
	if !slices.Equal(got, []string{"fallback query"}) {
		t.Fatalf("fallback terms=%v", got)
	}
}

func lexicalCandidateProbeResult(
	total uint64,
	identities ...string,
) *bleve.SearchResult {
	hits := make(search.DocumentMatchCollection, len(identities))
	for position, identity := range identities {
		hits[position] = &search.DocumentMatch{
			ID:    identity,
			Score: float64(len(identities) - position),
		}
	}

	return &bleve.SearchResult{
		Status: &bleve.SearchStatus{Total: 1, Successful: 1},
		Total:  total,
		Hits:   hits,
	}
}

func lexicalCandidateDocuments() []documentstore.Document {
	return []documentstore.Document{
		{
			NormalizedURL: "https://example.org/cat",
			Title:         "Quiet cat",
			ExtractedText: "A cat sleeps quietly beside economics notes.",
			Language:      "en",
		},
		{
			NormalizedURL: "https://example.org/die-hard",
			Title:         "Die Hard cat",
			ExtractedText: "The film includes economics and markets.",
			Language:      "en",
		},
		{
			NormalizedURL: "https://example.org/german",
			Title:         "Die Katze",
			ExtractedText: "Die Katze betrachtet archaeology research.",
			Language:      "de",
		},
		{
			NormalizedURL: "https://example.org/weather",
			Headings:      []string{"Meteorology"},
			ExtractedText: "Economics archaeology and meteorology.",
			Language:      "en",
		},
		{
			NormalizedURL: "https://example.org/obsolete",
			ExtractedText: "Obsolete economics material.",
			Language:      "en",
		},
		{
			NormalizedURL: "https://example.org/golang",
			Title:         "Golang",
			ExtractedText: "Golang systems programming.",
			Language:      "en",
		},
		{
			NormalizedURL: "https://outside.test/economics",
			Title:         "Economics outside the selected domain",
			ExtractedText: "Economics and markets.",
			Language:      "en",
		},
	}
}

func assertEquivalentBleveResults(
	t *testing.T,
	request SearchRequest,
	got *bleve.SearchResult,
	want *bleve.SearchResult,
) {
	t.Helper()
	if got.Total != want.Total || len(got.Hits) != len(want.Hits) {
		t.Fatalf(
			"result size=%d/%d, want=%d/%d",
			len(got.Hits),
			got.Total,
			len(want.Hits),
			want.Total,
		)
	}
	for position := range want.Hits {
		if got.Hits[position].ID != want.Hits[position].ID ||
			math.Abs(got.Hits[position].Score-want.Hits[position].Score) > 1e-12 {
			t.Fatalf(
				"hit %d=%s/%v, want=%s/%v",
				position,
				got.Hits[position].ID,
				got.Hits[position].Score,
				want.Hits[position].ID,
				want.Hits[position].Score,
			)
		}
		gotFieldScores := hitFieldScores(request, got.Hits[position])
		wantFieldScores := hitFieldScores(request, want.Hits[position])
		if !equalBleveFieldScores(gotFieldScores, wantFieldScores) {
			t.Fatalf(
				"hit %d field scores=%v, want=%v",
				position,
				gotFieldScores,
				wantFieldScores,
			)
		}
		if request.Explain &&
			got.Hits[position].Expl.String() != want.Hits[position].Expl.String() {
			t.Fatalf(
				"hit %d explanation differs\ngot: %s\nwant: %s",
				position,
				got.Hits[position].Expl.String(),
				want.Hits[position].Expl.String(),
			)
		}
	}
}

func equalBleveFieldScores(got map[string]float64, want map[string]float64) bool {
	return stdmaps.EqualFunc(got, want, func(gotScore, wantScore float64) bool {
		return math.Abs(gotScore-wantScore) <= 1e-12
	})
}
