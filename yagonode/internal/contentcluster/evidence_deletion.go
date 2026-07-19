package contentcluster

import (
	"context"
	"errors"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (i *Index) DeleteTransition(
	ctx context.Context,
	url string,
) (EvidenceDeletion, error) {
	normalizedURL, err := validateURL(url)
	if err != nil {
		return EvidenceDeletion{}, err
	}
	urls := []string{normalizedURL}
	leases, err := i.acquireReplacementLeases(ctx, urls)
	if err != nil {
		return EvidenceDeletion{}, err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			leases.close()
		}
	}()
	for {
		deletion, found, retry, err := i.executeDeletionAttempt(ctx, normalizedURL, leases)
		if err != nil {
			return EvidenceDeletion{}, err
		}
		if retry {
			continue
		}
		if !found {
			return EvidenceDeletion{}, nil
		}
		deletion.Finalization.urlLease = leases.url
		deletion.Finalization.projection = leases.projection
		handedOff = true

		return deletion, nil
	}
}

func (i *Index) executeDeletionAttempt(
	ctx context.Context,
	url string,
	leases *replacementLeases,
) (EvidenceDeletion, bool, bool, error) {
	attempt, err := i.buildDeletionAttempt(ctx, url, leases.identities)
	if errors.Is(err, errEvidenceTransitionConflict) {
		return EvidenceDeletion{}, false, true, nil
	}
	if err != nil {
		return EvidenceDeletion{}, false, false, err
	}
	expanded, err := i.expandReplacementLeases(ctx, leases, attempt.identities)
	if err != nil || expanded {
		return EvidenceDeletion{}, false, expanded, err
	}
	if !attempt.found {
		return EvidenceDeletion{}, false, false, nil
	}
	if err := i.commitDeletionTransition(ctx, attempt); err != nil {
		if errors.Is(err, errEvidenceTransitionConflict) {
			return EvidenceDeletion{}, false, true, nil
		}

		return EvidenceDeletion{}, false, false, err
	}

	return attempt.deletion, true, false, nil
}

type deletionAttempt struct {
	transition fingerprintTransition
	deletion   EvidenceDeletion
	identities []string
	found      bool
	persist    bool
}

func (i *Index) buildDeletionAttempt(
	ctx context.Context,
	url string,
	identities []string,
) (deletionAttempt, error) {
	pending, err := i.readTransitions(ctx, []string{url})
	if err != nil {
		return deletionAttempt{}, err
	}
	if err := i.reconcileTransitions(ctx, pending); err != nil {
		return deletionAttempt{}, err
	}
	if len(pending) == 1 && !pending[0].CurrentFound {
		return deletionAttempt{
			transition: pending[0],
			deletion:   deletionFromTransition(pending[0], true),
			identities: mergeAffectedClusterIDs(identities, pending[0].AffectedClusterIDs),
			found:      true,
		}, nil
	}
	transition, found, err := i.planDeletion(ctx, url, pending)
	if err != nil || !found {
		return deletionAttempt{}, err
	}

	return deletionAttempt{
		transition: transition,
		deletion:   deletionFromTransition(transition, len(pending) > 0),
		identities: mergeAffectedClusterIDs(identities, transition.AffectedClusterIDs),
		found:      true,
		persist:    true,
	}, nil
}

func (i *Index) commitDeletionTransition(
	ctx context.Context,
	attempt deletionAttempt,
) error {
	if !attempt.persist {
		return nil
	}
	transitions := []fingerprintTransition{attempt.transition}
	if err := i.persistTransitions(ctx, transitions); err != nil {
		return err
	}

	return i.reconcileTransitions(ctx, transitions)
}

func (i *Index) planDeletion(
	ctx context.Context,
	url string,
	pending []fingerprintTransition,
) (fingerprintTransition, bool, error) {
	var transition fingerprintTransition
	found := false
	err := i.vault.View(ctx, func(tx *vault.Txn) error {
		previous, exists, err := i.fingerprints.Get(tx, vault.Key(url))
		if err != nil {
			return fmt.Errorf("read deleted content fingerprint: %w", err)
		}
		if !exists {
			return nil
		}
		cluster, published, err := i.publishedRecordCluster(tx, ctx, previous)
		if err != nil {
			return err
		}
		var previousAssignment Assignment
		if published {
			previousAssignment = assignmentFrom(cluster)
		}
		var priorAffected []string
		if len(pending) > 0 {
			priorAffected = pending[0].AffectedClusterIDs
		}
		finalization := newEvidenceFinalization(url)
		transition = fingerprintTransition{
			Token:              finalization.token,
			URL:                url,
			Previous:           previous,
			PreviousFound:      true,
			PreviousAssignment: previousAssignment,
			AffectedClusterIDs: mergeAffectedClusterIDs(
				priorAffected,
				[]string{previous.ClusterID},
			),
		}
		found = true

		return nil
	})
	if err != nil {
		return fingerprintTransition{}, false, fmt.Errorf("plan content cluster deletion: %w", err)
	}

	return transition, found, nil
}

func deletionFromTransition(
	transition fingerprintTransition,
	replay bool,
) EvidenceDeletion {
	return EvidenceDeletion{
		Previous:           transition.PreviousAssignment,
		PreviousFound:      transitionPreviousAssignmentFound(transition),
		Deleted:            transition.PreviousFound,
		Replay:             replay,
		AffectedClusterIDs: transition.affectedClusterIDs(),
		Finalization:       transition.finalization(),
	}
}
