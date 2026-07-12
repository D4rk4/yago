package searchindex

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/search"
)

const (
	completeDiskSearchMaximumDuration = 5 * time.Second
	completeDiskSearchMaximumHits     = 100_000
	completeDiskSearchPageHits        = 256
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
		hits:   make([]*search.DocumentMatch, 0, min(req.MaxResults, indexedDocuments)),
		facets: newFacetCollector(req.WithFacets),
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
		pageSize := min(completeDiskSearchPageHits, remainingDocuments, maximumHits-offset)
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

	results, err := b.finalCompleteResults(ctx, req, accumulator.hits)
	if err != nil {
		return SearchResultSet{}, nil, err
	}
	rescoreStoredQuotedPhrasePrefix(results, req)
	rescoreStoredProximity(results, req)

	return SearchResultSet{
		Results: results,
		Total:   accumulator.total,
		Facets:  accumulator.facets.groups(),
	}, accumulator.orphans, nil
}

type completeSearchAccumulator struct {
	hits    []*search.DocumentMatch
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
	searchRequest := bleve.NewSearchRequest(bleveSearchQuery(
		req,
		b.multilingual,
		b.analyzerScope,
	))
	searchRequest.Size = pageSize
	searchRequest.SortBy([]string{"_id"})
	searchRequest.SetSearchAfter(searchAfter)
	searchRequest.Explain = req.Explain || req.IncludeFieldScores
	searchRequest.IncludeLocations = false
	searchRequest.Fields = storedSearchFields(req, b.storedCandidates)
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
		projection, found, err := index.loadSearchHitProjection(ctx, hit, req)
		if err != nil {
			return fmt.Errorf("load search document: %w", err)
		}
		if !found {
			accumulator.orphans = append(accumulator.orphans, hit.ID)

			continue
		}
		if !allowsDocument(projection.document, req) {
			continue
		}
		accumulator.facets.observe(projection.document)
		accumulator.total++
		accumulator.hits = insertCompleteHit(
			accumulator.hits,
			hit,
			req.MaxResults,
		)
	}

	return nil
}

func insertCompleteHit(
	results []*search.DocumentMatch,
	candidate *search.DocumentMatch,
	limit int,
) []*search.DocumentMatch {
	if limit <= 0 {
		return nil
	}
	position := sort.Search(len(results), func(index int) bool {
		if results[index].Score != candidate.Score {
			return results[index].Score < candidate.Score
		}

		return results[index].ID > candidate.ID
	})
	if position >= limit {
		return results
	}
	results = append(results, nil)
	copy(results[position+1:], results[position:])
	results[position] = candidate
	if len(results) > limit {
		results = results[:limit]
	}

	return results
}

func (b *BleveDiskIndex) finalCompleteResults(
	ctx context.Context,
	req SearchRequest,
	hits []*search.DocumentMatch,
) ([]SearchResult, error) {
	results := make([]SearchResult, 0, len(hits))
	for _, hit := range hits {
		projection, found, err := b.loadSearchHitProjection(ctx, hit, req)
		if err != nil {
			return nil, fmt.Errorf("reload final search document: %w", err)
		}
		if !found {
			continue
		}
		mapped, err := projection.result(ctx, hit, req)
		if err != nil {
			return nil, err
		}
		results = append(results, mapped)
	}

	return results, nil
}
