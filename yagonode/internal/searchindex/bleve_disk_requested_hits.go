package searchindex

import (
	"context"

	"github.com/blevesearch/bleve/v2"
)

func (b *BleveDiskIndex) searchRequestedHits(
	ctx context.Context,
	req SearchRequest,
	indexedDocuments int,
) (SearchResultSet, []string, error) {
	requestedSize := diskRequestedSearchSize(req.MaxResults, indexedDocuments)
	result, sampled, err := b.searchRequestedHitPageWithStatus(ctx, req, requestedSize)
	if err != nil {
		return SearchResultSet{}, nil, err
	}
	set, orphans, err := b.collectHits(ctx, req, result)
	if err != nil {
		return SearchResultSet{}, nil, err
	}
	set.Incomplete = sampled
	expandedSize := diskSearchSize(req.MaxResults, indexedDocuments)
	if len(orphans) == 0 ||
		result.Total <= uint64(len(result.Hits)) ||
		expandedSize <= requestedSize {
		return set, orphans, nil
	}

	result, expandedSampled, err := b.searchRequestedHitPageWithStatus(ctx, req, expandedSize)
	if err != nil {
		return SearchResultSet{}, nil, err
	}
	set, orphans, err = b.collectHits(ctx, req, result)
	set.Incomplete = sampled || expandedSampled

	return set, orphans, err
}

func (b *BleveDiskIndex) searchRequestedHitPageWithStatus(
	ctx context.Context,
	req SearchRequest,
	size int,
) (*bleve.SearchResult, bool, error) {
	return b.searchLexicalCandidateHitPageWithStatus(ctx, req, size)
}
