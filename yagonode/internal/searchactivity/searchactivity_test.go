package searchactivity

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func entry(query string, results int) Entry {
	return Entry{
		At: time.Unix(1_800_000_000, 0), Query: query,
		QueryLength: len([]rune(query)), Terms: 2, Results: results,
		Duration: 120 * time.Millisecond, Source: "global",
	}
}

// TestModesGovernWhatIsKept pins the privacy contract (UI-16): off records
// nothing at all, aggregate strips the query text, full keeps it.
func TestModesGovernWhatIsKept(t *testing.T) {
	if New(ModeOff) != nil || New(Mode("junk")) != nil {
		t.Fatal("off and unknown modes must disable the tracker")
	}
	var off *Tracker
	off.Record(entry("secret", 1))
	if entries, total, _ := off.Snapshot(); entries != nil || total != 0 {
		t.Fatal("nil tracker must stay empty")
	}
	if off.Mode() != ModeOff || off.TopWords(5) != nil {
		t.Fatal("nil tracker accessors must be inert")
	}

	aggregate := New(ModeAggregate)
	aggregate.Record(entry("тайный запрос", 3))
	entries, total, zero := aggregate.Snapshot()
	if total != 1 || zero != 0 || len(entries) != 1 {
		t.Fatalf("aggregate snapshot = %d/%d/%d", len(entries), total, zero)
	}
	if entries[0].Query != "" || entries[0].QueryLength == 0 {
		t.Fatalf("aggregate must keep shape, not text: %+v", entries[0])
	}
	if aggregate.TopWords(5) != nil {
		t.Fatal("aggregate mode must not tally words")
	}

	full := New(ModeFull)
	full.Record(entry("что такое осень", 0))
	fullEntries, _, fullZero := full.Snapshot()
	if fullEntries[0].Query != "что такое осень" || fullZero != 1 {
		t.Fatalf("full snapshot = %+v zero=%d", fullEntries[0], fullZero)
	}
}

// TestRingKeepsNewestAndOrders pins the ring semantics: capacity bounds the
// journal, snapshots come newest-first, and the lifetime counter keeps going.
func TestRingKeepsNewestAndOrders(t *testing.T) {
	tracker := New(ModeFull)
	for i := range journalCapacity + 25 {
		tracker.Record(entry(fmt.Sprintf("q%04d", i), 1))
	}
	entries, total, _ := tracker.Snapshot()
	if len(entries) != journalCapacity || total != journalCapacity+25 {
		t.Fatalf("ring = %d entries, total %d", len(entries), total)
	}
	if entries[0].Query != fmt.Sprintf("q%04d", journalCapacity+24) {
		t.Fatalf("newest first, got %q", entries[0].Query)
	}
	oldest := entries[len(entries)-1].Query
	if oldest != "q0025" {
		t.Fatalf("oldest kept = %q, want q0025", oldest)
	}
}

func TestModeReportsConfiguredMode(t *testing.T) {
	if got := New(ModeFull).Mode(); got != ModeFull {
		t.Fatalf("full tracker mode = %q, want full", got)
	}
	if got := New(ModeAggregate).Mode(); got != ModeAggregate {
		t.Fatalf("aggregate tracker mode = %q, want aggregate", got)
	}
}

func TestTopWords(t *testing.T) {
	tracker := New(ModeFull)
	tracker.Record(entry("осень ДДТ", 5))
	tracker.Record(entry("осень лирика", 2))
	tracker.Record(entry("x y", 1))
	words := tracker.TopWords(2)
	if len(words) != 2 || words[0] != (WordCount{Word: "осень", Count: 2}) {
		t.Fatalf("top words = %+v", words)
	}
	if tracker.TopWords(0) != nil {
		t.Fatal("non-positive limit must return nothing")
	}
}

func TestConcurrentRecording(t *testing.T) {
	tracker := New(ModeAggregate)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 100 {
				tracker.Record(entry(fmt.Sprintf("q%d", i), i%3))
			}
		}()
	}
	wg.Wait()
	if _, total, _ := tracker.Snapshot(); total != 800 {
		t.Fatalf("total = %d, want 800", total)
	}
}
