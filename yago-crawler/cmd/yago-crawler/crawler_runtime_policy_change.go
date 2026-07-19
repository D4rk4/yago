package main

import (
	"net/netip"
	"sync"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type browserSandboxPolicyTarget interface {
	SetSandbox(bool)
}

type crawlerRuntimePolicyChange struct {
	mu        sync.Mutex
	effective yagocrawlcontract.CrawlerRuntimePolicy
	browser   browserSandboxPolicyTarget
	restart   func()
}

func newCrawlerRuntimePolicyChange(
	effective yagocrawlcontract.CrawlerRuntimePolicy,
	source any,
	restart func(),
) *crawlerRuntimePolicyChange {
	change := &crawlerRuntimePolicyChange{
		effective: cloneCrawlerRuntimePolicy(effective),
		restart:   restart,
	}
	change.browser, _ = source.(browserSandboxPolicyTarget)

	return change
}

func (change *crawlerRuntimePolicyChange) Current() yagocrawlcontract.CrawlerRuntimePolicy {
	change.mu.Lock()
	defer change.mu.Unlock()

	return cloneCrawlerRuntimePolicy(change.effective)
}

func (change *crawlerRuntimePolicyChange) Apply(
	policy yagocrawlcontract.CrawlerRuntimePolicy,
) {
	change.mu.Lock()
	if change.effective.Equal(policy) {
		change.mu.Unlock()

		return
	}
	sandboxOnly := change.effective
	sandboxOnly.BrowserSandbox = policy.BrowserSandbox
	if sandboxOnly.Equal(policy) && change.browser != nil {
		change.browser.SetSandbox(policy.BrowserSandbox)
		change.effective.BrowserSandbox = policy.BrowserSandbox
		change.mu.Unlock()

		return
	}
	restart := change.restart
	change.mu.Unlock()
	if restart != nil {
		restart()
	}
}

func cloneCrawlerRuntimePolicy(
	policy yagocrawlcontract.CrawlerRuntimePolicy,
) yagocrawlcontract.CrawlerRuntimePolicy {
	policy.AllowedPrivateCIDRs = append([]netip.Prefix(nil), policy.AllowedPrivateCIDRs...)

	return policy
}
