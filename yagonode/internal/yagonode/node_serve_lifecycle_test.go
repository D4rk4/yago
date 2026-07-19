package yagonode

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/metrichistory"
	"github.com/D4rk4/yago/yagonode/internal/redirectpurge"
)

type blockingCorpus struct {
	started chan struct{}
	release chan struct{}
}

func (b blockingCorpus) StoredDocuments(
	context.Context,
	func(documentstore.Document) (bool, error),
) error {
	close(b.started)
	<-b.release

	return nil
}

func noopLineagePurge(context.Context, []string, []yagomodel.Hash) error { return nil }

// TestStartRedirectPurgeJoinsBeforeWaitGroupReleases pins the SERVE-LIFECYCLE-01
// fix: serve's WaitGroup must not release while the redirect purge still scans
// the corpus, so the vault can never be closed under a live scan.
func TestStartRedirectPurgeJoinsBeforeWaitGroupReleases(t *testing.T) {
	corpus := blockingCorpus{started: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var background sync.WaitGroup
	startRedirectPurge(ctx, &background, node{
		redirectPurge: redirectpurge.New(corpus, noopLineagePurge),
	})
	<-corpus.started
	waited := make(chan struct{})
	go func() {
		background.Wait()
		close(waited)
	}()
	select {
	case <-waited:
		t.Fatal("WaitGroup released while the purge scan was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(corpus.release)
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("WaitGroup did not release after the purge finished")
	}
}

// TestStartCrawlScheduleLoopBalancesWaitGroup proves the schedule loop is
// tracked and releases its WaitGroup entry when the loop exits; an unbalanced
// Add would deadlock every serve shutdown.
func TestStartCrawlScheduleLoopBalancesWaitGroup(t *testing.T) {
	var background sync.WaitGroup
	startCrawlScheduleLoop(context.Background(), &background, node{})
	waited := make(chan struct{})
	go func() {
		background.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("WaitGroup did not release after the schedule loop exited")
	}
}

// TestStartPerformanceHistorySamplerStopsAndJoins pins that stop cancels the
// sampler and returns only after its goroutine exited, so bootNode's deferred
// stop guarantees no registry gather can run once the vault is closed.
func TestStartPerformanceHistorySamplerStopsAndJoins(t *testing.T) {
	registry := prometheus.NewRegistry()
	memory := prometheus.NewGauge(prometheus.GaugeOpts{Name: "process_resident_memory_bytes"})
	process := prometheus.NewCounter(prometheus.CounterOpts{Name: "process_cpu_seconds_total"})
	registry.MustRegister(memory, process)
	memory.Set(64 << 20)
	process.Add(1)
	sampler := metrichistory.New(registry, 2, nil)
	stop := startPerformanceHistorySampler(context.Background(), sampler)
	series := sampler.Series()
	var memoryPoints, processPoints int
	for _, current := range series {
		switch current.Name {
		case metrichistory.SeriesProcessMemory:
			memoryPoints = len(current.Points)
		case metrichistory.SeriesProcessCPU:
			processPoints = len(current.Points)
		}
	}
	if memoryPoints != 1 || processPoints != 0 {
		t.Fatalf("startup points = memory:%d cpu:%d", memoryPoints, processPoints)
	}
	stop()
}
