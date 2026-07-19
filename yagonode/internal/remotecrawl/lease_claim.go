package remotecrawl

import (
	"fmt"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func (b *Broker) claimAvailableRecords(
	tx *vault.Txn,
	peer yagomodel.Hash,
	selected []queueRecord,
	leaseUntil time.Time,
) ([]queueRecord, error) {
	peerKey := vault.Key(peer.String())
	outstanding, _, err := b.leaseCounts.Get(tx, peerKey)
	if err != nil {
		return nil, fmt.Errorf("read remote crawl peer leases: %w", err)
	}
	available := availableLeaseSlots(b.config.OutstandingPerPeer, outstanding)
	claimed := make([]queueRecord, 0, min(available, len(selected)))
	for _, candidate := range selected {
		if len(claimed) == available {
			break
		}
		record, accepted, err := b.claimRecord(tx, peer, candidate, leaseUntil)
		if err != nil {
			return nil, err
		}
		if accepted {
			claimed = append(claimed, record)
		}
	}
	if len(claimed) == 0 {
		return claimed, nil
	}
	if err := b.leaseCounts.Put(tx, peerKey, outstanding+uint64(len(claimed))); err != nil {
		return nil, fmt.Errorf("store remote crawl peer leases: %w", err)
	}

	return claimed, nil
}

func (b *Broker) claimRecord(
	tx *vault.Txn,
	peer yagomodel.Hash,
	candidate queueRecord,
	leaseUntil time.Time,
) (queueRecord, bool, error) {
	record, found, err := b.orders.Get(tx, sequenceKey(candidate.Sequence))
	if err != nil {
		return queueRecord{}, false, fmt.Errorf("read remote crawl order: %w", err)
	}
	if !found || record.State != queueStatePending {
		return queueRecord{}, false, nil
	}
	record.State = queueStateLeased
	record.Peer = peer.String()
	record.LeaseUntil = leaseUntil.UnixNano()
	record.Attempts++
	if err := b.orders.Put(tx, sequenceKey(record.Sequence), record); err != nil {
		return queueRecord{}, false, fmt.Errorf("lease remote crawl order: %w", err)
	}
	if _, err := b.pending.Delete(tx, sequenceKey(record.Sequence)); err != nil {
		return queueRecord{}, false, fmt.Errorf("remove pending remote crawl order: %w", err)
	}
	if err := b.leaseExpiries.Put(
		tx,
		leaseExpiryKey(record.LeaseUntil, record.Sequence),
		leaseExpiryRecord{Sequence: record.Sequence},
	); err != nil {
		return queueRecord{}, false, fmt.Errorf("index remote crawl lease expiry: %w", err)
	}

	return record, true, nil
}
