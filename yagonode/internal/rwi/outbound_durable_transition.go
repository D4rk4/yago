package rwi

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (d postingDirectory) removeDurablySelectedPostings(
	ctx context.Context,
	selected []selectedPosting,
) error {
	if err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		return d.deleteOutboundSelection(tx, selected)
	}); err != nil {
		return fmt.Errorf("remove durably selected postings: %w", err)
	}

	return nil
}

func outboundPostingsFromWords(words []yagomodel.WordPostings) ([]selectedPosting, error) {
	selected := make([]selectedPosting, 0)
	for _, word := range words {
		wordHash, err := yagomodel.ParseHash(word.WordHash.String())
		if err != nil {
			return nil, fmt.Errorf("rwi posting word hash: %w", err)
		}
		for _, posting := range word.Postings {
			url, err := posting.URLHash()
			if err != nil {
				return nil, fmt.Errorf("rwi posting url hash: %w", err)
			}
			posting.WordHash = wordHash
			selected = append(selected, selectedPosting{
				key:  postingKey(wordHash, url.Hash()),
				word: wordHash,
				url:  url.Hash(),
				row:  posting,
			})
		}
	}

	return selected, nil
}

func (d postingDirectory) restoreOutboundPostings(
	ctx context.Context,
	selected []selectedPosting,
) (int, error) {
	if len(selected) == 0 {
		return 0, nil
	}
	if err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, posting := range selected {
			if err := d.restoreOutboundPosting(ctx, tx, posting); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return 0, fmt.Errorf("restore journaled postings: %w", err)
	}
	if err := d.releaseOutboundPostings(ctx, selected); err != nil {
		return 0, err
	}

	return len(selected), nil
}

func (d postingDirectory) releaseOutboundPostings(
	ctx context.Context,
	selected []selectedPosting,
) error {
	if err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		for _, posting := range selected {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("context: %w", err)
			}
			if _, err := d.outboundSelected.Delete(tx, posting.key); err != nil {
				return fmt.Errorf("delete outbound selected rwi: %w", err)
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("release restored postings: %w", err)
	}

	return nil
}
