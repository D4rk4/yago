package contentcluster

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (i *Index) reconcileTransitions(
	ctx context.Context,
	transitions []fingerprintTransition,
) error {
	if len(transitions) == 0 {
		return nil
	}
	err := i.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := i.prepareTransitionProjections(ctx, tx, transitions); err != nil {
			return fmt.Errorf("prepare content cluster projections: %w", err)
		}
		if err := i.publishTransitionMarkers(ctx, tx, transitions); err != nil {
			return fmt.Errorf("publish content cluster transitions: %w", err)
		}
		if err := i.cleanTransitionProjections(ctx, tx, transitions); err != nil {
			return fmt.Errorf("clean content cluster transitions: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("reconcile content cluster transitions: %w", err)
	}

	return nil
}

func (i *Index) prepareTransitionProjections(
	ctx context.Context,
	tx *vault.Txn,
	transitions []fingerprintTransition,
) error {
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
}

func (i *Index) publishTransitionMarkers(
	ctx context.Context,
	tx *vault.Txn,
	transitions []fingerprintTransition,
) error {
	for _, transition := range transitions {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("publish fingerprint transition: %w", err)
		}
		if err := i.publishTransitionMarker(tx, transition); err != nil {
			return err
		}
	}

	return nil
}

func (i *Index) cleanTransitionProjections(
	ctx context.Context,
	tx *vault.Txn,
	transitions []fingerprintTransition,
) error {
	for _, transition := range transitions {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("clean fingerprint transition: %w", err)
		}
		if err := i.cleanTransitionProjection(tx, ctx, transition); err != nil {
			return err
		}
	}

	return nil
}
