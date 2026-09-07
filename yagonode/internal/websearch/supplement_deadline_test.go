package websearch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type reservedProvider struct{ remaining time.Duration }

func (p *reservedProvider) Search(ctx context.Context, _ string, _ int) ([]Result, error) {
	deadline, _ := ctx.Deadline()
	p.remaining = time.Until(deadline)
	return nil, nil
}

func TestSupplementReservesAssemblyTimeInsideCallerDeadline(t *testing.T) {
	provider := &reservedProvider{}
	search := NewFallbackSearcher(
		&stubSearcher{},
		provider,
		enabled,
		WithProviderBudget(time.Second),
		WithProviderReserve(200*time.Millisecond),
	)
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	if _, err := search.Search(ctx, searchcore.Request{Query: "needle"}); err != nil {
		t.Fatal(err)
	}
	if provider.remaining <= 0 || provider.remaining > 300*time.Millisecond {
		t.Fatalf("provider remaining=%s", provider.remaining)
	}
}

type heldSupplementProvider struct {
	release  <-chan struct{}
	finished chan<- struct{}
}

func (p heldSupplementProvider) Search(context.Context, string, int) ([]Result, error) {
	<-p.release
	close(p.finished)
	return nil, nil
}

func TestSupplementReturnsAtDeadlineWhenProviderIsUncooperative(t *testing.T) {
	release, finished := make(chan struct{}), make(chan struct{})
	search := NewFallbackSearcher(
		&stubSearcher{},
		heldSupplementProvider{release, finished},
		enabled,
	)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer cancel()
	_, err := search.Search(ctx, searchcore.Request{Query: "needle"})
	close(release)
	<-finished
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error=%v", err)
	}
}

func TestParallelRecoveryKeepsPrimaryOrRecoversAnEmptyAnswer(t *testing.T) {
	for _, populated := range []bool{false, true} {
		primary := &stubSearcher{}
		if populated {
			primary.resp.Results = []searchcore.Result{{URL: "https://primary.example/page"}}
		}
		search := NewParallelSearcher(primary, &stubProvider{}, enabled, WithRecovery(
			func(_ context.Context, _ searchcore.Request, response searchcore.Response) (searchcore.Response, error) {
				if populated {
					t.Error("populated primary was retried")
				}
				response.Results = []searchcore.Result{{URL: "https://primary.example/recovered"}}
				return response, nil
			},
		))
		response, err := search.Search(t.Context(), searchcore.Request{Query: "needle"})
		if err != nil || len(response.Results) != 1 {
			t.Fatalf("response=%+v error=%v", response, err)
		}
	}
}

func TestParallelRecoveryDrainsCompletedWebAtCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	search := NewParallelSearcher(&stubSearcher{}, &stubProvider{}, enabled, WithRecovery(
		func(context.Context, searchcore.Request, searchcore.Response) (searchcore.Response, error) {
			panic("unexpected recovery")
		},
	))
	provider := make(chan parallelProviderOutcome, 1)
	provider <- parallelProviderOutcome{results: []Result{{URL: "https://web.example/page"}}}
	outcomes := search.collectSearch(
		ctx,
		searchcore.Request{},
		make(chan parallelPrimaryOutcome),
		provider,
	)
	if outcomes.primaryReady || !outcomes.providerReady || len(outcomes.provider.results) != 1 {
		t.Fatalf("outcomes=%+v", outcomes)
	}
}

func TestSupplementKeepsBothQueuedCompletions(t *testing.T) {
	for range 32 {
		primary := make(chan parallelPrimaryOutcome, 1)
		web := make(chan parallelProviderOutcome, 1)
		primary <- parallelPrimaryOutcome{response: searchcore.Response{Results: []searchcore.Result{{URL: "https://primary.example/ready"}}}}
		web <- parallelProviderOutcome{results: []Result{{URL: "https://web.example/ready"}}}
		outcomes := collectSupplementOutcomes(t.Context(), primary, web)
		if !outcomes.primaryReady || !outcomes.providerReady ||
			len(outcomes.primary.response.Results) != 1 {
			t.Fatalf("outcomes=%+v", outcomes)
		}
	}
}
