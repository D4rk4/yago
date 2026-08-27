package searchindex

import (
	"context"
	"fmt"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type preparedSearchHitProjections struct {
	projections []searchHitProjection
	found       []bool
}

func (b *BleveDiskIndex) prepareSearchHitProjections(
	ctx context.Context,
	hits []*search.DocumentMatch,
	req SearchRequest,
) (preparedSearchHitProjections, error) {
	if !b.supportsSearchHitProjectionSet(req) {
		return preparedSearchHitProjections{}, nil
	}
	projections, found, err := b.loadSearchHitProjections(ctx, hits, req)
	if err != nil {
		return preparedSearchHitProjections{}, err
	}

	return preparedSearchHitProjections{projections: projections, found: found}, nil
}

func (prepared preparedSearchHitProjections) load(
	ctx context.Context,
	index *BleveDiskIndex,
	position int,
	hit *search.DocumentMatch,
	req SearchRequest,
) (searchHitProjection, bool, error) {
	if prepared.projections == nil {
		return index.loadSearchHitProjectionIndividually(ctx, hit, req)
	}

	return prepared.projections[position], prepared.found[position], nil
}

func (b *BleveDiskIndex) loadSearchHitProjections(
	ctx context.Context,
	hits []*search.DocumentMatch,
	req SearchRequest,
) ([]searchHitProjection, []bool, error) {
	if len(hits) == 0 {
		return []searchHitProjection{}, []bool{}, nil
	}
	presence, supported := b.documentPresence.(documentstore.DocumentSetPresence)
	if !req.CandidateOnly || !b.storedCandidates || !supported {
		return b.loadSearchHitProjectionsIndividually(ctx, hits, req)
	}
	candidates := make([]storedCandidateProjection, len(hits))
	eligible := make([]bool, len(hits))
	identities := make([]string, 0, len(hits))
	positions := make([]int, 0, len(hits))
	for index, hit := range hits {
		candidate, err := decodeStoredCandidateProjection(hit)
		if err != nil || !candidate.supports(req) {
			continue
		}
		candidates[index] = candidate
		eligible[index] = true
		identities = append(identities, hit.ID)
		positions = append(positions, index)
	}
	exists, err := presence.DocumentsExist(ctx, identities)
	if err != nil {
		return nil, nil, fmt.Errorf("check stored candidate presence: %w", err)
	}
	if len(exists) != len(identities) {
		return nil, nil, fmt.Errorf(
			"document presence results = %d, want %d",
			len(exists),
			len(identities),
		)
	}
	projections := make([]searchHitProjection, len(hits))
	found := make([]bool, len(hits))
	for identityIndex, position := range positions {
		if !exists[identityIndex] {
			continue
		}
		candidate := candidates[position]
		projections[position] = searchHitProjection{
			document:  candidate.document(hits[position].ID),
			size:      candidate.Size,
			candidate: true,
		}
		found[position] = true
	}
	for index, hit := range hits {
		if eligible[index] {
			continue
		}
		projection, present, err := b.loadSearchHitProjectionIndividually(ctx, hit, req)
		if err != nil {
			return nil, nil, err
		}
		projections[index] = projection
		found[index] = present
	}

	return projections, found, nil
}

func (b *BleveDiskIndex) supportsSearchHitProjectionSet(req SearchRequest) bool {
	if !req.CandidateOnly || !b.storedCandidates {
		return false
	}
	_, supported := b.documentPresence.(documentstore.DocumentSetPresence)

	return supported
}

func (b *BleveDiskIndex) loadSearchHitProjectionsIndividually(
	ctx context.Context,
	hits []*search.DocumentMatch,
	req SearchRequest,
) ([]searchHitProjection, []bool, error) {
	projections := make([]searchHitProjection, len(hits))
	found := make([]bool, len(hits))
	for index, hit := range hits {
		projection, present, err := b.loadSearchHitProjectionIndividually(ctx, hit, req)
		if err != nil {
			return nil, nil, err
		}
		projections[index] = projection
		found[index] = present
	}

	return projections, found, nil
}
