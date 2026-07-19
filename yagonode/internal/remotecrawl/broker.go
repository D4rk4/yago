package remotecrawl

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/crawlurls"
	"github.com/D4rk4/yago/yagonode/internal/urlmeta"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var (
	ErrPeerNotTrusted = errors.New("remote crawl peer is not trusted")
	ErrRateLimited    = errors.New("remote crawl peer request rate exceeded")
	ErrQueueFull      = errors.New("remote crawl queue is full")
)

type URLMetadataReceiver interface {
	Receive(context.Context, []yagomodel.URIMetadataRow) (urlmeta.Receipt, error)
}

type Broker struct {
	mu            sync.Mutex
	storage       *vault.Vault
	orders        *vault.Collection[queueRecord]
	urlSequences  *vault.Keyspace[uint64]
	sequence      *vault.Keyspace[uint64]
	requestRates  *vault.Keyspace[requestRateRecord]
	leaseCounts   *vault.Collection[uint64]
	leaseExpiries *vault.Collection[leaseExpiryRecord]
	pending       *vault.Collection[pendingRecord]
	receiver      URLMetadataReceiver
	policy        destinationPolicy
	trusted       map[yagomodel.Hash]struct{}
	config        Config
	observers     []Observer
}

func (b *Broker) Trusted(peer yagomodel.Hash) bool {
	_, trusted := b.trusted[peer]

	return trusted
}

func (b *Broker) URLsForRemoteCrawl(
	ctx context.Context,
	peer yagomodel.Hash,
	count int,
	timeout time.Duration,
) ([]crawlurls.RemoteCrawlURL, error) {
	if !b.Trusted(peer) {
		b.observe(ctx, Observation{Action: "lease", Outcome: "untrusted", Peer: peer}, true)

		return nil, fmt.Errorf(
			"%w: %w: %s",
			crawlurls.ErrRemoteCrawlRejected,
			ErrPeerNotTrusted,
			peer,
		)
	}
	if count < 1 {
		return nil, nil
	}
	if count > MaximumRemoteCrawlBatch {
		count = MaximumRemoteCrawlBatch
	}
	leaseCtx, cancel := minimumDeadline(ctx, timeout)
	defer cancel()

	now := b.config.Now().UTC()
	pending, available, err := b.prepareLease(leaseCtx, peer, now)
	if err != nil {
		if errors.Is(err, ErrRateLimited) {
			b.observe(ctx, Observation{Action: "lease", Outcome: "rate_limited", Peer: peer}, true)

			return nil, fmt.Errorf("%w: %w", crawlurls.ErrRemoteCrawlRejected, err)
		}

		return nil, err
	}
	if count > available {
		count = available
	}
	if count < 1 {
		b.observe(ctx, Observation{Action: "lease", Outcome: "outstanding_limit", Peer: peer}, true)

		return nil, nil
	}
	selected, err := b.selectLeaseRecords(ctx, leaseCtx, pending, count)
	if err != nil {
		return nil, err
	}
	if len(selected) == 0 {
		return nil, nil
	}
	claimed, err := b.finalizeLease(
		leaseCtx,
		peer,
		selected,
		now.Add(b.config.LeaseTTL),
	)
	if err != nil {
		return nil, err
	}
	items := remoteCrawlURLs(claimed)
	b.observe(ctx, Observation{
		Action: "lease", Outcome: "accepted", Peer: peer, Count: len(items),
	}, false)

	return items, nil
}

func publicationTime(unixNano int64) time.Time {
	if unixNano == 0 {
		return time.Time{}
	}

	return time.Unix(0, unixNano).UTC()
}

func (b *Broker) prepareLease(
	ctx context.Context,
	peer yagomodel.Hash,
	now time.Time,
) ([]queueRecord, int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.requeueExpired(ctx, now); err != nil {
		return nil, 0, err
	}
	if err := b.consumePeerRequest(ctx, peer, now); err != nil {
		return nil, 0, err
	}
	outstanding, pending, err := b.leaseCandidates(ctx, peer)
	if err != nil {
		return nil, 0, err
	}

	return pending, availableLeaseSlots(b.config.OutstandingPerPeer, outstanding), nil
}

func (b *Broker) finalizeLease(
	ctx context.Context,
	peer yagomodel.Hash,
	selected []queueRecord,
	leaseUntil time.Time,
) ([]queueRecord, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.claim(ctx, peer, selected, leaseUntil)
}

func (b *Broker) leaseCandidates(
	ctx context.Context,
	peer yagomodel.Hash,
) (uint64, []queueRecord, error) {
	var outstanding uint64
	pendingCapacity := b.config.QueueCapacity
	if pendingCapacity > MaximumRemoteCrawlBatch {
		pendingCapacity = MaximumRemoteCrawlBatch
	}
	pending := make([]queueRecord, 0, pendingCapacity)
	err := b.storage.View(ctx, func(tx *vault.Txn) error {
		var err error
		outstanding, _, err = b.leaseCounts.Get(tx, vault.Key(peer.String()))
		if err != nil {
			return fmt.Errorf("read remote crawl peer leases: %w", err)
		}

		return b.pending.Scan(tx, nil, func(_ vault.Key, indexed pendingRecord) (bool, error) {
			record, found, err := b.orders.Get(tx, sequenceKey(indexed.Sequence))
			if err != nil {
				return false, fmt.Errorf("read pending remote crawl order: %w", err)
			}
			if !found || record.State != queueStatePending {
				return false, fmt.Errorf("remote crawl pending index is inconsistent")
			}
			pending = append(pending, record)

			return len(pending) < MaximumRemoteCrawlBatch, nil
		})
	})
	if err != nil {
		return 0, nil, fmt.Errorf("read remote crawl queue: %w", err)
	}

	return outstanding, pending, nil
}

