package websearch

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type providerContextProbe struct {
	deadline bool
	done     chan struct{}
}

func (p *providerContextProbe) Search(
	ctx context.Context,
	_ string,
	_ int,
) ([]Result, error) {
	_, p.deadline = ctx.Deadline()
	if p.done == nil {
		return nil, nil
	}
	<-ctx.Done()
	close(p.done)

	return nil, fmt.Errorf("provider context: %w", ctx.Err())
}

func TestProviderBudgetCancelsExternalSearch(t *testing.T) {
	provider := &providerContextProbe{done: make(chan struct{})}
	searcher := NewFallbackSearcher(
		&stubSearcher{},
		provider,
		enabled,
		WithProviderBudget(10*time.Millisecond),
	)

	response, err := searcher.Search(t.Context(), searchcore.Request{Query: "gap", Limit: 10})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(response.Results) != 0 {
		t.Fatalf("results = %#v", response.Results)
	}
	if !provider.deadline {
		t.Fatal("provider context has no deadline")
	}
	select {
	case <-provider.done:
	default:
		t.Fatal("provider did not observe budget cancellation")
	}
}

func TestZeroProviderBudgetPreservesParentContext(t *testing.T) {
	provider := &providerContextProbe{}
	searcher := NewFallbackSearcher(&stubSearcher{}, provider, enabled)

	if _, err := searcher.Search(
		context.Background(),
		searchcore.Request{Query: "gap", Limit: 10},
	); err != nil {
		t.Fatalf("search: %v", err)
	}
	if provider.deadline {
		t.Fatal("zero provider budget added a deadline")
	}
}
