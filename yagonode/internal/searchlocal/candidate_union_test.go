package searchlocal

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type candidateIndex struct {
	strict    searchindex.SearchResultSet
	relaxed   searchindex.SearchResultSet
	strictErr error
	relaxErr  error
	requests  []searchindex.SearchRequest
}

type deferredEvidenceCandidateIndex struct {
	candidateIndex
	evidenceCalls int
	evidenceReq   searchindex.SearchRequest
	evidenceInput []searchindex.SearchResult
}

func (d *deferredEvidenceCandidateIndex) SearchEvidence(
	_ context.Context,
	req searchindex.SearchRequest,
	results []searchindex.SearchResult,
) ([]searchindex.SearchResult, error) {
	d.evidenceCalls++
	d.evidenceReq = req
	d.evidenceInput = append([]searchindex.SearchResult(nil), results...)
	for index := range results {
		results[index].Snippet = "matched evidence"
	}

	return results, nil
}

func (c *candidateIndex) Index(context.Context, documentstore.Document) error { return nil }
func (c *candidateIndex) Delete(context.Context, string) error                { return nil }
func (c *candidateIndex) Stats(context.Context) (searchindex.IndexStats, error) {
	return searchindex.IndexStats{}, nil
}

func (c *candidateIndex) Search(
	_ context.Context,
	req searchindex.SearchRequest,
) (searchindex.SearchResultSet, error) {
	c.requests = append(c.requests, req)
	if req.MinimumTermMatches > 0 {
		return c.relaxed, c.relaxErr
	}

	return c.strict, c.strictErr
}

func TestSearchCandidatesFusesStrictAndRelaxedBranches(t *testing.T) {
	strictOnly := searchindex.SearchResult{
		DocumentID: "strict", URL: "https://example.org/strict", Score: 8,
	}
	sharedStrict := searchindex.SearchResult{
		DocumentID: "shared", URL: "https://example.org/shared", Score: 7,
	}
	sharedRelaxed := sharedStrict
	sharedRelaxed.Score = 6
	relaxedOnly := searchindex.SearchResult{
		URL: "https://example.org/relaxed", Score: 5,
	}
	facets := []searchindex.FacetGroup{{Name: "host"}}
	index := &candidateIndex{
		strict: searchindex.SearchResultSet{
			Results: []searchindex.SearchResult{strictOnly, sharedStrict, strictOnly}, Total: 2,
		},
		relaxed: searchindex.SearchResultSet{
			Results: []searchindex.SearchResult{
				sharedRelaxed,
				relaxedOnly,
			},
			Total:  3,
			Facets: facets,
		},
	}
	set, err := (localSearcher{index: index}).searchCandidates(
		t.Context(),
		searchindex.SearchRequest{
			Query: "alpha beta", Terms: []string{"alpha", "beta"}, MaxResults: 3, WithFacets: true,
		},
	)
	if err != nil {
		t.Fatalf("searchCandidates: %v", err)
	}
	if len(index.requests) != 2 || index.requests[0].WithFacets ||
		index.requests[0].MaxResults != 3 || index.requests[1].MaxResults != 3 ||
		index.requests[1].MinimumTermMatches != 1 || !index.requests[1].WithFacets {
		t.Fatalf("requests = %#v", index.requests)
	}
	if set.Total != 3 || !reflect.DeepEqual(set.Facets, facets) || len(set.Results) != 3 {
		t.Fatalf("set = %#v", set)
	}
	if set.Results[0].DocumentID != "shared" || set.Results[0].StrictRank != 2 ||
		set.Results[0].RelaxedRank != 1 || set.Results[0].StrictScore != 7 ||
		set.Results[0].RelaxedScore != 6 {
		t.Fatalf("shared result = %#v", set.Results[0])
	}
}

