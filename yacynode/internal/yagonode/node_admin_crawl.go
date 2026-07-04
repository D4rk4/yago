package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yacycrawlcontract"
	"github.com/D4rk4/yago/yacynode/internal/adminui"
	"github.com/D4rk4/yago/yacynode/internal/crawldispatch"
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
		Name:            start.Name,
		Seeds:           start.Seeds,
		StartMode:       start.Mode,
		Scope:           start.Scope,
		MaxDepth:        start.MaxDepth,
		MaxPagesPerHost: yacycrawlcontract.UnlimitedPagesPerHost,
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
