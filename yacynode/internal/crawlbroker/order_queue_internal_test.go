package crawlbroker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacynode/internal/memvault"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

type scriptedEngine struct {
	buckets         map[vault.Name]map[string][]byte
	provisionErrors map[vault.Name]error
	putErrors       map[vault.Name]error
	deleteErrors    map[vault.Name]error
	scanErrors      map[vault.Name]error
}

func newScriptedEngine() *scriptedEngine {
	return &scriptedEngine{
		buckets:         map[vault.Name]map[string][]byte{},
		provisionErrors: map[vault.Name]error{},
		putErrors:       map[vault.Name]error{},
		deleteErrors:    map[vault.Name]error{},
		scanErrors:      map[vault.Name]error{},
	}
}

func (e *scriptedEngine) Update(ctx context.Context, fn func(vault.EngineTxn) error) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("context: %w", err)
	}

	return fn(scriptedTxn{engine: e, writable: true})
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

func (b scriptedBucket) Put(key vault.Key, raw []byte) error {
	if err := b.engine.putErrors[b.name]; err != nil {
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

func testOrder(name string) yacycrawlcontract.CrawlOrder {
	return yacycrawlcontract.CrawlOrder{
		Provenance: []byte("admin"),
		Profile:    yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{Name: name}),
		Requests:   []yacycrawlcontract.CrawlRequest{{URL: "https://example.org/" + name}},
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
		data, _, err := queue.leaseNext(ctx, "worker")
		if err != nil {
			t.Fatalf("dequeue: %v", err)
		}
		order, err := yacycrawlcontract.UnmarshalCrawlOrder(data)
		if err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if order.Profile.Name != want {
			t.Fatalf("order = %q, want %q", order.Profile.Name, want)
		}
	}
}

func TestDurableOrderQueueDequeueBlocksThenWakes(t *testing.T) {
	queue := memQueue(t)
	ctx := context.Background()
	parked := signalOnQueueWait(t)

	got := make(chan string, 1)
	go func() {
		data, _, err := queue.leaseNext(ctx, "worker")
		if err != nil {
			got <- "error"

			return
		}
		order, _ := yacycrawlcontract.UnmarshalCrawlOrder(data)
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
		_, _, err := queue.leaseNext(ctx, "worker")
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
	if _, _, err := queue.leaseNext(ctx, "worker"); err == nil {
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
	for _, bucket := range []vault.Name{orderBucket, seqBucket, leaseBucket} {
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
	marshalCrawlOrder = func(yacycrawlcontract.CrawlOrder) ([]byte, error) {
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

	seqDecode := scriptedQueue(t)
	seqDecode.engine.buckets[seqBucket][string(seqKey)] = []byte{1, 2, 3}
	if err := seqDecode.queue.Publish(ctx, testOrder("z")); err == nil {
		t.Fatal("expected enqueue error on sequence decode failure")
	}

	scanFail := scriptedQueue(t)
	_ = scanFail.queue.Publish(ctx, testOrder("y"))
	scanFail.engine.scanErrors[orderBucket] = errors.New("scan failed")
	if _, _, _, err := scanFail.queue.leasePop(ctx, "worker"); err == nil {
		t.Fatal("expected pop error on scan failure")
	}

	deleteFail := scriptedQueue(t)
	_ = deleteFail.queue.Publish(ctx, testOrder("z"))
	deleteFail.engine.deleteErrors[orderBucket] = errors.New("delete failed")
	if _, _, _, err := deleteFail.queue.leasePop(ctx, "worker"); err == nil {
		t.Fatal("expected pop error on delete failure")
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
