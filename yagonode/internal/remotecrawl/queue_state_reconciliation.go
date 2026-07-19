package remotecrawl

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const queueStateMutationBatch = 100

type queueStateSnapshot struct {
	pendingRecords map[string]pendingRecord
	leaseExpiries  map[string]leaseExpiryRecord
	leasesByPeer   map[string]uint64
}

type queueStatePart uint8

const (
	queuePendingPart queueStatePart = iota + 1
	queueExpiryPart
	queuePeerLeasePart
)

type queueStateMutation struct {
	part       queueStatePart
	key        vault.Key
	remove     bool
	sequence   uint64
	leaseTotal uint64
}

func reconcileQueueState(storage *vault.Vault, collections collections) error {
	current, err := queueStateVersion(storage, collections)
	if err != nil {
		return err
	}
	if current == currentQueueStateVersion {
		return nil
	}
	if current > currentQueueStateVersion {
		return fmt.Errorf("remote crawl queue state version %d is unsupported", current)
	}
	desired, actual, err := readQueueState(storage, collections)
	if err != nil {
		return err
	}
	mutations := queueStateMutations(desired, actual)
	for start := 0; start < len(mutations); start += queueStateMutationBatch {
		end := min(start+queueStateMutationBatch, len(mutations))
		if err := applyQueueStateMutations(storage, collections, mutations[start:end]); err != nil {
			return err
		}
	}
	if err := storage.Update(context.Background(), func(tx *vault.Txn) error {
		return collections.schema.Put(tx, queueStateVersionKey, currentQueueStateVersion)
	}); err != nil {
		return fmt.Errorf("store remote crawl queue state version: %w", err)
	}

	return nil
}

func queueStateVersion(storage *vault.Vault, collections collections) (uint64, error) {
	var version uint64
	if err := storage.View(context.Background(), func(tx *vault.Txn) error {
		var err error
		version, _, err = collections.schema.Get(tx, queueStateVersionKey)
		if err != nil {
			return fmt.Errorf("read remote crawl queue schema: %w", err)
		}

		return nil
	}); err != nil {
		return 0, fmt.Errorf("read remote crawl queue state version: %w", err)
	}

	return version, nil
}

func readQueueState(
	storage *vault.Vault,
	collections collections,
) (queueStateSnapshot, queueStateSnapshot, error) {
	var desired queueStateSnapshot
	var actual queueStateSnapshot
	err := storage.View(context.Background(), func(tx *vault.Txn) error {
		var err error
		desired, err = readDesiredQueueState(tx, collections)
		if err != nil {
			return err
		}
		actual, err = readActualQueueState(tx, collections)

		return err
	})
	if err != nil {
		return queueStateSnapshot{}, queueStateSnapshot{}, fmt.Errorf(
			"reconcile remote crawl queue state: %w",
			err,
		)
	}

	return desired, actual, nil
}

func newQueueStateSnapshot() queueStateSnapshot {
	return queueStateSnapshot{
		pendingRecords: map[string]pendingRecord{},
		leaseExpiries:  map[string]leaseExpiryRecord{},
		leasesByPeer:   map[string]uint64{},
	}
}

func queueStateMutations(desired, actual queueStateSnapshot) []queueStateMutation {
	mutations := pendingQueueStateMutations(desired, actual)
	mutations = append(mutations, expiryQueueStateMutations(desired, actual)...)
	mutations = append(mutations, peerLeaseQueueStateMutations(desired, actual)...)

	return sortQueueStateMutations(mutations)
}

func applyQueueStateMutations(
	storage *vault.Vault,
	collections collections,
	mutations []queueStateMutation,
) error {
	if err := storage.Update(context.Background(), func(tx *vault.Txn) error {
		for _, mutation := range mutations {
			if err := applyQueueStateMutation(tx, collections, mutation); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("update remote crawl queue state: %w", err)
	}

	return nil
}
