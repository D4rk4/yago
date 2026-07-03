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
	orderBucket vault.Name = "crawlorders"
	seqBucket   vault.Name = "crawlorderseq"
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
	leases, err := vault.Register(v, leaseBucket, leaseRecordCodec{})
	if err != nil {
		return nil, fmt.Errorf("register crawl order leases: %w", err)
	}

	return &DurableOrderQueue{
		vault:    v,
		orders:   orders,
		seq:      seq,
		leases:   leases,
		leaseTTL: leaseTTL,
		notify:   make(chan struct{}, 1),
	}, nil
}

// Publish enqueues a crawl order for delivery. It satisfies the crawl dispatch
// endpoint's order queue port.
func (q *DurableOrderQueue) Publish(ctx context.Context, order yacycrawlcontract.CrawlOrder) error {
	data, err := marshalCrawlOrder(order)
	if err != nil {
		return fmt.Errorf("encode crawl order: %w", err)
	}

	return q.enqueue(ctx, data)
}

func (q *DurableOrderQueue) enqueue(ctx context.Context, data []byte) error {
	if err := q.vault.Update(ctx, func(tx *vault.Txn) error {
		return q.enqueueTx(tx, data)
	}); err != nil {
		return fmt.Errorf("enqueue crawl order: %w", err)
	}
	q.signal()

	return nil
}

func (q *DurableOrderQueue) enqueueTx(tx *vault.Txn, data []byte) error {
	next, _, err := q.seq.Get(tx, seqKey)
	if err != nil {
		return fmt.Errorf("read order sequence: %w", err)
	}
	if err := q.orders.Put(tx, orderKey(next), data); err != nil {
		return fmt.Errorf("store crawl order: %w", err)
	}
	if err := q.seq.Put(tx, seqKey, next+1); err != nil {
		return fmt.Errorf("advance order sequence: %w", err)
	}

	return nil
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
