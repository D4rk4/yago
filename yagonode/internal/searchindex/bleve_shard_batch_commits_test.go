package searchindex

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blevesearch/bleve/v2"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

type controlledBatchShard struct {
	bleveIndexContract
	staging       bleve.Index
	started       chan struct{}
	startOnce     sync.Once
	release       <-chan struct{}
	closed        chan struct{}
	closeOnce     sync.Once
	commitDelay   time.Duration
	commitFailure error
	commitMu      sync.Mutex
	commitCalls   int
	concurrency   *batchCommitConcurrency
}

type batchCommitConcurrency struct {
	mu      sync.Mutex
	active  int
	maximum int
}

func (c *batchCommitConcurrency) enter() {
	c.mu.Lock()
	c.active++
	c.maximum = max(c.maximum, c.active)
	c.mu.Unlock()
}

func (c *batchCommitConcurrency) leave() {
	c.mu.Lock()
	c.active--
	c.mu.Unlock()
}

func (c *batchCommitConcurrency) snapshot() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.active, c.maximum
}

func newControlledBatchShard(
	testingTarget testing.TB,
	release <-chan struct{},
) *controlledBatchShard {
	testingTarget.Helper()
	staging, err := bleve.NewMemOnly(bleve.NewIndexMapping())
	if err != nil {
		testingTarget.Fatalf("create staging index: %v", err)
	}

	return &controlledBatchShard{
		staging:     staging,
		started:     make(chan struct{}),
		release:     release,
		closed:      make(chan struct{}),
		concurrency: &batchCommitConcurrency{},
	}
}

func (s *controlledBatchShard) NewBatch() *bleve.Batch {
	return s.staging.NewBatch()
}

func (s *controlledBatchShard) Batch(*bleve.Batch) error {
	s.concurrency.enter()
	defer s.concurrency.leave()
	s.commitMu.Lock()
	s.commitCalls++
	s.commitMu.Unlock()
	s.startOnce.Do(func() { close(s.started) })
	if s.release != nil {
		<-s.release
	}
	if s.commitDelay > 0 {
		time.Sleep(s.commitDelay)
	}

	return s.commitFailure
}

func (s *controlledBatchShard) committed() int {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()

	return s.commitCalls
}

func (s *controlledBatchShard) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	if err := s.staging.Close(); err != nil {
		return fmt.Errorf("close staging index: %w", err)
	}

	return nil
}

func documentForShard(
	testingTarget testing.TB,
	shards []bleve.Index,
	shardNumber int,
) documentstore.Document {
	testingTarget.Helper()
	for candidate := range 10_000 {
		document := batchDoc(
			fmt.Sprintf("https://shard.example/%d", candidate),
			fmt.Sprintf("document %d", candidate),
		)
		if diskShardNumber(shards, documentID(document)) == shardNumber {
			return document
		}
	}
	testingTarget.Fatalf("find document for shard %d", shardNumber)

	return documentstore.Document{}
}

func awaitBatchSignal(
	testingTarget testing.TB,
	signal <-chan struct{},
	name string,
) {
	testingTarget.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		testingTarget.Fatalf("timed out waiting for %s", name)
	}
}

