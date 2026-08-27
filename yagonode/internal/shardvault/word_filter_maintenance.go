package shardvault

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/FastFilter/xorfilter"
	"github.com/cespare/xxhash/v2"
	bolt "go.etcd.io/bbolt"
)

const (
	wordFilterMaintenancePageRecords = 4096
	wordFilterMaintenanceYield       = 10 * time.Millisecond
	wordFilterMaintenanceFailure     = "vault word filter maintenance failed"
)

var pauseWordFilterMaintenance = waitWordFilterMaintenance

type wordFilterMaintenance struct {
	lock     sync.Mutex
	pending  map[int]struct{}
	wake     chan struct{}
	cancel   context.CancelFunc
	done     chan struct{}
	active   int
	progress startupProgress
}

type wordFilterPage struct {
	terms    []uint64
	last     []byte
	complete bool
}

func newWordFilterMaintenance(progress startupProgress) *wordFilterMaintenance {
	if progress.logger == nil {
		progress.logger = slog.New(slog.DiscardHandler)
	}
	if progress.clock == nil {
		progress.clock = time.Now
	}

	return &wordFilterMaintenance{
		pending:  make(map[int]struct{}),
		wake:     make(chan struct{}, 1),
		progress: progress,
	}
}

func (e *engine) installConservativeWordFilters(progress startupProgress) {
	if e.wordFilterBucket == "" {
		return
	}
	e.wordFilters = make([]*wordFilter, len(e.shards))
	e.wordFilterMaintenance = newWordFilterMaintenance(progress)
	for index := range e.wordFilters {
		e.wordFilters[index] = &wordFilter{degraded: true}
		e.wordFilterMaintenance.schedule(index)
	}
}

func (e *engine) startWordFilterMaintenance() {
	if e.wordFilterMaintenance == nil {
		return
	}
	e.wordFilterMaintenance.start(e)
}

func (e *engine) stopWordFilterMaintenance() {
	if e.wordFilterMaintenance == nil {
		return
	}
	e.wordFilterMaintenance.stop()
}

