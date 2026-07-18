package frontier_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlpace"
	"github.com/D4rk4/yago/yago-crawler/internal/frontier"
	"github.com/D4rk4/yago/yago-crawler/internal/frontiercheckpoint"
	"github.com/D4rk4/yago/yago-crawler/internal/weburl"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

type fixedCheckpointPace struct {
	state crawlpace.HostState
}

func (pace fixedCheckpointPace) DueAt(_ crawljob.CrawlJob, now time.Time) time.Time {
	return now
}

func (fixedCheckpointPace) Visited(crawljob.CrawlJob, time.Time) {}

func (pace fixedCheckpointPace) SnapshotHost(string) crawlpace.HostState {
	return pace.state
}

func (fixedCheckpointPace) RestoreHost(string, crawlpace.HostState) {}

func (fixedCheckpointPace) Capacity() int { return 8 }

func TestRepeatedHostFailuresFinishSingleHostRun(t *testing.T) {
	f := frontier.NewFrontier(1, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	urls := make([]string, 10)
	for index := range urls {
		urls[index] = fmt.Sprintf("https://failed.example/%d", index)
	}
	finished := make(chan bool, 1)
	f.SeedRun(
		t.Context(),
		requestsFor(profile.Profile.Handle, urls...),
		[]byte("failed-run"),
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	for range 5 {
		job := receiveJob(t, f)
		f.RecordHostFetchOutcome(t.Context(), job, true)
		f.Done(job, successfulPageOutcome())
	}
	select {
	case succeeded := <-finished:
		if !succeeded {
			t.Fatal("retired host run did not finish successfully")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("retired single-host run did not finish")
	}
	assertNoJob(t, f, 50*time.Millisecond)
}

func TestReverseDonePreservesNewestHostOutcomeGeneration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "frontier.db")
	checkpoint, err := frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("open reverse outcome checkpoint: %v", err)
	}
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	pace := fixedCheckpointPace{state: crawlpace.HostState{
		NextDueAt:  time.Date(2026, 7, 17, 5, 0, 0, 0, time.UTC),
		Generation: 7,
	}}
	provenance := []byte("reverse-host-outcome")
	crawlFrontier := frontier.NewFrontier(
		5,
		pace,
		frontier.WithCheckpoint(checkpoint),
	)
	urls := make([]string, 5)
	for index := range urls {
		urls[index] = fmt.Sprintf("https://reverse.example/%d", index)
	}
	crawlFrontier.SeedRunWithPriority(
		context.Background(),
		frontier.CrawlRunSeed{
			Requests:      requestsFor(profile.Profile.Handle, urls...),
			Provenance:    provenance,
			OrderIdentity: []byte("reverse-host-outcome-order"),
		},
		profile,
		nil,
	)
	jobs := make([]crawljob.CrawlJob, 5)
	for index := range jobs {
		jobs[index] = receiveJob(t, crawlFrontier)
		crawlFrontier.RecordHostFetchOutcome(context.Background(), jobs[index], true)
	}
	crawlFrontier.Done(jobs[4], successfulPageOutcome())
	for index := range 4 {
		crawlFrontier.Done(jobs[index], successfulPageOutcome())
	}
	if err := checkpoint.Close(); err != nil {
		t.Fatalf("close reverse outcome checkpoint: %v", err)
	}
	checkpoint, err = frontiercheckpoint.Open(path)
	if err != nil {
		t.Fatalf("reopen reverse outcome checkpoint: %v", err)
	}
	t.Cleanup(func() { _ = checkpoint.Close() })
	snapshot, err := checkpoint.Load(context.Background(), provenance)
	if err != nil {
		t.Fatalf("load reverse outcome checkpoint: %v", err)
	}
	host := snapshot.HostStates["reverse.example"]
	if !snapshot.Completed || !host.Retired || host.Failures != 0 || host.Generation != 5 {
		t.Fatalf("reverse outcome snapshot = %+v", snapshot)
	}
	paces, err := checkpoint.HostPaces(context.Background(), 8)
	if err != nil || paces["reverse.example"] != pace.state {
		t.Fatalf("reverse outcome paces = %+v, %v", paces, err)
	}
}

