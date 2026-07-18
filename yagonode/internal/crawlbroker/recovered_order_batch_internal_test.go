package crawlbroker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawlcontract/crawlrpc"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestRecoveredOrderStreamFramesOneBatch(t *testing.T) {
	server := newExchangeServer(memQueue(t), nil)
	generation, err := server.sessions.activate(
		"worker",
		"session",
		func() {},
		func() error { return nil },
	)
	if err != nil {
		t.Fatalf("activate worker session: %v", err)
	}
	stream := &fakeOrderStream{ctx: context.Background()}
	orders := []leasedCrawlOrder{
		{LeaseID: "lease-a", OrderData: []byte("a")},
		{LeaseID: "lease-b", OrderData: []byte("b")},
		{LeaseID: "lease-c", OrderData: []byte("c")},
	}
	if err := server.streamRecoveredOrders(
		stream,
		"worker",
		"session",
		generation,
		orders,
	); err != nil {
		t.Fatalf("stream recovered orders: %v", err)
	}
	if len(stream.sent) != len(orders) {
		t.Fatalf("streamed recovered orders = %d, want %d", len(stream.sent), len(orders))
	}
	for index, message := range stream.sent {
		if !message.GetRecovered() || message.GetRecoveredBatchEnd() != (index == len(orders)-1) {
			t.Fatalf("recovered frame %d = %+v", index, message)
		}
		if index == 0 {
			if len(message.GetRecoveredLeaseIds()) != len(orders) {
				t.Fatalf("recovered lease header = %v", message.GetRecoveredLeaseIds())
			}
		} else if len(message.GetRecoveredLeaseIds()) != 0 {
			t.Fatalf("repeated recovered lease header at frame %d", index)
		}
	}
	if err := server.streamRecoveredOrders(
		&fakeOrderStream{ctx: context.Background()},
		"worker",
		"session",
		generation,
		nil,
	); err != nil {
		t.Fatalf("stream empty recovered batch: %v", err)
	}
}

func TestRecoveredOrderStreamRejectsLeaseOverflow(t *testing.T) {
	server := newExchangeServer(memQueue(t), nil)
	orders := make([]leasedCrawlOrder, yagocrawlcontract.MaximumHeartbeatActiveLeases+1)
	err := server.streamRecoveredOrders(
		&fakeOrderStream{ctx: context.Background()},
		"worker",
		"session",
		1,
		orders,
	)
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("overflow status = %v, want resource exhausted", status.Code(err))
	}
}

func TestRecoveredHeartbeatReadsEachLeaseOnceAtScale(t *testing.T) {
	set := withClock(t)
	set(time.Unix(70_000, 0))
	for _, size := range []int{270, yagocrawlcontract.MaximumHeartbeatActiveLeases} {
		t.Run(fmt.Sprintf("leases-%d", size), func(t *testing.T) {
			assertRecoveredHeartbeatLeaseReads(t, size)
		})
	}
}

func assertRecoveredHeartbeatLeaseReads(t *testing.T, size int) {
	t.Helper()
	engine := newLeaseReadCountingEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatalf("open counting vault: %v", err)
	}
	queue, err := newDurableOrderQueue(storage, time.Minute)
	if err != nil {
		t.Fatalf("open order queue: %v", err)
	}
	leaseIDs, err := seedRecoveredHeartbeatLeases(t.Context(), storage, queue, size)
	if err != nil {
		t.Fatalf("seed leases: %v", err)
	}
	adopted, err := queue.adoptWorkerSession(
		t.Context(),
		"worker",
		"current-session",
	)
	if err != nil || len(adopted) != size {
		t.Fatalf("adopted leases = %d, error = %v", len(adopted), err)
	}
	engine.leaseReads = 0
	renewed, _, err := queue.renewLeases(
		t.Context(),
		"worker",
		"current-session",
		leaseIDs,
	)
	if err != nil || len(renewed) != size {
		t.Fatalf("renewed leases = %d, error = %v", len(renewed), err)
	}
	if engine.leaseReads != size {
		t.Fatalf("lease reads = %d, want %d", engine.leaseReads, size)
	}
}

func seedRecoveredHeartbeatLeases(
	ctx context.Context,
	storage *vault.Vault,
	queue *DurableOrderQueue,
	size int,
) ([]string, error) {
	leaseIDs := make([]string, size)
	err := storage.Update(ctx, func(tx *vault.Txn) error {
		for index := range size {
			leaseID := fmt.Sprintf("lease-%04d", index)
			leaseIDs[index] = leaseID
			if err := queue.leases.Put(tx, vault.Key(leaseID), leaseRecord{
				WorkerID:        "worker",
				WorkerSessionID: "old-session",
			}); err != nil {
				return fmt.Errorf("store recovered crawl lease: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("seed recovered crawl leases: %w", err)
	}

	return leaseIDs, nil
}

type leaseReadCountingEngine struct {
	*scriptedEngine
	leaseReads int
}

func newLeaseReadCountingEngine() *leaseReadCountingEngine {
	return &leaseReadCountingEngine{scriptedEngine: newScriptedEngine()}
}

func (e *leaseReadCountingEngine) Update(
	ctx context.Context,
	apply func(vault.EngineTxn) error,
) error {
	return e.scriptedEngine.Update(ctx, func(tx vault.EngineTxn) error {
		return apply(leaseReadCountingTxn{EngineTxn: tx, engine: e})
	})
}

func (e *leaseReadCountingEngine) View(
	ctx context.Context,
	apply func(vault.EngineTxn) error,
) error {
	return e.scriptedEngine.View(ctx, func(tx vault.EngineTxn) error {
		return apply(leaseReadCountingTxn{EngineTxn: tx, engine: e})
	})
}

type leaseReadCountingTxn struct {
	vault.EngineTxn
	engine *leaseReadCountingEngine
}

func (t leaseReadCountingTxn) Bucket(name vault.Name) vault.EngineBucket {
	return leaseReadCountingBucket{
		EngineBucket: t.EngineTxn.Bucket(name),
		engine:       t.engine,
		leaseBucket:  name == leaseBucket,
	}
}

type leaseReadCountingBucket struct {
	vault.EngineBucket
	engine      *leaseReadCountingEngine
	leaseBucket bool
}

func (b leaseReadCountingBucket) Get(key vault.Key) []byte {
	if b.leaseBucket {
		b.engine.leaseReads++
	}

	return b.EngineBucket.Get(key)
}

var _ crawlrpc.CrawlExchange_StreamOrdersServer = (*fakeOrderStream)(nil)
