package searchindex

import (
	"context"
	"errors"
	"fmt"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

const maximumSearchEvidenceResults = 10

func (b *BleveMemoryIndex) SearchEvidence(
	ctx context.Context,
	req SearchRequest,
	results []SearchResult,
) ([]SearchResult, error) {
	documents := make([]documentstore.Document, len(results))
	found := make([]bool, len(results))
	b.mu.RLock()
	for index, result := range results {
		documents[index], found[index] = b.documents[result.DocumentID]
	}
	b.mu.RUnlock()

	return searchEvidenceResults(ctx, req, results, func(index int) (
		documentstore.Document,
		bool,
		error,
	) {
		return documents[index], found[index], nil
	})
}

func (b *BleveDiskIndex) SearchEvidence(
	ctx context.Context,
	req SearchRequest,
	results []SearchResult,
) ([]SearchResult, error) {
	b.mu.RLock()
	closed := b.closed
	b.mu.RUnlock()
	if closed {
		return nil, fmt.Errorf("search index closed")
	}

	orphans := make([]string, 0)
	enriched, err := searchEvidenceResults(ctx, req, results, func(index int) (
		documentstore.Document,
		bool,
		error,
	) {
		doc, found, loadErr := b.documents.Document(ctx, results[index].DocumentID)
		if loadErr == nil && !found {
			orphans = append(orphans, results[index].DocumentID)
		}

		if loadErr != nil {
			return documentstore.Document{}, false, fmt.Errorf(
				"load stored search evidence: %w",
				loadErr,
			)
		}

		return doc, found, nil
	})
	b.dropOrphanedEntries(ctx, orphans)

	return enriched, err
}

func searchEvidenceResults(
	ctx context.Context,
	req SearchRequest,
	results []SearchResult,
	document func(int) (documentstore.Document, bool, error),
) ([]SearchResult, error) {
	req.CandidateOnly = false
	enriched := make([]SearchResult, 0, len(results))
	limit := min(maximumSearchEvidenceResults, len(results))
	processedAll := true
	tailStart := limit
	for index, candidate := range results[:limit] {
		if ctx.Err() != nil {
			processedAll = false
			tailStart = index
			break
		}
		mapped, found, err := searchEvidenceResult(ctx, req, candidate, index, document)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				processedAll = false
				tailStart = index
				break
			}

			return nil, err
		}
		if found {
			enriched = append(enriched, mapped)
		}
	}
	rescoreStoredQuotedPhrasePrefix(enriched, req)
	if !processedAll {
		enriched = append(enriched, results[tailStart:]...)
	}
	if processedAll && limit < len(results) {
		enriched = append(enriched, results[limit:]...)
	}
	if processedAll && limit == len(results) && req.IncludePositions {
		rescoreStoredProximity(enriched, req)
	}

	return enriched, nil
}

func searchEvidenceResult(
	ctx context.Context,
	req SearchRequest,
	candidate SearchResult,
	index int,
	document func(int) (documentstore.Document, bool, error),
) (SearchResult, bool, error) {
	doc, found, err := document(index)
	if err != nil {
		return SearchResult{}, false, fmt.Errorf("load search evidence document: %w", err)
	}
	if !found {
		return SearchResult{}, false, nil
	}
	mapped, err := storedEvidenceResult(ctx, req, candidate, doc)
	if err != nil {
		return SearchResult{}, false, err
	}

	return mapped, true, nil
}

func storedEvidenceResult(
	ctx context.Context,
	req SearchRequest,
	candidate SearchResult,
	doc documentstore.Document,
) (SearchResult, error) {
	fields := map[string]interface{}{}
	if candidate.Analyzer != "" {
		fields[documentAnalyzerField] = candidate.Analyzer
	}
	hit := &search.DocumentMatch{
		ID:     candidate.DocumentID,
		Score:  candidate.Score,
		Fields: fields,
	}
	mapped, err := searchResultFromStoredEvidence(ctx, hit, doc, req)
	if err != nil {
		return SearchResult{}, err
	}
	mapped.Explanation = candidate.Explanation
	mapped.FieldScores = candidate.FieldScores
	mapped.StrictScore = candidate.StrictScore
	mapped.StrictRank = candidate.StrictRank
	mapped.RelaxedScore = candidate.RelaxedScore
	mapped.RelaxedRank = candidate.RelaxedRank

	return mapped, nil
}
