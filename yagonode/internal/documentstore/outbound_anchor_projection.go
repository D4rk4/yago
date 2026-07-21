package documentstore

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const outboundAnchorProjectionPageSize = 16

func (d documentVault) VisitOutboundAnchorDocuments(
	ctx context.Context,
	finalizations []OutboundAnchorFinalization,
	visit func([]Document) error,
) error {
	if len(finalizations) == 0 {
		return nil
	}
	lease, err := activeOutboundAnchorLease(finalizations)
	if err != nil {
		return err
	}
	for start := 0; start < len(lease.targets); start += outboundAnchorProjectionPageSize {
		end := min(start+outboundAnchorProjectionPageSize, len(lease.targets))
		documents, err := d.readOutboundAnchorProjection(
			ctx,
			lease.targets[start:end],
		)
		if err != nil {
			return err
		}
		if len(documents) == 0 {
			continue
		}
		if err := visit(documents); err != nil {
			return fmt.Errorf("visit outbound anchor documents: %w", err)
		}
	}

	return nil
}

func (d documentVault) readOutboundAnchorProjection(
	ctx context.Context,
	urls []string,
) ([]Document, error) {
	documents := make([]Document, 0, len(urls))
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		for _, targetURL := range urls {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("context: %w", err)
			}
			document, _, found, err := d.readStoredDocument(tx, targetURL)
			if err != nil {
				return err
			}
			if found {
				documents = append(documents, normalizedDocument(document))
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read outbound anchor projection: %w", err)
	}

	return documents, nil
}

func activeOutboundAnchorLease(
	finalizations []OutboundAnchorFinalization,
) (*outboundAnchorLease, error) {
	if len(finalizations) > MaximumOutboundAnchorSourcesPerReplacement {
		return nil, fmt.Errorf("outbound anchor finalization source limit exceeded")
	}
	lease := finalizations[0].lease
	if lease == nil || lease.released.Load() {
		return nil, fmt.Errorf("outbound anchor finalization lease is not active")
	}
	for _, finalization := range finalizations[1:] {
		if finalization.lease != lease {
			return nil, fmt.Errorf("outbound anchor finalizations do not share one lease")
		}
	}

	return lease, nil
}
