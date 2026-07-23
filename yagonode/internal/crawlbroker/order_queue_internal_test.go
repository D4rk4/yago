package crawlbroker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type scriptedEngine struct {
	buckets         map[vault.Name]map[string][]byte
	provisionErrors map[vault.Name]error
	putErrors       map[vault.Name]error
	putKeyErrors    map[vault.Name]map[string]error
	readErrors      map[vault.Name]error
	deleteErrors    map[vault.Name]error
	scanErrors      map[vault.Name]error
	replayNext      bool
	betweenReplay   func()
}

func newScriptedEngine() *scriptedEngine {
	return &scriptedEngine{
		buckets:         map[vault.Name]map[string][]byte{},
		provisionErrors: map[vault.Name]error{},
		putErrors:       map[vault.Name]error{},
		putKeyErrors:    map[vault.Name]map[string]error{},
		readErrors:      map[vault.Name]error{},
		deleteErrors:    map[vault.Name]error{},
		scanErrors:      map[vault.Name]error{},
	}
}

func (e *scriptedEngine) Update(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}
	if e.replayNext {
		e.replayNext = false
		before := cloneScriptedBuckets(e.buckets)
		if err := fn(scriptedTxn{engine: e, writable: true}); err != nil {
			return err
		}
		e.buckets = before
		if e.betweenReplay != nil {
			e.betweenReplay()
		}
	}

	return fn(scriptedTxn{engine: e, writable: true})
}

func cloneScriptedBuckets(
	source map[vault.Name]map[string][]byte,
) map[vault.Name]map[string][]byte {
	cloned := make(map[vault.Name]map[string][]byte, len(source))
	for name, bucket := range source {
		cloned[name] = make(map[string][]byte, len(bucket))
		for key, value := range bucket {
			cloned[name][key] = append([]byte(nil), value...)
		}
	}

	return cloned
}

func (e *scriptedEngine) View(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}

	return fn(scriptedTxn{engine: e})
}

func (e *scriptedEngine) Provision(name vault.Name) error {
	if err := e.provisionErrors[name]; err != nil {
		return err
	}
	if e.buckets[name] == nil {
		e.buckets[name] = map[string][]byte{}
	}

	return nil
}

func (e *scriptedEngine) UsedBytes(context.Context) (int64, error) { return 0, nil }
func (e *scriptedEngine) QuotaBytes() int64                        { return 0 }
func (e *scriptedEngine) Close() error                             { return nil }

type scriptedTxn struct {
	engine   *scriptedEngine
	writable bool
}

func (t scriptedTxn) Bucket(name vault.Name) vault.EngineBucket {
	return scriptedBucket{engine: t.engine, name: name}
}

func (t scriptedTxn) Writable() bool { return t.writable }

type scriptedBucket struct {
	engine *scriptedEngine
	name   vault.Name
}

func (b scriptedBucket) Get(key vault.Key) []byte {
	raw, ok := b.engine.buckets[b.name][string(key)]
	if !ok {
		return nil
	}

	return append([]byte(nil), raw...)
}

func (b scriptedBucket) ReadValue(key vault.Key) ([]byte, bool, error) {
	if err := b.engine.readErrors[b.name]; err != nil {
		return nil, false, err
	}
	raw := b.Get(key)

	return raw, raw != nil, nil
}

func (b scriptedBucket) Put(key vault.Key, raw []byte) error {
	if err := b.engine.putErrors[b.name]; err != nil {
		return err
	}
	if err := b.engine.putKeyErrors[b.name][string(key)]; err != nil {
		return err
	}
	b.engine.buckets[b.name][string(key)] = append([]byte(nil), raw...)

	return nil
}

func (b scriptedBucket) Delete(key vault.Key) error {
	if err := b.engine.deleteErrors[b.name]; err != nil {
		return err
	}
	delete(b.engine.buckets[b.name], string(key))

	return nil
}