func (maintenance *wordFilterMaintenance) start(e *engine) {
	maintenance.lock.Lock()
	if maintenance.cancel != nil {
		maintenance.lock.Unlock()

		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	maintenance.cancel = cancel
	maintenance.done = done
	maintenance.lock.Unlock()

	go e.runWordFilterMaintenance(ctx, done)
}

func (maintenance *wordFilterMaintenance) stop() {
	maintenance.lock.Lock()
	cancel := maintenance.cancel
	done := maintenance.done
	maintenance.cancel = nil
	maintenance.done = nil
	maintenance.lock.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

func (maintenance *wordFilterMaintenance) schedule(index int) {
	maintenance.lock.Lock()
	maintenance.pending[index] = struct{}{}
	maintenance.lock.Unlock()
	select {
	case maintenance.wake <- struct{}{}:
	default:
	}
}

func (maintenance *wordFilterMaintenance) takePending() []int {
	maintenance.lock.Lock()
	batch := make([]int, 0, len(maintenance.pending))
	for index := range maintenance.pending {
		batch = append(batch, index)
		delete(maintenance.pending, index)
	}
	maintenance.active = len(batch)
	maintenance.lock.Unlock()
	sort.Ints(batch)

	return batch
}

func (maintenance *wordFilterMaintenance) finishPending() {
	maintenance.lock.Lock()
	maintenance.active = 0
	maintenance.lock.Unlock()
}

func (e *engine) runWordFilterMaintenance(ctx context.Context, done chan struct{}) {
	defer close(done)
	maintenance := e.wordFilterMaintenance
	for {
		batch := maintenance.takePending()
		if len(batch) == 0 {
			select {
			case <-ctx.Done():
				return
			case <-maintenance.wake:
				continue
			}
		}
		started := maintenance.progress.wordFilterBuilding(len(batch))
		degraded := 0
		for _, index := range batch {
			if err := e.optimizeWordFilter(ctx, index); err != nil {
				if errors.Is(err, context.Canceled) {
					maintenance.finishPending()
					return
				}
				degraded++
				maintenance.progress.logger.WarnContext(
					context.Background(),
					wordFilterMaintenanceFailure,
					slog.Int("shard", index+1),
					slog.Any("error", err),
				)
			}
		}
		maintenance.finishPending()
		maintenance.progress.wordFilterInitialized(started, len(batch), degraded)
	}
}

func (e *engine) optimizeWordFilter(ctx context.Context, index int) error {
	keys, err := e.collectWordFilterTerms(ctx, index)
	if err != nil {
		return err
	}
	var static *xorfilter.BinaryFuse[uint8]
	if len(keys) > 0 {
		static, err = buildFuse(keys)
		if err != nil {
			return fmt.Errorf("construct shard %d word filter: %w", index+1, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("publish shard %d word filter: %w", index+1, err)
	}

	e.globalGate.Lock()
	defer e.globalGate.Unlock()
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("publish shard %d word filter: %w", index+1, err)
	}
	current := e.wordFilters[index]
	current.mu.Lock()
	replacement := &wordFilter{static: static, side: current.side}
	current.mu.Unlock()
	e.wordFilters[index] = replacement

	return nil
}

func (e *engine) collectWordFilterTerms(ctx context.Context, index int) ([]uint64, error) {
	seen := make(map[uint64]struct{})
	var after []byte
	for {
		page, err := e.readWordFilterPage(ctx, index, after)
		if err != nil {
			return nil, err
		}
		for _, term := range page.terms {
			seen[term] = struct{}{}
		}
		if page.complete {
			break
		}
		after = page.last
		if e.wordFilterMaintenanceDemand() {
			if err := pauseWordFilterMaintenance(ctx); err != nil {
				return nil, err
			}
		}
	}
	keys := make([]uint64, 0, len(seen))
	for term := range seen {
		keys = append(keys, term)
	}

	return keys, nil
}

func (e *engine) readWordFilterPage(
	ctx context.Context,
	index int,
	after []byte,
) (wordFilterPage, error) {
	if err := acquireGlobalRead(ctx, &e.globalGate); err != nil {
		return wordFilterPage{}, fmt.Errorf("read word filter page: %w", err)
	}
	defer e.globalGate.RUnlock()
	page := wordFilterPage{terms: make([]uint64, 0, wordFilterMaintenancePageRecords)}
	err := e.shards[index].View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(e.wordFilterBucket))
		if bucket == nil {
			page.complete = true

			return nil
		}
		cursor := bucket.Cursor()
		key, _ := cursor.First()
		if len(after) > 0 {
			key, _ = cursor.Seek(after)
			if bytes.Equal(key, after) {
				key, _ = cursor.Next()
			}
		}
		for records := 0; key != nil && records < wordFilterMaintenancePageRecords; records++ {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("observe word filter page context: %w", err)
			}
			page.last = append(page.last[:0], key...)
			if len(key) >= e.wordFilterWidth {
				page.terms = append(page.terms, xxhash.Sum64(key[:e.wordFilterWidth]))
			}
			key, _ = cursor.Next()
		}
		page.complete = key == nil

		return nil
	})
	if err != nil {
		return wordFilterPage{}, fmt.Errorf("scan shard %d word keys: %w", index+1, err)
	}

	return page, nil
}

func (e *engine) wordFilterMaintenanceDemand() bool {
	if e.viewsInFlight.Load() > 0 {
		return true
	}
	e.writeAdmission.lock.Lock()
	busy := e.writeAdmission.concurrent > 0 || e.writeAdmission.contended ||
		e.writeAdmission.pendingContended > 0
	e.writeAdmission.lock.Unlock()

	return busy
}

func waitWordFilterMaintenance(ctx context.Context) error {
	timer := time.NewTimer(wordFilterMaintenanceYield)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return fmt.Errorf("word filter maintenance interrupted: %w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

func (e *engine) scheduleWordFilterRebuild(index int) {
	if e.wordFilterMaintenance == nil {
		return
	}
	e.wordFilters[index] = &wordFilter{degraded: true}
	e.wordFilterMaintenance.schedule(index)
}

func (e *engine) appendConservativeWordFilter() {
	if e.wordFilterMaintenance == nil {
		return
	}
	index := len(e.wordFilters)
	e.wordFilters = append(e.wordFilters, &wordFilter{degraded: true})
	e.wordFilterMaintenance.schedule(index)
}
