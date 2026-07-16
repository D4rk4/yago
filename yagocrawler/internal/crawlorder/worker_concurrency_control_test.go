package crawlorder

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestWorkerConcurrencyControlHandlerAppliesBoundedProcessDirective(t *testing.T) {
	var applied []int
	next := &recordingControlHandler{}
	handler := NewWorkerConcurrencyControlHandler(func(workers int) {
		applied = append(applied, workers)
	}, next)

	handler.Apply(t.Context(), yagocrawlcontract.CrawlControlDirective{
		Kind:         yagocrawlcontract.CrawlControlSetWorkers,
		FetchWorkers: 12,
	})
	for _, workers := range []uint32{0, yagocrawlcontract.MaximumFetchWorkerConcurrency + 1} {
		handler.Apply(t.Context(), yagocrawlcontract.CrawlControlDirective{
			Kind:         yagocrawlcontract.CrawlControlSetWorkers,
			FetchWorkers: workers,
		})
	}
	if len(applied) != 1 || applied[0] != 12 {
		t.Fatalf("applied concurrency = %v, want [12]", applied)
	}
	if got := next.snapshot(); len(got) != 0 {
		t.Fatalf("worker directives leaked to run controller: %+v", got)
	}
}

func TestWorkerConcurrencyControlHandlerDelegatesRunDirective(t *testing.T) {
	next := &recordingControlHandler{}
	handler := NewWorkerConcurrencyControlHandler(nil, next)
	directive := yagocrawlcontract.CrawlControlDirective{Kind: yagocrawlcontract.CrawlControlPause}
	handler.Apply(context.Background(), directive)
	if got := next.snapshot(); len(got) != 1 || got[0].Kind != directive.Kind {
		t.Fatalf("delegated directives = %+v, want pause", got)
	}
	NewWorkerConcurrencyControlHandler(nil, nil).Apply(context.Background(), directive)
}
