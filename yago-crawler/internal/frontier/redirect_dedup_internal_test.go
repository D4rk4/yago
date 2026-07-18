package frontier

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
)

func redirectRunFrontier() (*Frontier, uuid.UUID) {
	runID := uuid.New()
	f := &Frontier{state: &frontierState{
		runs: map[uuid.UUID]*crawlRun{runID: {
			visited:   map[string]struct{}{"https://example.com/": {}},
			hostPages: map[string]int{"example.com": 1},
			profiles:  nil,
		}},
		tally: noopRunTally{},
	}}

	return f, runID
}

func TestResolveRedirectSkipsVisitedTarget(t *testing.T) {
	f, runID := redirectRunFrontier()
	job := crawljob.CrawlJob{
		URL: "https://example.com/a", RunID: runID, Provenance: []byte("run"),
	}

	if f.ResolveRedirect(job, "https://EXAMPLE.com/") {
		t.Error("a redirect to an already visited URL must be skipped")
	}
}

func TestResolveRedirectRecordsFreshSameHostTarget(t *testing.T) {
	f, runID := redirectRunFrontier()
	job := crawljob.CrawlJob{URL: "https://example.com/a", RunID: runID}

	if !f.ResolveRedirect(job, "https://example.com/target") {
		t.Fatal("a fresh redirect target must be admitted")
	}
	run := f.state.runs[runID]
	if _, seen := run.visited["https://example.com/target"]; !seen {
		t.Error("fresh target must join the run's visited-set")
	}
	if run.hostPages["example.com"] != 1 {
		t.Errorf("same-host pages = %d, want 1 (no double count)", run.hostPages["example.com"])
	}
	if !f.ResolveRedirect(job, "https://example.com/target") {
		t.Error("the same source must be able to replay its redirect target")
	}
	if run.hostPages["example.com"] != 1 {
		t.Errorf("same-host pages after replay = %d, want 1", run.hostPages["example.com"])
	}
	lockReturned := make(chan struct{})
	go func() {
		f.WasCancelled([]byte("run"))
		close(lockReturned)
	}()
	select {
	case <-lockReturned:
	case <-time.After(time.Second):
		t.Fatal("same-target redirect replay retained the frontier lock")
	}
}

func TestResolveRedirectCountsCrossHostTarget(t *testing.T) {
	f, runID := redirectRunFrontier()
	job := crawljob.CrawlJob{URL: "https://example.com/a", RunID: runID}

	if !f.ResolveRedirect(job, "https://other.example/x") {
		t.Fatal("a fresh cross-host target must be admitted")
	}
	if got := f.state.runs[runID].hostPages["other.example"]; got != 1 {
		t.Errorf("cross-host pages = %d, want 1", got)
	}
}

func TestResolveRedirectAdmitsUnknownRun(t *testing.T) {
	f, _ := redirectRunFrontier()
	job := crawljob.CrawlJob{URL: "https://example.com/a", RunID: uuid.New()}

	if !f.ResolveRedirect(job, "https://example.com/") {
		t.Error("a completed (unknown) run must not break in-flight work")
	}
}

func TestResolveRedirectAdmitsUnnormalizableTarget(t *testing.T) {
	f, runID := redirectRunFrontier()
	job := crawljob.CrawlJob{URL: "https://example.com/a", RunID: runID}

	if !f.ResolveRedirect(job, "ftp://example.com/") {
		t.Error("an unnormalizable final URL must be admitted unchecked")
	}
}
