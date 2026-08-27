package searchindex

import (
	"context"
	"fmt"

	"github.com/blevesearch/bleve/v2"
)

func (b *BleveDiskIndex) searchRequestedHits(
	ctx context.Context,
	req SearchRequest,
	indexedDocuments int,
) (SearchResultSet, []string, error) {
	requestedSize := diskRequestedSearchSize(req.MaxResults, indexedDocuments)
	result, err := b.searchRequestedHitPage(ctx, req, requestedSize)
	if err != nil {
		return SearchResultSet{}, nil, err
	}
	set, orphans, err := b.collectHits(ctx, req, result)
	if err != nil {
		return SearchResultSet{}, nil, err
	}
	expandedSize := diskSearchSize(req.MaxResults, indexedDocuments)
	if len(orphans) == 0 ||
		result.Total <= uint64(len(result.Hits)) ||
		expandedSize <= requestedSize {
		return set, orphans, nil
	}

	result, err = b.searchRequestedHitPage(ctx, req, expandedSize)
	if err != nil {
		return SearchResultSet{}, nil, err
	}

	return b.collectHits(ctx, req, result)
}

func (b *BleveDiskIndex) searchRequestedHitPage(
	ctx context.Context,
	req SearchRequest,
	size int,
) (*bleve.SearchResult, error) {
	searchRequest := bleve.NewSearchRequest(bleveSearchQuery(
		req,
		b.multilingual,
		b.analyzerScope,
	))
	searchRequest.Size = size
	searchRequest.Explain = req.Explain || req.IncludeFieldScores
	searchRequest.IncludeLocations = false
	searchRequest.Fields = storedSearchFields(req, b.storedCandidates)
	result, err := b.alias.SearchInContext(ctx, searchRequest)
	if err != nil {
		return nil, fmt.Errorf(
			"search documents: %w",
			bleveSearchOperationError(ctx, err),
		)
	}
	if err := bleveSearchCompletionError(ctx, result); err != nil {
		return nil, fmt.Errorf("search documents: %w", err)
	}

	return result, nil
}