func (b scriptedBucket) Scan(prefix vault.Key, fn func(vault.Key, []byte) (bool, error)) error {
	if err := b.engine.scanErrors[b.name]; err != nil {
		return err
	}
	keys := make([]string, 0, len(b.engine.buckets[b.name]))
	for key := range b.engine.buckets[b.name] {
		if bytes.HasPrefix([]byte(key), prefix) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	for _, key := range keys {
		again, err := fn(vault.Key(key), append([]byte(nil), b.engine.buckets[b.name][key]...))
		if err != nil {
			return err
		}
		if !again {
			return nil
		}
	}

	return nil
}

func testOrder(name string) yagocrawlcontract.CrawlOrder {
	return yagocrawlcontract.CrawlOrder{
		Provenance: []byte("admin"),
		Profile:    yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{Name: name}),
		Requests:   []yagocrawlcontract.CrawlRequest{{URL: "https://example.org/" + name}},
	}
}

func memQueue(t *testing.T) *DurableOrderQueue {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("open memvault: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	queue, err := newDurableOrderQueue(v, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}

	return queue
}

func TestDurableOrderQueueFIFO(t *testing.T) {
	queue := memQueue(t)
	ctx := context.Background()
	for _, name := range []string{"a", "b", "c"} {
		if err := queue.Publish(ctx, testOrder(name)); err != nil {
			t.Fatalf("publish %s: %v", name, err)
		}
	}
	for _, want := range []string{"a", "b", "c"} {
		data, err := queue.leaseNext(ctx)
		if err != nil {
			t.Fatalf("dequeue: %v", err)
		}
		order, err := yagocrawlcontract.UnmarshalCrawlOrder(data)
		if err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if order.Profile.Name != want {
			t.Fatalf("order = %q, want %q", order.Profile.Name, want)
		}
	}
}

func TestPublishOnceIsIdempotent(t *testing.T) {
	queue := memQueue(t)
	ctx := context.Background()

	dup, err := queue.PublishOnce(ctx, "start", testOrder("x"))
	if err != nil || dup {
		t.Fatalf("first publish dup=%v err=%v, want false/nil", dup, err)
	}
	dup, err = queue.PublishOnce(ctx, "start", testOrder("y"))
	if err != nil || !dup {
		t.Fatalf("second publish dup=%v err=%v, want true/nil", dup, err)
	}
	if _, err := queue.PublishOnce(ctx, "", testOrder("z")); err != nil {
		t.Fatalf("keyless publish: %v", err)
	}

	for _, want := range []string{"x", "z"} {
		data, err := queue.leaseNext(ctx)
		if err != nil {
			t.Fatalf("dequeue: %v", err)
		}
		order, err := yagocrawlcontract.UnmarshalCrawlOrder(data)
		if err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if order.Profile.Name != want {
			t.Fatalf("order = %q, want %q (duplicate y must be dropped)", order.Profile.Name, want)
		}
	}
}

func TestPublishOnceSurfacesKeyErrors(t *testing.T) {
	ctx := context.Background()

	putFail := scriptedQueue(t)
	putFail.engine.putErrors[idempotencyBucket] = errors.New("key put failed")
	if _, err := putFail.queue.PublishOnce(ctx, "k", testOrder("x")); err == nil {
		t.Fatal("expected error on idempotency key put failure")
	}

	getDecode := scriptedQueue(t)
	getDecode.engine.buckets[idempotencyBucket]["k"] = []byte{1, 2, 3}
	if _, err := getDecode.queue.PublishOnce(ctx, "k", testOrder("y")); err == nil {
		t.Fatal("expected error on idempotency key decode failure")
	}
}

func TestDurableOrderQueueDequeueBlocksThenWakes(t *testing.T) {
	queue := memQueue(t)
	ctx := context.Background()
	parked := signalOnQueueWait(t)

	got := make(chan string, 1)
	go func() {
		data, err := queue.leaseNext(ctx)
		if err != nil {
			got <- "error"

			return
		}
		order, _ := yagocrawlcontract.UnmarshalCrawlOrder(data)
		got <- order.Profile.Name
	}()

	<-parked
	if err := queue.Publish(ctx, testOrder("late")); err != nil {
		t.Fatalf("publish: %v", err)
	}
	select {
	case name := <-got:
		if name != "late" {
			t.Fatalf("dequeued %q, want late", name)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dequeue did not wake after publish")
	}
}

func TestDurableOrderQueueDequeueCancelledWhileBlocked(t *testing.T) {
	queue := memQueue(t)
	ctx, cancel := context.WithCancel(context.Background())
	parked := signalOnQueueWait(t)

	done := make(chan error, 1)
	go func() {
		_, err := queue.leaseNext(ctx)
		done <- err
	}()

	<-parked
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected cancellation error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("dequeue did not observe cancellation")
	}
}

func TestDurableOrderQueueDequeueHonorsContext(t *testing.T) {
	queue := memQueue(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := queue.leaseNext(ctx); err == nil {
		t.Fatal("expected dequeue to fail on cancelled context")
	}
}

// signalOnQueueWait routes the queue's pre-wait hook to a channel so a test can
// block until the dequeue goroutine has parked in its select, then drive the
// notify or cancellation arm deterministically.
func signalOnQueueWait(t *testing.T) <-chan struct{} {
	t.Helper()
	restore := beforeQueueWait
	t.Cleanup(func() { beforeQueueWait = restore })
	parked := make(chan struct{}, 1)
	beforeQueueWait = func() {
		select {
		case parked <- struct{}{}:
		default:
		}
	}

	return parked
}

func TestSequenceCodecRejectsBadLength(t *testing.T) {
	if _, err := (sequenceCodec{}).Decode([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected decode error for short sequence")
	}
}

func TestNewDurableOrderQueueRegisterErrors(t *testing.T) {
	for _, bucket := range []vault.Name{
		orderBucket, normalOrderIndexBucket, automaticOrderIndexBucket,
		seqBucket, idempotencyBucket, leaseBucket, leaseSettlementBucket,
		leaseSettlementOrderBucket, leaseSettlementExpiryBucket, leaseControlTargetBucket,
		completedLeaseControlTargetBucket, terminalSettlementSecretBucket,
	} {
		engine := newScriptedEngine()
		engine.provisionErrors[bucket] = errors.New("provision failed")
		v, err := vault.New(engine)
		if err != nil {
			t.Fatalf("vault.New: %v", err)
		}
		if _, err := newDurableOrderQueue(v, DefaultLeaseTTL); err == nil {
			t.Fatalf("expected register error for bucket %s", bucket)
		}
	}
}

func TestPublishReportsMarshalError(t *testing.T) {
	restore := marshalCrawlOrder
	t.Cleanup(func() { marshalCrawlOrder = restore })
	marshalCrawlOrder = func(yagocrawlcontract.CrawlOrder) ([]byte, error) {
		return nil, errors.New("marshal failed")
	}
	if err := memQueue(t).Publish(context.Background(), testOrder("x")); err == nil {
		t.Fatal("expected publish to surface a marshal error")
	}
}

func TestDurableOrderQueueSurfaceVaultErrors(t *testing.T) {
	ctx := context.Background()

	seqPut := scriptedQueue(t)
	seqPut.engine.putErrors[seqBucket] = errors.New("seq put failed")
	if err := seqPut.queue.Publish(ctx, testOrder("x")); err == nil {
		t.Fatal("expected enqueue error on sequence put failure")
	}

	ordersPut := scriptedQueue(t)
	ordersPut.engine.putErrors[orderBucket] = errors.New("orders put failed")
	if err := ordersPut.queue.Publish(ctx, testOrder("y")); err == nil {
		t.Fatal("expected enqueue error on orders put failure")
	}

	priorityPut := scriptedQueue(t)
	priorityPut.engine.putErrors[normalOrderIndexBucket] = errors.New("priority put failed")
	if err := priorityPut.queue.Publish(ctx, testOrder("priority")); err == nil {
		t.Fatal("expected enqueue error on priority put failure")
	}

	priorityWatermarkPut := scriptedQueue(t)
	priorityWatermarkPut.engine.putKeyErrors[seqBucket] = map[string]error{
		string(priorityIndexNextKey): errors.New("priority watermark put failed"),
	}
	if err := priorityWatermarkPut.queue.Publish(ctx, testOrder("watermark")); err == nil {
		t.Fatal("expected enqueue error on priority watermark failure")
	}

	seqDecode := scriptedQueue(t)
	seqDecode.engine.buckets[seqBucket][string(seqKey)] = []byte{1, 2, 3}
	if err := seqDecode.queue.Publish(ctx, testOrder("z")); err == nil {
		t.Fatal("expected enqueue error on sequence decode failure")
	}

	scanFail := scriptedQueue(t)
	_ = scanFail.queue.Publish(ctx, testOrder("y"))
	scanFail.engine.scanErrors[normalOrderIndexBucket] = errors.New("scan failed")
	if _, _, _, err := scanFail.queue.leasePop(ctx, "worker"); err == nil {
		t.Fatal("expected pop error on scan failure")
	}

	automaticScanFail := scriptedQueue(t)
	automaticScanFail.engine.scanErrors[automaticOrderIndexBucket] = errors.New("scan failed")
	if _, _, _, err := automaticScanFail.queue.leasePop(ctx, "worker"); err == nil {
		t.Fatal("expected pop error on automatic scan failure")
	}

	burstDecode := scriptedQueue(t)
	burstDecode.engine.buckets[seqBucket][string(priorityBurstKey)] = []byte{1, 2, 3}
	if _, _, _, err := burstDecode.queue.leasePop(ctx, "worker"); err == nil {
		t.Fatal("expected pop error on priority burst decode failure")
	}

	burstPut := scriptedQueue(t)
	_ = burstPut.queue.Publish(ctx, testOrder("normal"))
	_ = burstPut.queue.Publish(ctx, automaticOrder("automatic"))
	burstPut.engine.putErrors[seqBucket] = errors.New("burst put failed")
	if _, _, _, err := burstPut.queue.leasePop(ctx, "worker"); err == nil {
		t.Fatal("expected pop error on priority burst put failure")
	}

	deleteFail := scriptedQueue(t)
	_ = deleteFail.queue.Publish(ctx, testOrder("z"))
	deleteFail.engine.deleteErrors[orderBucket] = errors.New("delete failed")
	if _, _, _, err := deleteFail.queue.leasePop(ctx, "worker"); err == nil {
		t.Fatal("expected pop error on delete failure")
	}

	priorityDeleteFail := scriptedQueue(t)
	_ = priorityDeleteFail.queue.Publish(ctx, testOrder("priority-delete"))
	priorityDeleteFail.engine.deleteErrors[normalOrderIndexBucket] = errors.New("delete failed")
	if _, _, _, err := priorityDeleteFail.queue.leasePop(ctx, "worker"); err == nil {
		t.Fatal("expected pop error on priority delete failure")
	}
}

type scriptedQueueFixture struct {
	engine *scriptedEngine
	queue  *DurableOrderQueue
}

func scriptedQueue(t *testing.T) scriptedQueueFixture {
	t.Helper()
	engine := newScriptedEngine()
	v, err := vault.New(engine)
	if err != nil {
		t.Fatalf("vault.New: %v", err)
	}
	queue, err := newDurableOrderQueue(v, DefaultLeaseTTL)
	if err != nil {
		t.Fatalf("new queue: %v", err)
	}

	return scriptedQueueFixture{engine: engine, queue: queue}
}
