package frontier

import (
	"testing"

	"github.com/D4rk4/yago/yago-crawler/internal/crawladmission"
	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestLegacyAutomaticDiscoveryOrderUsesHostLimitAsRunLimit(t *testing.T) {
	t.Parallel()

	profile, err := crawladmission.CompileProfile(yagocrawlcontract.NewCrawlProfile(
		yagocrawlcontract.CrawlProfile{
			Scope:           yagocrawlcontract.ScopeWide,
			URLMustMatch:    yagocrawlcontract.MatchAll,
			MaxPagesPerHost: 2,
		},
	))
	if err != nil {
		t.Fatalf("compile profile: %v", err)
	}
	frontier := NewFrontier(8, nil)
	seeded := frontier.SeedRunWithPriority(
		t.Context(),
		CrawlRunSeed{
			Requests: internalRequests(
				profile,
				"https://one.example/page",
				"https://two.example/page",
				"https://three.example/page",
			),
			Provenance: []byte("legacy-automatic"),
			Priority:   yagocrawlcontract.CrawlOrderPriorityAutomaticDiscovery,
		},
		profile,
		nil,
	)
	if seeded.Queued != 2 {
		t.Fatalf("queued = %d, want 2", seeded.Queued)
	}
	for range seeded.Queued {
		frontier.Done(internalReceive(t, frontier), successfulPageOutcome())
	}
}
