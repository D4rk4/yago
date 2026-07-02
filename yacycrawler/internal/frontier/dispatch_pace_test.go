package frontier_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacycrawler/internal/crawladmission"
	"github.com/D4rk4/yago/yacycrawler/internal/crawljob"
	"github.com/D4rk4/yago/yacycrawler/internal/frontier"
	"github.com/D4rk4/yago/yacycrawler/internal/weburl"
)

type cooldownPace struct {
	delay   time.Duration
	mu      sync.Mutex
	nextDue map[string]time.Time
}

func newCooldownPace(delay time.Duration) *cooldownPace {
	return &cooldownPace{delay: delay, nextDue: make(map[string]time.Time)}
}

func (p *cooldownPace) DueAt(job crawljob.CrawlJob, now time.Time) time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	if due, ok := p.nextDue[weburl.Host(job.URL)]; ok {
		return due
	}
	return now
}

func (p *cooldownPace) Visited(job crawljob.CrawlJob, at time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.nextDue[weburl.Host(job.URL)] = at.Add(p.delay)
}

type stallPace struct {
	stalledHost string
	until       time.Time
}

func (p *stallPace) DueAt(job crawljob.CrawlJob, now time.Time) time.Time {
	if weburl.Host(job.URL) == p.stalledHost {
		return p.until
	}
	return now
}

func (p *stallPace) Visited(crawljob.CrawlJob, time.Time) {}

func openProfile(t *testing.T) crawladmission.AdmissionProfile {
	return compiled(t, yacycrawlcontract.CrawlProfile{
		Scope:           yacycrawlcontract.ScopeDomain,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
	})
}

func TestDispatchSkipsStalledHost(t *testing.T) {
	pace := &stallPace{stalledHost: "slow.example", until: time.Now().Add(time.Hour)}
	f := frontier.NewFrontier(8, pace)
	profile := openProfile(t)
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle,
			"https://slow.example/a",
			"https://fast.example/a",
		),
		nil,
		profile,
		func() {},
	)
	job := receiveJob(t, f)
	if weburl.Host(job.URL) != "fast.example" {
		t.Errorf("dispatched %q, want fast.example to skip the stalled host", job.URL)
	}
	f.Done(job)
}

func TestDispatchReleasesCooldownJobWhenDue(t *testing.T) {
	f := frontier.NewFrontier(8, newCooldownPace(50*time.Millisecond))
	profile := openProfile(t)
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle,
			"https://example.com/a",
			"https://example.com/b",
		),
		nil,
		profile,
		func() {},
	)
	first := receiveJob(t, f)
	f.Done(first)
	start := time.Now()
	second := receiveJob(t, f)
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("second job released after %v, want at least the crawl delay", elapsed)
	}
	f.Done(second)
}

func TestDispatchDrainsCooldownJobOnClose(t *testing.T) {
	f := frontier.NewFrontier(8, newCooldownPace(50*time.Millisecond))
	profile := openProfile(t)
	f.Hold()
	f.SeedRun(
		context.Background(),
		requestsFor(profile.Profile.Handle,
			"https://example.com/a",
			"https://example.com/b",
		),
		nil,
		profile,
		func() {},
	)
	f.Done(receiveJob(t, f))
	f.Release()
	f.Done(receiveJob(t, f))
	select {
	case _, ok := <-f.Jobs():
		if ok {
			t.Fatal("expected jobs channel to be closed after drain")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("jobs channel never closed after cooldown drain")
	}
}
