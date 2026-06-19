package yacycrawler_test

import (
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"
	"github.com/nikitakarpei/yacy-rwi-node/yacycrawler"
)

func defaultCrawlOrder(
	cfg yacycrawler.CrawlConfig,
	provenance []byte,
	seeds ...string,
) yacycrawlcontract.CrawlOrder {
	profile := yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
		Name:            "default",
		Scope:           cfg.Scope,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxDepth:        cfg.MaxDepth,
		MaxPagesPerHost: cfg.MaxPagesPerHost,
		CrawlDelay:      cfg.CrawlDelay,
	})
	requests := make([]yacycrawlcontract.CrawlRequest, 0, len(seeds))
	for _, seed := range seeds {
		requests = append(requests, yacycrawlcontract.CrawlRequest{
			URL:           seed,
			ProfileHandle: profile.Handle,
		})
	}
	return yacycrawlcontract.CrawlOrder{
		Provenance: provenance,
		Profile:    profile,
		Requests:   requests,
	}
}
