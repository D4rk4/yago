package yagonode

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

const (
	webSeedPublishAttempts = 3
	webSeedRetryDelay      = 25 * time.Millisecond
)

func (s *webCrawlSeeder) publishWebDiscoveryOrder(
	ctx context.Context,
	target string,
	instant time.Time,
) {
	profile := s.profile
	maximum := automaticDiscoveryPageLimit(
		s.maximumPages,
		s.crawlerMaximum(),
	)
	profile.MaxPagesPerRun = &maximum
	profile = yagocrawlcontract.NewCrawlProfile(profile)
	s.publishWebSeedOrder(ctx, target, s.webSeedOrder(target, instant, profile))
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
) {
	var err error
	for attempt := range webSeedPublishAttempts {
		_, err = s.queue.PublishOnce(ctx, identity, order)
		if err == nil {
			return
		}
		if attempt+1 < webSeedPublishAttempts &&
			!waitWebSeedRetry(ctx, webSeedRetryDelay*time.Duration(1<<attempt)) {
			break
		}
	}
	slog.WarnContext(
		ctx,
		msgWebSeedFailed,
		slog.String("profile", order.Profile.Name),
		slog.Any("error", err),
	)
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
