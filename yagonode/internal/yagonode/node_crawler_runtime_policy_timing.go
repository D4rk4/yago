package yagonode

import (
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func applyCrawlerTimingBootstrap(
	getenv func(string) string,
	policy *yagocrawlcontract.CrawlerRuntimePolicy,
) error {
	fields := []struct {
		key       string
		fallback  time.Duration
		allowZero bool
		target    *time.Duration
	}{
		{envCrawlerConnectTimeout, policy.ConnectTimeout, false, &policy.ConnectTimeout},
		{envCrawlerCrawlDelay, policy.CrawlDelay, true, &policy.CrawlDelay},
		{envCrawlerHeaderTimeout, policy.HeaderTimeout, false, &policy.HeaderTimeout},
		{envCrawlerRequestTimeout, policy.RequestTimeout, false, &policy.RequestTimeout},
		{envCrawlerTLSTimeout, policy.TLSTimeout, false, &policy.TLSTimeout},
		{envCrawlerShutdownGrace, policy.ShutdownGrace, false, &policy.ShutdownGrace},
	}
	for _, field := range fields {
		value, err := crawlerDurationEnv(
			getenv,
			field.key,
			field.fallback,
			field.allowZero,
		)
		if err != nil {
			return err
		}
		*field.target = value
	}

	return nil
}
