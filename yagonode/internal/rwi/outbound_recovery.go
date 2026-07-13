package rwi

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (d postingDirectory) ConfirmOutbound(
	ctx context.Context,
	postings []yagomodel.RWIPosting,
) (int, error) {
	confirmed := 0
	err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		confirmed = 0
		for _, posting := range postings {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("context: %w", err)
			}
			key, err := postingRecoveryKey(posting)
			if err != nil {
				return err
			}
			deleted, err := d.outboundSelected.Delete(tx, key)
			if err != nil {
				return fmt.Errorf("delete outbound selected rwi: %w", err)
			}
			if deleted {
				confirmed++
			}
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("confirm outbound rwi: %w", err)
	}

	return confirmed, nil
}

func (d postingDirectory) RecoverOutbound(ctx context.Context) (int, error) {
	recovered := 0
	err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		recovered = 0
		pending, err := d.pendingOutboundSelections(ctx, tx)
		if err != nil {
			return err
		}
		for _, posting := range pending {
			if err := d.restoreOutboundPosting(ctx, tx, posting.word, posting.row); err != nil {
				return err
			}
			recovered++
		}

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("recover outbound rwi: %w", err)
	}

	return recovered, nil
}

func (d postingDirectory) pendingOutboundSelections(
	ctx context.Context,
	tx *vault.Txn,
) ([]selectedPosting, error) {
	var pending []selectedPosting
	if err := d.outboundSelected.Scan(tx, nil, func(
		key vault.Key,
		entry yagomodel.RWIPosting,
	) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, fmt.Errorf("context: %w", err)
		}
		word, url, err := postingKeyHashes(key)
		if err != nil {
			return false, err
		}
		entry.WordHash = word
		pending = append(pending, selectedPosting{key: key, word: word, url: url, row: entry})

		return true, nil
	}); err != nil {
		return nil, fmt.Errorf("scan outbound selected rwi: %w", err)
	}

	return pending, nil
}

func (d postingDirectory) journalOutboundSelection(
	tx *vault.Txn,
	selected []selectedPosting,
) error {
	for _, posting := range selected {
		if err := d.outboundSelected.Put(tx, posting.key, posting.row); err != nil {
			return fmt.Errorf("journal outbound selected rwi: %w", err)
		}
	}

	return nil
}

func postingRecoveryKey(posting yagomodel.RWIPosting) (vault.Key, error) {
	word, err := yagomodel.ParseHash(posting.WordHash.String())
	if err != nil {
		return nil, fmt.Errorf("rwi posting word hash: %w", err)
	}
	url, err := posting.URLHash()
	if err != nil {
		return nil, fmt.Errorf("rwi posting url hash: %w", err)
	}

	return postingKey(word, url.Hash()), nil
}
