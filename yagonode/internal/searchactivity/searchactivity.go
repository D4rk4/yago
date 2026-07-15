// Package searchactivity keeps a bounded in-memory journal of recent search
// requests for the admin console — YaCy's AccessTracker_p parity (UI-16).
// What gets recorded follows the operator's query-log privacy mode: off
// records nothing, aggregate records only shapes (lengths, counts, latency —
// never the text), full records the query text too, enabling the top-words
// view. The journal is memory-only on purpose: restart wipes it, and nothing
// here ever reaches disk or another peer.
package searchactivity

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// Mode mirrors the node's query-log privacy setting.
type Mode string

const (
	ModeOff       Mode = "off"
	ModeAggregate Mode = "aggregate"
	ModeFull      Mode = "full"
)

// journalCapacity bounds the ring; two hundred entries cover an operator's
// "what just happened" question without growing state.
const journalCapacity = 200

// Entry is one recorded search.
type Entry struct {
	At          time.Time
	Query       string
	QueryLength int
	Terms       int
	Results     int
	Failed      bool
	Incomplete  bool
	Duration    time.Duration
	Source      string
}

// Tracker is the concurrency-safe ring journal.
type Tracker struct {
	mu            sync.Mutex
	mode          Mode
	entries       []Entry
	next          int
	total         uint64
	confirmedZero uint64
}

// New builds a tracker for the given privacy mode; ModeOff returns nil and
// every method on a nil tracker is a no-op, so wiring stays unconditional.
func New(mode Mode) *Tracker {
	if mode != ModeAggregate && mode != ModeFull {
		return nil
	}

	return &Tracker{mode: mode, entries: make([]Entry, 0, journalCapacity)}
}

// Record journals one search. The query text is kept only in full mode.
func (t *Tracker) Record(entry Entry) {
	if t == nil {
		return
	}
	if t.mode != ModeFull {
		entry.Query = ""
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.total++
	if !entry.Failed && !entry.Incomplete && entry.Results == 0 {
		t.confirmedZero++
	}
	if len(t.entries) < journalCapacity {
		t.entries = append(t.entries, entry)

		return
	}
	t.entries[t.next] = entry
	t.next = (t.next + 1) % journalCapacity
}

// Snapshot returns the journal newest-first plus the lifetime counters.
func (t *Tracker) Snapshot() (entries []Entry, total, confirmedZeroResults uint64) {
	if t == nil {
		return nil, 0, 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	entries = make([]Entry, 0, len(t.entries))
	for i := len(t.entries) - 1; i >= 0; i-- {
		entries = append(entries, t.entries[(t.next+i)%len(t.entries)])
	}

	return entries, t.total, t.confirmedZero
}

// Mode reports the privacy mode the tracker runs in ("" for a nil tracker).
func (t *Tracker) Mode() Mode {
	if t == nil {
		return ModeOff
	}

	return t.mode
}

// WordCount is one entry of the top-words tally.
type WordCount struct {
	Word  string
	Count int
}

// TopWords tallies the most frequent query words across the journal — only
// meaningful in full mode, empty otherwise. Words shorter than two runes are
// noise and skipped.
func (t *Tracker) TopWords(limit int) []WordCount {
	if t == nil || t.mode != ModeFull || limit <= 0 {
		return nil
	}
	t.mu.Lock()
	counts := map[string]int{}
	for _, entry := range t.entries {
		for _, word := range strings.Fields(strings.ToLower(entry.Query)) {
			if len([]rune(word)) >= 2 {
				counts[word]++
			}
		}
	}
	t.mu.Unlock()
	words := make([]WordCount, 0, len(counts))
	for word, count := range counts {
		words = append(words, WordCount{Word: word, Count: count})
	}
	sort.Slice(words, func(i, j int) bool {
		if words[i].Count != words[j].Count {
			return words[i].Count > words[j].Count
		}

		return words[i].Word < words[j].Word
	})
	if len(words) > limit {
		words = words[:limit]
	}

	return words
}
