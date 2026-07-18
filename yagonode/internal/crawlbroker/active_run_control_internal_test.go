package crawlbroker

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestControlRegistryConvergesMaximumActiveRuns(t *testing.T) {
	registry := newControlRegistry(crawlerControlDefaults{maximumActiveRuns: 32})
	registry.register("worker-a")
	registry.register("worker-b")
	for _, worker := range []string{"worker-a", "worker-b"} {
		initial := deliveredControls(t, registry, worker)
		if len(initial) != 2 ||
			initial[0].Kind != yagocrawlcontract.CrawlControlSetActiveRuns ||
			initial[0].MaximumActiveRuns != 32 ||
			initial[1].Kind != yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority {
			t.Fatalf("%s initial directives = %+v, want active runs 32", worker, initial)
		}
		deliveredControls(t, registry, worker, controlDirectiveIDs(initial)...)
	}
	if signalled := registry.SetMaximumActiveRuns(37); signalled != 2 {
		t.Fatalf("set maximum active runs signalled %d workers, want 2", signalled)
	}
	if got := registry.MaximumActiveRuns(); got != 37 {
		t.Fatalf("maximum active runs = %d, want 37", got)
	}
	for _, worker := range []string{"worker-a", "worker-b"} {
		directives := deliveredControls(t, registry, worker)
		if len(directives) != 1 || directives[0].MaximumActiveRuns != 37 {
			t.Fatalf("%s directives = %+v, want active runs 37", worker, directives)
		}
	}
	for _, invalid := range []int{0, 257} {
		if signalled := registry.SetMaximumActiveRuns(invalid); signalled != 0 {
			t.Fatalf("invalid maximum %d signalled %d workers", invalid, signalled)
		}
	}
}
