package crawlbroker

import (
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestControlRegistryConvergesMaximumRedirects(t *testing.T) {
	registry := newControlRegistry(crawlerControlDefaults{
		maximumRedirects:    10,
		maximumRedirectsSet: true,
	})
	registry.register("crawler-a")
	initial, err := registry.deliverForHeartbeat(t.Context(), "crawler-a", nil)
	if err != nil {
		t.Fatalf("initial heartbeat: %v", err)
	}
	if !hasMaximumRedirects(initial, 10) {
		t.Fatalf("initial directives = %+v, want maximum redirects 10", initial)
	}
	if signalled := registry.SetMaximumRedirects(7); signalled != 1 {
		t.Fatalf("signalled workers = %d, want 1", signalled)
	}
	updated, err := registry.deliverForHeartbeat(t.Context(), "crawler-a", nil)
	if err != nil {
		t.Fatalf("updated heartbeat: %v", err)
	}
	if !hasMaximumRedirects(updated, 7) || registry.MaximumRedirects() != 7 {
		t.Fatalf("updated directives/maximum = %+v/%d", updated,
			registry.MaximumRedirects())
	}
	for _, invalid := range []int{-1, yagocrawlcontract.MaximumPageRedirects + 1} {
		if signalled := registry.SetMaximumRedirects(invalid); signalled != 0 {
			t.Fatalf("invalid maximum %d signalled %d workers", invalid, signalled)
		}
	}
}

func hasMaximumRedirects(
	directives []yagocrawlcontract.CrawlControlDirective,
	maximum uint32,
) bool {
	for _, directive := range directives {
		if directive.Kind == yagocrawlcontract.CrawlControlSetMaximumRedirects &&
			directive.MaximumRedirects == maximum {
			return true
		}
	}

	return false
}
