package shardvault

import (
	"sync"

	"github.com/FastFilter/xorfilter"
	"github.com/cespare/xxhash/v2"

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
