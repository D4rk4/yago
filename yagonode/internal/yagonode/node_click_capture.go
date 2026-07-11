package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/clickcapture"
	"github.com/D4rk4/yago/yagonode/internal/yacysearch"
)

type clickCaptureAdapter struct {
	store *clickcapture.Store
}

func newClickCaptureAdapter(store *clickcapture.Store) yacysearch.ImpressionRecorder {
	if store == nil {
		return nil
	}

	return clickCaptureAdapter{store: store}
}

func (a clickCaptureAdapter) PrepareImpression(
	ctx context.Context,
	query string,
	candidates []yacysearch.ImpressionCandidate,
) (yacysearch.PreparedImpression, error) {
	converted := make([]clickcapture.Candidate, len(candidates))
	for index, candidate := range candidates {
		converted[index] = clickcapture.Candidate{
			URLIdentity:     candidate.URLIdentity,
			ClusterIdentity: candidate.ClusterIdentity,
			Position:        candidate.Position,
		}
	}
	prepared, err := a.store.PrepareImpression(ctx, query, converted)
	if err != nil {
		return yacysearch.PreparedImpression{}, fmt.Errorf("prepare click impression: %w", err)
	}
	order := make([]int, len(prepared.Candidates))
	for index, candidate := range prepared.Candidates {
		order[index] = candidate.OriginalIndex
	}

	return yacysearch.PreparedImpression{Token: prepared.Token, Order: order}, nil
}

func (a clickCaptureAdapter) RecordClick(
	ctx context.Context,
	token string,
	urlIdentity string,
	position int,
) error {
	if err := a.store.RecordClick(ctx, token, urlIdentity, position); err != nil {
		return fmt.Errorf("record impression click: %w", err)
	}

	return nil
}
