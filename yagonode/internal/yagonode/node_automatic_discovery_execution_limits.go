package yagonode

import "github.com/D4rk4/yago/yagocrawlcontract"

func withAutomaticDiscoveryExecutionLimits(config nodeConfig) nodeConfig {
	policy := config.Crawl.RuntimePolicy
	policy = withAutomaticDiscoveryExecutionLimit(
		policy,
		automaticDiscoveryExecutionLimit(
			webSeedProfileName,
			config.WebFallback.SeedDepth,
			config.WebFallback.SeedMaxPages,
			config.Crawl.MaxPagesPerRun,
		),
	)
	policy = withAutomaticDiscoveryExecutionLimit(
		policy,
		automaticDiscoveryExecutionLimit(
			swarmSeedProfileName,
			config.SwarmSeed.SeedDepth,
			config.SwarmSeed.SeedMaxPages,
			config.Crawl.MaxPagesPerRun,
		),
	)
	config.Crawl.RuntimePolicy = policy

	return config
}

func automaticDiscoveryExecutionLimit(
	profileName string,
	maximumDepth int,
	maximumPages int,
	crawlerMaximumPages int,
) yagocrawlcontract.AutomaticDiscoveryExecutionLimit {
	return yagocrawlcontract.AutomaticDiscoveryExecutionLimit{
		ProfileName:         profileName,
		MaximumDepth:        maximumDepth,
		MaximumPagesPerHost: maximumPages,
		MaximumPagesPerRun: automaticDiscoveryPageLimit(
			maximumPages,
			crawlerMaximumPages,
		),
	}
}

func withAutomaticDiscoveryExecutionLimit(
	policy yagocrawlcontract.CrawlerRuntimePolicy,
	limit yagocrawlcontract.AutomaticDiscoveryExecutionLimit,
) yagocrawlcontract.CrawlerRuntimePolicy {
	policy.AutomaticDiscoveryLimits = append(
		[]yagocrawlcontract.AutomaticDiscoveryExecutionLimit(nil),
		policy.AutomaticDiscoveryLimits...,
	)
	for index := range policy.AutomaticDiscoveryLimits {
		if policy.AutomaticDiscoveryLimits[index].ProfileName == limit.ProfileName {
			policy.AutomaticDiscoveryLimits[index] = limit

			return policy
		}
	}
	policy.AutomaticDiscoveryLimits = append(
		policy.AutomaticDiscoveryLimits,
		limit,
	)

	return policy
}

func (t *runtimeToggles) refreshAutomaticDiscoveryExecutionLimits() {
	if t == nil {
		return
	}
	webLimit := automaticDiscoveryExecutionLimit(
		webSeedProfileName,
		int(t.webSeedDepth.Load()),
		int(t.webSeedMaxPages.Load()),
		int(t.crawlerDefaultPageBudget.Load()),
	)
	swarmLimit := automaticDiscoveryExecutionLimit(
		swarmSeedProfileName,
		int(t.swarmSeedDepth.Load()),
		int(t.swarmSeedMaxPages.Load()),
		int(t.crawlerDefaultPageBudget.Load()),
	)
	t.UpdateCrawlerRuntimePolicy(func(policy *yagocrawlcontract.CrawlerRuntimePolicy) {
		*policy = withAutomaticDiscoveryExecutionLimit(*policy, webLimit)
		*policy = withAutomaticDiscoveryExecutionLimit(*policy, swarmLimit)
	})
}
