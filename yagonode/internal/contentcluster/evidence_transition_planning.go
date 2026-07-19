package contentcluster

import (
	"context"
	"fmt"
	"math/bits"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (i *Index) planReplacements(
	ctx context.Context,
	prepared []preparedEvidence,
	pending []fingerprintTransition,
) ([]fingerprintTransition, []EvidenceReplacement, error) {
	pendingByURL := make(map[string]fingerprintTransition, len(pending))
	for _, transition := range pending {
		pendingByURL[transition.URL] = transition
	}
	plans := make([]fingerprintTransition, 0, len(prepared))
	outputs := make([]EvidenceReplacement, len(prepared))
	err := i.vault.View(ctx, func(tx *vault.Txn) error {
		planned := replacementBatchProjection{
			current:  make([]fingerprintRecord, 0, len(prepared)),
			previous: make([]fingerprintRecord, 0, len(prepared)),
		}
		for position, item := range prepared {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("plan fingerprint transition: %w", err)
			}
			transition, output, record, persist, err := i.planReplacement(
				tx,
				ctx,
				item,
				pendingByURL[item.URL],
				planned,
			)
			if err != nil {
				return err
			}
			if persist {
				plans = append(plans, transition)
				if transition.PreviousFound {
					planned.previous = append(planned.previous, transition.Previous)
				}
			} else if output.Replay && pendingByURL[item.URL].PreviousFound {
				planned.previous = append(
					planned.previous,
					pendingByURL[item.URL].Previous,
				)
			}
			outputs[position] = output
			planned.current = append(planned.current, record)
		}

		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("plan content cluster replacements: %w", err)
	}

	return plans, outputs, nil
}

func (i *Index) planReplacement(
	tx *vault.Txn,
	ctx context.Context,
	prepared preparedEvidence,
	pending fingerprintTransition,
	planned replacementBatchProjection,
) (fingerprintTransition, EvidenceReplacement, fingerprintRecord, bool, error) {
	pendingFound := pending.Token != ""
	if pendingFound && pending.CurrentFound && sameEvidence(pending.Current, prepared) {
		return fingerprintTransition{}, replacementFromTransition(pending, true),
			pending.Current, false, nil
	}
	previous, previousFound, err := i.fingerprints.Get(tx, vault.Key(prepared.URL))
	if err != nil {
		return fingerprintTransition{}, EvidenceReplacement{}, fingerprintRecord{}, false,
			fmt.Errorf("read replaced content fingerprint: %w", err)
	}
	var previousCluster clusterRecord
	var previousPublished bool
	if previousFound {
		previousCluster, previousPublished, err = i.publishedRecordCluster(tx, ctx, previous)
		if err != nil {
			return fingerprintTransition{}, EvidenceReplacement{}, fingerprintRecord{}, false, err
		}
	}
	if previousFound && sameEvidence(previous, prepared) && previousPublished {
		assignment := assignmentFrom(previousCluster)
		output := EvidenceReplacement{
			Previous:           assignment,
			PreviousFound:      true,
			Current:            assignment,
			AffectedClusterIDs: []string{assignment.ClusterID},
		}

		return fingerprintTransition{}, output, previous, false, nil
	}
	if previousFound && !previousPublished && sameClusterContent(previous, prepared) {
		current := recordFrom(
			prepared,
			i.recoveredMembershipClusterID(
				previous,
				previousCluster,
				planned,
			),
		)
		transition := newReplacementTransition(
			previous,
			true,
			Assignment{},
			current,
			pending,
		)

		return transition, replacementFromTransition(transition, pendingFound),
			current, true, nil
	}
	clusterID, err := i.plannedClusterID(
		tx,
		ctx,
		prepared,
		planned,
	)
	if err != nil {
		return fingerprintTransition{}, EvidenceReplacement{}, fingerprintRecord{}, false, err
	}
	current := recordFrom(prepared, clusterID)
	var previousAssignment Assignment
	if previousPublished {
		previousAssignment = assignmentFrom(previousCluster)
	}
	transition := newReplacementTransition(
		previous,
		previousFound,
		previousAssignment,
		current,
		pending,
	)

	return transition, replacementFromTransition(transition, pendingFound), current, true, nil
}

func (i *Index) plannedClusterID(
	tx *vault.Txn,
	ctx context.Context,
	prepared preparedEvidence,
	planned replacementBatchProjection,
) (string, error) {
	existing, found, err := i.findMatch(tx, ctx, prepared)
	if err != nil {
		return "", err
	}
	if found {
		found, err = i.plannedClusterAvailable(
			tx,
			ctx,
			existing.record.ClusterID,
			prepared.URL,
			planned,
		)
		if err != nil {
			return "", err
		}
	}
	exact := existing
	exactFound := found && existing.record.ContentHash == prepared.ContentHash
	exactSelection, err := i.bestPlannedExactCandidate(
		tx,
		ctx,
		prepared,
		planned,
		candidateSelection{candidate: exact, found: exactFound},
	)
	if err != nil {
		return "", err
	}
	if exactSelection.found {
		return exactSelection.candidate.record.ClusterID, nil
	}
	nearSelection, err := i.bestPlannedNearCandidate(
		tx,
		ctx,
		prepared,
		planned,
		candidateSelection{candidate: existing, found: found},
	)
	if err != nil {
		return "", err
	}
	if nearSelection.found {
		return nearSelection.candidate.record.ClusterID, nil
	}

	return stableClusterID(prepared.URL, prepared.ContentHash), nil
}

type candidateSelection struct {
	candidate candidateMatch
	found     bool
}

func (i *Index) bestPlannedExactCandidate(
	tx *vault.Txn,
	ctx context.Context,
	prepared preparedEvidence,
	planned replacementBatchProjection,
	selection candidateSelection,
) (candidateSelection, error) {
	for _, record := range planned.current {
		if record.URL == prepared.URL || record.ContentHash != prepared.ContentHash {
			continue
		}
		available, err := i.plannedClusterAvailable(
			tx,
			ctx,
			record.ClusterID,
			prepared.URL,
			planned,
		)
		if err != nil {
			return candidateSelection{}, err
		}
		if !available {
			continue
		}
		candidate := candidateMatch{
			record:     record,
			similarity: 1,
			distance:   bitsOnesDistance(prepared.Fingerprint, record.Fingerprint),
		}
		if !selection.found || betterCandidate(candidate, selection.candidate) {
			selection.candidate = candidate
		}
		selection.found = true
	}

	return selection, nil
}

func (i *Index) bestPlannedNearCandidate(
	tx *vault.Txn,
	ctx context.Context,
	prepared preparedEvidence,
	planned replacementBatchProjection,
	selection candidateSelection,
) (candidateSelection, error) {
	for _, record := range planned.current {
		if record.URL == prepared.URL {
			continue
		}
		available, err := i.plannedClusterAvailable(
			tx,
			ctx,
			record.ClusterID,
			prepared.URL,
			planned,
		)
		if err != nil {
			return candidateSelection{}, err
		}
		if !available {
			continue
		}
		similarity := boundedJaccard(prepared.Shingles, record.Shingles)
		if similarity < i.limits.MinimumJaccard {
			continue
		}
		candidate := candidateMatch{
			record:     record,
			similarity: similarity,
			distance:   bitsOnesDistance(prepared.Fingerprint, record.Fingerprint),
		}
		if !selection.found || betterCandidate(candidate, selection.candidate) {
			selection.candidate = candidate
			selection.found = true
		}
	}

	return selection, nil
}

func (i *Index) plannedClusterAvailable(
	tx *vault.Txn,
	ctx context.Context,
	clusterID string,
	url string,
	planned replacementBatchProjection,
) (bool, error) {
	cluster, found, err := i.publishedCluster(tx, ctx, clusterID)
	if err != nil {
		return false, err
	}
	if !found {
		cluster = clusterRecord{ID: clusterID}
	}
	members := clusterMembershipAfterEarlierPlans(
		cluster,
		clusterID,
		planned,
	)
	if _, found := members[url]; found {
		return true, nil
	}

	return len(members) < i.limits.MaximumClusterMembers, nil
}

func bitsOnesDistance(left uint64, right uint64) int {
	return bits.OnesCount64(left ^ right)
}

func replacementFromTransition(
	transition fingerprintTransition,
	replay bool,
) EvidenceReplacement {
	return EvidenceReplacement{
		Previous:           transition.PreviousAssignment,
		PreviousFound:      transitionPreviousAssignmentFound(transition),
		Current:            transition.CurrentAssignment,
		Replay:             replay,
		AffectedClusterIDs: transition.affectedClusterIDs(),
		Finalization:       transition.finalization(),
	}
}

func (i *Index) completeReplacementOutputs(
	ctx context.Context,
	outputs []EvidenceReplacement,
) error {
	err := i.vault.View(ctx, func(tx *vault.Txn) error {
		for position := range outputs {
			if outputs[position].Finalization.token == "" {
				continue
			}
			transition, found, err := i.fingerprints.transition(
				tx,
				outputs[position].Finalization.url,
			)
			if err != nil {
				return err
			}
			if !found || transition.Token != outputs[position].Finalization.token {
				return fmt.Errorf("fingerprint transition changed before completion")
			}
			cluster, published, err := i.publishedRecordCluster(tx, ctx, transition.Current)
			if err != nil {
				return err
			}
			if !published {
				return fmt.Errorf(
					"content cluster %q does not publish %q",
					transition.Current.ClusterID,
					transition.Current.URL,
				)
			}
			outputs[position].Current = assignmentFrom(cluster)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("complete content cluster replacements: %w", err)
	}

	return nil
}