func (b *Broker) claim(
	ctx context.Context,
	peer yagomodel.Hash,
	selected []queueRecord,
	leaseUntil time.Time,
) ([]queueRecord, error) {
	var claimed []queueRecord
	err := b.storage.Update(ctx, func(tx *vault.Txn) error {
		var err error
		claimed, err = b.claimAvailableRecords(tx, peer, selected, leaseUntil)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("claim remote crawl orders: %w", err)
	}

	return claimed, nil
}

func (b *Broker) requeueExpired(ctx context.Context, now time.Time) error {
	expired, err := b.expiredLeases(ctx, now)
	if err != nil {
		return err
	}
	if len(expired) == 0 {
		return nil
	}

	if err := b.storage.Update(ctx, func(tx *vault.Txn) error {
		for _, expiry := range expired {
			if err := b.requeueExpiredLease(tx, expiry); err != nil {
				return err
			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("requeue expired remote crawl leases: %w", err)
	}

	return nil
}

func (b *Broker) consumePeerRequest(
	ctx context.Context,
	peer yagomodel.Hash,
	now time.Time,
) error {
	if err := b.storage.Update(ctx, func(tx *vault.Txn) error {
		key := vault.Key(peer.String())
		rate, found, err := b.requestRates.Get(tx, key)
		if err != nil {
			return fmt.Errorf("read remote crawl peer rate: %w", err)
		}
		windowStart := time.Unix(0, rate.WindowStart)
		if !found || now.Sub(windowStart) >= time.Minute || now.Before(windowStart) {
			rate = requestRateRecord{WindowStart: now.UnixNano()}
		}
		if rate.Requests >= b.config.RequestsPerMinute {
			return ErrRateLimited
		}
		rate.Requests++
		if err := b.requestRates.Put(tx, key, rate); err != nil {
			return fmt.Errorf("store remote crawl peer rate: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("update remote crawl peer request rate: %w", err)
	}

	return nil
}

func (b *Broker) deleteOrder(ctx context.Context, record queueRecord) error {
	if err := b.storage.Update(ctx, func(tx *vault.Txn) error {
		if err := b.releaseLease(tx, record); err != nil {
			return err
		}
		if _, err := b.orders.Delete(tx, sequenceKey(record.Sequence)); err != nil {
			return fmt.Errorf("delete remote crawl order: %w", err)
		}
		if _, err := b.urlSequences.Delete(tx, vault.Key(record.URLHash)); err != nil {
			return fmt.Errorf("delete remote crawl URL sequence: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("remove completed remote crawl order: %w", err)
	}

	return nil
}

func (b *Broker) releaseLease(tx *vault.Txn, record queueRecord) error {
	if record.State != queueStateLeased || record.Peer == "" {
		return nil
	}
	peerKey := vault.Key(record.Peer)
	outstanding, found, err := b.leaseCounts.Get(tx, peerKey)
	if err != nil {
		return fmt.Errorf("read remote crawl peer leases: %w", err)
	}
	if !found || outstanding == 0 {
		return fmt.Errorf("remote crawl peer lease count is inconsistent")
	}
	if outstanding == 1 {
		if _, err := b.leaseCounts.Delete(tx, peerKey); err != nil {
			return fmt.Errorf("delete remote crawl peer leases: %w", err)
		}
	} else if err := b.leaseCounts.Put(tx, peerKey, outstanding-1); err != nil {
		return fmt.Errorf("store remote crawl peer leases: %w", err)
	}
	if _, err := b.leaseExpiries.Delete(
		tx,
		leaseExpiryKey(record.LeaseUntil, record.Sequence),
	); err != nil {
		return fmt.Errorf("delete remote crawl lease expiry: %w", err)
	}

	return nil
}

func (b *Broker) deletePendingOrder(ctx context.Context, record queueRecord) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.storage.Update(ctx, func(tx *vault.Txn) error {
		current, found, err := b.orders.Get(tx, sequenceKey(record.Sequence))
		if err != nil {
			return fmt.Errorf("read rejected remote crawl order: %w", err)
		}
		if !found || current.State != queueStatePending || current.URLHash != record.URLHash {
			return nil
		}
		if _, err := b.orders.Delete(tx, sequenceKey(record.Sequence)); err != nil {
			return fmt.Errorf("delete rejected remote crawl order: %w", err)
		}
		if _, err := b.pending.Delete(tx, sequenceKey(record.Sequence)); err != nil {
			return fmt.Errorf("delete rejected remote crawl pending index: %w", err)
		}
		if _, err := b.urlSequences.Delete(tx, vault.Key(record.URLHash)); err != nil {
			return fmt.Errorf("delete rejected remote crawl URL sequence: %w", err)
		}

		return nil
	}); err != nil {
		return fmt.Errorf("remove rejected remote crawl order: %w", err)
	}

	return nil
}
