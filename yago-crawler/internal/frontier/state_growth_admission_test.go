package frontier_test

import (
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type frontierStateGrowthToggle struct {
	err error
}

func (toggle *frontierStateGrowthToggle) CheckGrowth() error {
	return toggle.err
}

func TestFrontierStateGrowthAllowsDiscoveryBelowMaximum(t *testing.T) {
	crawlFrontier := frontier.NewFrontier(
		1,
		nil,
		frontier.WithStateGrowthAdmission(&frontierStateGrowthToggle{}),
	)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        1,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	settled := make(chan bool, 1)
	seeded := crawlFrontier.SeedRun(
		t.Context(),
		requestsFor(profile.Profile.Handle, "https://example.com/"),
		nil,
		profile,
		func(succeeded bool) { settled <- succeeded },
	)
	parent := receiveJob(t, crawlFrontier)
	if duplicates := crawlFrontier.Submit(
		t.Context(),
		parent,
		discoveredLinks("https://example.com/new"),
	); duplicates != 0 {
		t.Fatalf("admitted discovery duplicates = %d", duplicates)
	}
	if pending := crawlFrontier.RunPending(seeded.RunID); pending != 2 {
		t.Fatalf("pending after admitted discovery = %d, want 2", pending)
	}
	crawlFrontier.Done(parent, successfulPageOutcome())
	child := receiveJob(t, crawlFrontier)
	crawlFrontier.Done(child, successfulPageOutcome())
	select {
	case succeeded := <-settled:
		if !succeeded {
			t.Fatal("admitted discovery run settled as failed")
		}
	case <-time.After(time.Second):
		t.Fatal("admitted discovery run did not settle")
	}
}

func TestFrontierStateMaximumPreservesExistingWorkAndRefusesDiscovery(t *testing.T) {
	stateGrowth := &frontierStateGrowthToggle{}
	storage := &toggleGrowthAdmission{allowed: true}
	crawlFrontier := frontier.NewFrontier(
		2,
		nil,
		frontier.WithGrowthAdmission(storage),
		frontier.WithStateGrowthAdmission(stateGrowth),
	)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        1,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	settled := make(chan bool, 1)
	seeded := crawlFrontier.SeedRun(
		t.Context(),
		requestsFor(profile.Profile.Handle, "https://example.com/"),
		nil,
		profile,
		func(succeeded bool) { settled <- succeeded },
	)
	if seeded.Queued != 1 {
		t.Fatalf("seeded pages = %d, want 1", seeded.Queued)
	}
	stateGrowth.err = frontiercheckpoint.ErrStateMaximum
	job := receiveJob(t, crawlFrontier)
	if job.URL != "https://example.com/" {
		t.Fatalf("existing job = %q", job.URL)
	}
	if duplicates := crawlFrontier.Submit(
		t.Context(),
		job,
		discoveredLinks("https://example.com/new"),
	); duplicates != 0 {
		t.Fatalf("refused discovery duplicates = %d", duplicates)
	}
	if pending := crawlFrontier.RunPending(seeded.RunID); pending != 1 {
		t.Fatalf("pending after refused discovery = %d, want 1", pending)
	}
	crawlFrontier.Done(job, successfulPageOutcome())
	select {
	case succeeded := <-settled:
		if !succeeded {
			t.Fatal("existing run settled as failed")
		}
	case <-time.After(time.Second):
		t.Fatal("existing run did not settle at the state maximum")
	}
}

func TestFrontierStateInspectionFailureStopsDiscovery(t *testing.T) {
	want := errors.New("inspect frontier state")
	stateGrowth := &frontierStateGrowthToggle{err: want}
	crawlFrontier := frontier.NewFrontier(
		1,
		nil,
		frontier.WithStateGrowthAdmission(stateGrowth),
	)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        1,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	seeded := crawlFrontier.SeedRun(
		t.Context(),
		requestsFor(profile.Profile.Handle, "https://example.com/"),
		nil,
		profile,
		nil,
	)
	job := receiveJob(t, crawlFrontier)
	if duplicates := crawlFrontier.Submit(
		t.Context(),
		job,
		discoveredLinks("https://example.com/new"),
	); duplicates != 0 {
		t.Fatalf("failed discovery duplicates = %d", duplicates)
	}
	if !errors.Is(crawlFrontier.CheckpointFailure(), want) {
		t.Fatalf("checkpoint failure = %v, want %v", crawlFrontier.CheckpointFailure(), want)
	}
	if pending := crawlFrontier.RunPending(seeded.RunID); pending != 1 {
		t.Fatalf("pending after failed inspection = %d, want 1", pending)
	}
}
