package remotecrawl

import (
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func readDesiredQueueState(
	tx *vault.Txn,
	collections collections,
) (queueStateSnapshot, error) {
	snapshot := newQueueStateSnapshot()
	length, err := collections.orders.Len(tx)
	if err != nil {
		return queueStateSnapshot{}, fmt.Errorf("read remote crawl queue depth: %w", err)
	}
	if length > MaximumQueueCapacity {
		return queueStateSnapshot{}, fmt.Errorf("remote crawl queue exceeds maximum capacity")
	}
	ordersRead := 0
	if err := collections.orders.Scan(tx, nil, func(
		key vault.Key,
		record queueRecord,
	) (bool, error) {
		ordersRead++
		if ordersRead > MaximumQueueCapacity {
			return false, fmt.Errorf("remote crawl queue exceeds maximum capacity")
		}
		if err := addDesiredQueueRecord(snapshot, key, record); err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return queueStateSnapshot{}, fmt.Errorf("scan remote crawl orders: %w", err)
	}

	return snapshot, nil
}

func addDesiredQueueRecord(
	snapshot queueStateSnapshot,
	key vault.Key,
	record queueRecord,
) error {
	if string(key) != string(sequenceKey(record.Sequence)) {
		return fmt.Errorf("remote crawl order sequence is inconsistent")
	}
	switch record.State {
	case queueStatePending:
		snapshot.pendingRecords[string(key)] = pendingRecord{Sequence: record.Sequence}
	case queueStateLeased:
		return addDesiredLease(snapshot, record)
	default:
		return fmt.Errorf("remote crawl order state %q is unsupported", record.State)
	}

	return nil
}

func addDesiredLease(snapshot queueStateSnapshot, record queueRecord) error {
	if record.Peer == "" || record.LeaseUntil <= 0 {
		return fmt.Errorf("remote crawl leased order is malformed")
	}
	key := string(leaseExpiryKey(record.LeaseUntil, record.Sequence))
	snapshot.leaseExpiries[key] = leaseExpiryRecord{Sequence: record.Sequence}
	snapshot.leasesByPeer[record.Peer]++

	return nil
}

func readActualQueueState(
	tx *vault.Txn,
	collections collections,
) (queueStateSnapshot, error) {
	snapshot := newQueueStateSnapshot()
	if err := scanPendingIndex(tx, collections, snapshot); err != nil {
		return queueStateSnapshot{}, err
	}
	if err := scanLeaseExpiryIndex(tx, collections, snapshot); err != nil {
		return queueStateSnapshot{}, err
	}
	if err := scanLeaseCountIndex(tx, collections, snapshot); err != nil {
		return queueStateSnapshot{}, err
	}

	return snapshot, nil
}

func scanPendingIndex(
	tx *vault.Txn,
	collections collections,
	snapshot queueStateSnapshot,
) error {
	if err := collections.pending.Scan(tx, nil, func(
		key vault.Key,
		record pendingRecord,
	) (bool, error) {
		if len(snapshot.pendingRecords) >= MaximumQueueCapacity {
			return false, fmt.Errorf("remote crawl pending index exceeds maximum capacity")
		}
		snapshot.pendingRecords[string(key)] = record

		return true, nil
	}); err != nil {
		return fmt.Errorf("scan remote crawl pending index: %w", err)
	}

	return nil
}

func scanLeaseExpiryIndex(
	tx *vault.Txn,
	collections collections,
	snapshot queueStateSnapshot,
) error {
	if err := collections.leaseExpiries.Scan(tx, nil, func(
		key vault.Key,
		record leaseExpiryRecord,
	) (bool, error) {
		if len(snapshot.leaseExpiries) >= MaximumQueueCapacity {
			return false, fmt.Errorf("remote crawl lease expiry index exceeds maximum capacity")
		}
		snapshot.leaseExpiries[string(key)] = record

		return true, nil
	}); err != nil {
		return fmt.Errorf("scan remote crawl lease expiry index: %w", err)
	}

	return nil
}

func scanLeaseCountIndex(
	tx *vault.Txn,
	collections collections,
	snapshot queueStateSnapshot,
) error {
	if err := collections.leaseCounts.Scan(tx, nil, func(
		key vault.Key,
		total uint64,
	) (bool, error) {
		if len(snapshot.leasesByPeer) >= MaximumQueueCapacity {
			return false, fmt.Errorf("remote crawl lease count index exceeds maximum capacity")
		}
		snapshot.leasesByPeer[string(key)] = total

		return true, nil
	}); err != nil {
		return fmt.Errorf("scan remote crawl lease count index: %w", err)
	}

	return nil
}
