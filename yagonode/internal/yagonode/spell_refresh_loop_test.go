package yagonode

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/spellcheck"
)

func spellCorpus() []documentstore.Document {
	return []documentstore.Document{
		{Title: "Golang tutorial", ExtractedText: "learn golang basics"},
		{Title: "Golang guide", ExtractedText: "golang tutorial advanced"},
		{Title: "singleton page", ExtractedText: "unique mention only"},
	}
}

func TestSpellRefreshOnceBuildsCorrector(t *testing.T) {
	holder := spellcheck.NewHolder()
	sweeper := spellSweeper{
		documents: scriptedStoredDocuments{docs: spellCorpus()},
		holder:    holder,
	}

	sweeper.refreshOnce(context.Background())

	// A repeated word becomes a correction target.
	if got, ok := holder.Current().Suggest("golnag"); !ok || got != "golang" {
		t.Fatalf("Suggest(golnag) = %q %v, want golang", got, ok)
	}
	// A word seen only once is pruned, so a typo of it is not "corrected".
	if _, ok := holder.Current().Suggest("singletom"); ok {
		t.Fatal("singleton term should be pruned from the dictionary")
	}
}

func TestSpellRefreshOnceSkipsOnScanError(t *testing.T) {
	holder := spellcheck.NewHolder()
	sweeper := spellSweeper{
		documents: scriptedStoredDocuments{err: errors.New("scan failed")},
		holder:    holder,
	}

	sweeper.refreshOnce(context.Background())

	if _, ok := holder.Current().Suggest("golnag"); ok {
		t.Fatal("failed scan produced a dictionary")
	}
}

func TestSpellRefreshOnceNoopWithoutSourceOrHolder(t *testing.T) {
	// Nil holder: nothing to publish, must not panic.
	spellSweeper{documents: scriptedStoredDocuments{docs: spellCorpus()}}.
		refreshOnce(context.Background())

	// Nil documents: leaves the holder empty.
	holder := spellcheck.NewHolder()
	spellSweeper{holder: holder}.refreshOnce(context.Background())
	if _, ok := holder.Current().Suggest("golnag"); ok {
		t.Fatal("nil document source produced a dictionary")
	}
}

func TestSpellRefreshLoopRunsAndStops(t *testing.T) {
	oldTicks := newSpellRefreshTicks
	t.Cleanup(func() { newSpellRefreshTicks = oldTicks })
	ticks := make(chan time.Time, 1)
	var stopped atomic.Bool
	newSpellRefreshTicks = func(time.Duration) (<-chan time.Time, func()) {
		return ticks, func() { stopped.Store(true) }
	}

	holder := spellcheck.NewHolder()
	sweeper := spellSweeper{
		documents: signalingStoredDocuments{docs: spellCorpus(), scans: make(chan struct{}, 4)},
		holder:    holder,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runSpellRefreshLoop(ctx, sweeper)
		close(done)
	}()

	scans := sweeper.documents.(signalingStoredDocuments).scans
	select {
	case <-scans:
	case <-time.After(time.Second):
		t.Fatal("immediate refresh did not run")
	}
	ticks <- time.Now()
	select {
	case <-scans:
	case <-time.After(time.Second):
		t.Fatal("ticked refresh did not run")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("loop did not stop on context cancel")
	}
	if !stopped.Load() {
		t.Fatal("ticker was not stopped")
	}
}
