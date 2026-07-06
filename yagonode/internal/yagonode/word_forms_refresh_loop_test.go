package yagonode

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/metrics"
	"github.com/D4rk4/yago/yagonode/internal/wordforms"
)

func wordFormsCorpus() []documentstore.Document {
	return []documentstore.Document{
		{Title: "Черногория сегодня", ExtractedText: "погода в черногории"},
		{Title: "виза в черногорию", ExtractedText: "черногория и черногории"},
	}
}

func TestWordFormsRefreshOnceBuildsExpander(t *testing.T) {
	holder := wordforms.NewHolder()
	sweeper := wordFormsSweeper{
		documents: scriptedStoredDocuments{docs: wordFormsCorpus()},
		holder:    holder,
	}

	sweeper.refreshOnce(context.Background())

	got := holder.Current().Variants("черногория")
	if !slices.Contains(got, "черногории") || !slices.Contains(got, "черногорию") {
		t.Fatalf("inflected forms not grouped by stem: %v", got)
	}
}

func TestWordFormsRefreshOnceSkipsOnScanError(t *testing.T) {
	holder := wordforms.NewHolder()
	sweeper := wordFormsSweeper{
		documents: scriptedStoredDocuments{err: errors.New("scan failed")},
		holder:    holder,
	}

	sweeper.refreshOnce(context.Background())

	if got := holder.Current().Variants("черногория"); len(got) != 1 {
		t.Fatalf("failed scan produced an expander: %v", got)
	}
}

func TestWordFormsRefreshOnceNoopWithoutSourceOrHolder(t *testing.T) {
	// Nil holder: nothing to publish, must not panic.
	wordFormsSweeper{documents: scriptedStoredDocuments{docs: wordFormsCorpus()}}.
		refreshOnce(context.Background())

	// Nil documents: leaves the holder empty.
	holder := wordforms.NewHolder()
	wordFormsSweeper{holder: holder}.refreshOnce(context.Background())
	if got := holder.Current().Variants("черногория"); len(got) != 1 {
		t.Fatalf("nil document source produced an expander: %v", got)
	}
}

func TestWordFormsRefreshLoopRunsAndStops(t *testing.T) {
	oldTicks := newWordFormsRefreshTicks
	t.Cleanup(func() { newWordFormsRefreshTicks = oldTicks })
	ticks := make(chan time.Time, 1)
	var stopped atomic.Bool
	newWordFormsRefreshTicks = func(time.Duration) (<-chan time.Time, func()) {
		return ticks, func() { stopped.Store(true) }
	}

	holder := wordforms.NewHolder()
	sweeper := wordFormsSweeper{
		documents: signalingStoredDocuments{docs: wordFormsCorpus(), scans: make(chan struct{}, 4)},
		holder:    holder,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runWordFormsRefreshLoop(ctx, sweeper)
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

func TestServeStartsWordFormsLoopWhenEnabled(t *testing.T) {
	config := testConfig(t)
	config.SwarmMorphology = true
	assembled := assembleTestNode(t, config, openTestVault(t))
	if !assembled.swarmMorph {
		t.Fatal("swarm morphology flag not threaded to the node")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := serve(
		ctx,
		assembled,
		metrics.NewEvictionMetrics(prometheus.NewRegistry()),
		namedServer{"peer protocol", buildServer("127.0.0.1:0", assembled.peerMux)},
	); err != nil {
		t.Fatalf("serve: %v", err)
	}
}

func TestSwarmMorphologyExpander(t *testing.T) {
	holder := wordforms.NewHolder()
	holder.Store(wordforms.New(
		map[string]int{"черногория": 5, "черногории": 3},
		func(word string) string { return string([]rune(word)[:6]) },
	))

	// Enabled with a provider: returns a working expansion function.
	expand := swarmMorphologyExpander(publicSearchAssembly{
		swarmMorphology: true,
		wordForms:       holder.Current,
	})
	if expand == nil {
		t.Fatal("expected an expander when swarm morphology is on")
	}
	if got := expand("черногория"); len(got) < 2 {
		t.Fatalf("expander did not expand: %v", got)
	}

	// Disabled, or wired without a provider: no expansion function.
	if swarmMorphologyExpander(publicSearchAssembly{wordForms: holder.Current}) != nil {
		t.Fatal("expander built while swarm morphology is off")
	}
	if swarmMorphologyExpander(publicSearchAssembly{swarmMorphology: true}) != nil {
		t.Fatal("expander built without a provider")
	}
}
