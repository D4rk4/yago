package searchindex

import (
	"context"

	"github.com/blevesearch/bleve/v2/search"
)

type preparedSearchHitProjections struct {
	hits            []*search.DocumentMatch
	projections     []searchHitProjection
	found           []bool
	pageStart       int
	pageSize        int
	usesSetPresence bool
}

func (b *BleveDiskIndex) prepareSearchHitProjections(
	hits []*search.DocumentMatch,
	req SearchRequest,
) preparedSearchHitProjections {
	return preparedSearchHitProjections{
		hits:            hits,
		pageSize:        max(req.MaxResults, 1),
		usesSetPresence: b.supportsSearchHitProjectionSet(req),
	}
}

func (prepared *preparedSearchHitProjections) load(
	ctx context.Context,
	index *BleveDiskIndex,
	position int,
	hit *search.DocumentMatch,
	req SearchRequest,
) (searchHitProjection, bool, error) {
	if !prepared.usesSetPresence {
		return index.loadSearchHitProjectionIndividually(ctx, hit, req)
	}
	if position >= prepared.pageStart+len(prepared.projections) {
		if err := prepared.loadPage(ctx, index, position, req); err != nil {
			return searchHitProjection{}, false, err
		}
	}
	pagePosition := position - prepared.pageStart

	return prepared.projections[pagePosition], prepared.found[pagePosition], nil
}

func (prepared *preparedSearchHitProjections) loadPage(
	ctx context.Context,
	index *BleveDiskIndex,
	start int,
	req SearchRequest,
) error {
	end := min(start+prepared.pageSize, len(prepared.hits))
	projections, found, err := index.loadSearchHitProjections(ctx, prepared.hits[start:end], req)
	if err != nil {
		return err
	}
	prepared.pageStart = start
	prepared.projections = projections
	prepared.found = found

	return nil
}
