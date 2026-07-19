package yagonode

import (
	"fmt"
	"strings"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func applyCrawlerLimitBootstrap(
	getenv func(string) string,
	policy *yagocrawlcontract.CrawlerRuntimePolicy,
) error {
	frontierStateMaximumBytes, err := parseCrawlerFrontierStateMaximumBytes(envWithDefault(
		getenv,
		envCrawlerFrontierStateMaximumBytes,
		formatCrawlerFrontierStateMaximumBytes(policy.FrontierStateMaximumBytes),
	))
	if err != nil {
		return fmt.Errorf("%s: %w", envCrawlerFrontierStateMaximumBytes, err)
	}
	policy.FrontierStateMaximumBytes = frontierStateMaximumBytes
	fields := []struct {
		key      string
		fallback int
		minimum  int
		maximum  int
		target   *int
	}{
		{
			key: envCrawlerBrowserFailureLimit, fallback: policy.BrowserFailureThreshold,
			maximum: yagocrawlcontract.MaximumCrawlerBrowserFailureThreshold,
			target:  &policy.BrowserFailureThreshold,
		},
		{
			key: envCrawlerMaximumDepth, fallback: policy.MaximumDepth, minimum: 1,
			maximum: yagocrawlcontract.MaximumCrawlerMaximumDepth,
			target:  &policy.MaximumDepth,
		},
		{
			key: envCrawlerMaximumHostFetches, fallback: policy.MaximumHostConcurrency,
			minimum: 1, maximum: yagocrawlcontract.MaximumCrawlerMaximumHostConcurrency,
			target: &policy.MaximumHostConcurrency,
		},
		{
			key: envCrawlerSitemapURLLimit, fallback: policy.SitemapURLLimit, minimum: 1,
			maximum: yagocrawlcontract.MaximumCrawlerSitemapURLLimit,
			target:  &policy.SitemapURLLimit,
		},
	}
	for _, field := range fields {
		value, err := intRangeEnv(
			getenv,
			field.key,
			field.fallback,
			field.minimum,
			field.maximum,
		)
		if err != nil {
			return err
		}
		*field.target = value
	}
	if raw := strings.TrimSpace(getenv(envCrawlerRunPagesPerMinute)); raw != "" {
		value, err := yagocrawlcontract.ParseCrawlerRunPagesPerMinute(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", envCrawlerRunPagesPerMinute, err)
		}
		policy.RunPagesPerMinute = value
	}

	return nil
}
