package frontier_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawler/internal/weburl"
)

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
		f.Done(job, false)
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
			f.Done(job, false)
			failureStreak = 0
			resetDone = true

			continue
		}
		f.RecordHostFetchOutcome(t.Context(), job, true)
		f.Done(job, false)
		failureStreak++
	}
	if len(healthy) > 0 {
		f.Submit(t.Context(), healthy[0], discoveredLinks("https://failed.example/new"))
	}
	for _, held := range healthy {
		f.RecordHostFetchOutcome(t.Context(), held, false)
		f.Done(held, false)
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
		f.Done(job, false)
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
		f.Done(job, false)
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
		f.Done(job, false)
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
