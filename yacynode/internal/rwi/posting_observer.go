package rwi

import (
	"fmt"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type postingObservers []PostingObserver

func (o postingObservers) stored(tx *vault.Txn, word, url yacymodel.Hash) error {
	for _, observer := range o {
		if err := observer.PostingStored(tx, word, url); err != nil {
			return fmt.Errorf("posting observer: %w", err)
		}
	}

	return nil
}

func (o postingObservers) purged(tx *vault.Txn, word, url yacymodel.Hash) error {
	for _, observer := range o {
		if err := observer.PostingPurged(tx, word, url); err != nil {
			return fmt.Errorf("posting observer: %w", err)
		}
	}

	return nil
}
