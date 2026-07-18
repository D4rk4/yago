package crawlbroker

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	orderBucket                vault.Name = "crawlorders"
	normalOrderIndexBucket     vault.Name = "crawlordersnormal"
	automaticOrderIndexBucket  vault.Name = "crawlordersautomatic"
	seqBucket                  vault.Name = "crawlorderseq"
	idempotencyBucket          vault.Name = "crawlorderkeys"
	priorityIndexFormatVersion uint64     = 1
)

var (
	seqKey                 = vault.Key("next")
	priorityIndexNextKey   = vault.Key("priorityIndexNext")
	priorityIndexFormatKey = vault.Key("priorityIndexFormat")
	priorityIndexMarker    = []byte{1}
	marshalCrawlOrder      = yagocrawlcontract.MarshalCrawlOrder
	beforeQueueWait        = func() {}
)

type orderCodec struct{}

func (orderCodec) Encode(v []byte) ([]byte, error) { return v, nil }

func (orderCodec) Decode(raw []byte) ([]byte, error) { return raw, nil }

type sequenceCodec struct{}

func (sequenceCodec) Encode(v uint64) ([]byte, error) {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, v)

	return buf, nil
}

func (sequenceCodec) Decode(raw []byte) (uint64, error) {
	if len(raw) != 8 {
		return 0, fmt.Errorf("decode order sequence: length %d", len(raw))
	}

	return binary.BigEndian.Uint64(raw), nil
}

// DurableOrderQueue is a FIFO of crawl orders persisted in the node's storage so
// queued orders survive a node restart and stay claimable across crawler
// restarts. Orders move from the pending FIFO into a leased state when streamed
// to a worker; a lease is settled by an ack, requeued by a nak, and reclaimed
// when it expires without a heartbeat.
type DurableOrderQueue struct {
	vault                    *vault.Vault
	orders                   *vault.Collection[[]byte]
	normalOrderIndex         *vault.Collection[[]byte]
	automaticOrderIndex      *vault.Collection[[]byte]
	seq                      *vault.Collection[uint64]
	keys                     *vault.Collection[uint64]
	leases                   *vault.Collection[leaseRecord]
	leaseSettlements         *vault.Collection[leaseSettlementRecord]
	leaseSettlementOrder     *vault.Collection[[]byte]
	leaseSettlementExpiry    *vault.Collection[[]byte]
	leaseControlTargets      *vault.Collection[leaseControlTarget]
	completedControlTargets  *vault.Collection[leaseControlTarget]
	terminalSettlementSecret []byte
	leaseTTL                 time.Duration
	notify                   chan struct{}
	prioritizeAutomatic      atomic.Bool
	// extendedAt remembers when each worker's leases were last durably
	// extended, so frequent heartbeats (they also carry control directives, so
	// their cadence is short) skip the per-beat fsync and only refresh the
	// durable deadlines every leaseTTL/4 (IO-AGG-02).
	mu              sync.Mutex
	extendedAt      map[string]time.Time
	leaseMutation   sync.RWMutex
	growthAdmission GrowthAdmission
}

func newDurableOrderQueue(
	v *vault.Vault,
	leaseTTL time.Duration,
	admissions ...GrowthAdmission,
) (*DurableOrderQueue, error) {
	collections, err := registerOrderQueueCollections(v)
	if err != nil {
		return nil, err
	}

	queue := &DurableOrderQueue{
		vault:                    v,
		orders:                   collections.orders,
		normalOrderIndex:         collections.normalOrderIndex,
		automaticOrderIndex:      collections.automaticOrderIndex,
		seq:                      collections.sequence,
		keys:                     collections.idempotencyKeys,
		leases:                   collections.leases,
		leaseSettlements:         collections.leaseSettlements,
		leaseSettlementOrder:     collections.leaseSettlementOrder,
		leaseSettlementExpiry:    collections.leaseSettlementExpiry,
		leaseControlTargets:      collections.leaseControlTargets,
		completedControlTargets:  collections.completedControlTargets,
		terminalSettlementSecret: collections.terminalSettlementSecret,
		leaseTTL:                 leaseTTL,
		notify:                   make(chan struct{}, 1),
		extendedAt:               map[string]time.Time{},
	}
	if len(admissions) > 0 {
		queue.growthAdmission = admissions[0]
	}
	queue.prioritizeAutomatic.Store(true)

	return queue, nil
}

// Publish enqueues a crawl order for delivery without idempotency. It satisfies
// the crawl dispatch endpoint's order queue port through PublishOnce.
func (q *DurableOrderQueue) Publish(ctx context.Context, order yagocrawlcontract.CrawlOrder) error {
	_, err := q.PublishOnce(ctx, "", order)

	return err
}

// PublishOnce enqueues a crawl order for delivery. When key is non-empty and has
// already been accepted, nothing is enqueued and duplicate is true, so a retried
// crawl-start request with the same idempotency key does not create a second
// order. An empty key disables idempotency and always enqueues.
func (q *DurableOrderQueue) PublishOnce(
	ctx context.Context,
	key string,
	order yagocrawlcontract.CrawlOrder,
) (bool, error) {
	data, err := marshalCrawlOrder(order)
	if err != nil {
		return false, fmt.Errorf("encode crawl order: %w", err)
	}
	duplicate, err := q.admitOrderGrowth(ctx, key)
	if err != nil {
		return false, fmt.Errorf("enqueue crawl order: %w", err)
	}
	if duplicate {
		return true, nil
	}

	duplicate = false
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		if key != "" {
			_, seen, err := q.keys.Get(tx, vault.Key(key))
			if err != nil {
				return fmt.Errorf("read idempotency key: %w", err)
			}
			if seen {
				duplicate = true

				return nil
			}
		}
		seq, err := q.enqueueTx(tx, data, order.Priority)
		if err != nil {
			return err
		}
		if key != "" {
			if err := q.keys.Put(tx, vault.Key(key), seq); err != nil {
				return fmt.Errorf("record idempotency key: %w", err)
			}
		}

		return nil
	}); err != nil {
		return false, fmt.Errorf("enqueue crawl order: %w", err)
	}
	if !duplicate {
		q.signal()
	}

	return duplicate, nil
}

func (q *DurableOrderQueue) enqueueTx(
	tx *vault.Txn,
	data []byte,
	priority yagocrawlcontract.CrawlOrderPriority,
) (uint64, error) {
	next, _, err := q.seq.Get(tx, seqKey)
	if err != nil {
		return 0, fmt.Errorf("read order sequence: %w", err)
	}
	key := orderKey(next)
	if err := q.orders.Put(tx, key, data); err != nil {
		return 0, fmt.Errorf("store crawl order: %w", err)
	}
	if err := q.priorityIndex(priority).Put(tx, key, priorityIndexMarker); err != nil {
		return 0, fmt.Errorf("store crawl order priority: %w", err)
	}
	if err := q.seq.Put(tx, seqKey, next+1); err != nil {
		return 0, fmt.Errorf("advance order sequence: %w", err)
	}
	if err := q.seq.Put(tx, priorityIndexNextKey, next+1); err != nil {
		return 0, fmt.Errorf("advance crawl order priority index: %w", err)
	}

	return next, nil
}

func (q *DurableOrderQueue) signal() {
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

func orderKey(seq uint64) vault.Key {
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, seq)

	return vault.Key(buf)
}
