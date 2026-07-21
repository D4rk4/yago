package websearch

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
			PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceRemoteYaCy,
				Reason: "late",
			}},
			Facets: []searchcore.FacetGroup{{Name: "host"}},
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

func TestParallelSearcherKeepsVerifiedWebAnswerAfterPrimaryFailure(t *testing.T) {
	response, err := NewParallelSearcher(
		&stubSearcher{err: errors.New("private primary detail")},
		&stubProvider{results: []Result{{
			Title: "Web gap", URL: "https://web.example/gap",
		}}},
		enabled,
	).Search(t.Context(), searchcore.Request{Query: "gap", Limit: 10})
	if err != nil || len(response.Results) != 1 ||
		response.Results[0].Source != searchcore.SourceWeb || response.TotalResults != 1 ||
		len(response.PartialFailures) != 1 ||
		response.PartialFailures[0] != (searchcore.PartialFailure{
			Source: searchcore.PartialFailureSourceLocalSearch,
			Reason: msgParallelPrimaryFailed,
		}) {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestParallelSearcherKeepsPrimaryRowsAfterOperationalFailure(t *testing.T) {
	response, err := NewParallelSearcher(
		&stubSearcher{
			resp: searchcore.Response{
				Results: []searchcore.Result{{
					Title: "Local gap", URL: "https://local.example/gap",
				}},
				TotalResults: 1,
			},
			err: errors.New("private primary detail"),
		},
		&stubProvider{err: errors.New("private provider detail")},
		enabled,
	).Search(t.Context(), searchcore.Request{Query: "gap", Limit: 10})
	if err != nil || len(response.Results) != 1 || response.TotalResults != 1 ||
		len(response.PartialFailures) != 2 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
	for _, failure := range response.PartialFailures {
		if strings.Contains(failure.Reason, "private") {
			t.Fatalf("private failure escaped: %#v", response.PartialFailures)
		}
	}
}

func TestParallelSearcherReturnsStableFailureWhenBothBranchesFail(t *testing.T) {
	response, err := NewParallelSearcher(
		&stubSearcher{err: errors.New("private primary detail")},
		&stubProvider{err: errors.New("private provider detail")},
		enabled,
	).Search(t.Context(), searchcore.Request{Query: "gap", Limit: 10})
	if !errors.Is(err, errParallelSearchUnavailable) ||
		strings.Contains(err.Error(), "private") || len(response.PartialFailures) != 2 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

type parallelCompletedProvider struct {
	completed chan struct{}
	results   []Result
}

func (p parallelCompletedProvider) Search(
	context.Context,
	string,
	int,
) ([]Result, error) {
	close(p.completed)

	return p.results, nil
}

type parallelCanceledPrimary struct{}

func (parallelCanceledPrimary) Search(
	ctx context.Context,
	_ searchcore.Request,
) (searchcore.Response, error) {
	<-ctx.Done()

	return searchcore.Response{}, fmt.Errorf("primary cancellation: %w", ctx.Err())
}

func TestParallelSearcherDeadlineCannotReplaceCompletedWebBranch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	completed := make(chan struct{})
	outcome := make(chan parallelPrimaryOutcome, 1)
	go func() {
		response, err := NewParallelSearcher(
			parallelCanceledPrimary{},
			parallelCompletedProvider{
				completed: completed,
				results:   []Result{{Title: "Web gap", URL: "https://web.example/gap"}},
			},
			enabled,
		).Search(ctx, searchcore.Request{Query: "gap", Limit: 10})
		outcome <- parallelPrimaryOutcome{response: response, err: err}
	}()
	<-completed
	cancel()
	result := <-outcome
	if result.err != nil || len(result.response.Results) != 1 ||
		result.response.Results[0].Source != searchcore.SourceWeb {
		t.Fatalf("response = %#v, error = %v", result.response, result.err)
	}
}

type parallelCompletedPrimary struct {
	completed chan struct{}
}

func (p parallelCompletedPrimary) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	close(p.completed)

	return searchcore.Response{
		Results: []searchcore.Result{{
			Title: "Local gap", URL: "https://local.example/gap",
		}},
		TotalResults: 1,
	}, nil
}

type parallelCanceledProvider struct{}

func (parallelCanceledProvider) Search(
	ctx context.Context,
	_ string,
	_ int,
) ([]Result, error) {
	<-ctx.Done()

	return nil, fmt.Errorf("provider cancellation: %w", ctx.Err())
}

func TestParallelSearcherDeadlineCannotReplaceCompletedPrimaryBranch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	completed := make(chan struct{})
	outcome := make(chan parallelPrimaryOutcome, 1)
	go func() {
		response, err := NewParallelSearcher(
			parallelCompletedPrimary{completed: completed},
			parallelCanceledProvider{},
			enabled,
		).Search(ctx, searchcore.Request{Query: "gap", Limit: 10})
		outcome <- parallelPrimaryOutcome{response: response, err: err}
	}()
	<-completed
	cancel()
	result := <-outcome
	if result.err != nil || len(result.response.Results) != 1 ||
		result.response.Results[0].URL != "https://local.example/gap" {
		t.Fatalf("response = %#v, error = %v", result.response, result.err)
	}
}

