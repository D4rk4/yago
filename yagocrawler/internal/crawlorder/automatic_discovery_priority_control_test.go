package crawlorder

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestAutomaticDiscoveryPriorityControlHandlerAppliesGlobalDirective(t *testing.T) {
	var applied []bool
	next := &recordingControlHandler{}
	handler := NewAutomaticDiscoveryPriorityControlHandler(func(enabled bool) {
		applied = append(applied, enabled)
	}, next)

	for _, enabled := range []bool{false, true} {
		handler.Apply(t.Context(), yagocrawlcontract.CrawlControlDirective{
			Kind:                         yagocrawlcontract.CrawlControlSetAutomaticDiscoveryPriority,
			PrioritizeAutomaticDiscovery: enabled,
		})
	}
	if len(applied) != 2 || applied[0] || !applied[1] {
		t.Fatalf("applied priority states = %v, want [false true]", applied)
	}
	if got := next.snapshot(); len(got) != 0 {
		t.Fatalf("priority directives leaked to run controller: %+v", got)
	}
}

func TestAutomaticDiscoveryPriorityControlHandlerDelegatesRunDirective(t *testing.T) {
	next := &recordingControlHandler{}
	handler := NewAutomaticDiscoveryPriorityControlHandler(nil, next)
	directive := yagocrawlcontract.CrawlControlDirective{Kind: yagocrawlcontract.CrawlControlPause}
	handler.Apply(context.Background(), directive)
	if got := next.snapshot(); len(got) != 1 || got[0].Kind != directive.Kind {
		t.Fatalf("delegated directives = %+v, want pause", got)
	}
	NewAutomaticDiscoveryPriorityControlHandler(nil, nil).Apply(context.Background(), directive)
}
