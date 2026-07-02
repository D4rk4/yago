package frontier

import (
	"time"

	"github.com/D4rk4/yago/yacycrawler/internal/crawljob"
)

type CrawlPace interface {
	DueAt(job crawljob.CrawlJob, now time.Time) time.Time
	Visited(job crawljob.CrawlJob, at time.Time)
}

type alwaysDuePace struct{}

func (alwaysDuePace) DueAt(_ crawljob.CrawlJob, now time.Time) time.Time { return now }

func (alwaysDuePace) Visited(crawljob.CrawlJob, time.Time) {}
