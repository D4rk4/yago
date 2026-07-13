package frontier_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagocrawler/internal/crawladmission"
	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawler/internal/frontier"
	"github.com/D4rk4/yago/yagocrawler/internal/weburl"
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
	return compiled(t, yagocrawlcontract.CrawlProfile{
		Scope:           yagocrawlcontract.ScopeDomain,
		URLMustMatch:    yagocrawlcontract.MatchAll,
		MaxPagesPerHost: yagocrawlcontract.UnlimitedPagesPerHost,
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
		func(bool) {},
	)
	job := receiveJob(t, f)
	if weburl.Host(job.URL) != "fast.example" {
		t.Errorf("dispatched %q, want fast.example to skip the stalled host", job.URL)
	}
	f.Done(job, false)
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
		func(bool) {},
	)
	first := receiveJob(t, f)
	f.Done(first, false)
	start := time.Now()
	second := receiveJob(t, f)
	if elapsed := time.Since(start); elapsed < 40*time.Millisecond {
		t.Errorf("second job released after %v, want at least the crawl delay", elapsed)
	}
	f.Done(second, false)
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
		func(bool) {},
	)
	f.Done(receiveJob(t, f), false)
	f.Release()
	f.Done(receiveJob(t, f), false)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if _, ok := f.Take(ctx); ok {
		t.Fatal("expected frontier to close after drain")
	}
	if ctx.Err() != nil {
		t.Fatal("frontier never closed after cooldown drain")
	}
}
