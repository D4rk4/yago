package yagonode

import (
	"fmt"
	"strings"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func loadCrawlerRuntimePolicy(
	getenv func(string) string,
) (yagocrawlcontract.CrawlerRuntimePolicy, error) {
	policy := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	policy.UserAgent = "yago-crawler/" + Version() + " (+https://github.com/D4rk4/yago/)"
	for _, apply := range []func(
		func(string) string,
		*yagocrawlcontract.CrawlerRuntimePolicy,
	) error{
		applyCrawlerAccessBootstrap,
		applyCrawlerFacilityBootstrap,
		applyCrawlerLimitBootstrap,
		applyCrawlerTimingBootstrap,
	} {
		if err := apply(getenv, &policy); err != nil {
			return yagocrawlcontract.CrawlerRuntimePolicy{}, err
		}
	}
	if err := policy.Validate(); err != nil {
		return yagocrawlcontract.CrawlerRuntimePolicy{}, fmt.Errorf(
			"crawler runtime policy: %w",
			err,
		)
	}

	return policy, nil
}

func crawlerDurationEnv(
	getenv func(string) string,
	key string,
	fallback time.Duration,
	allowZero bool,
) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	if value < 0 || (!allowZero && value == 0) {
		return 0, fmt.Errorf("%s: invalid duration", key)
	}

	return value, nil
}
