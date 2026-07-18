package crawlorder

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestActiveRunControlHandlerAppliesBoundedProcessDirective(t *testing.T) {
	var applied []int
	next := &recordingControlHandler{}
	handler := NewActiveRunControlHandler(func(maximum int) {
		applied = append(applied, maximum)
	}, next)

	handler.Apply(t.Context(), yagocrawlcontract.CrawlControlDirective{
		Kind:              yagocrawlcontract.CrawlControlSetActiveRuns,
		MaximumActiveRuns: 37,
	})
	for _, maximum := range []uint32{
		0,
		yagocrawlcontract.MaximumActiveCrawlRunConcurrency + 1,
	} {
		handler.Apply(t.Context(), yagocrawlcontract.CrawlControlDirective{
			Kind:              yagocrawlcontract.CrawlControlSetActiveRuns,
			MaximumActiveRuns: maximum,
		})
	}
	if len(applied) != 1 || applied[0] != 37 {
		t.Fatalf("applied active-run concurrency = %v, want [37]", applied)
	}
	if got := next.snapshot(); len(got) != 0 {
		t.Fatalf("active-run directives leaked to run controller: %+v", got)
	}
}

func TestActiveRunControlHandlerDelegatesRunDirective(t *testing.T) {
	next := &recordingControlHandler{}
	handler := NewActiveRunControlHandler(nil, next)
	directive := yagocrawlcontract.CrawlControlDirective{Kind: yagocrawlcontract.CrawlControlPause}
	handler.Apply(context.Background(), directive)
	if got := next.snapshot(); len(got) != 1 || got[0].Kind != directive.Kind {
		t.Fatalf("delegated directives = %+v, want pause", got)
	}
	NewActiveRunControlHandler(nil, nil).Apply(context.Background(), directive)
}
