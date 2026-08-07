package main

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestAutomaticDiscoveryLimitChangeRequestsGracefulRestart(t *testing.T) {
	effective := yagocrawlcontract.DefaultCrawlerRuntimePolicy()
	effective.AutomaticDiscoveryLimits = []yagocrawlcontract.AutomaticDiscoveryExecutionLimit{
		{
			ProfileName:         "web-fallback-seed",
			MaximumDepth:        5,
			MaximumPagesPerHost: 250,
			MaximumPagesPerRun:  250,
		},
	}
	restarts := 0
	change := newCrawlerRuntimePolicyChange(effective, nil, nil, func() { restarts++ })
	updated := effective
	updated.AutomaticDiscoveryLimits = append(
		[]yagocrawlcontract.AutomaticDiscoveryExecutionLimit(nil),
		effective.AutomaticDiscoveryLimits...,
	)
	updated.AutomaticDiscoveryLimits[0] = yagocrawlcontract.AutomaticDiscoveryExecutionLimit{
		ProfileName:         "web-fallback-seed",
		MaximumDepth:        1,
		MaximumPagesPerHost: 25,
		MaximumPagesPerRun:  25,
	}
	change.Apply(updated)
	if restarts != 1 || !change.Current().Equal(effective) {
		t.Fatalf("automatic limit restart = %d, current = %+v", restarts, change.Current())
	}
}
