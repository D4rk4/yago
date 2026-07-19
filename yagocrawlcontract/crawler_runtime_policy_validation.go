package yagocrawlcontract

import (
	"fmt"
	"math"
	"strings"
	"time"
)

func (policy CrawlerRuntimePolicy) Validate() error {
	for _, validate := range []func(CrawlerRuntimePolicy) error{
		validateCrawlerRuntimeAccess,
		validateCrawlerRuntimeLimits,
		validateCrawlerRuntimeTiming,
		validateCrawlerRuntimeIdentity,
		validateCrawlerRuntimeFacilities,
	} {
		if err := validate(policy); err != nil {
			return err
		}
	}

	return nil
}

func validateCrawlerRuntimeFacilities(policy CrawlerRuntimePolicy) error {
	browserPath, err := ParseCrawlerBrowserPath(policy.BrowserPath)
	if err != nil {
		return err
	}
	if browserPath != policy.BrowserPath {
		return fmt.Errorf("browser path must use its canonical form")
	}
	metricsAddress, err := ParseCrawlerMetricsAddress(policy.MetricsAddress)
	if err != nil {
		return err
	}
	if metricsAddress != policy.MetricsAddress {
		return fmt.Errorf("metrics address must use its canonical form")
	}

	return nil
}

func validateCrawlerRuntimeAccess(policy CrawlerRuntimePolicy) error {
	_, err := ParseCrawlerPrivateCIDRs(
		FormatCrawlerPrivateCIDRs(policy.AllowedPrivateCIDRs),
	)

	return err
}

func validateCrawlerRuntimeLimits(policy CrawlerRuntimePolicy) error {
	if policy.FrontierStateMaximumBytes > math.MaxInt64 {
		return fmt.Errorf("frontier state maximum bytes exceed the supported range")
	}
	if policy.BrowserFailureThreshold < 0 ||
		policy.BrowserFailureThreshold > MaximumCrawlerBrowserFailureThreshold {
		return fmt.Errorf(
			"browser failure threshold must be between 0 and %d",
			MaximumCrawlerBrowserFailureThreshold,
		)
	}
	if policy.MaximumDepth < 1 || policy.MaximumDepth > MaximumCrawlerMaximumDepth {
		return fmt.Errorf(
			"maximum depth must be between 1 and %d",
			MaximumCrawlerMaximumDepth,
		)
	}
	if policy.MaximumHostConcurrency < 1 ||
		policy.MaximumHostConcurrency > MaximumCrawlerMaximumHostConcurrency {
		return fmt.Errorf(
			"maximum host concurrency must be between 1 and %d",
			MaximumCrawlerMaximumHostConcurrency,
		)
	}
	if policy.RunPagesPerMinute > MaximumCrawlerRunPagesPerMinute {
		return fmt.Errorf(
			"run pages per minute must not exceed %d",
			MaximumCrawlerRunPagesPerMinute,
		)
	}
	if policy.SitemapURLLimit < 1 ||
		policy.SitemapURLLimit > MaximumCrawlerSitemapURLLimit {
		return fmt.Errorf(
			"sitemap URL limit must be between 1 and %d",
			MaximumCrawlerSitemapURLLimit,
		)
	}

	return nil
}

func validateCrawlerRuntimeTiming(policy CrawlerRuntimePolicy) error {
	fields := []struct {
		name    string
		value   time.Duration
		maximum time.Duration
	}{
		{"connect timeout", policy.ConnectTimeout, MaximumCrawlerPhaseTimeout},
		{"header timeout", policy.HeaderTimeout, MaximumCrawlerPhaseTimeout},
		{"request timeout", policy.RequestTimeout, MaximumCrawlerRequestTimeout},
		{"TLS timeout", policy.TLSTimeout, MaximumCrawlerPhaseTimeout},
		{"shutdown grace", policy.ShutdownGrace, MaximumCrawlerShutdownGrace},
	}
	for _, field := range fields {
		if err := validateCrawlerPositiveDuration(
			field.name,
			field.value,
			field.maximum,
		); err != nil {
			return err
		}
	}
	if policy.CrawlDelay < 0 || policy.CrawlDelay > MaximumCrawlerCrawlDelay ||
		policy.CrawlDelay%time.Millisecond != 0 {
		return fmt.Errorf(
			"crawl delay must be zero or a whole-millisecond duration no greater than %s",
			MaximumCrawlerCrawlDelay,
		)
	}

	return nil
}

func validateCrawlerRuntimeIdentity(policy CrawlerRuntimePolicy) error {
	if policy.UserAgent == "" || len(policy.UserAgent) > MaximumCrawlerUserAgentBytes ||
		strings.ContainsAny(policy.UserAgent, "\r\n\x00") {
		return fmt.Errorf(
			"user agent must be one line between 1 and %d bytes",
			MaximumCrawlerUserAgentBytes,
		)
	}

	return nil
}

func validateCrawlerPositiveDuration(name string, value, maximum time.Duration) error {
	if value < MinimumCrawlerPositiveTimeout || value > maximum ||
		value%time.Millisecond != 0 {
		return fmt.Errorf(
			"%s must be a whole-millisecond duration between %s and %s",
			name,
			MinimumCrawlerPositiveTimeout,
			maximum,
		)
	}

	return nil
}
