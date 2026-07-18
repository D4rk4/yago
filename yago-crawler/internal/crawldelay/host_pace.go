package crawldelay

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/D4rk4/yago/yago-crawler/internal/crawljob"
	"github.com/D4rk4/yago/yago-crawler/internal/crawlpace"
	"github.com/D4rk4/yago/yago-crawler/internal/weburl"
)

const DefaultCrawlDelay = 1 * time.Second

type HostPace struct {
	delay      time.Duration
	capacity   int
	mu         sync.Mutex
	generation atomic.Uint64
	nextDue    *lru.Cache[string, hostDeadline]
}

type hostDeadline struct {
	at         time.Time
	generation uint64
}

func NewHostPace(delay time.Duration, hostCacheSize int) (*HostPace, error) {
	nextDue, err := lru.New[string, hostDeadline](hostCacheSize)
	if err != nil {
		return nil, fmt.Errorf("crawl pace host cache: %w", err)
	}
	return &HostPace{delay: delay, capacity: hostCacheSize, nextDue: nextDue}, nil
}

func (p *HostPace) DueAt(job crawljob.CrawlJob, now time.Time) time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	if due, ok := p.nextDue.Get(weburl.Host(job.URL)); ok {
		return due.at
	}
	return now
}

// Visited records the host's next-eligible fetch time. A job whose profile sets a
// CrawlDelay uses that delay; otherwise the crawler's global default applies.
func (p *HostPace) Visited(job crawljob.CrawlJob, at time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delay := p.delay
	if job.CrawlDelay > 0 {
		delay = job.CrawlDelay
	}
	host := weburl.Host(job.URL)
	due := at.Add(delay)
	if current, found := p.nextDue.Get(host); found && current.at.After(due) {
		due = current.at
	}
	p.nextDue.Add(host, hostDeadline{at: due, generation: p.nextGeneration()})
}

func (p *HostPace) SnapshotHost(rawURL string) crawlpace.HostState {
	p.mu.Lock()
	defer p.mu.Unlock()
	due, _ := p.nextDue.Get(weburl.Host(rawURL))

	return crawlpace.HostState{NextDueAt: due.at, Generation: due.generation}
}

func (p *HostPace) RestoreHost(host string, state crawlpace.HostState) {
	if state.Generation == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.restoreGeneration(state.Generation)
	if state.NextDueAt.IsZero() {
		return
	}
	current, found := p.nextDue.Get(host)
	if found && current.generation > state.Generation {
		return
	}
	if found && current.generation == state.Generation && current.at.After(state.NextDueAt) {
		state.NextDueAt = current.at
	}
	p.nextDue.Add(host, hostDeadline{at: state.NextDueAt, generation: state.Generation})
}

func (p *HostPace) Capacity() int {
	return p.capacity
}

func (p *HostPace) nextGeneration() uint64 {
	return p.generation.Add(1)
}

func (p *HostPace) restoreGeneration(generation uint64) {
	for {
		current := p.generation.Load()
		if current >= generation || p.generation.CompareAndSwap(current, generation) {
			return
		}
	}
}
