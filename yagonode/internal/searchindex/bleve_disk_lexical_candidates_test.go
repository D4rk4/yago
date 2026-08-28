package searchindex

import (
	"context"
	"errors"
	stdmaps "maps"
	"math"
	"slices"
	"testing"

	"github.com/blevesearch/bleve/v2"
	blevequery "github.com/blevesearch/bleve/v2/search/query"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type bleveLexicalCandidatePageProbe struct {
	bleveIndexContract
	requests []*bleve.SearchRequest
	result   *bleve.SearchResult
	err      error
}

func (probe *bleveLexicalCandidatePageProbe) SearchInContext(
	_ context.Context,
	request *bleve.SearchRequest,
) (*bleve.SearchResult, error) {
	probe.requests = append(probe.requests, request)

	return probe.result, probe.err
}

func TestBleveDiskLexicalCandidatePageUsesOneSnapshotQuery(t *testing.T) {
	result := &bleve.SearchResult{
		Status: &bleve.SearchStatus{Total: 1, Successful: 1},
	}
	probe := &bleveLexicalCandidatePageProbe{result: result}
	index := &BleveDiskIndex{alias: probe, analyzerScope: true, multilingual: true}
	request := SearchRequest{Query: "needle", Terms: []string{"needle"}, MaxResults: 2}
	got, err := index.searchLexicalCandidateHitPage(t.Context(), request, 2)
	if err != nil || got != result || len(probe.requests) != 1 {
		t.Fatalf("result=%p/%p requests=%d error=%v", got, result, len(probe.requests), err)
	}
	bounded, ok := bleveSearchDeadlineInnerQuery(
		t,
		probe.requests[0].Query,
	).(*blevequery.ConjunctionQuery)
	if !ok || len(bounded.Conjuncts) != 2 {
		t.Fatalf("bounded query=%T/%#v", probe.requests[0].Query, probe.requests[0].Query)
	}
	if _, ok := bounded.Conjuncts[1].(*bleveLexicalCandidateSnapshotQuery); !ok ||
		probe.requests[0].Size != 2 || probe.requests[0].Score == bleve.ScoreNone {
		t.Fatalf("bounded request=%#v", probe.requests[0])
	}

	probe.requests = nil
	index.analyzerScope = false
	if _, err := index.searchLexicalCandidateHitPage(t.Context(), request, 2); err != nil ||
		len(probe.requests) != 1 {
		t.Fatalf("unscoped requests=%d error=%v", len(probe.requests), err)
	}
	if _, ok := bleveSearchDeadlineInnerQuery(
		t,
		probe.requests[0].Query,
	).(*bleveLexicalCandidateSnapshotQuery); ok {
		t.Fatalf("unscoped query=%T", probe.requests[0].Query)
	}

	probe.requests = nil
	index.analyzerScope = true
	request.Explain = true
	if _, err := index.searchLexicalCandidateHitPage(t.Context(), request, 2); err != nil ||
		len(probe.requests) != 1 || !probe.requests[0].Explain {
		t.Fatalf("explained requests=%#v error=%v", probe.requests, err)
	}
	if conjunction, ok := bleveSearchDeadlineInnerQuery(
		t,
		probe.requests[0].Query,
	).(*blevequery.ConjunctionQuery); ok &&
		len(conjunction.Conjuncts) == 2 {
		if _, candidate := conjunction.Conjuncts[1].(*bleveLexicalCandidateSnapshotQuery); candidate {
			t.Fatalf("explained query=%T", probe.requests[0].Query)
		}
	}
}

func bleveSearchDeadlineInnerQuery(
	t *testing.T,
	query blevequery.Query,
) blevequery.Query {
	t.Helper()
	bounded, ok := query.(bleveSearchDeadlineQuery)
	if !ok {
		t.Fatalf("deadline query=%T", query)
	}

	return bounded.inner
}

func TestBleveDiskLexicalCandidatePageReturnsAliasFailures(t *testing.T) {
	sentinel := errors.New("alias failed")
	probe := &bleveLexicalCandidatePageProbe{err: sentinel}
	index := &BleveDiskIndex{alias: probe, analyzerScope: true}
	request := SearchRequest{Query: "needle", Terms: []string{"needle"}, MaxResults: 1}
	if _, err := index.searchLexicalCandidateHitPage(
		t.Context(), request, 1,
	); !errors.Is(err, sentinel) {
		t.Fatalf("alias error=%v", err)
	}

	probe.err = nil
	probe.result = &bleve.SearchResult{Status: &bleve.SearchStatus{Total: 2, Failed: 1}}
	if _, err := index.searchLexicalCandidateHitPage(
		t.Context(), request, 1,
	); !errors.Is(err, errIncompleteBleveSearch) {
		t.Fatalf("incomplete error=%v", err)
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
