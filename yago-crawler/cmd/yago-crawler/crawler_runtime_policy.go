package main

import "github.com/D4rk4/yago/yagocrawlcontract"

func (config ServiceConfig) runtimePolicy() yagocrawlcontract.CrawlerRuntimePolicy {
	return yagocrawlcontract.CrawlerRuntimePolicy{
		AllowPrivateNetworks:    config.EgressAllowLAN,
		AllowedPrivateCIDRs:     config.EgressAllowedCIDRs,
		BrowserFailureThreshold: config.Crawl.BrowserFailureThreshold,
		BrowserPath:             config.Crawl.BrowserPath,
		BrowserSandbox:          config.Crawl.BrowserSandbox,
		ConnectTimeout:          config.Crawl.ConnectTimeout,
		CrawlDelay:              config.Crawl.CrawlDelay,
		HeaderTimeout:           config.Crawl.HeaderTimeout,
		MaximumDepth:            config.Crawl.MaxDepth,
		MaximumHostConcurrency:  config.Crawl.MaxHostConcurrency,
		MetricsAddress:          config.MetricsAddr,
		RequestTimeout:          config.Crawl.RequestTimeout,
		RunPagesPerMinute:       config.Crawl.RunPagesPerMinute,
		SitemapURLLimit:         config.Crawl.SitemapURLLimit,
		TLSTimeout:              config.Crawl.TLSTimeout,
		ShutdownGrace:           config.ShutdownGrace,
		UserAgent:               config.Crawl.UserAgent,
	}
}

func (config ServiceConfig) withRuntimePolicy(
	policy yagocrawlcontract.CrawlerRuntimePolicy,
) ServiceConfig {
	config.EgressAllowLAN = policy.AllowPrivateNetworks
	config.EgressAllowedCIDRs = append(config.EgressAllowedCIDRs[:0], policy.AllowedPrivateCIDRs...)
	config.Crawl.BrowserFailureThreshold = policy.BrowserFailureThreshold
	config.Crawl.BrowserPath = policy.BrowserPath
	config.Crawl.BrowserSandbox = policy.BrowserSandbox
	config.Crawl.ConnectTimeout = policy.ConnectTimeout
	config.Crawl.CrawlDelay = policy.CrawlDelay
	config.Crawl.HeaderTimeout = policy.HeaderTimeout
	config.Crawl.MaxDepth = policy.MaximumDepth
	config.Crawl.MaxHostConcurrency = policy.MaximumHostConcurrency
	config.MetricsAddr = policy.MetricsAddress
	config.Crawl.RequestTimeout = policy.RequestTimeout
	config.Crawl.RunPagesPerMinute = policy.RunPagesPerMinute
	config.Crawl.SitemapURLLimit = policy.SitemapURLLimit
	config.Crawl.TLSTimeout = policy.TLSTimeout
	config.ShutdownGrace = policy.ShutdownGrace
	config.Crawl.UserAgent = policy.UserAgent

	return config
}
