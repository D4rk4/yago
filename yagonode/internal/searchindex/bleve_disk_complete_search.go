package searchindex

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/blevesearch/bleve/v2"
)

const (
	completeDiskSearchMaximumDuration = 5 * time.Second
	completeDiskSearchMaximumHits     = 100_000
)

var ErrCompleteSearchBudgetExceeded = errors.New("complete search scan budget exceeded")

func (b *BleveDiskIndex) searchCompleteHits(
	ctx context.Context,
	req SearchRequest,
	indexedDocuments int,
) (SearchResultSet, []string, error) {
	return b.searchCompleteHitsWithin(
		ctx, req, indexedDocuments, completeDiskSearchMaximumHits,
	)
}

func (b *BleveDiskIndex) searchCompleteHitsWithin(
	ctx context.Context,
	req SearchRequest,
	indexedDocuments int,
	maximumHits int,
) (SearchResultSet, []string, error) {
	ctx, cancel := context.WithTimeoutCause(
		ctx,
		completeDiskSearchMaximumDuration,
		ErrCompleteSearchBudgetExceeded,
	)
	defer cancel()
	accumulator := completeSearchAccumulator{
		results: make([]SearchResult, 0, min(req.MaxResults, indexedDocuments)),
		facets:  newFacetCollector(req.WithFacets),
	}
	offset := 0
	matchedDocuments := indexedDocuments
	matchedDocumentsKnown := false
	var searchAfter []string
	for !matchedDocumentsKnown || offset < matchedDocuments {
		remainingDocuments := indexedDocuments - offset
		if matchedDocumentsKnown {
			remainingDocuments = matchedDocuments - offset
		}
		pageSize := min(bleveSearchHitCap, remainingDocuments, maximumHits-offset)
		page, err := b.completeSearchPage(ctx, req, pageSize, searchAfter)
		if err != nil {
			return SearchResultSet{}, nil, err
		}
		if !matchedDocumentsKnown {
			matchedDocuments, err = completeSearchMatchTotal(page.Total, maximumHits)
			if err != nil {
				return SearchResultSet{}, nil, err
			}
			matchedDocumentsKnown = true
		}
		if err := completeSearchPageProgress(
			offset, matchedDocuments, len(page.Hits), pageSize,
		); err != nil {
			return SearchResultSet{}, nil, err
		}
		if len(page.Hits) == 0 {
			break
		}
		if err := accumulator.collect(ctx, b, req, page); err != nil {
			return SearchResultSet{}, nil, err
		}
		offset += len(page.Hits)
		searchAfter = append(searchAfter[:0], page.Hits[len(page.Hits)-1].DecodedSort...)
		if len(page.Hits) < pageSize {
			break
		}
	}

	return SearchResultSet{
		Results: accumulator.results,
		Total:   accumulator.total,
		Facets:  accumulator.facets.groups(),
	}, accumulator.orphans, nil
}

type completeSearchAccumulator struct {
	results []SearchResult
	orphans []string
	total   int
	facets  *facetCollector
}

func (b *BleveDiskIndex) completeSearchPage(
	ctx context.Context,
	req SearchRequest,
	pageSize int,
	searchAfter []string,
) (*bleve.SearchResult, error) {
	searchRequest := bleve.NewSearchRequest(bleveSearchQuery(req, b.gram, b.multilingual))
	searchRequest.Size = pageSize
	searchRequest.SortBy([]string{"_id"})
	searchRequest.SetSearchAfter(searchAfter)
	searchRequest.Explain = req.Explain || req.IncludeFieldScores
	searchRequest.IncludeLocations = req.IncludePositions
	page, err := b.alias.SearchInContext(ctx, searchRequest)
	if err != nil {
		return nil, fmt.Errorf(
			"search documents: %w",
			bleveSearchOperationError(ctx, err),
		)
	}
	if err := bleveSearchCompletionError(ctx, page); err != nil {
		return nil, fmt.Errorf("search documents: %w", err)
	}

	return page, nil
}

func completeSearchMatchTotal(total uint64, maximumHits int) (int, error) {
	matchedDocuments := bleveDocumentCount(total)
	if matchedDocuments > maximumHits {
		return 0, ErrCompleteSearchBudgetExceeded
	}

	return matchedDocuments, nil
}

func completeSearchPageProgress(
	offset int,
	matchedDocuments int,
	hits int,
	pageSize int,
) error {
	remaining := matchedDocuments - offset
	if remaining > 0 && (hits == 0 || hits < min(pageSize, remaining)) {
		return errIncompleteBleveSearch
	}

	return nil
}

func (accumulator *completeSearchAccumulator) collect(
	ctx context.Context,
	index *BleveDiskIndex,
	req SearchRequest,
	page *bleve.SearchResult,
) error {
	for _, hit := range page.Hits {
		doc, found, err := index.documents.Document(ctx, hit.ID)
		if err != nil {
			return fmt.Errorf("load search document: %w", err)
		}
		if !found {
			accumulator.orphans = append(accumulator.orphans, hit.ID)

			continue
		}
		if !allowsDocument(doc, req) {
			continue
		}
		accumulator.facets.observe(doc)
		accumulator.total++
		accumulator.results = insertCompleteResult(
			accumulator.results,
			searchResultFromDocument(hit, doc, req),
			req.MaxResults,
		)
	}

	return nil
}

func insertCompleteResult(
	results []SearchResult,
	candidate SearchResult,
	limit int,
) []SearchResult {
	if limit <= 0 {
		return nil
	}
	position := sort.Search(len(results), func(index int) bool {
		if results[index].Score != candidate.Score {
			return results[index].Score < candidate.Score
		}

		return results[index].DocumentID > candidate.DocumentID
	})
	if position >= limit {
		return results
	}
	results = append(results, SearchResult{})
	copy(results[position+1:], results[position:])
	results[position] = candidate
	if len(results) > limit {
		results = results[:limit]
	}

	return results
}
