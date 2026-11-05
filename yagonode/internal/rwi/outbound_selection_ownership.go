package rwi

import (
	"bytes"
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (d postingDirectory) ownsOutboundSelection(
	tx *vault.Txn,
	posting selectedPosting,
) (bool, error) {
	journaled, found, err := d.outboundSelected.Get(tx, posting.key)
	if err != nil {
		return false, fmt.Errorf("read outbound selected rwi: %w", err)
	}

	return found && sameStoredPosting(journaled, posting.row), nil
}

func (d postingDirectory) releaseOwnedOutboundSelection(
	tx *vault.Txn,
	posting selectedPosting,
) (bool, error) {
	owned, err := d.ownsOutboundSelection(tx, posting)
	if err != nil || !owned {
		return false, err
	}
	if _, err := d.outboundSelected.Delete(tx, posting.key); err != nil {
		return false, fmt.Errorf("delete outbound selected rwi: %w", err)
	}

	return true, nil
}

func sameStoredPosting(left, right yagomodel.RWIPosting) bool {
	return bytes.Equal(encodeStoredPosting(left), encodeStoredPosting(right))
}
