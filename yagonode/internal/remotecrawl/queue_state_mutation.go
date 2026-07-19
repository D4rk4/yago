package remotecrawl

import (
	"fmt"
	"sort"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func pendingQueueStateMutations(
	desired queueStateSnapshot,
	actual queueStateSnapshot,
) []queueStateMutation {
	mutations := make([]queueStateMutation, 0)
	for key, record := range actual.pendingRecords {
		if wanted, found := desired.pendingRecords[key]; !found || wanted != record {
			mutations = append(mutations, queueStateMutation{
				part: queuePendingPart, key: vault.Key(key), remove: true,
			})
		}
	}
	for key, record := range desired.pendingRecords {
		if existing, found := actual.pendingRecords[key]; !found || existing != record {
			mutations = append(mutations, queueStateMutation{
				part: queuePendingPart, key: vault.Key(key), sequence: record.Sequence,
			})
		}
	}

	return mutations
}

func expiryQueueStateMutations(
	desired queueStateSnapshot,
	actual queueStateSnapshot,
) []queueStateMutation {
	mutations := make([]queueStateMutation, 0)
	for key, record := range actual.leaseExpiries {
		if wanted, found := desired.leaseExpiries[key]; !found || wanted != record {
			mutations = append(mutations, queueStateMutation{
				part: queueExpiryPart, key: vault.Key(key), remove: true,
			})
		}
	}
	for key, record := range desired.leaseExpiries {
		if existing, found := actual.leaseExpiries[key]; !found || existing != record {
			mutations = append(mutations, queueStateMutation{
				part: queueExpiryPart, key: vault.Key(key), sequence: record.Sequence,
			})
		}
	}

	return mutations
}

func peerLeaseQueueStateMutations(
	desired queueStateSnapshot,
	actual queueStateSnapshot,
) []queueStateMutation {
	mutations := make([]queueStateMutation, 0)
	for key, total := range actual.leasesByPeer {
		if wanted, found := desired.leasesByPeer[key]; !found || wanted != total {
			mutations = append(mutations, queueStateMutation{
				part: queuePeerLeasePart, key: vault.Key(key), remove: true,
			})
		}
	}
	for key, total := range desired.leasesByPeer {
		if existing, found := actual.leasesByPeer[key]; !found || existing != total {
			mutations = append(mutations, queueStateMutation{
				part: queuePeerLeasePart, key: vault.Key(key), leaseTotal: total,
			})
		}
	}

	return mutations
}

func sortQueueStateMutations(mutations []queueStateMutation) []queueStateMutation {
	sort.Slice(mutations, func(left, right int) bool {
		if mutations[left].part != mutations[right].part {
			return mutations[left].part < mutations[right].part
		}
		if string(mutations[left].key) != string(mutations[right].key) {
			return string(mutations[left].key) < string(mutations[right].key)
		}

		return mutations[left].remove
	})

	return mutations
}

func applyQueueStateMutation(
	tx *vault.Txn,
	collections collections,
	mutation queueStateMutation,
) error {
	switch mutation.part {
	case queuePendingPart:
		return applyPendingQueueStateMutation(tx, collections, mutation)
	case queueExpiryPart:
		return applyExpiryQueueStateMutation(tx, collections, mutation)
	case queuePeerLeasePart:
		return applyPeerLeaseQueueStateMutation(tx, collections, mutation)
	default:
		return fmt.Errorf("remote crawl queue state mutation is invalid")
	}
}

func applyPendingQueueStateMutation(
	tx *vault.Txn,
	collections collections,
	mutation queueStateMutation,
) error {
	if mutation.remove {
		if _, err := collections.pending.Delete(tx, mutation.key); err != nil {
			return fmt.Errorf("delete remote crawl pending index: %w", err)
		}

		return nil
	}
	if err := collections.pending.Put(
		tx,
		mutation.key,
		pendingRecord{Sequence: mutation.sequence},
	); err != nil {
		return fmt.Errorf("store remote crawl pending index: %w", err)
	}

	return nil
}

func applyExpiryQueueStateMutation(
	tx *vault.Txn,
	collections collections,
	mutation queueStateMutation,
) error {
	if mutation.remove {
		if _, err := collections.leaseExpiries.Delete(tx, mutation.key); err != nil {
			return fmt.Errorf("delete remote crawl lease expiry index: %w", err)
		}

		return nil
	}
	if err := collections.leaseExpiries.Put(
		tx,
		mutation.key,
		leaseExpiryRecord{Sequence: mutation.sequence},
	); err != nil {
		return fmt.Errorf("store remote crawl lease expiry index: %w", err)
	}

	return nil
}

func applyPeerLeaseQueueStateMutation(
	tx *vault.Txn,
	collections collections,
	mutation queueStateMutation,
) error {
	if mutation.remove {
		if _, err := collections.leaseCounts.Delete(tx, mutation.key); err != nil {
			return fmt.Errorf("delete remote crawl lease count index: %w", err)
		}

		return nil
	}
	if err := collections.leaseCounts.Put(tx, mutation.key, mutation.leaseTotal); err != nil {
		return fmt.Errorf("store remote crawl lease count index: %w", err)
	}

	return nil
}
