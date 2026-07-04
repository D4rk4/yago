package frontier_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawladmission"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/crawljob"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/frontier"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler/internal/weburl"
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
	f := frontier.NewFrontier(8, pace, 0)
	profile := openProfile(t)
	f.SeedRun(
		context.Background(),
		runSeeds(profile, nil, requestsFor(profile.Profile.Handle,
			"https://slow.example/a",
			"https://fast.example/a",
		)),
		func(bool) {},
	)
	job := receiveJob(t, f)
	if weburl.Host(job.URL) != "fast.example" {
		t.Errorf("dispatched %q, want fast.example to skip the stalled host", job.URL)
	}
	f.Done(job, false)
}

func TestDispatchReleasesCooldownJobWhenDue(t *testing.T) {
	f := frontier.NewFrontier(8, newCooldownPace(50*time.Millisecond), 0)
	profile := openProfile(t)
	f.SeedRun(
		context.Background(),
		runSeeds(profile, nil, requestsFor(profile.Profile.Handle,
			"https://example.com/a",
			"https://example.com/b",
		)),
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

func TestDispatchInterleavesManyHostsWithoutStarvation(t *testing.T) {
	f := frontier.NewFrontier(64, newCooldownPace(10*time.Millisecond), 0)
	profile := openProfile(t)
	const hosts = 20
	urls := make([]string, 0, hosts*2)
	for i := 0; i < hosts; i++ {
		host := "https://host" + string(rune('a'+i)) + ".example"
		urls = append(urls, host+"/1", host+"/2")
	}
	f.SeedRun(
		context.Background(),
		runSeeds(profile, nil, requestsFor(profile.Profile.Handle, urls...)),
		func(bool) {},
	)

	firstRound := make(map[string]bool)
	for i := 0; i < hosts; i++ {
		job := receiveJob(t, f)
		host := weburl.Host(job.URL)
		if firstRound[host] {
			t.Fatalf("host %q dispatched twice before every host was served once", host)
		}
		if job.URL[len(job.URL)-1] != '1' {
			t.Fatalf("host %q dispatched its second url before its first", host)
		}
		firstRound[host] = true
		f.Done(job, false)
	}
	if len(firstRound) != hosts {
		t.Fatalf("first round covered %d distinct hosts, want %d", len(firstRound), hosts)
	}

	for i := 0; i < hosts; i++ {
		job := receiveJob(t, f)
		if job.URL[len(job.URL)-1] != '2' {
			t.Fatalf("expected second-round url, got %q", job.URL)
		}
		f.Done(job, false)
	}
}

func TestDispatchDrainsCooldownJobOnClose(t *testing.T) {
	f := frontier.NewFrontier(8, newCooldownPace(50*time.Millisecond), 0)
	profile := openProfile(t)
	f.Hold()
	f.SeedRun(
		context.Background(),
		runSeeds(profile, nil, requestsFor(profile.Profile.Handle,
			"https://example.com/a",
			"https://example.com/b",
		)),
		func(bool) {},
	)
	f.Done(receiveJob(t, f), false)
	f.Release()
	f.Done(receiveJob(t, f), false)
	select {
	case _, ok := <-f.Jobs():
		if ok {
			t.Fatal("expected jobs channel to be closed after drain")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("jobs channel never closed after cooldown drain")
	}
}