func TestSearchCandidatesDefersEvidenceUntilAfterFusion(t *testing.T) {
	index := &deferredEvidenceCandidateIndex{candidateIndex: candidateIndex{
		strict: searchindex.SearchResultSet{Results: []searchindex.SearchResult{
			{DocumentID: "shared", Score: 4},
			{DocumentID: "strict", Score: 3},
		}},
		relaxed: searchindex.SearchResultSet{Results: []searchindex.SearchResult{
			{DocumentID: "shared", Score: 2},
			{DocumentID: "relaxed", Score: 1},
		}},
	}}
	set, err := (localSearcher{index: index}).searchCandidates(
		t.Context(),
		searchindex.SearchRequest{
			Query: "alpha beta", Terms: []string{"alpha", "beta"}, MaxResults: 2,
		},
	)
	if err != nil {
		t.Fatalf("searchCandidates: %v", err)
	}
	if len(index.requests) != 2 || !index.requests[0].CandidateOnly ||
		!index.requests[1].CandidateOnly {
		t.Fatalf("candidate requests = %#v", index.requests)
	}
	if index.evidenceCalls != 1 || index.evidenceReq.CandidateOnly ||
		len(index.evidenceInput) != 2 {
		t.Fatalf(
			"evidence calls=%d req=%#v input=%#v",
			index.evidenceCalls,
			index.evidenceReq,
			index.evidenceInput,
		)
	}
	if len(set.Results) != 2 || set.Results[0].Snippet != "matched evidence" {
		t.Fatalf("results = %#v", set.Results)
	}
}

func TestSearchCandidatesLimitsUnionAndUsesStableTie(t *testing.T) {
	strict := searchindex.SearchResult{DocumentID: "b", Score: 1}
	relaxed := searchindex.SearchResult{DocumentID: "a", Score: 1}
	set := fuseCandidateSets(
		searchindex.SearchResultSet{Results: []searchindex.SearchResult{strict}},
		searchindex.SearchResultSet{Results: []searchindex.SearchResult{relaxed}},
		1,
	)
	if len(set.Results) != 1 || set.Results[0].DocumentID != "a" || set.Total != 1 {
		t.Fatalf("set = %#v", set)
	}
}

func TestSearchCandidatesSkipsRelaxedIneligibleRequests(t *testing.T) {
	requests := []searchindex.SearchRequest{
		{Query: "one"},
		{Terms: []string{"same", "same"}},
		{Terms: []string{"one", "two"}, Fuzzy: true},
		{Terms: []string{"one", "two"}, Near: true},
		{Terms: []string{"one", "two"}, MinimumTermMatches: 1},
	}
	for _, req := range requests {
		index := &candidateIndex{}
		if _, err := (localSearcher{index: index}).searchCandidates(t.Context(), req); err != nil {
			t.Fatalf("searchCandidates(%#v): %v", req, err)
		}
		if len(index.requests) != 1 {
			t.Fatalf("requests for %#v = %#v", req, index.requests)
		}
	}
	if got := relaxedMinimumTermMatches(searchindex.SearchRequest{
		Terms: []string{"one", "two", "three", "four", "five"},
	}); got != 3 {
		t.Fatalf("five-term minimum = %d", got)
	}
}

func TestSearchCandidatesReturnsBranchErrors(t *testing.T) {
	sentinel := errors.New("down")
	index := &candidateIndex{strictErr: sentinel}
	_, err := (localSearcher{index: index}).searchCandidates(t.Context(), searchindex.SearchRequest{
		Terms: []string{"one", "two"},
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("strict error = %v", err)
	}
	index = &candidateIndex{relaxErr: sentinel}
	_, err = (localSearcher{index: index}).searchCandidates(t.Context(), searchindex.SearchRequest{
		Terms: []string{"one", "two"},
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("relaxed error = %v", err)
	}
}

func TestCandidateIdentityFallsBackToURL(t *testing.T) {
	if got := candidateIdentity(
		searchindex.SearchResult{URL: "https://example.org/"},
	); got != "url:https://example.org/" {
		t.Fatalf("identity = %q", got)
	}
}
