package yacycrawler

import "github.com/nikitakarpei/yacy-rwi-node/yacycrawlcontract"

const defaultProfileName = "default"

func (c Config) DefaultProfile() yacycrawlcontract.CrawlProfile {
	return yacycrawlcontract.NewCrawlProfile(yacycrawlcontract.CrawlProfile{
		Name:            defaultProfileName,
		Scope:           c.Scope,
		URLMustMatch:    yacycrawlcontract.MatchAll,
		MaxDepth:        c.MaxDepth,
		MaxPagesPerHost: c.MaxPagesPerHost,
		CrawlDelay:      c.CrawlDelay,
	})
}

func (c Config) DefaultOrder(provenance []byte) yacycrawlcontract.CrawlOrder {
	profile := c.DefaultProfile()
	requests := make([]yacycrawlcontract.CrawlRequest, 0, len(c.SeedURLs))
	for _, seed := range c.SeedURLs {
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
