package rwi

import (
	"fmt"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (d postingDirectory) DropWaitingOutboundPostings(
	tx *vault.Txn,
	urls []yagomodel.Hash,
) error {
	if len(urls) == 0 {
		return nil
	}
	deletedURLs := make(map[yagomodel.Hash]struct{}, len(urls))
	for _, url := range urls {
		deletedURLs[url] = struct{}{}
	}
	var waiting []vault.Key
	if err := d.outboundSelected.Scan(tx, nil, func(
		key vault.Key,
		_ yagomodel.RWIPosting,
	) (bool, error) {
		_, url, err := postingKeyHashes(key)
		if err != nil {
			return false, err
		}
		if _, deleted := deletedURLs[url]; deleted {
			waiting = append(waiting, key)
		}

		return true, nil
	}); err != nil {
		return fmt.Errorf("scan waiting outbound rwi postings: %w", err)
	}
	for _, key := range waiting {
		if err := d.dropWaitingOutboundPosting(tx, key); err != nil {
			return err
		}
	}

	return nil
}

func (d postingDirectory) dropWaitingOutboundPosting(tx *vault.Txn, key vault.Key) error {
	if _, err := d.outboundSelected.Delete(tx, key); err != nil {
		return fmt.Errorf("drop waiting outbound rwi posting: %w", err)
	}

	return nil
}
