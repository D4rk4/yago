package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/adminui"
	"github.com/D4rk4/yago/yagonode/internal/crawldispatch"
)

type crawlSource struct {
	dispatcher *crawldispatch.Dispatcher
}

func newCrawlSource(dispatcher *crawldispatch.Dispatcher) *crawlSource {
	return &crawlSource{dispatcher: dispatcher}
}

func (s *crawlSource) Start(
	ctx context.Context,
	start adminui.CrawlStart,
) (adminui.CrawlDispatch, error) {
	accepted, err := s.dispatcher.Dispatch(ctx, crawldispatch.OperatorRequest{
		Name:                 start.Name,
		Seeds:                start.Seeds,
		StartMode:            start.Mode,
		Scope:                start.Scope,
		MaxDepth:             start.MaxDepth,
		URLMustMatch:         start.URLMustMatch,
		URLMustNotMatch:      start.URLMustNotMatch,
		IndexURLMustMatch:    start.IndexURLMustMatch,
		IndexURLMustNotMatch: start.IndexURLMustNotMatch,
		MaxPagesPerHost:      pagesPerHostOrUnlimited(start.MaxPagesPerHost),
		AllowQueryURLs:       start.AllowQueryURLs,
		FollowNoFollowLinks:  start.FollowNoFollowLinks,
		IgnoreTLSAuthority:   start.IgnoreTLSAuthority,
		RecrawlIfOlder:       start.RecrawlIfOlder,
		CrawlDelay:           start.CrawlDelay,
	}, "")
	if err != nil {
		return adminui.CrawlDispatch{}, fmt.Errorf("start crawl: %w", err)
	}

	return adminui.CrawlDispatch{
		ProfileHandle: accepted.ProfileHandle,
		Seeds:         accepted.Seeds,
		Duplicate:     accepted.Duplicate,
	}, nil
}

// pagesPerHostOrUnlimited treats a blank or non-positive per-host cap as unlimited,
// preserving the simple-start default while letting an expert cap the fetch count.
func pagesPerHostOrUnlimited(maxPagesPerHost int) int {
	if maxPagesPerHost <= 0 {
		return yagocrawlcontract.UnlimitedPagesPerHost
	}

	return maxPagesPerHost
}
