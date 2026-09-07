package websearch

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestSupplementSkipsCachedAndEngineDuplicates(t *testing.T) {
	var requests atomic.Int32
	provider := NewDDGSProvider(DDGSConfig{
		Client: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			requests.Add(1)
			return htmlResponse(http.StatusOK, "answer"), nil
		})}, CacheTTL: time.Minute,
	})
	duplicate := Result{Title: "Needle reference", URL: "https://primary.example:443/page#fragment"}
	novel := Result{Title: "Needle reference", URL: "https://web.example/new"}
	provider.engines = []engine{tracingEngine("first", duplicate), tracingEngine("second", novel)}
	request := searchcore.Request{Query: "needle", Limit: 10}
	query := newProviderQueryForRequest(request)
	provider.cache.put(query.cacheIdentity, []Result{duplicate})
	primary := &stubSearcher{
		resp: searchcore.Response{
			Results: []searchcore.Result{{URL: "https://primary.example/page"}},
		},
	}
	search := NewFallbackSearcher(primary, provider, enabled)
	response, err := search.Search(t.Context(), request)
	if err != nil || len(response.Results) != 2 || requests.Load() != 2 {
		t.Fatalf("response=%+v requests=%d error=%v", response, requests.Load(), err)
	}
	if _, err := search.Search(t.Context(), request); err != nil || requests.Load() != 2 {
		t.Fatalf("cached requests=%d error=%v", requests.Load(), err)
	}
}

func TestParallelEnginesContinueAfterPrimaryDuplicates(t *testing.T) {
	var calls atomic.Int32
	provider := NewDDGSProvider(
		DDGSConfig{
			Client: &http.Client{
				Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls.Add(1)
					return htmlResponse(http.StatusOK, "answer"), nil
				}),
			},
		},
	)
	provider.engines = []engine{
		tracingEngine(
			"first",
			Result{Title: "Needle", URL: "https://primary.example:443/page#same"},
		),
		tracingEngine("second", Result{Title: "Needle", URL: "https://web.example/new"}),
	}
	primary := &stubSearcher{
		resp: searchcore.Response{
			Results: []searchcore.Result{{URL: "https://primary.example/page"}},
		},
	}
	response, err := NewParallelSearcher(
		primary,
		provider,
		enabled,
	).Search(t.Context(), searchcore.Request{Query: "needle", Limit: 10})
	if err != nil || calls.Load() != 2 || len(response.Results) != 2 {
		t.Fatalf("response=%+v calls=%d error=%v", response, calls.Load(), err)
	}
}

func TestSupplementRetainsRecoveryAfterHonestEmptyAndProviderFailure(t *testing.T) {
	for _, providerError := range []error{nil, errors.New("unavailable")} {
		search := NewFallbackSearcher(
			&stubSearcher{},
			&stubProvider{err: providerError},
			enabled,
			WithRecovery(
				func(_ context.Context, _ searchcore.Request, response searchcore.Response) (searchcore.Response, error) {
					response.Results = []searchcore.Result{
						{URL: "https://primary.example/recovered"},
					}
					return response, nil
				},
			),
		)
		response, err := search.Search(t.Context(), searchcore.Request{Query: "needle"})
		if err != nil || len(response.Results) != 1 ||
			response.Results[0].Source == searchcore.SourceWeb ||
			(len(response.PartialFailures) > 0) != (providerError != nil) {
			t.Fatalf("response=%+v error=%v", response, err)
		}
	}
}

func TestSupplementRecoveryFailureRemainsIncomplete(t *testing.T) {
	search := NewFallbackSearcher(&stubSearcher{}, &stubProvider{}, enabled, WithRecovery(
		func(_ context.Context, _ searchcore.Request, response searchcore.Response) (searchcore.Response, error) {
			return response, errors.New("recovery unavailable")
		},
	))
	response, err := search.Search(t.Context(), searchcore.Request{Query: "needle"})
	if err != nil || len(response.Results) != 0 || response.LostSourceFailures() != 1 {
		t.Fatalf("response=%+v error=%v", response, err)
	}
}

func TestParallelSupplementCancelsOptionalRecovery(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	recoverResults := func(ctx context.Context, _ searchcore.Request, response searchcore.Response) (searchcore.Response, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return response, ctx.Err()
	}
	search := NewParallelSearcher(
		&stubSearcher{},
		&recoveryBarrierProvider{started: started},
		enabled,
		WithRecovery(recoverResults),
	)
	response, err := search.Search(t.Context(), searchcore.Request{Query: "needle", Limit: 10})
	if err != nil || len(response.Results) != 1 {
		t.Fatalf("response=%+v error=%v", response, err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("recovery was not canceled")
	}
}

func TestSupplementPreservesRecoveryAndProviderPanics(t *testing.T) {
	for _, recoveryFailure := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "provider", true: "recovery"}[recoveryFailure],
			func(t *testing.T) {
				search := NewFallbackSearcher(
					&stubSearcher{},
					panicSupplementProvider{},
					enabled,
					WithRecovery(
						func(_ context.Context, _ searchcore.Request, response searchcore.Response) (searchcore.Response, error) {
							if recoveryFailure {
								panic("recovery")
							}
							return response, nil
						},
					),
				)
				if recoveryFailure {
					search.provider = &stubProvider{}
				}
				defer func() {
					if recover() == nil {
						t.Fatal("panic was lost")
					}
				}()
				_, _ = search.Search(t.Context(), searchcore.Request{Query: "needle"})
			},
		)
	}
}

type panicSupplementProvider struct{}

func (panicSupplementProvider) Search(context.Context, string, int) ([]Result, error) {
	panic("provider")
}
