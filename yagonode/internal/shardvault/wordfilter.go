package shardvault

import (
	"fmt"
	"sync"

	"github.com/FastFilter/xorfilter"
	"github.com/cespare/xxhash/v2"
	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// Option configures optional engine behavior at open time.
type Option func(*engine)

// WithWordFilter enables per-shard binary-fuse membership filters over the term
// keys of one bucket (PERF-READ-01): a fan-out read seeks only the shards whose
// filter admits the term prefix, skipping the shards that provably hold no
// posting for it. bucket names the filtered collection and keyWidth is the
// term-prefix length; the assembly layer supplies both so the storage engine
// stays independent of the RWI key layout (ADR-0039).
func WithWordFilter(bucket vault.Name, keyWidth int) Option {
	return func(e *engine) {
		e.wordFilterBucket = bucket
		e.wordFilterWidth = keyWidth
	}
}

// buildFuse is the fuse-filter constructor seam so tests can force the
// construction-error branch.
var buildFuse = xorfilter.NewBinaryFuse[uint8]

// wordFilter is one shard's approximate membership over term-key prefixes: an
// immutable binary-fuse filter built from the keys present when it was built,
// plus a mutable side-set of keys written since (ADR-0039). A miss on both is
// authoritative — the shard holds no posting for that term — so the reader skips
// it; a hit only means "maybe", costing at worst a wasted seek. A failed build
// degrades to matching everything so a filter glitch can never hide a result.
type wordFilter struct {
	mu       sync.Mutex
	static   *xorfilter.BinaryFuse[uint8]
	degraded bool
	side     map[uint64]struct{}
}

// mayContain reports whether the shard might hold the term key. It is
// deliberately conservative: a nil filter, a degraded filter, or any hit answers
// true; only a built, non-degraded filter that misses both the static set and
// the side-set answers false, and false is the only answer that skips a shard.
func (f *wordFilter) mayContain(key uint64) bool {
	if f == nil {
		return true
	}
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.degraded {
		return true
	}
	if f.static != nil && f.static.Contains(key) {
		return true
	}
	_, ok := f.side[key]

	return ok
}

// add records a key written after the static filter was built so a concurrent
// read still sees it. It runs on the write path under the shared gate, so the
// side-set carries its own lock.
func (f *wordFilter) add(key uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.side == nil {
		f.side = make(map[uint64]struct{})
	}
	f.side[key] = struct{}{}
}

// initWordFilters builds a filter for every shard when the feature is configured;
// without WithWordFilter it is a no-op and reads never skip. It runs at open,
// before the store serves, so it needs no gate.
func (e *engine) initWordFilters() int {
	if e.wordFilterBucket == "" {
		return 0
	}
	e.wordFilters = make([]*wordFilter, len(e.shards))
	degraded := 0
	for i, db := range e.shards {
		e.wordFilters[i] = e.buildWordFilter(db)
		if e.wordFilters[i].degraded {
			degraded++
		}
	}

	return degraded
}

// rebuildWordFilter rebuilds one shard's filter from its current keys, folding
// any side-set additions into a fresh static filter. Called under the exclusive
// gate after a shard's contents are rewritten (compaction).
func (e *engine) rebuildWordFilter(index int) {
	if e.wordFilterBucket == "" {
		return
	}
	e.wordFilters[index] = e.buildWordFilter(e.shards[index])
}

// appendWordFilter builds and appends a filter for a newly split-in shard,
// preserving the len(wordFilters)==len(shards) invariant. Called under the
// exclusive gate.
func (e *engine) appendWordFilter(db *bolt.DB) {
	if e.wordFilterBucket == "" {
		return
	}
	e.wordFilters = append(e.wordFilters, e.buildWordFilter(db))
}

// buildWordFilter reads one shard's term-key prefixes and constructs its static
// fuse filter. An empty shard yields a filter that matches nothing (so it is
// skipped until a key is added); a read or construction failure yields a
// degraded filter that matches everything (so the shard is never skipped).
func (e *engine) buildWordFilter(db *bolt.DB) *wordFilter {
	keys, err := e.collectWordKeys(db)
	if err != nil {
		return &wordFilter{degraded: true}
	}
	if len(keys) == 0 {
		return &wordFilter{}
	}
	static, err := buildFuse(keys)
	if err != nil {
		return &wordFilter{degraded: true}
	}

	return &wordFilter{static: static}
}

// collectWordKeys reads the distinct term-key prefixes of one shard's filtered
// bucket, hashed to uint64 for the fuse filter.
func (e *engine) collectWordKeys(db *bolt.DB) ([]uint64, error) {
	seen := make(map[uint64]struct{})
	if err := db.View(func(tx *bolt.Tx) error {
		bucket := tx.Bucket([]byte(e.wordFilterBucket))
		if bucket == nil {
			return nil
		}
		cursor := bucket.Cursor()
		for key, _ := cursor.First(); key != nil; key, _ = cursor.Next() {
			if len(key) >= e.wordFilterWidth {
				seen[xxhash.Sum64(key[:e.wordFilterWidth])] = struct{}{}
			}
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("scan word keys: %w", err)
	}

	keys := make([]uint64, 0, len(seen))
	for hash := range seen {
		keys = append(keys, hash)
	}

	return keys, nil
}

// noteWordKey records a freshly written term key in its shard's filter side-set
// so a concurrent read still sees it before the next rebuild. It runs on the
// write path; a write to any other bucket, or a key shorter than the term
// prefix, is ignored.
func (e *engine) noteWordKey(bucket vault.Name, key vault.Key) {
	if e.wordFilterBucket == "" || bucket != e.wordFilterBucket || len(key) < e.wordFilterWidth {
		return
	}
	e.wordFilters[e.route(bucket, key)].add(xxhash.Sum64(key[:e.wordFilterWidth]))
}

// canSkipShard reports whether shard index provably lacks the term whose key is
// prefix, so a fan-out read may skip it. It engages only for the configured
// filter bucket and a full-width term prefix; a different bucket, or a partial
// or empty prefix, is never skipped.
func (e *engine) canSkipShard(index int, bucket vault.Name, prefix vault.Key) bool {
	if e.wordFilterBucket == "" || bucket != e.wordFilterBucket ||
		len(prefix) != e.wordFilterWidth {
		return false
	}

	return !e.wordFilters[index].mayContain(xxhash.Sum64(prefix))
}
