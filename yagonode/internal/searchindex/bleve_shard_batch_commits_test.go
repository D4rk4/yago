package searchindex

import (
	"errors"
	"fmt"
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
		staging: staging,
		started: make(chan struct{}),
		release: release,
		closed:  make(chan struct{}),
	}
}

func (s *controlledBatchShard) NewBatch() *bleve.Batch {
	return s.staging.NewBatch()
}

func (s *controlledBatchShard) Batch(*bleve.Batch) error {
	s.startOnce.Do(func() { close(s.started) })
	if s.release != nil {
		<-s.release
	}
	if s.commitDelay > 0 {
		time.Sleep(s.commitDelay)
	}

	return s.commitFailure
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

func TestBleveShardBatchCommitsRunInParallelAndWaitForEveryShard(t *testing.T) {
	firstRelease := make(chan struct{})
	secondRelease := make(chan struct{})
	first := newControlledBatchShard(t, firstRelease)
	second := newControlledBatchShard(t, secondRelease)
	shards := []bleve.Index{first, second}
	index := &BleveDiskIndex{shards: shards, now: time.Now}
	documents := []documentstore.Document{
		documentForShard(t, shards, 0),
		documentForShard(t, shards, 1),
	}

	finished := make(chan error, 1)
	go func() {
		finished <- index.IndexBatch(t.Context(), documents)
	}()
	awaitBatchSignal(t, first.started, "first shard commit")
	awaitBatchSignal(t, second.started, "second shard commit")
	close(firstRelease)
	select {
	case err := <-finished:
		close(secondRelease)
		t.Fatalf("batch returned before every shard completed: %v", err)
	case <-time.After(25 * time.Millisecond):
	}
	close(secondRelease)
	if err := <-finished; err != nil {
		t.Fatalf("IndexBatch: %v", err)
	}
	if err := index.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestBleveShardBatchCommitFailureIsDeterministic(t *testing.T) {
	released := make(chan struct{})
	close(released)
	firstFailure := errors.New("first shard failed")
	secondFailure := errors.New("second shard failed")
	first := newControlledBatchShard(t, released)
	first.commitDelay = 25 * time.Millisecond
	first.commitFailure = firstFailure
	second := newControlledBatchShard(t, released)
	second.commitFailure = secondFailure
	shards := []bleve.Index{first, second}
	index := &BleveDiskIndex{shards: shards, now: time.Now}
	documents := []documentstore.Document{
		documentForShard(t, shards, 0),
		documentForShard(t, shards, 1),
	}

	err := index.IndexBatch(t.Context(), documents)
	if !errors.Is(err, firstFailure) || errors.Is(err, secondFailure) {
		t.Fatalf("IndexBatch error = %v, want lowest shard failure %v", err, firstFailure)
	}
	if !index.lastUpdate().IsZero() {
		t.Fatalf("failed batch updated timestamp to %v", index.lastUpdate())
	}
	if err := index.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestBleveDiskIndexCloseWaitsForParallelBatchCommits(t *testing.T) {
	firstRelease := make(chan struct{})
	secondRelease := make(chan struct{})
	first := newControlledBatchShard(t, firstRelease)
	second := newControlledBatchShard(t, secondRelease)
	shards := []bleve.Index{first, second}
	index := &BleveDiskIndex{shards: shards, now: time.Now}
	documents := []documentstore.Document{
		documentForShard(t, shards, 0),
		documentForShard(t, shards, 1),
	}

	batchFinished := make(chan error, 1)
	go func() {
		batchFinished <- index.IndexBatch(t.Context(), documents)
	}()
	awaitBatchSignal(t, first.started, "first shard commit")
	awaitBatchSignal(t, second.started, "second shard commit")
	closeFinished := make(chan error, 1)
	closeStarted := make(chan struct{})
	go func() {
		close(closeStarted)
		closeFinished <- index.Close()
	}()
	awaitBatchSignal(t, closeStarted, "index close")
	select {
	case <-first.closed:
		t.Fatal("first shard closed during batch commit")
	case <-second.closed:
		t.Fatal("second shard closed during batch commit")
	case <-time.After(25 * time.Millisecond):
	}
	close(firstRelease)
	close(secondRelease)
	if err := <-batchFinished; err != nil {
		t.Fatalf("IndexBatch: %v", err)
	}
	if err := <-closeFinished; err != nil {
		t.Fatalf("Close: %v", err)
	}
	awaitBatchSignal(t, first.closed, "first shard close")
	awaitBatchSignal(t, second.closed, "second shard close")
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
	b.Run("parallel", func(b *testing.B) {
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
