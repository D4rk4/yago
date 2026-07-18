package crawlbroker

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestControlDirectiveValidation(t *testing.T) {
	valid := []yagocrawlcontract.CrawlControlDirective{
		{Kind: yagocrawlcontract.CrawlControlPause, RunID: "ab"},
		{Kind: yagocrawlcontract.CrawlControlResume, RunID: "ab"},
		{Kind: yagocrawlcontract.CrawlControlCancel, RunID: "ab"},
		{Kind: yagocrawlcontract.CrawlControlSetRate, RunID: "ab"},
		{Kind: yagocrawlcontract.CrawlControlRestart},
		{Kind: yagocrawlcontract.CrawlControlSetWorkers, FetchWorkers: 1},
		{Kind: yagocrawlcontract.CrawlControlSetActiveRuns, MaximumActiveRuns: 1},
		{Kind: yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority},
	}
	for _, directive := range valid {
		if !validControlDirective(directive) {
			t.Fatalf("valid directive rejected: %+v", directive)
		}
	}
	invalid := []yagocrawlcontract.CrawlControlDirective{
		{Kind: yagocrawlcontract.CrawlControlPause},
		{Kind: yagocrawlcontract.CrawlControlPause, RunID: "not-hex"},
		{Kind: yagocrawlcontract.CrawlControlRestart, RunID: "ab"},
		{Kind: yagocrawlcontract.CrawlControlSetWorkers},
		{
			Kind:         yagocrawlcontract.CrawlControlSetWorkers,
			FetchWorkers: yagocrawlcontract.MaximumFetchWorkerConcurrency + 1,
		},
		{Kind: yagocrawlcontract.CrawlControlSetActiveRuns},
		{
			Kind:              yagocrawlcontract.CrawlControlSetActiveRuns,
			MaximumActiveRuns: yagocrawlcontract.MaximumActiveCrawlRunConcurrency + 1,
		},
		{Kind: yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority, RunID: "ab"},
		{Kind: yagocrawlcontract.CrawlControlKind("unknown")},
	}
	for _, directive := range invalid {
		if validControlDirective(directive) {
			t.Fatalf("invalid directive accepted: %+v", directive)
		}
	}
}

func TestControlRegistryRejectsInvalidDirective(t *testing.T) {
	registry := newControlRegistry()
	registry.register("worker")
	if registry.Enqueue("worker", yagocrawlcontract.CrawlControlDirective{
		Kind:  yagocrawlcontract.CrawlControlPause,
		RunID: "not-hex",
	}) {
		t.Fatal("invalid directive was persisted")
	}
}