func TestBleveShardBatchCommitsUseFourLanesAndWaitForEveryShard(t *testing.T) {
	release := make(chan struct{})
	concurrency := &batchCommitConcurrency{}
	controlled := make([]*controlledBatchShard, 5)
	shards := make([]bleve.Index, len(controlled))
	batches := make([]*bleve.Batch, len(controlled))
	for shardNumber := range controlled {
		controlled[shardNumber] = newControlledBatchShard(t, release)
		controlled[shardNumber].concurrency = concurrency
		shards[shardNumber] = controlled[shardNumber]
		batches[shardNumber] = controlled[shardNumber].NewBatch()
	}
	t.Cleanup(func() { closeBleveShards(shards) })
	finished := make(chan error, 1)
	go func() {
		finished <- commitBleveShardBatches(shards, batches)
	}()
	for shardNumber := range bleveShardCommitLanes {
		awaitBatchSignal(
			t,
			controlled[shardNumber].started,
			fmt.Sprintf("shard %d commit", shardNumber),
		)
	}
	select {
	case <-controlled[bleveShardCommitLanes].started:
		close(release)
		t.Fatal("queued shard commit started while every lane was occupied")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if err := <-finished; err != nil {
		t.Fatalf("commitBleveShardBatches: %v", err)
	}
	for shardNumber, shard := range controlled {
		if shard.committed() != 1 {
			t.Fatalf("shard %d commits = %d, want 1", shardNumber, shard.committed())
		}
	}
	active, maximum := concurrency.snapshot()
	if active != 0 || maximum != bleveShardCommitLanes {
		t.Fatalf("commit concurrency active=%d maximum=%d, want 0/%d",
			active, maximum, bleveShardCommitLanes)
	}
}

func TestBleveShardBatchCommitFailureIsDeterministic(t *testing.T) {
	firstRelease := make(chan struct{})
	released := make(chan struct{})
	close(released)
	firstFailure := errors.New("first shard failed")
	lastFailure := errors.New("last shard failed")
	first := newControlledBatchShard(t, firstRelease)
	first.commitFailure = firstFailure
	second := newControlledBatchShard(t, released)
	third := newControlledBatchShard(t, released)
	last := newControlledBatchShard(t, released)
	last.commitFailure = lastFailure
	controlled := []*controlledBatchShard{first, second, third, last}
	shards := []bleve.Index{first, second, third, last}
	index := &BleveDiskIndex{shards: shards, now: time.Now}
	documents := make([]documentstore.Document, len(shards))
	for shardNumber := range shards {
		documents[shardNumber] = documentForShard(t, shards, shardNumber)
	}

	finished := make(chan error, 1)
	go func() { finished <- index.IndexBatch(t.Context(), documents) }()
	for shardNumber, shard := range controlled {
		awaitBatchSignal(t, shard.started, fmt.Sprintf("shard %d commit", shardNumber))
	}
	select {
	case err := <-finished:
		close(firstRelease)
		t.Fatalf("batch returned before lowest shard completed: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(firstRelease)
	err := <-finished
	if !errors.Is(err, firstFailure) || errors.Is(err, lastFailure) ||
		!strings.Contains(err.Error(), "shard 1") {
		t.Fatalf("IndexBatch error = %v, want lowest shard failure %v", err, firstFailure)
	}
	for shardNumber, shard := range controlled {
		if shard.committed() != 1 {
			t.Fatalf("shard %d commits = %d, want 1", shardNumber, shard.committed())
		}
	}
	if !index.lastUpdate().IsZero() {
		t.Fatalf("failed batch updated timestamp to %v", index.lastUpdate())
	}
	if err := index.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestBleveDiskIndexCloseWaitsForBoundedBatchCommits(t *testing.T) {
	release := make(chan struct{})
	controlled := make([]*controlledBatchShard, 4)
	shards := make([]bleve.Index, len(controlled))
	for shardNumber := range controlled {
		controlled[shardNumber] = newControlledBatchShard(t, release)
		shards[shardNumber] = controlled[shardNumber]
	}
	index := &BleveDiskIndex{shards: shards, now: time.Now}
	documents := make([]documentstore.Document, len(shards))
	for shardNumber := range shards {
		documents[shardNumber] = documentForShard(t, shards, shardNumber)
	}

	batchFinished := make(chan error, 1)
	go func() {
		batchFinished <- index.IndexBatch(t.Context(), documents)
	}()
	awaitBatchSignal(t, controlled[0].started, "first shard commit")
	awaitBatchSignal(t, controlled[1].started, "second shard commit")
	closeFinished := make(chan error, 1)
	closeStarted := make(chan struct{})
	go func() {
		close(closeStarted)
		closeFinished <- index.Close()
	}()
	awaitBatchSignal(t, closeStarted, "index close")
	select {
	case <-controlled[0].closed:
		t.Fatal("shard closed during batch commit")
	case <-time.After(25 * time.Millisecond):
	}
	close(release)
	if err := <-batchFinished; err != nil {
		t.Fatalf("IndexBatch: %v", err)
	}
	if err := <-closeFinished; err != nil {
		t.Fatalf("Close: %v", err)
	}
	for shardNumber, shard := range controlled {
		awaitBatchSignal(t, shard.closed, fmt.Sprintf("shard %d close", shardNumber))
		if shard.committed() != 1 {
			t.Fatalf("shard %d commits = %d, want 1", shardNumber, shard.committed())
		}
	}
}

func TestBleveShardBatchCommitsSkipUntouchedShards(t *testing.T) {
	released := make(chan struct{})
	close(released)
	shard := newControlledBatchShard(t, released)
	t.Cleanup(func() { _ = shard.Close() })
	if err := commitBleveShardBatches([]bleve.Index{shard}, []*bleve.Batch{nil}); err != nil {
		t.Fatalf("commitBleveShardBatches: %v", err)
	}
	if shard.committed() != 0 {
		t.Fatalf("commits = %d, want 0", shard.committed())
	}
}

func BenchmarkBleveShardBatchCommits(b *testing.B) {
	newBenchmarkShards := func(testingTarget testing.TB) ([]bleve.Index, []*bleve.Batch) {
		testingTarget.Helper()
		released := make(chan struct{})
		close(released)
		shards := make([]bleve.Index, diskShardCount)
		batches := make([]*bleve.Batch, diskShardCount)
		for shardNumber := range diskShardCount {
			shard := newControlledBatchShard(testingTarget, released)
			shard.commitDelay = time.Millisecond
			shards[shardNumber] = shard
			batches[shardNumber] = shard.NewBatch()
		}

		return shards, batches
	}
	b.Run("four_lanes", func(b *testing.B) {
		shards, batches := newBenchmarkShards(b)
		b.Cleanup(func() { closeBleveShards(shards) })
		b.ResetTimer()
		for range b.N {
			if err := commitBleveShardBatches(shards, batches); err != nil {
				b.Fatalf("commitBleveShardBatches: %v", err)
			}
		}
	})
	b.Run("serial_reference", func(b *testing.B) {
		shards, batches := newBenchmarkShards(b)
		b.Cleanup(func() { closeBleveShards(shards) })
		b.ResetTimer()
		for range b.N {
			for shardNumber, shard := range shards {
				if err := shard.Batch(batches[shardNumber]); err != nil {
					b.Fatalf("shard Batch: %v", err)
				}
			}
		}
	})
}
