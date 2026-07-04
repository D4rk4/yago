package crawldelay

import (
	"fmt"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"

	"github.com/D4rk4/yago/yagocrawler/internal/crawljob"
	"github.com/D4rk4/yago/yagocrawler/internal/weburl"
)

const DefaultCrawlDelay = 1 * time.Second

type HostPace struct {
	delay   time.Duration
	nextDue *lru.Cache[string, time.Time]
}

func NewHostPace(delay time.Duration, hostCacheSize int) (*HostPace, error) {
	nextDue, err := lru.New[string, time.Time](hostCacheSize)
	if err != nil {
		return nil, fmt.Errorf("crawl pace host cache: %w", err)
	}
	return &HostPace{delay: delay, nextDue: nextDue}, nil
}

func (p *HostPace) DueAt(job crawljob.CrawlJob, now time.Time) time.Time {
	if due, ok := p.nextDue.Get(weburl.Host(job.URL)); ok {
		return due
	}
	return now
}

func (p *HostPace) Visited(job crawljob.CrawlJob, at time.Time) {
	p.nextDue.Add(weburl.Host(job.URL), at.Add(p.delay))
}
