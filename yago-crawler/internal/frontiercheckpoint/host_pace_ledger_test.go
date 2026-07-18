package frontiercheckpoint

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlpace"
)

func TestHostPaceLedgerSurvivesRunDeletionAndEvictsOldest(t *testing.T) {
	path := testCheckpointPath(t)
	checkpoint := openTestCheckpoint(t, path)
	provenance := []byte("pace-ledger-run")
	beginTestRun(t, checkpoint, provenance, []byte("pace-ledger-identity"))
	pages := []Page{
		testPage("https://a.example/page", "a.example", "a-observation", 0),
		testPage("https://b.example/page", "b.example", "b-observation", 0),
		testPage("https://c.example/page", "c.example", "c-observation", 0),
	}
	if admitted, err := checkpoint.Admit(
		testContext,
		provenance,
		pages,
	); err != nil ||
		admitted != 3 {
		t.Fatalf("admit host pace pages = %d, %v", admitted, err)
	}
	now := time.Date(2026, 7, 16, 15, 0, 0, 0, time.UTC)
	states := map[string]crawlpace.HostState{
		"a.example": {NextDueAt: now.Add(time.Second), Generation: 1},
		"b.example": {NextDueAt: now.Add(2 * time.Second), Generation: 2},
		"c.example": {
			NextDueAt:       now.Add(3 * time.Second),
			BackoffUntil:    now.Add(10 * time.Minute),
			BackoffPenalty:  10 * time.Minute,
			BackoffFailures: 2,
			Generation:      3,
		},
	}
	for _, host := range []string{"a.example", "b.example", "a.example", "c.example"} {
		if err := checkpoint.RecordHostState(
			testContext,
			provenance,
			host,
			HostProgress{Pace: states[host], PaceCapacity: 2},
			nil,
		); err != nil {
			t.Fatalf("record %s pace: %v", host, err)
		}
	}
	loaded, err := checkpoint.HostPaces(testContext, 2)
	if err != nil {
		t.Fatalf("load host paces: %v", err)
	}
	if len(loaded) != 2 || loaded["a.example"] != states["a.example"] ||
		loaded["c.example"] != states["c.example"] {
		t.Fatalf("bounded host paces = %+v, want latest a and c", loaded)
	}
	if err := checkpoint.Delete(testContext, provenance); err != nil {
		t.Fatalf("delete source run: %v", err)
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close host pace checkpoint: %v", err)
	}

	reopened := openTestCheckpoint(t, path)
	loaded, err = reopened.HostPaces(testContext, 1)
	if err != nil {
		t.Fatalf("load pruned host paces: %v", err)
	}
	if len(loaded) != 1 || loaded["c.example"] != states["c.example"] {
		t.Fatalf("persisted host paces = %+v, want newest c state", loaded)
	}
}

func TestHostPaceLedgerRejectsInvalidInputs(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	if _, err := checkpoint.HostPaces(testContext, 0); !errors.Is(err, ErrInvalidHostState) {
		t.Fatalf("zero pace capacity error = %v", err)
	}
	provenance := []byte("invalid-pace")
	beginTestRun(t, checkpoint, provenance, []byte("invalid-pace-identity"))
	page := testPage("https://example.com/page", "example.com", "observation", 0)
	if _, err := checkpoint.Admit(testContext, provenance, []Page{page}); err != nil {
		t.Fatalf("admit invalid pace page: %v", err)
	}
	invalid := []HostProgress{
		{PaceCapacity: -1},
		{Pace: crawlpace.HostState{NextDueAt: time.Now()}},
		{PaceCapacity: 1},
		{PaceCapacity: 1, Pace: crawlpace.HostState{Generation: 1, BackoffPenalty: -time.Second}},
		{PaceCapacity: 1, Pace: crawlpace.HostState{Generation: 1, BackoffFailures: 1}},
		{PaceCapacity: 1, Pace: crawlpace.HostState{Generation: 1, BackoffPenalty: time.Second}},
	}
	for index, progress := range invalid {
		if err := checkpoint.RecordHostState(
			context.Background(),
			provenance,
			page.Host,
			progress,
			nil,
		); !errors.Is(err, ErrInvalidHostState) {
			t.Fatalf("invalid pace %d error = %v", index, err)
		}
	}
}

func TestHostPaceLedgerRejectsStaleAndConflictingGenerations(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("pace-generation")
	beginTestRun(t, checkpoint, provenance, []byte("pace-generation-identity"))
	page := testPage("https://busy.example/page", "busy.example", "observation", 0)
	if _, err := checkpoint.Admit(testContext, provenance, []Page{page}); err != nil {
		t.Fatalf("admit pace generation page: %v", err)
	}
	now := time.Date(2026, 7, 16, 17, 0, 0, 0, time.UTC)
	newer := crawlpace.HostState{
		NextDueAt:       now.Add(time.Second),
		BackoffUntil:    now.Add(10 * time.Minute),
		BackoffPenalty:  10 * time.Minute,
		BackoffFailures: 2,
		Generation:      2,
	}
	stale := crawlpace.HostState{NextDueAt: now.Add(2 * time.Second), Generation: 1}
	for _, state := range []crawlpace.HostState{newer, stale} {
		if err := checkpoint.RecordHostState(
			testContext,
			provenance,
			page.Host,
			HostProgress{Pace: state, PaceCapacity: 8},
			nil,
		); err != nil {
			t.Fatalf("record generated pace %+v: %v", state, err)
		}
	}
	loaded, err := checkpoint.HostPaces(testContext, 8)
	if err != nil {
		t.Fatalf("load generated pace: %v", err)
	}
	if loaded[page.Host] != newer {
		t.Fatalf("pace after stale write = %+v, want %+v", loaded[page.Host], newer)
	}
	conflict := newer
	conflict.NextDueAt = conflict.NextDueAt.Add(time.Second)
	if err := checkpoint.RecordHostState(
		testContext,
		provenance,
		page.Host,
		HostProgress{Pace: conflict, PaceCapacity: 8},
		nil,
	); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("conflicting pace generation error = %v", err)
	}
}
