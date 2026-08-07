package crawlorder

import "github.com/D4rk4/yago/yagocrawlcontract"

type crawlOrderExecutionLimits struct {
	automaticDiscovery []yagocrawlcontract.AutomaticDiscoveryExecutionLimit
}

func (c *CrawlOrderConsumer) WithAutomaticDiscoveryExecutionLimits(
	limits []yagocrawlcontract.AutomaticDiscoveryExecutionLimit,
) *CrawlOrderConsumer {
	c.limits.automaticDiscovery = append(
		[]yagocrawlcontract.AutomaticDiscoveryExecutionLimit(nil),
		limits...,
	)

	return c
}

func (c *CrawlOrderConsumer) executionLimitedProfile(
	order yagocrawlcontract.CrawlOrder,
) yagocrawlcontract.CrawlProfile {
	profile := order.Profile
	if c.maximumDepth > 0 && profile.MaxDepth > c.maximumDepth {
		profile.MaxDepth = c.maximumDepth
	}
	if order.Priority != yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery {
		return profile
	}
	for _, limit := range c.limits.automaticDiscovery {
		if limit.ProfileName != profile.Name {
			continue
		}
		if profile.MaxDepth > limit.MaximumDepth {
			profile.MaxDepth = limit.MaximumDepth
		}
		if profile.MaxPagesPerHost == yagocrawlcontract.UnlimitedPagesPerHost ||
			profile.MaxPagesPerHost > limit.MaximumPagesPerHost {
			profile.MaxPagesPerHost = limit.MaximumPagesPerHost
		}
		maximumPagesPerRun := profile.EffectiveMaxPagesPerRun(0)
		if maximumPagesPerRun == 0 || maximumPagesPerRun > limit.MaximumPagesPerRun {
			maximumPagesPerRun = limit.MaximumPagesPerRun
			profile.MaxPagesPerRun = &maximumPagesPerRun
		}

		return profile
	}

	return profile
}
