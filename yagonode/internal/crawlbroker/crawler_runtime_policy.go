package crawlbroker

import (
	"net/netip"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func (r *ControlRegistry) RuntimePolicy() yagocrawlcontract.CrawlerRuntimePolicy {
	r.mu.Lock()
	defer r.mu.Unlock()

	return cloneCrawlerRuntimePolicy(r.runtimePolicy)
}

func (r *ControlRegistry) SetRuntimePolicy(
	policy yagocrawlcontract.CrawlerRuntimePolicy,
) bool {
	if err := policy.Validate(); err != nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.runtimePolicy.Equal(policy) {
		return true
	}
	r.runtimePolicy = cloneCrawlerRuntimePolicy(policy)

	return true
}

func cloneCrawlerRuntimePolicy(
	policy yagocrawlcontract.CrawlerRuntimePolicy,
) yagocrawlcontract.CrawlerRuntimePolicy {
	policy.AllowedPrivateCIDRs = append(
		[]netip.Prefix(nil),
		policy.AllowedPrivateCIDRs...,
	)

	return policy
}
