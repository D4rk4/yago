package yagonode

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
)

type signalingStoredDocuments struct {
	docs  []documentstore.Document
	scans chan struct{}
}

func (s signalingStoredDocuments) StoredDocuments(
	_ context.Context,
	visit func(documentstore.Document) (bool, error),
) error {
	for _, doc := range s.docs {
		if again, err := visit(doc); err != nil || !again {
			return err
		}
	}
	s.scans <- struct{}{}

	return nil
}

func citedDocuments() []documentstore.Document {
	return []documentstore.Document{
		{
			NormalizedURL: "http://source.net/a.html",
			Outlinks:      []string{"http://target.com/one.html"},
		},
		{
			NormalizedURL: "http://other.org/b.html",
			Outlinks:      []string{"http://target.com/two.html"},
		},
	}
}

func TestHostRankRefreshOnceBuildsAuthorityTable(t *testing.T) {
	holder := hostrank.NewHolder()
	sweeper := hostRankSweeper{
		documents: scriptedStoredDocuments{docs: citedDocuments()},
		holder:    holder,
	}

	sweeper.refreshOnce(context.Background())

	table := holder.Current()
	target := testHostHash(t, "http://target.com/one.html")
	source := testHostHash(t, "http://source.net/a.html")
	if table.Rank(target) != 1 {
		t.Fatalf("cited host rank = %v, want normalized 1: %v", table.Rank(target), table)
	}
	if table.Rank(target) <= table.Rank(source) {
		t.Fatalf("cited host (%v) must outrank the citing host (%v)",
			table.Rank(target), table.Rank(source))
	}
}

func TestHostRankRefreshOnceIgnoresNilDocuments(t *testing.T) {
	holder := hostrank.NewHolder()

	hostRankSweeper{documents: nil, holder: holder}.refreshOnce(context.Background())

	if got := holder.Current(); len(got) != 0 {
		t.Fatalf("nil documents produced a table: %v", got)
	}
}

func TestHostRankRefreshOnceSkipsOnScanError(t *testing.T) {
	holder := hostrank.NewHolder()
	sweeper := hostRankSweeper{
		documents: scriptedStoredDocuments{err: errors.New("scan failed")},
		holder:    holder,
	}

	sweeper.refreshOnce(context.Background())

	if got := holder.Current(); len(got) != 0 {
		t.Fatalf("failed scan produced a table: %v", got)
	}
}

func TestHostRankRefreshLoopRunsAndStops(t *testing.T) {
	oldTicks := newHostRankRefreshTicks
	t.Cleanup(func() { newHostRankRefreshTicks = oldTicks })
	ticks := make(chan time.Time, 1)
	var stopped atomic.Bool
	newHostRankRefreshTicks = func(time.Duration) (<-chan time.Time, func()) {
		return ticks, func() { stopped.Store(true) }
	}

	holder := hostrank.NewHolder()
	sweeper := hostRankSweeper{
		documents: signalingStoredDocuments{docs: citedDocuments(), scans: make(chan struct{}, 4)},
		holder:    holder,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runHostRankRefreshLoop(ctx, sweeper)
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
		t.Fatal("loop did not stop after cancel")
	}
	if !stopped.Load() {
		t.Fatal("ticker was not stopped")
	}
}
