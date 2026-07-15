package documentstore

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (d documentVault) outboundAnchorTargetURLs(
	ctx context.Context,
	sets []OutboundAnchorSet,
) ([]string, error) {
	targetURLs := make([]string, 0)
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		for _, set := range sets {
			sourceURL := strings.TrimSpace(set.SourceURL)
			previous, err := d.readOutboundAnchorPublication(tx, sourceURL)
			if err != nil {
				return fmt.Errorf("read outbound targets for locking: %w", err)
			}
			_, incoming := canonicalOutboundAnchors(sourceURL, set.Anchors)
			targetURLs = append(targetURLs, previous.Targets...)
			targetURLs = append(targetURLs, incoming...)
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("resolve outbound anchor targets: %w", err)
	}

	return uniqueSortedStrings(targetURLs), nil
}

func (d documentVault) outboundAnchorUpdateAtCapacity(
	ctx context.Context,
	sets []OutboundAnchorSet,
) (bool, error) {
	if !outboundAnchorSetsCarryEdges(sets) {
		return false, nil
	}
	atCapacity, err := d.vault.AtCapacity(ctx)
	if err != nil {
		return false, fmt.Errorf("check capacity: %w", err)
	}

	return atCapacity, nil
}

func (d documentVault) replaceOutboundAnchorSets(
	ctx context.Context,
	sets []OutboundAnchorSet,
) ([]string, []OutboundAnchorFinalization, error) {
	affected := make(map[string]struct{})
	finalizations := make([]OutboundAnchorFinalization, 0, len(sets))
	err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		affected = make(map[string]struct{})
		finalizations = make([]OutboundAnchorFinalization, 0, len(sets))
		for _, set := range sets {
			finalization, pending, err := d.replaceOutboundAnchorSet(
				ctx,
				tx,
				set,
				affected,
			)
			if err != nil {
				return err
			}
			if pending {
				finalizations = append(finalizations, finalization)
			}
		}

		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("replace outbound anchors: %w", err)
	}

	return slices.Sorted(maps.Keys(affected)), finalizations, nil
}