func TestParallelSearcherReturnsContextCauseWithoutCompletedBranch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	response, err := NewParallelSearcher(
		parallelCanceledPrimary{},
		parallelCanceledProvider{},
		enabled,
	).Search(ctx, searchcore.Request{Query: "gap", Limit: 10})
	if !errors.Is(err, context.Canceled) || len(response.Results) != 0 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestParallelSearcherReturnsHonestEmptyAnswer(t *testing.T) {
	response, err := NewParallelSearcher(
		&stubSearcher{},
		&stubProvider{},
		enabled,
	).Search(t.Context(), searchcore.Request{Query: "gap", Limit: 10})
	if err != nil || len(response.Results) != 0 || len(response.PartialFailures) != 0 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestDrainParallelOutcomesKeepsQueuedCompletions(t *testing.T) {
	primaryOutcomes := make(chan parallelPrimaryOutcome, 1)
	providerOutcomes := make(chan parallelProviderOutcome, 1)
	primaryOutcomes <- parallelPrimaryOutcome{err: errors.New("primary")}
	providerOutcomes <- parallelProviderOutcome{err: errors.New("provider")}
	outcomes := drainParallelOutcomes(
		parallelOutcomes{},
		primaryOutcomes,
		providerOutcomes,
	)
	if !outcomes.primaryReady || !outcomes.providerReady ||
		outcomes.primary.err == nil || outcomes.provider.err == nil {
		t.Fatalf(
			"primary = %#v/%v, provider = %#v/%v",
			outcomes.primary,
			outcomes.primaryReady,
			outcomes.provider,
			outcomes.providerReady,
		)
	}
	outcomes = drainParallelOutcomes(
		parallelOutcomes{},
		primaryOutcomes,
		providerOutcomes,
	)
	if outcomes.primaryReady || outcomes.providerReady {
		t.Fatalf("empty drain = %v/%v", outcomes.primaryReady, outcomes.providerReady)
	}
}

func TestCollectParallelOutcomesKeepsCompletionQueuedBeforeCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	primaryOutcomes := make(chan parallelPrimaryOutcome, 1)
	providerOutcomes := make(chan parallelProviderOutcome)
	primaryOutcomes <- parallelPrimaryOutcome{response: searchcore.Response{
		Results: []searchcore.Result{{Title: "Local gap"}},
	}}
	cancel()
	outcomes := collectParallelOutcomes(ctx, primaryOutcomes, providerOutcomes)
	if !outcomes.primaryReady || len(outcomes.primary.response.Results) != 1 ||
		outcomes.providerReady {
		t.Fatalf("outcomes = %#v", outcomes)
	}
}

type parallelBlockedPrimary struct {
	release <-chan struct{}
}

func (p parallelBlockedPrimary) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	<-p.release

	return searchcore.Response{}, nil
}

type parallelBlockedProvider struct {
	release <-chan struct{}
}

func (p parallelBlockedProvider) Search(
	context.Context,
	string,
	int,
) ([]Result, error) {
	<-p.release

	return nil, nil
}

func TestParallelSearcherCancellationDrainRemainsBounded(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	release := make(chan struct{})
	defer close(release)
	started := time.Now()
	response, err := NewParallelSearcher(
		parallelBlockedPrimary{release: release},
		parallelBlockedProvider{release: release},
		enabled,
	).Search(ctx, searchcore.Request{Query: "gap", Limit: 10})
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("elapsed = %s", elapsed)
	}
	if !errors.Is(err, context.Canceled) || len(response.Results) != 0 {
		t.Fatalf("response = %#v, error = %v", response, err)
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
	if err != nil || len(response.Results) != 1 || response.Results[0].Title != "Local gap" ||
		len(response.PartialFailures) != 1 || response.PartialFailures[0] != webProviderFailure() {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestParallelSearcherKeepsAndSeedsVerifiedPartialProviderRows(t *testing.T) {
	seeder := &stubSeeder{}
	searcher := NewParallelSearcher(
		&stubSearcher{},
		&stubProvider{
			results: []Result{{Title: "Web gap", URL: "https://web.example/gap"}},
			err:     errors.New("provider failed after rows"),
		},
		enabled,
		WithSeeder(seeder),
	)
	searcher.fallback.spawnSeedWork = func(
		_ string,
		ctx context.Context,
		work func(context.Context),
	) bool {
		work(ctx)

		return true
	}
	response, err := searcher.Search(
		t.Context(),
		searchcore.Request{Query: "gap", Limit: 10},
	)
	if err != nil || len(response.Results) != 1 ||
		response.Results[0].Source != searchcore.SourceWeb ||
		len(response.PartialFailures) != 1 || seeder.calls != 1 ||
		len(seeder.urls) != 1 || seeder.urls[0] != "https://web.example/gap" {
		t.Fatalf("response = %#v, seeder = %#v, error = %v", response, seeder, err)
	}
}

func TestParallelSearcherRejectsAndDoesNotSeedUnverifiedPartialProviderRows(t *testing.T) {
	seeder := &stubSeeder{}
	searcher := NewParallelSearcher(
		&stubSearcher{},
		&stubProvider{
			results: []Result{{Title: "Unrelated", URL: "https://web.example/other"}},
			err:     errors.New("provider failed after rows"),
		},
		enabled,
		WithSeeder(seeder),
	)
	searcher.fallback.spawnSeedWork = func(
		_ string,
		ctx context.Context,
		work func(context.Context),
	) bool {
		work(ctx)

		return true
	}
	response, err := searcher.Search(
		t.Context(),
		searchcore.Request{Query: "gap", Limit: 10},
	)
	if err != nil || len(response.Results) != 0 ||
		len(response.PartialFailures) != 1 || seeder.calls != 0 {
		t.Fatalf("response = %#v, seeder = %#v, error = %v", response, seeder, err)
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
