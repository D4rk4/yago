package yagonode

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func automaticDiscoveryLimitByName(
	t *testing.T,
	policy yagocrawlcontract.CrawlerRuntimePolicy,
	name string,
) yagocrawlcontract.AutomaticDiscoveryExecutionLimit {
	t.Helper()
	for _, limit := range policy.AutomaticDiscoveryLimits {
		if limit.ProfileName == name {
			return limit
		}
	}
	t.Fatalf("automatic discovery limit %q is missing", name)

	return yagocrawlcontract.AutomaticDiscoveryExecutionLimit{}
}

func automaticDiscoveryLimitConfig() nodeConfig {
	return nodeConfig{
		Crawl: crawlConfig{
			MaxPagesPerRun: 40,
			RuntimePolicy:  yagocrawlcontract.DefaultCrawlerRuntimePolicy(),
		},
		WebFallback: webFallbackConfig{SeedDepth: 3, SeedMaxPages: 50},
		SwarmSeed:   swarmSeedConfig{SeedDepth: 1, SeedMaxPages: 25},
	}
}

func TestAutomaticDiscoveryExecutionLimitsFollowEffectiveConfiguration(t *testing.T) {
	config := automaticDiscoveryLimitConfig()
	config.Crawl.RuntimePolicy.AutomaticDiscoveryLimits = []yagocrawlcontract.AutomaticDiscoveryExecutionLimit{
		{
			ProfileName:         "future-profile",
			MaximumDepth:        2,
			MaximumPagesPerHost: 3,
			MaximumPagesPerRun:  3,
		},
		{
			ProfileName:         webSeedProfileName,
			MaximumDepth:        8,
			MaximumPagesPerHost: 250,
			MaximumPagesPerRun:  250,
		},
	}
	effective := withAutomaticDiscoveryExecutionLimits(config)
	if err := effective.Crawl.RuntimePolicy.Validate(); err != nil {
		t.Fatalf("effective policy: %v", err)
	}
	web := automaticDiscoveryLimitByName(t, effective.Crawl.RuntimePolicy, webSeedProfileName)
	if web.MaximumDepth != 3 || web.MaximumPagesPerHost != 50 || web.MaximumPagesPerRun != 40 {
		t.Fatalf("web execution limit = %+v", web)
	}
	swarm := automaticDiscoveryLimitByName(t, effective.Crawl.RuntimePolicy, swarmSeedProfileName)
	if swarm.MaximumDepth != 1 || swarm.MaximumPagesPerHost != 25 ||
		swarm.MaximumPagesPerRun != 25 {
		t.Fatalf("swarm execution limit = %+v", swarm)
	}
	future := automaticDiscoveryLimitByName(t, effective.Crawl.RuntimePolicy, "future-profile")
	if future.MaximumPagesPerRun != 3 {
		t.Fatalf("unrelated execution limit = %+v", future)
	}
	if original := automaticDiscoveryLimitByName(
		t,
		config.Crawl.RuntimePolicy,
		webSeedProfileName,
	); original.MaximumPagesPerRun != 250 {
		t.Fatalf("source policy mutated: %+v", original)
	}
}

func TestAutomaticDiscoveryExecutionLimitsFollowLiveLimitChanges(t *testing.T) {
	config := withAutomaticDiscoveryExecutionLimits(automaticDiscoveryLimitConfig())
	toggles := newRuntimeToggles(config)
	var current yagocrawlcontract.CrawlerRuntimePolicy
	toggles.SetCrawlerRuntimePolicySink(func(policy yagocrawlcontract.CrawlerRuntimePolicy) bool {
		current = policy

		return true
	})
	pageBudget := -1
	toggles.SetCrawlerMaxPagesPerRunSink(func(value int) { pageBudget = value })

	toggles.SetWebSeedDepth(2)
	toggles.SetWebSeedMaxPages(25)
	web := automaticDiscoveryLimitByName(t, current, webSeedProfileName)
	if web.MaximumDepth != 2 || web.MaximumPagesPerHost != 25 || web.MaximumPagesPerRun != 25 {
		t.Fatalf("live web execution limit = %+v", web)
	}
	toggles.SetSwarmSeedDepth(0)
	toggles.SetSwarmSeedMaxPages(30)
	toggles.ApplyCrawlerMaxPagesPerRun(10)
	web = automaticDiscoveryLimitByName(t, current, webSeedProfileName)
	swarm := automaticDiscoveryLimitByName(t, current, swarmSeedProfileName)
	if pageBudget != 10 || web.MaximumPagesPerRun != 10 ||
		swarm.MaximumDepth != 0 || swarm.MaximumPagesPerHost != 30 ||
		swarm.MaximumPagesPerRun != 10 {
		t.Fatalf("live limits = budget %d web %+v swarm %+v", pageBudget, web, swarm)
	}

	var absent *runtimeToggles
	absent.refreshAutomaticDiscoveryExecutionLimits()
}

func TestAutomaticDiscoveryExecutionLimitReachesConnectedCrawlerPolicy(t *testing.T) {
	runtime := liveCrawlRuntime(t)
	config := withAutomaticDiscoveryExecutionLimits(automaticDiscoveryLimitConfig())
	toggles := newRuntimeToggles(config)
	attachCrawlRuntimeSettings(runtime, toggles)
	toggles.SetWebSeedMaxPages(25)
	web := automaticDiscoveryLimitByName(
		t,
		runtime.controlRegistry().RuntimePolicy(),
		webSeedProfileName,
	)
	if web.MaximumPagesPerHost != 25 || web.MaximumPagesPerRun != 25 {
		t.Fatalf("connected crawler web limit = %+v", web)
	}
}