func TestHostFailureRetirementResetsAndIsolatesHosts(t *testing.T) {
	f := frontier.NewFrontier(32, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeWide,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxDepth:        1,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	urls := make([]string, 0, 15)
	for index := range 12 {
		urls = append(urls, fmt.Sprintf("https://failed.example/%d", index))
	}
	for index := range 3 {
		urls = append(urls, fmt.Sprintf("https://healthy.example/%d", index))
	}
	finished := make(chan bool, 1)
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle, urls...),
		[]byte("mixed-run"),
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	healthy := make([]crawljob.CrawlJob, 0, 3)
	failureStreak := 0
	resetDone := false
	for failureStreak < 5 {
		job := receiveJob(t, f)
		if weburl.Host(job.URL) == "healthy.example" {
			healthy = append(healthy, job)

			continue
		}
		if failureStreak == 4 && !resetDone {
			f.RecordHostFetchOutcome(t.Context(), job, false)
			f.Done(job, successfulPageOutcome())
			failureStreak = 0
			resetDone = true

			continue
		}
		f.RecordHostFetchOutcome(t.Context(), job, true)
		f.Done(job, successfulPageOutcome())
		failureStreak++
	}
	if len(healthy) > 0 {
		f.Submit(t.Context(), healthy[0], discoveredLinks("https://failed.example/new"))
	}
	for _, held := range healthy {
		f.RecordHostFetchOutcome(t.Context(), held, false)
		f.Done(held, successfulPageOutcome())
	}
	for {
		ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
		job, ok := f.Take(ctx)
		cancel()
		if !ok {
			break
		}
		if weburl.Host(job.URL) == "failed.example" {
			t.Fatalf("retired host dispatched %q", job.URL)
		}
		f.RecordHostFetchOutcome(t.Context(), job, false)
		f.Done(job, successfulPageOutcome())
	}
	select {
	case succeeded := <-finished:
		if !succeeded {
			t.Fatal("mixed run did not finish successfully")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("mixed run did not finish")
	}
}

func TestHostFailureRetirementIgnoresLaterInflightOutcome(t *testing.T) {
	f := frontier.NewFrontier(6, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	urls := make([]string, 6)
	for index := range urls {
		urls[index] = fmt.Sprintf("https://failed.example/%d", index)
	}
	finished := make(chan bool, 1)
	f.SeedRun(
		t.Context(),
		requestsFor(profile.Profile.Handle, urls...),
		[]byte("inflight-run"),
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	jobs := make([]crawljob.CrawlJob, 6)
	for index := range jobs {
		jobs[index] = receiveJob(t, f)
	}
	for _, job := range jobs {
		f.RecordHostFetchOutcome(t.Context(), job, true)
		f.Done(job, successfulPageOutcome())
	}
	select {
	case succeeded := <-finished:
		if !succeeded {
			t.Fatal("in-flight host run did not finish successfully")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight host run did not finish")
	}
}

func TestHostFailureRetirementFinishesAfterSettledOutcomes(t *testing.T) {
	f := frontier.NewFrontier(10, nil)
	profile := compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
	})
	urls := make([]string, 10)
	for index := range urls {
		urls[index] = fmt.Sprintf("https://failed.example/%d", index)
	}
	finished := make(chan bool, 1)
	f.SeedRun(
		t.Context(),
		requestsFor(profile.Profile.Handle, urls...),
		[]byte("settled-run"),
		profile,
		func(succeeded bool) { finished <- succeeded },
	)
	for range 5 {
		job := receiveJob(t, f)
		f.Done(job, successfulPageOutcome())
		f.RecordHostFetchOutcome(t.Context(), job, true)
	}
	select {
	case succeeded := <-finished:
		if !succeeded {
			t.Fatal("settled host run did not finish successfully")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("settled host run did not finish")
	}
	assertNoJob(t, f, 50*time.Millisecond)
}

func TestHostFailureOutcomeIgnoresUnknownWork(t *testing.T) {
	f := frontier.NewFrontier(1, nil)
	f.RecordHostFetchOutcome(t.Context(), crawljob.CrawlJob{
		URL: "https://unknown.example/", RunID: uuid.New(),
	}, true)
	f.RecordHostFetchOutcome(t.Context(), crawljob.CrawlJob{
		URL: "://invalid", RunID: uuid.New(),
	}, true)
}
