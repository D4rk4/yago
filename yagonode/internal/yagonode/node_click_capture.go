package yagonode

import (
	"context"
	"fmt"
	"sort"

	"github.com/D4rk4/yago/yagonode/internal/clickcapture"
	"github.com/D4rk4/yago/yagonode/internal/learnedrank"
	"github.com/D4rk4/yago/yagonode/internal/publicportal"
	"github.com/D4rk4/yago/yagonode/internal/yacysearch"
)

type clickCaptureAdapter struct {
	store  *clickcapture.Store
	ranker *learnedrank.Ranker
}

type portalClickCaptureAdapter struct {
	store  *clickcapture.Store
	ranker *learnedrank.Ranker
}

type capturedImpressionPlan struct {
	query        string
	revision     string
	candidates   []clickcapture.Candidate
	lexicalOrder []int
	comparable   bool
}

func newClickCaptureAdapter(
	store *clickcapture.Store,
	ranker *learnedrank.Ranker,
) yacysearch.ImpressionRecorder {
	if store == nil {
		return nil
	}

	return clickCaptureAdapter{store: store, ranker: ranker}
}

func newPortalClickCaptureAdapter(
	store *clickcapture.Store,
	ranker *learnedrank.Ranker,
) publicportal.ImpressionRecorder {
	if store == nil {
		return nil
	}

	return portalClickCaptureAdapter{store: store, ranker: ranker}
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
	prepared, err := a.prepareImpression(ctx, query, candidates, converted)
	if err != nil {
		return yacysearch.PreparedImpression{}, fmt.Errorf("prepare click impression: %w", err)
	}
	order := make([]int, len(prepared.Candidates))
	indices := make(map[string]int, len(converted))
	for index, candidate := range converted {
		indices[candidate.URLIdentity] = index
	}
	for index, candidate := range prepared.Candidates {
		order[index] = indices[candidate.URLIdentity]
	}

	return yacysearch.PreparedImpression{Token: prepared.Token, Order: order}, nil
}

func (a clickCaptureAdapter) prepareImpression(
	ctx context.Context,
	query string,
	candidates []yacysearch.ImpressionCandidate,
	converted []clickcapture.Candidate,
) (clickcapture.PreparedImpression, error) {
	revision := activeRankingRevision(a.ranker)
	order, comparable := lexicalCandidateOrder(candidates)

	return prepareCapturedImpression(
		ctx,
		a.store,
		capturedImpressionPlan{
			query: query, revision: revision, candidates: converted,
			lexicalOrder: order, comparable: comparable,
		},
	)
}

func (a portalClickCaptureAdapter) PrepareImpression(
	ctx context.Context,
	query string,
	candidates []publicportal.ImpressionCandidate,
) (publicportal.PreparedImpression, error) {
	converted := make([]clickcapture.Candidate, len(candidates))
	positions := make([]int, len(candidates))
	indices := make(map[string]int, len(candidates))
	for index, candidate := range candidates {
		converted[index] = clickcapture.Candidate{
			URLIdentity: candidate.URLIdentity, ClusterIdentity: candidate.ClusterIdentity,
			Position: candidate.Position,
		}
		positions[index] = candidate.LexicalPosition
		indices[candidate.URLIdentity] = index
	}
	order, comparable := sortedLexicalOrder(positions)
	prepared, err := prepareCapturedImpression(
		ctx,
		a.store,
		capturedImpressionPlan{
			query: query, revision: activeRankingRevision(a.ranker),
			candidates: converted, lexicalOrder: order, comparable: comparable,
		},
	)
	if err != nil {
		return publicportal.PreparedImpression{}, fmt.Errorf(
			"prepare portal click impression: %w",
			err,
		)
	}
	displayOrder := make([]int, len(prepared.Candidates))
	for index, candidate := range prepared.Candidates {
		displayOrder[index] = indices[candidate.URLIdentity]
	}

	return publicportal.PreparedImpression{Token: prepared.Token, Order: displayOrder}, nil
}

func prepareCapturedImpression(
	ctx context.Context,
	store *clickcapture.Store,
	plan capturedImpressionPlan,
) (clickcapture.PreparedImpression, error) {
	if plan.revision == "" || !plan.comparable {
		prepared, err := store.PrepareImpression(ctx, plan.query, plan.candidates)
		if err != nil {
			return clickcapture.PreparedImpression{}, fmt.Errorf(
				"prepare fair-pair impression: %w",
				err,
			)
		}

		return prepared, nil
	}
	lexical := make([]clickcapture.Candidate, len(plan.lexicalOrder))
	for index, original := range plan.lexicalOrder {
		lexical[index] = plan.candidates[original]
	}
	prepared, err := store.PrepareTeamDraft(
		ctx,
		plan.query,
		clickcapture.DraftRanking{Revision: plan.revision, Candidates: plan.candidates},
		clickcapture.DraftRanking{
			Revision: clickcapture.LexicalRevision, Candidates: lexical,
		},
		len(plan.candidates),
	)
	if err != nil {
		return clickcapture.PreparedImpression{}, fmt.Errorf(
			"prepare team-draft impression: %w",
			err,
		)
	}

	return prepared, nil
}

func activeRankingRevision(ranker *learnedrank.Ranker) string {
	if ranker == nil {
		return ""
	}
	snapshot, active := ranker.ActiveSnapshot()
	if !active {
		return ""
	}

	return snapshot.Revision()
}

func lexicalCandidateOrder(candidates []yacysearch.ImpressionCandidate) ([]int, bool) {
	positions := make([]int, len(candidates))
	for index, candidate := range candidates {
		positions[index] = candidate.LexicalPosition
	}

	return sortedLexicalOrder(positions)
}

func sortedLexicalOrder(positions []int) ([]int, bool) {
	order := make([]int, len(positions))
	seen := make(map[int]struct{}, len(positions))
	for index, position := range positions {
		if position < 1 {
			return nil, false
		}
		if _, duplicate := seen[position]; duplicate {
			return nil, false
		}
		seen[position] = struct{}{}
		order[index] = index
	}
	sort.SliceStable(order, func(left, right int) bool {
		return positions[order[left]] < positions[order[right]]
	})

	return order, true
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
