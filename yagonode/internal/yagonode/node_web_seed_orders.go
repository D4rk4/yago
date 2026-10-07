package yagonode

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/tracectx"
)

const (
	webSeedPublishAttempts = 3
	webSeedRetryDelay      = 25 * time.Millisecond
)

func (s *webCrawlSeeder) publishWebDiscoveryOrder(
	ctx context.Context,
	target string,
	instant time.Time,
) webSeedPublication {
	bounds := s.bounds()
	profile := s.profile
	profile.MaxDepth = bounds.depth
	profile.MaxPagesPerHost = bounds.maxPages
	maximum := automaticDiscoveryPageLimit(
		bounds.maxPages,
		s.crawlerMaximum(),
	)
	profile.MaxPagesPerRun = &maximum
	profile = yagocrawlcontract.NewCrawlProfile(profile)
	return s.publishWebSeedOrder(ctx, target, s.webSeedOrder(target, instant, profile))
}

func (s *webCrawlSeeder) webSeedOrder(
	target string,
	instant time.Time,
	profile yagocrawlcontract.CrawlProfile,
) yagocrawlcontract.CrawlOrder {
	return yagocrawlcontract.CrawlOrder{
		Provenance: mintProvenance(),
		Priority:   yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		Profile:    profile,
		Requests: []yagocrawlcontract.CrawlRequest{{
			URL:           target,
			Mode:          yagocrawlcontract.CrawlRequestModeURL,
			ProfileHandle: profile.Handle,
			Initiator:     s.initiator,
			AppDate:       instant,
		}},
	}
}

func (s *webCrawlSeeder) publishWebSeedOrder(
	ctx context.Context,
	identity string,
	order yagocrawlcontract.CrawlOrder,
) webSeedPublication {
	var err error
	for attempt := range webSeedPublishAttempts {
		duplicate, publishErr := s.queue.PublishOnce(ctx, identity, order)
		err = publishErr
		if err == nil {
			outcome := "published"
			if duplicate {
				outcome = "coalesced"
			}
			slog.DebugContext(
				ctx,
				msgWebSeedPublished,
				slog.String("outcome", outcome),
				tracectx.ServerSpanAttribute(ctx),
			)

			if duplicate {
				return webSeedPublicationCoalesced
			}

			return webSeedPublicationPublished
		}
		if attempt+1 < webSeedPublishAttempts &&
			!waitWebSeedRetry(ctx, webSeedRetryDelay*time.Duration(1<<attempt)) {
			break
		}
	}
	slog.WarnContext(
		ctx,
		msgWebSeedFailed,
		slog.String("outcome", "failed"),
		slog.String("cause", searchcore.FailureCauseLabel(searchcore.FailureCauseFor(err))),
		tracectx.ServerSpanAttribute(ctx),
	)

	return webSeedPublicationFailed
}

func waitWebSeedRetry(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
