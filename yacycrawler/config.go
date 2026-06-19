package yacycrawler

import (
	"time"

	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
)

type Config struct {
	SeedURLs        []string
	Workers         int
	JobQueueSize    int
	IngestQueueSize int
	MaxBodyBytes    int64
	RequestTimeout  time.Duration
	UserAgent       string
	CrawlDelay      time.Duration
	MaxDepth        int
	Scope           yacycrawlcontract.CrawlScope
	MaxPagesPerHost int
}

func DefaultConfig() Config {
	return Config{
		SeedURLs:        nil,
		Workers:         4,
		JobQueueSize:    256,
		IngestQueueSize: 256,
		MaxBodyBytes:    DefaultMaxBodyBytes,
		RequestTimeout:  15 * time.Second,
		UserAgent:       DefaultUserAgent,
		CrawlDelay:      DefaultCrawlDelay,
		MaxDepth:        2,
		Scope:           yacycrawlcontract.ScopeDomain,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
	}
}
