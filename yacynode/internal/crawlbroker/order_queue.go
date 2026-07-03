package crawlbroker

import (
	"context"
	"encoding/binary"
	"fmt"
	"time"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

const (
	orderBucket       vault.Name = "crawlorders"
	seqBucket         vault.Name = "crawlorderseq"
	idempotencyBucket vault.Name = "crawlorderkeys"
)

var (
	seqKey            = vault.Key("next")
	marshalCrawlOrder = yacycrawlcontract.MarshalCrawlOrder
	beforeQueueWait   = func() {}
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
	vault    *vault.Vault
	orders   *vault.Collection[[]byte]
	seq      *vault.Collection[uint64]
	keys     *vault.Collection[uint64]
	leases   *vault.Collection[leaseRecord]
	leaseTTL time.Duration
	notify   chan struct{}
}

func newDurableOrderQueue(v *vault.Vault, leaseTTL time.Duration) (*DurableOrderQueue, error) {
	orders, err := vault.Register(v, orderBucket, orderCodec{})
	if err != nil {
		return nil, fmt.Errorf("register crawl order queue: %w", err)
	}
	seq, err := vault.Register(v, seqBucket, sequenceCodec{})
	if err != nil {
		return nil, fmt.Errorf("register crawl order sequence: %w", err)
	}
	keys, err := vault.Register(v, idempotencyBucket, sequenceCodec{})
	if err != nil {
		return nil, fmt.Errorf("register crawl order idempotency keys: %w", err)
	}
	leases, err := vault.Register(v, leaseBucket, leaseRecordCodec{})
	if err != nil {
		return nil, fmt.Errorf("register crawl order leases: %w", err)
	}

	return &DurableOrderQueue{
		vault:    v,
		orders:   orders,
		seq:      seq,
		keys:     keys,
		leases:   leases,
		leaseTTL: leaseTTL,
		notify:   make(chan struct{}, 1),
	}, nil
}

// Publish enqueues a crawl order for delivery without idempotency. It satisfies
// the crawl dispatch endpoint's order queue port through PublishOnce.
func (q *DurableOrderQueue) Publish(ctx context.Context, order yacycrawlcontract.CrawlOrder) error {
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
	order yacycrawlcontract.CrawlOrder,
) (bool, error) {
	data, err := marshalCrawlOrder(order)
	if err != nil {
		return false, fmt.Errorf("encode crawl order: %w", err)
	}

	duplicate := false
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
		seq, err := q.enqueueTx(tx, data)
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

func (q *DurableOrderQueue) enqueueTx(tx *vault.Txn, data []byte) (uint64, error) {
	next, _, err := q.seq.Get(tx, seqKey)
	if err != nil {
		return 0, fmt.Errorf("read order sequence: %w", err)
	}
	if err := q.orders.Put(tx, orderKey(next), data); err != nil {
		return 0, fmt.Errorf("store crawl order: %w", err)
	}
	if err := q.seq.Put(tx, seqKey, next+1); err != nil {
		return 0, fmt.Errorf("advance order sequence: %w", err)
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
