package services

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

type fakeEvictor struct {
	mu           sync.Mutex
	used         int64
	bytesPerURL  int64
	pool         []yacymodel.Hash
	selectCalls  int
	deletedURLs  []yacymodel.Hash
	usedBytesErr error
}

func (e *fakeEvictor) UsedBytes(context.Context) (int64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.used, e.usedBytesErr
}

func (e *fakeEvictor) SelectEvictionCandidates(
	_ context.Context,
	limit int,
) ([]yacymodel.Hash, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.selectCalls++
	if limit > len(e.pool) {
		limit = len(e.pool)
	}
	out := e.pool[:limit]
	e.pool = e.pool[limit:]

	return out, nil
}

func (e *fakeEvictor) DeleteURLs(
	_ context.Context,
	urls []yacymodel.Hash,
) (ports.EvictionResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.deletedURLs = append(e.deletedURLs, urls...)
	e.used -= int64(len(urls)) * e.bytesPerURL
	if e.used < 0 {
		e.used = 0
	}

	return ports.EvictionResult{URLsDeleted: len(urls), PostingsDeleted: len(urls)}, nil
}

func evictorPool(n int) []yacymodel.Hash {
	pool := make([]yacymodel.Hash, n)
	for i := range pool {
		pool[i] = hashFor("url" + decimalForTest(byte(i)))
	}

	return pool
}

func TestSweepEvictsDownToLowWater(t *testing.T) {
	evictor := &fakeEvictor{used: 100, bytesPerURL: 10, pool: evictorPool(20)}
	sweeper := NewRWIEvictionSweeper(evictor, NewDropEvictionPolicy(evictor), 90, 50, 2)

	if err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if evictor.used > 50 {
		t.Fatalf("used = %d, want <= low water 50", evictor.used)
	}
	if sweeper.Sweeps() != 1 {
		t.Fatalf("sweeps = %d, want 1", sweeper.Sweeps())
	}
	if sweeper.URLsEvicted() == 0 {
		t.Fatalf("urls evicted = 0, want > 0")
	}
}

func TestSweepSkipsBelowHighWater(t *testing.T) {
	evictor := &fakeEvictor{used: 50, bytesPerURL: 10, pool: evictorPool(20)}
	sweeper := NewRWIEvictionSweeper(evictor, NewDropEvictionPolicy(evictor), 90, 50, 2)

	if err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if evictor.selectCalls != 0 {
		t.Fatalf("select calls = %d, want 0", evictor.selectCalls)
	}
}

func TestSweepStopsWhenNoCandidates(t *testing.T) {
	evictor := &fakeEvictor{used: 100, bytesPerURL: 10, pool: evictorPool(2)}
	sweeper := NewRWIEvictionSweeper(evictor, NewDropEvictionPolicy(evictor), 90, 0, 2)

	if err := sweeper.Sweep(context.Background()); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if len(evictor.deletedURLs) != 2 {
		t.Fatalf("deleted = %d, want 2 (pool exhausted)", len(evictor.deletedURLs))
	}
}

func TestTriggerSweepsAsynchronously(t *testing.T) {
	evictor := &fakeEvictor{used: 100, bytesPerURL: 10, pool: evictorPool(20)}
	sweeper := NewRWIEvictionSweeper(evictor, NewDropEvictionPolicy(evictor), 90, 50, 2)

	sweeper.Trigger()

	deadline := time.Now().Add(2 * time.Second)
	for sweeper.Sweeps() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if sweeper.Sweeps() != 1 {
		t.Fatalf("sweeps = %d, want 1", sweeper.Sweeps())
	}

	used, _ := evictor.UsedBytes(context.Background())
	if used > 50 {
		t.Fatalf("used = %d, want <= low water 50", used)
	}
	if sweeper.PostingsEvicted() == 0 {
		t.Fatalf("postings evicted = 0, want > 0")
	}
}

var _ ports.RWIEvictor = (*fakeEvictor)(nil)
