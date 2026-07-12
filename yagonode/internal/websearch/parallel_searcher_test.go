package websearch

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type parallelBarrierSearcher struct {
	started         chan struct{}
	providerStarted chan struct{}
	response        searchcore.Response
}

func (s parallelBarrierSearcher) Search(
	ctx context.Context,
	_ searchcore.Request,
) (searchcore.Response, error) {
	close(s.started)
	select {
	case <-s.providerStarted:
		return s.response, nil
	case <-ctx.Done():
		return searchcore.Response{}, fmt.Errorf("primary barrier: %w", ctx.Err())
	}
}

type parallelBarrierProvider struct {
	started        chan struct{}
	primaryStarted chan struct{}
	results        []Result
}

func (p parallelBarrierProvider) Search(
	ctx context.Context,
	_ string,
	_ int,
) ([]Result, error) {
	close(p.started)
	select {
	case <-p.primaryStarted:
		return p.results, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("provider barrier: %w", ctx.Err())
	}
}

func TestParallelSearcherStartsBothBranchesAndFusesTheirRankings(t *testing.T) {
	primaryStarted := make(chan struct{})
	providerStarted := make(chan struct{})
	sharedURL := "https://shared.example/gap"
	primary := parallelBarrierSearcher{
		started: primaryStarted, providerStarted: providerStarted,
		response: searchcore.Response{
			TotalResults: 2,
			Results: []searchcore.Result{
				{
					Title: "Local gap", URL: "https://local.example/gap",
					Source: searchcore.SourceLocal,
				},
				{
					Title: "Shared local gap", URL: sharedURL, URLHash: "shared-hash",
					Source: searchcore.SourceLocal,
				},
			},
			PartialFailures: []searchcore.PartialFailure{{Source: "remote-yacy", Reason: "late"}},
			Facets:          []searchcore.FacetGroup{{Name: "host"}},
		},
	}
	provider := parallelBarrierProvider{
		started: providerStarted, primaryStarted: primaryStarted,
		results: []Result{
			{Title: "Shared web gap", URL: sharedURL},
			{Title: "Web gap", URL: "https://web.example/gap"},
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	response, err := NewParallelSearcher(primary, provider, enabled).Search(
		ctx,
		searchcore.Request{Query: "gap", Limit: 10},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 3 || response.TotalResults != 3 {
		t.Fatalf("response = %#v", response)
	}
	if len(response.PartialFailures) != 1 || len(response.Facets) != 1 {
		t.Fatalf("primary metadata = %#v", response)
	}
	webRows := 0
	for _, result := range response.Results {
		if result.Source != searchcore.SourceWeb {
			continue
		}
		webRows++
		if _, known := result.Evidence.Value(searchcore.SignalLocalRank); known {
			t.Fatalf("web result has local-rank evidence: %#v", result)
		}
	}
	if webRows != 1 {
		t.Fatalf("web rows = %d, response = %#v", webRows, response.Results)
	}
}

type parallelCancellationProvider struct {
	canceled chan struct{}
}

func (p parallelCancellationProvider) Search(
	ctx context.Context,
	_ string,
	_ int,
) ([]Result, error) {
	<-ctx.Done()
	close(p.canceled)

	return nil, fmt.Errorf("provider cancellation: %w", ctx.Err())
}

func TestParallelSearcherCancelsProviderAfterPrimaryFailure(t *testing.T) {
	want := errors.New("primary failed")
	provider := parallelCancellationProvider{canceled: make(chan struct{})}
	_, err := NewParallelSearcher(
		&stubSearcher{err: want},
		provider,
		enabled,
	).Search(t.Context(), searchcore.Request{Query: "gap", Limit: 10})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
	select {
	case <-provider.canceled:
	case <-time.After(time.Second):
		t.Fatal("provider did not observe cancellation")
	}
}

func TestParallelSearcherProviderFailureKeepsPrimaryAnswer(t *testing.T) {
	primary := &stubSearcher{resp: searchcore.Response{
		TotalResults: 1,
		Results:      []searchcore.Result{{Title: "Local gap", URL: "https://local.example/gap"}},
	}}
	response, err := NewParallelSearcher(
		primary,
		&stubProvider{err: errors.New("provider failed")},
		enabled,
	).Search(t.Context(), searchcore.Request{Query: "gap", Limit: 10})
	if err != nil || len(response.Results) != 1 || response.Results[0].Title != "Local gap" {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

type parallelPanicSearcher struct {
	failure any
}

func (s parallelPanicSearcher) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	panic(s.failure)
}

type parallelPanicProvider struct {
	failure any
}

func (p parallelPanicProvider) Search(context.Context, string, int) ([]Result, error) {
	panic(p.failure)
}

func TestParallelSearcherForwardsBranchPanics(t *testing.T) {
	for _, test := range []struct {
		name     string
		primary  searchcore.Searcher
		provider Provider
	}{
		{
			name: "primary", primary: parallelPanicSearcher{failure: "primary panic"},
			provider: &stubProvider{},
		},
		{
			name: "provider", primary: &stubSearcher{},
			provider: parallelPanicProvider{failure: "provider panic"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("branch panic was not forwarded")
				}
			}()
			_, _ = NewParallelSearcher(test.primary, test.provider, enabled).Search(
				t.Context(),
				searchcore.Request{Query: "gap", Limit: 10},
			)
		})
	}
}

func TestParallelSearcherSeedsCapsAndKeepsExistingIdentity(t *testing.T) {
	seeder := &stubSeeder{done: make(chan struct{})}
	primary := &stubSearcher{resp: searchcore.Response{
		TotalResults: 2,
		Results: []searchcore.Result{
			{Title: "First gap", URL: "https://first.example/gap", URLHash: "existing"},
			{Title: "Second gap", URL: "https://second.example/gap"},
		},
	}}
	provider := &stubProvider{results: []Result{
		{Title: "Third gap", URL: "https://third.example/gap"},
		{Title: "Fourth gap", URL: "https://fourth.example/gap"},
	}}
	response, err := NewParallelSearcher(
		primary,
		provider,
		enabled,
		WithSeeder(seeder),
	).Search(t.Context(), searchcore.Request{Query: "gap", Limit: 2})
	if err != nil || len(response.Results) != 2 || response.TotalResults != 4 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
	select {
	case <-seeder.done:
	case <-time.After(time.Second):
		t.Fatal("seeder did not run")
	}
}

func TestParallelSearcherKeepsPrimaryWhenWebRowsFailVerification(t *testing.T) {
	primary := &stubSearcher{resp: searchcore.Response{
		Results: []searchcore.Result{{Title: "Local gap", URL: "https://local.example/gap"}},
	}}
	response, err := NewParallelSearcher(
		primary,
		&stubProvider{results: []Result{{Title: "Unrelated", URL: "https://web.example/"}}},
		enabled,
	).Search(t.Context(), searchcore.Request{Query: "gap", Limit: 10})
	if err != nil || len(response.Results) != 1 || response.Results[0].Title != "Local gap" {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestParallelSearcherKeepsEgressGuards(t *testing.T) {
	tests := []struct {
		name    string
		permit  func(searchcore.Request) bool
		request searchcore.Request
	}{
		{name: "privacy", permit: disabled, request: searchcore.Request{Query: "gap"}},
		{
			name: "local", permit: enabled,
			request: searchcore.Request{Query: "gap", Source: searchcore.SourceLocal},
		},
		{name: "blank", permit: enabled, request: searchcore.Request{Query: "  "}},
		{
			name: "image", permit: enabled,
			request: searchcore.Request{
				Query: "gap", ContentDomain: searchcore.ContentDomainImage,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			provider := &stubProvider{results: []Result{{Title: "Web gap"}}}
			if _, err := NewParallelSearcher(&stubSearcher{}, provider, test.permit).Search(
				t.Context(),
				test.request,
			); err != nil {
				t.Fatal(err)
			}
			if provider.calls != 0 {
				t.Fatalf("provider calls = %d", provider.calls)
			}
		})
	}
}

func TestParallelSearcherWrapsIneligiblePrimaryError(t *testing.T) {
	want := errors.New("primary failed")
	_, err := NewParallelSearcher(
		&stubSearcher{err: want},
		&stubProvider{},
		disabled,
	).Search(t.Context(), searchcore.Request{Query: "gap"})
	if !errors.Is(err, want) {
		t.Fatalf("error = %v", err)
	}
}
