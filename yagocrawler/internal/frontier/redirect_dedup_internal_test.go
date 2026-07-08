package frontier

import (
	"testing"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
)

type recordingDuplicateTally struct {
	provenances []string
}

func (r *recordingDuplicateTally) Duplicate(provenance []byte) {
	r.provenances = append(r.provenances, string(provenance))
}

func redirectRunFrontier(tally RunTally) (*Frontier, uuid.UUID) {
	runID := uuid.New()
	f := &Frontier{state: &frontierState{
		runs: map[uuid.UUID]*crawlRun{runID: {
			visited:   map[string]struct{}{"https://example.com/": {}},
			hostPages: map[string]int{"example.com": 1},
			profiles:  nil,
		}},
		tally: tally,
	}}

	return f, runID
}

func TestResolveRedirectSkipsVisitedTarget(t *testing.T) {
	tally := &recordingDuplicateTally{}
	f, runID := redirectRunFrontier(tally)
	job := crawljob.CrawlJob{
		URL: "https://example.com/a", RunID: runID, Provenance: []byte("run"),
	}

	if f.ResolveRedirect(job, "https://EXAMPLE.com/") {
		t.Error("a redirect to an already visited URL must be skipped")
	}
	if len(tally.provenances) != 1 || tally.provenances[0] != "run" {
		t.Errorf("duplicate tally = %v, want one entry keyed run", tally.provenances)
	}
}

func TestResolveRedirectRecordsFreshSameHostTarget(t *testing.T) {
	f, runID := redirectRunFrontier(noopRunTally{})
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
	if f.ResolveRedirect(job, "https://example.com/target") {
		t.Error("the second redirect to the recorded target must be skipped")
	}
}

func TestResolveRedirectCountsCrossHostTarget(t *testing.T) {
	f, runID := redirectRunFrontier(noopRunTally{})
	job := crawljob.CrawlJob{URL: "https://example.com/a", RunID: runID}

	if !f.ResolveRedirect(job, "https://other.example/x") {
		t.Fatal("a fresh cross-host target must be admitted")
	}
	if got := f.state.runs[runID].hostPages["other.example"]; got != 1 {
		t.Errorf("cross-host pages = %d, want 1", got)
	}
}

func TestResolveRedirectAdmitsUnknownRun(t *testing.T) {
	f, _ := redirectRunFrontier(noopRunTally{})
	job := crawljob.CrawlJob{URL: "https://example.com/a", RunID: uuid.New()}

	if !f.ResolveRedirect(job, "https://example.com/") {
		t.Error("a completed (unknown) run must not break in-flight work")
	}
}

func TestResolveRedirectAdmitsUnnormalizableTarget(t *testing.T) {
	f, runID := redirectRunFrontier(noopRunTally{})
	job := crawljob.CrawlJob{URL: "https://example.com/a", RunID: runID}

	if !f.ResolveRedirect(job, "ftp://example.com/") {
		t.Error("an unnormalizable final URL must be admitted unchecked")
	}
}
