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
		planned := make([]fingerprintRecord, 0, len(prepared))
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
			}
			outputs[position] = output
			planned = append(planned, record)
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
	planned []fingerprintRecord,
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
	if previousFound && sameEvidence(previous, prepared) {
		assignment, err := i.publishedAssignment(tx, ctx, previous.ClusterID)
		if err != nil {
			return fingerprintTransition{}, EvidenceReplacement{}, fingerprintRecord{}, false, err
		}
		output := EvidenceReplacement{
			Previous:           assignment,
			PreviousFound:      true,
			Current:            assignment,
			AffectedClusterIDs: []string{assignment.ClusterID},
		}

		return fingerprintTransition{}, output, previous, false, nil
	}
	clusterID, err := i.plannedClusterID(tx, ctx, prepared, planned)
	if err != nil {
		return fingerprintTransition{}, EvidenceReplacement{}, fingerprintRecord{}, false, err
	}
	current := recordFrom(prepared, clusterID)
	transition := fingerprintTransition{
		Token:         newEvidenceFinalization(prepared.URL).token,
		URL:           prepared.URL,
		Previous:      previous,
		PreviousFound: previousFound,
		Current:       current,
		CurrentFound:  true,
	}
	if previousFound {
		transition.PreviousAssignment, err = i.publishedAssignment(tx, ctx, previous.ClusterID)
		if err != nil {
			return fingerprintTransition{}, EvidenceReplacement{}, fingerprintRecord{}, false, err
		}
	}
	transition.AffectedClusterIDs = mergeAffectedClusterIDs(
		pending.AffectedClusterIDs,
		[]string{previous.ClusterID, current.ClusterID},
	)

	return transition, replacementFromTransition(transition, pendingFound), current, true, nil
}

func (i *Index) plannedClusterID(
	tx *vault.Txn,
	ctx context.Context,
	prepared preparedEvidence,
	planned []fingerprintRecord,
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
	planned []fingerprintRecord,
	selection candidateSelection,
) (candidateSelection, error) {
	for _, record := range planned {
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
	planned []fingerprintRecord,
	selection candidateSelection,
) (candidateSelection, error) {
	for _, record := range planned {
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
	planned []fingerprintRecord,
) (bool, error) {
	members := make(map[string]struct{}, i.limits.MaximumClusterMembers)
	cluster, found, err := i.publishedCluster(tx, ctx, clusterID)
	if err != nil {
		return false, err
	}
	if found {
		for _, member := range cluster.Members {
			members[member] = struct{}{}
		}
	}
	for _, record := range planned {
		if record.ClusterID == clusterID {
			members[record.URL] = struct{}{}
		}
	}
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
		PreviousFound:      transition.PreviousFound,
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
			assignment, err := i.publishedAssignment(tx, ctx, transition.Current.ClusterID)
			if err != nil {
				return err
			}
			outputs[position].Current = assignment
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("complete content cluster replacements: %w", err)
	}

	return nil
}
