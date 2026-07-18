package searchlocal

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/searchindex"
)

type pageEvidenceSearcher struct {
	inner    searchcore.Searcher
	evidence searchindex.SearchEvidenceSource
}

func NewPageEvidenceSearcher(
	inner searchcore.Searcher,
	evidence searchindex.SearchEvidenceSource,
) searchcore.Searcher {
	if inner == nil || evidence == nil {
		return inner
	}

	return pageEvidenceSearcher{inner: inner, evidence: evidence}
}

func (s pageEvidenceSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	response, err := s.inner.Search(ctx, req)
	if err != nil {
		return searchcore.Response{}, fmt.Errorf("page evidence search: %w", err)
	}
	candidates := make([]searchindex.SearchResult, 0, len(response.Results))
	for _, result := range response.Results {
		if !result.StoredLocally() || result.BodyQueryMatches != nil ||
			result.DocumentID == "" {
			continue
		}
		candidates = append(candidates, searchindex.SearchResult{
			DocumentID: result.DocumentID,
			Analyzer:   result.Analyzer,
			Score:      result.Score,
		})
	}
	if len(candidates) == 0 {
		return response, nil
	}
	indexReq := (localSearcher{}).indexRequest(req)
	if response.Recovered != "" {
		indexReq.Fuzzy = true
	}
	enriched, err := s.evidence.SearchEvidence(ctx, indexReq, candidates)
	if err != nil {
		return pageEvidenceFailure(response, err)
	}
	byDocument := make(map[string]searchindex.SearchResult, len(enriched))
	for _, result := range enriched {
		byDocument[result.DocumentID] = result
	}
	queryMatches := newRequestSnippetMatches(req)
	for index := range response.Results {
		result, found := byDocument[response.Results[index].DocumentID]
		if !found || !result.EvidenceReady {
			continue
		}
		response.Results[index].Snippet = result.Snippet
		response.Results[index].QueryMatches = queryMatches.result(result)
		response.Results[index].BodyQueryMatches = coreBodyQueryMatches(
			result.BodyQueryMatches,
		)
		response.Results[index].EvidenceReady = result.EvidenceReady
	}

	return response, nil
}

func pageEvidenceFailure(
	response searchcore.Response,
	err error,
) (searchcore.Response, error) {
	response.PartialFailures = append(response.PartialFailures, searchcore.PartialFailure{
		Source: "local-evidence",
		Reason: err.Error(),
	})

	return response, nil
}
