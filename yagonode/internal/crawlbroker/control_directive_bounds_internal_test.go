package crawlbroker

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

type boundedControlDirective struct {
	name       string
	atMaximum  yagocrawlcontract.CrawlControlDirective
	pastLimit  yagocrawlcontract.CrawlControlDirective
	runScoped  yagocrawlcontract.CrawlControlDirective
	belowFloor yagocrawlcontract.CrawlControlDirective
}

// TestControlDirectiveBoundsAdmitTheLimitAndRefusePastIt pins both sides of every
// bounded fleet directive. The limit itself is the operator's legitimate maximum:
// a validator tightened to reject it would silently make the documented ceiling
// unreachable, and no assertion on the rejecting side alone can notice that.
func TestControlDirectiveBoundsAdmitTheLimitAndRefusePastIt(t *testing.T) {
	for _, bounded := range boundedControlDirectives() {
		t.Run(bounded.name, func(t *testing.T) {
			if !validControlDirective(bounded.atMaximum) {
				t.Fatalf("directive at its contract maximum rejected: %+v", bounded.atMaximum)
			}
			if validControlDirective(bounded.pastLimit) {
				t.Fatalf("directive past its contract maximum accepted: %+v", bounded.pastLimit)
			}
			if bounded.belowFloor.Kind != "" && validControlDirective(bounded.belowFloor) {
				t.Fatalf("directive below its floor accepted: %+v", bounded.belowFloor)
			}
		})
	}
}

// TestFleetControlDirectivesRefuseRunScoping pins the RunID arm of every
// fleet-wide directive kind. These directives reconfigure the whole crawler
// fleet, so the ledger delivers them to every worker regardless of what run a
// worker happens to be serving. Accepting a RunID on one would let an operator
// or a peer believe the change was scoped to a single crawl run while it in fact
// retunes every connected crawler.
func TestFleetControlDirectivesRefuseRunScoping(t *testing.T) {
	for _, bounded := range boundedControlDirectives() {
		t.Run(bounded.name, func(t *testing.T) {
			if validControlDirective(bounded.runScoped) {
				t.Fatalf("run-scoped fleet directive accepted: %+v", bounded.runScoped)
			}
		})
	}
}

func boundedControlDirectives() []boundedControlDirective {
	return []boundedControlDirective{
		{
			name: "workers",
			atMaximum: yagocrawlcontract.CrawlControlDirective{
				Kind:         yagocrawlcontract.CrawlControlSetWorkers,
				FetchWorkers: yagocrawlcontract.MaximumFetchWorkerConcurrency,
			},
			pastLimit: yagocrawlcontract.CrawlControlDirective{
				Kind:         yagocrawlcontract.CrawlControlSetWorkers,
				FetchWorkers: yagocrawlcontract.MaximumFetchWorkerConcurrency + 1,
			},
			runScoped: yagocrawlcontract.CrawlControlDirective{
				Kind:         yagocrawlcontract.CrawlControlSetWorkers,
				FetchWorkers: 1,
				RunID:        "ab",
			},
			belowFloor: yagocrawlcontract.CrawlControlDirective{
				Kind:         yagocrawlcontract.CrawlControlSetWorkers,
				FetchWorkers: 0,
			},
		},
		{
			name: "active-runs",
			atMaximum: yagocrawlcontract.CrawlControlDirective{
				Kind:              yagocrawlcontract.CrawlControlSetActiveRuns,
				MaximumActiveRuns: yagocrawlcontract.MaximumActiveCrawlRunConcurrency,
			},
			pastLimit: yagocrawlcontract.CrawlControlDirective{
				Kind:              yagocrawlcontract.CrawlControlSetActiveRuns,
				MaximumActiveRuns: yagocrawlcontract.MaximumActiveCrawlRunConcurrency + 1,
			},
			runScoped: yagocrawlcontract.CrawlControlDirective{
				Kind:              yagocrawlcontract.CrawlControlSetActiveRuns,
				MaximumActiveRuns: 1,
				RunID:             "ab",
			},
			belowFloor: yagocrawlcontract.CrawlControlDirective{
				Kind:              yagocrawlcontract.CrawlControlSetActiveRuns,
				MaximumActiveRuns: 0,
			},
		},
		{
			name: "process-rate",
			atMaximum: yagocrawlcontract.CrawlControlDirective{
				Kind:                  yagocrawlcontract.CrawlControlSetProcessRate,
				ProcessPagesPerSecond: yagocrawlcontract.MaximumProcessPagesPerSecond,
			},
			pastLimit: yagocrawlcontract.CrawlControlDirective{
				Kind:                  yagocrawlcontract.CrawlControlSetProcessRate,
				ProcessPagesPerSecond: yagocrawlcontract.MaximumProcessPagesPerSecond + 1,
			},
			runScoped: yagocrawlcontract.CrawlControlDirective{
				Kind:                  yagocrawlcontract.CrawlControlSetProcessRate,
				ProcessPagesPerSecond: 1,
				RunID:                 "ab",
			},
		},
		{
			name: "maximum-redirects",
			atMaximum: yagocrawlcontract.CrawlControlDirective{
				Kind:             yagocrawlcontract.CrawlControlSetMaximumRedirects,
				MaximumRedirects: yagocrawlcontract.MaximumPageRedirects,
			},
			pastLimit: yagocrawlcontract.CrawlControlDirective{
				Kind:             yagocrawlcontract.CrawlControlSetMaximumRedirects,
				MaximumRedirects: yagocrawlcontract.MaximumPageRedirects + 1,
			},
			runScoped: yagocrawlcontract.CrawlControlDirective{
				Kind:             yagocrawlcontract.CrawlControlSetMaximumRedirects,
				MaximumRedirects: 1,
				RunID:            "ab",
			},
		},
	}
}
