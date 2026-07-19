package crawldispatch

import (
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (request OperatorRequest) Profile(
	defaultMaxPagesPerRun int,
) (yagocrawlcontract.CrawlProfile, error) {
	scope, ok := crawlScopeByName[request.Scope]
	if !ok {
		return yagocrawlcontract.CrawlProfile{}, fmt.Errorf(
			"unknown crawl scope %q",
			request.Scope,
		)
	}
	recrawl, err := yagocrawlcontract.ParseRecrawlInterval(request.RecrawlIfOlder)
	if err != nil {
		return yagocrawlcontract.CrawlProfile{}, fmt.Errorf("recrawlIfOlder: %w", err)
	}
	delay, err := optionalDuration(request.CrawlDelay)
	if err != nil {
		return yagocrawlcontract.CrawlProfile{}, fmt.Errorf("crawlDelay: %w", err)
	}
	maxPagesPerRun := request.MaxPagesPerRun
	if maxPagesPerRun == nil {
		maxPagesPerRun = &defaultMaxPagesPerRun
	}
	profile := yagocrawlcontract.NewCrawlProfile(yagocrawlcontract.CrawlProfile{
		Name:                     request.Name,
		Scope:                    scope,
		URLMustMatch:             matchOrAll(request.URLMustMatch),
		URLMustNotMatch:          request.URLMustNotMatch,
		IndexURLMustMatch:        matchOrAll(request.IndexURLMustMatch),
		IndexURLMustNotMatch:     request.IndexURLMustNotMatch,
		MaxDepth:                 request.MaxDepth,
		AllowQueryURLs:           request.AllowQueryURLs,
		IgnoreTLSAuthority:       request.IgnoreTLSAuthority,
		IgnoreRobots:             request.IgnoreRobots,
		DisableBrowser:           request.DisableBrowser,
		FollowNoFollowLinks:      request.FollowNoFollowLinks,
		NoindexCanonicalMismatch: request.NoindexCanonicalMismatch,
		MaxPagesPerHost:          request.MaxPagesPerHost,
		MaxPagesPerRun:           maxPagesPerRun,
		RecrawlIfOlder:           recrawl,
		CrawlDelay:               delay,
	})
	if err := profile.Validate(); err != nil {
		return yagocrawlcontract.CrawlProfile{}, fmt.Errorf("invalid crawl profile: %w", err)
	}

	return profile, nil
}
