package contentcluster

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (i *Index) readTransitions(
	ctx context.Context,
	urls []string,
) ([]fingerprintTransition, error) {
	transitions := make([]fingerprintTransition, 0, len(urls))
	err := i.vault.View(ctx, func(tx *vault.Txn) error {
		for _, url := range urls {
			transition, found, err := i.fingerprints.transition(tx, url)
			if err != nil {
				return fmt.Errorf("read fingerprint transition: %w", err)
			}
			if found {
				transitions = append(transitions, transition)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read content cluster transitions: %w", err)
	}

	return transitions, nil
}

func (i *Index) persistTransitions(
	ctx context.Context,
	transitions []fingerprintTransition,
) error {
	err := i.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, transition := range transitions {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("persist fingerprint transition: %w", err)
			}
			current, found, err := i.fingerprints.Get(tx, vault.Key(transition.URL))
			if err != nil {
				return fmt.Errorf("read transition predecessor: %w", err)
			}
			if found != transition.PreviousFound || found &&
				!sameFingerprintRecord(current, transition.Previous) {
				return errEvidenceTransitionConflict
			}
			if err := i.fingerprints.putTransition(tx, transition); err != nil {
				return fmt.Errorf("store fingerprint transition: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("persist content cluster transitions: %w", err)
	}

	return nil
}

func (i *Index) reconcileTransitions(
	ctx context.Context,
	transitions []fingerprintTransition,
) error {
	if len(transitions) == 0 {
		return nil
	}
	if err := i.prepareTransitionProjections(ctx, transitions); err != nil {
		return err
	}
	if err := i.publishTransitionMarkers(ctx, transitions); err != nil {
		return err
	}
	if err := i.cleanTransitionProjections(ctx, transitions); err != nil {
		return err
	}

	return nil
}

func (i *Index) prepareTransitionProjections(
	ctx context.Context,
	transitions []fingerprintTransition,
) error {
	prepared := false
	for _, transition := range transitions {
		if transition.CurrentFound {
			prepared = true
			break
		}
	}
	if !prepared {
		return nil
	}
	err := i.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, transition := range transitions {
			if !transition.CurrentFound {
				continue
			}
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("prepare fingerprint transition: %w", err)
			}
			if err := i.prepareRecordPostings(tx, ctx, transition.Current); err != nil {
				return fmt.Errorf("prepare fingerprint postings: %w", err)
			}
			if err := i.attachProjectedCluster(tx, ctx, transition.Current); err != nil {
				return fmt.Errorf("prepare fingerprint cluster: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("prepare content cluster projections: %w", err)
	}

	return nil
}

func (i *Index) publishTransitionMarkers(
	ctx context.Context,
	transitions []fingerprintTransition,
) error {
	err := i.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, transition := range transitions {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("publish fingerprint transition: %w", err)
			}
			if err := i.publishTransitionMarker(tx, transition); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("publish content cluster transitions: %w", err)
	}

	return nil
}

func (i *Index) publishTransitionMarker(
	tx *vault.Txn,
	transition fingerprintTransition,
) error {
	if transition.CurrentFound {
		return i.publishReplacementMarker(tx, transition)
	}

	return i.publishDeletionMarker(tx, transition)
}

func (i *Index) publishReplacementMarker(
	tx *vault.Txn,
	transition fingerprintTransition,
) error {
	current, found, err := i.fingerprints.Get(tx, vault.Key(transition.URL))
	if err != nil {
		return fmt.Errorf("read published content fingerprint: %w", err)
	}
	if found && sameFingerprintRecord(current, transition.Current) {
		return nil
	}
	if found != transition.PreviousFound || found &&
		!sameFingerprintRecord(current, transition.Previous) {
		return errEvidenceTransitionConflict
	}
	if err := i.fingerprints.Put(
		tx,
		vault.Key(transition.URL),
		transition.Current,
	); err != nil {
		return fmt.Errorf("publish content fingerprint: %w", err)
	}

	return nil
}

func (i *Index) publishDeletionMarker(
	tx *vault.Txn,
	transition fingerprintTransition,
) error {
	current, found, err := i.fingerprints.Get(tx, vault.Key(transition.URL))
	if err != nil {
		return fmt.Errorf("read unpublished content fingerprint: %w", err)
	}
	if !found {
		return nil
	}
	if !transition.PreviousFound || !sameFingerprintRecord(current, transition.Previous) {
		return errEvidenceTransitionConflict
	}
	if _, err := i.fingerprints.Delete(tx, vault.Key(transition.URL)); err != nil {
		return fmt.Errorf("unpublish content fingerprint: %w", err)
	}

	return nil
}

func (i *Index) cleanTransitionProjections(
	ctx context.Context,
	transitions []fingerprintTransition,
) error {
	err := i.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, transition := range transitions {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("clean fingerprint transition: %w", err)
			}
			if err := i.cleanTransitionProjection(tx, ctx, transition); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("clean content cluster transitions: %w", err)
	}

	return nil
}

func (i *Index) cleanTransitionProjection(
	tx *vault.Txn,
	ctx context.Context,
	transition fingerprintTransition,
) error {
	if transition.PreviousFound {
		if err := i.removeRecordPostings(tx, ctx, transition.Previous); err != nil {
			return fmt.Errorf("clean previous fingerprint postings: %w", err)
		}
	}
	if transition.CurrentFound {
		if err := i.finalizeRecordPostings(tx, ctx, transition.Current); err != nil {
			return fmt.Errorf("finalize fingerprint postings: %w", err)
		}
	}
	clusterIDs := mergeAffectedClusterIDs(
		[]string{transition.Previous.ClusterID},
		[]string{transition.Current.ClusterID},
	)
	for _, clusterID := range clusterIDs {
		if err := i.normalizeProjectedCluster(tx, ctx, clusterID); err != nil {
			return fmt.Errorf("clean fingerprint cluster: %w", err)
		}
	}

	return nil
}

func (i *Index) FinalizeEvidenceTransitions(
	ctx context.Context,
	finalizations []EvidenceFinalization,
) error {
	if len(finalizations) == 0 {
		return nil
	}
	needsBoundary := make([]string, 0, len(finalizations))
	for _, finalization := range finalizations {
		if finalization.token == "" {
			continue
		}
		if finalization.urlLease == nil {
			needsBoundary = append(needsBoundary, finalization.url)
		}
	}
	release, err := i.boundaries.acquire(ctx, needsBoundary)
	if err != nil {
		return fmt.Errorf("acquire finalization boundaries: %w", err)
	}
	defer release()
	err = i.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, finalization := range finalizations {
			if finalization.token == "" {
				continue
			}
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("finalize fingerprint transition: %w", err)
			}
			if _, err := i.fingerprints.deleteTransition(tx, finalization); err != nil {
				return fmt.Errorf("delete fingerprint transition: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("finalize content cluster transitions: %w", err)
	}
	releaseEvidenceLeases(finalizations)

	return nil
}

func (i *Index) ReleaseEvidenceTransitions(finalizations []EvidenceFinalization) {
	releaseEvidenceLeases(finalizations)
}
