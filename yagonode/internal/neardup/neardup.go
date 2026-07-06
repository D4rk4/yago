// Package neardup detects near-duplicate page text at crawl ingest with 64-bit
// SimHash fingerprints (Charikar; Manku et al., WWW 2007: Hamming distance ≤3
// on 64 bits identifies near-duplicates), so mirrors and spun copies collapse
// to one index entry instead of wasting index space. Fingerprints hash content
// tokens only — function words would make unrelated short texts collide — and
// live in a bounded in-memory window scanned brute-force, which at this scale
// beats Manku's permuted tables (a popcount scan over the recent window is
// memory-bandwidth-bound and exact).
package neardup

import (
	"hash/fnv"
	"math/bits"
	"strings"
	"sync"

	"github.com/D4rk4/yago/yagonode/internal/stopwords"
)

const (
	// maxHammingDistance is the near-duplicate threshold on 64-bit fingerprints.
	maxHammingDistance = 3
	// minContentTokens keeps short texts out of comparison: sparse fingerprints
	// would match each other regardless of content.
	minContentTokens = 8
	// DefaultWindowCapacity bounds the recent-fingerprint window; at ~16 bytes a
	// fingerprint the default costs a few hundred kilobytes.
	DefaultWindowCapacity = 8192
)

type entry struct {
	fingerprint uint64
	key         string
}

// Window is a bounded, concurrency-safe ring of recently observed document
// fingerprints keyed by document identity.
type Window struct {
	mu       sync.Mutex
	capacity int
	next     int
	entries  []entry
}

// NewWindow returns a window remembering up to capacity fingerprints; a
// non-positive capacity takes the default.
func NewWindow(capacity int) *Window {
	if capacity <= 0 {
		capacity = DefaultWindowCapacity
	}

	return &Window{capacity: capacity}
}

// Observe fingerprints the text under the given key and reports the key of a
// recently seen near-duplicate, if any. A refetch of the same key refreshes
// its fingerprint and is never a duplicate of itself; a text too short to
// fingerprint is never a duplicate. New texts are recorded, duplicates are
// not — the first copy stays the canonical one.
func (w *Window) Observe(key, text string) (string, bool) {
	fingerprint, comparable := fingerprintText(text)
	if !comparable {
		return "", false
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.entries {
		if w.entries[i].key == key {
			w.entries[i].fingerprint = fingerprint

			return "", false
		}
	}
	for i := range w.entries {
		if bits.OnesCount64(w.entries[i].fingerprint^fingerprint) <= maxHammingDistance {
			return w.entries[i].key, true
		}
	}
	if len(w.entries) < w.capacity {
		w.entries = append(w.entries, entry{fingerprint: fingerprint, key: key})

		return "", false
	}
	w.entries[w.next] = entry{fingerprint: fingerprint, key: key}
	w.next = (w.next + 1) % w.capacity

	return "", false
}

// fingerprintText builds the 64-bit SimHash of the text's content tokens; the
// second return reports whether enough content tokens were present to compare.
func fingerprintText(text string) (uint64, bool) {
	var weights [64]int
	tokens := 0
	for _, token := range strings.Fields(strings.ToLower(text)) {
		if stopwords.IsStopword(strings.Trim(token, ".,!?…:;\"'()[]«»—-")) {
			continue
		}
		hasher := fnv.New64a()
		_, _ = hasher.Write([]byte(token))
		tokenHash := hasher.Sum64()
		for bit := range weights {
			if tokenHash&(1<<bit) != 0 {
				weights[bit]++
			} else {
				weights[bit]--
			}
		}
		tokens++
	}
	if tokens < minContentTokens {
		return 0, false
	}

	var fingerprint uint64
	for bit, weight := range weights {
		if weight > 0 {
			fingerprint |= 1 << bit
		}
	}

	return fingerprint, true
}
