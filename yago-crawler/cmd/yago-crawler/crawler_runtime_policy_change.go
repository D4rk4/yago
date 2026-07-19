package main

import (
	"net/netip"
	"sync"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type browserSandboxPolicyTarget interface {
	SetSandbox(bool)
}

type frontierStateMaximumPolicyTarget interface {
	SetStateMaximumBytes(uint64)
}

type crawlerRuntimePolicyChange struct {
	mu        sync.Mutex
	effective yagocrawlcontract.CrawlerRuntimePolicy
	browser   browserSandboxPolicyTarget
	frontier  frontierStateMaximumPolicyTarget
	restart   func()
}

func newCrawlerRuntimePolicyChange(
	effective yagocrawlcontract.CrawlerRuntimePolicy,
	browserSource any,
	frontierSource any,
	restart func(),
) *crawlerRuntimePolicyChange {
	change := &crawlerRuntimePolicyChange{
		effective: cloneCrawlerRuntimePolicy(effective),
		restart:   restart,
	}
	change.browser, _ = browserSource.(browserSandboxPolicyTarget)
	change.frontier, _ = frontierSource.(frontierStateMaximumPolicyTarget)

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
	livePolicy := change.effective
	livePolicy.BrowserSandbox = policy.BrowserSandbox
	livePolicy.FrontierStateMaximumBytes = policy.FrontierStateMaximumBytes
	browserChanged := change.effective.BrowserSandbox != policy.BrowserSandbox
	frontierChanged := change.effective.FrontierStateMaximumBytes != policy.FrontierStateMaximumBytes
	canApply := (!browserChanged || change.browser != nil) &&
		(!frontierChanged || change.frontier != nil)
	if livePolicy.Equal(policy) && canApply {
		if browserChanged {
			change.browser.SetSandbox(policy.BrowserSandbox)
		}
		if frontierChanged {
			change.frontier.SetStateMaximumBytes(policy.FrontierStateMaximumBytes)
		}
		change.effective = livePolicy
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
