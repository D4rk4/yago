package searchcore

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeCoreSearcher struct {
	response Response
	err      error
	got      Request
}

func (s *fakeCoreSearcher) Search(_ context.Context, req Request) (Response, error) {
	s.got = req

	return s.response, s.err
}

func TestFederatedSearcherCallsLocalOnlyForLocalRequest(t *testing.T) {
	local := &fakeCoreSearcher{response: Response{Results: []Result{{URL: "local"}}}}
	remote := &fakeCoreSearcher{}

	resp, err := NewFederatedSearcher(local, remote).Search(
		t.Context(),
		Request{Source: SourceLocal, Limit: 10},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 1 || remote.got.Limit != 0 {
		t.Fatalf("response=%#v remote=%#v", resp, remote.got)
	}
}

func TestFederatedSearcherMergesDeduplicatesAndOffsets(t *testing.T) {
	local := &fakeCoreSearcher{response: Response{
		TotalResults: 2,
		Results: []Result{
			{URL: "https://local", URLHash: "same", Score: 10},
			{URL: "https://local-only", URLHash: "local-only", Score: 6},
		},
	}}
	// Remote scores arrive in [0, 1] and are calibrated by the best local
	// score (10), landing at 8 and 7 for the merge.
	remote := &fakeCoreSearcher{response: Response{
		TotalResults: 2,
		Results: []Result{
			{URL: "https://remote", URLHash: "remote", Score: 0.8},
			{URL: "https://duplicate", URLHash: "same", Score: 0.7},
		},
		PartialFailures: []PartialFailure{{Source: "peer-a", Reason: "timeout"}},
	}}

	resp, err := NewFederatedSearcher(local, remote).Search(
		t.Context(),
		Request{Source: SourceGlobal, Limit: 2, Offset: 1},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if local.got.Offset != 0 || local.got.Limit != 3 || remote.got.Limit != 3 {
		t.Fatalf("window local=%#v remote=%#v", local.got, remote.got)
	}
	if resp.TotalResults != 4 ||
		len(resp.Results) != 2 ||
		resp.Results[0].URL != "https://remote" ||
		resp.Results[1].URL != "https://local-only" ||
		len(resp.PartialFailures) != 1 {
		t.Fatalf("response = %#v", resp)
	}
}

func TestFederatedSearcherAppliesDomainConstraintsBeforeFusionAndTruncation(t *testing.T) {
	blocked := Result{
		URL: "https://blocked.example/duplicate", URLHash: "blocked", Score: 10,
	}
	allowed := Result{
		URL: "https://docs.allowed.example/result", URLHash: "allowed", Score: 5,
	}
	local := &fakeCoreSearcher{response: Response{
		TotalResults: 2,
		Results:      []Result{blocked, allowed},
	}}
	remote := &fakeCoreSearcher{response: Response{
		TotalResults: 1,
		Results:      []Result{blocked},
	}}

	response, err := NewFederatedSearcher(local, remote).Search(
		t.Context(),
		Request{
			Source: SourceGlobal, Limit: 1,
			IncludeDomains: []string{"allowed.example"},
			ExcludeDomains: []string{"blocked.example"},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if response.TotalResults != 1 || len(response.Results) != 1 ||
		response.Results[0].URL != allowed.URL {
		t.Fatalf("response = %#v", response)
	}
}

func TestFederatedSearcherReportsRemoteErrorAsPartialFailure(t *testing.T) {
	local := &fakeCoreSearcher{response: Response{Results: []Result{{URL: "local"}}}}
	remote := &fakeCoreSearcher{err: errors.New("remote down")}

	resp, err := NewFederatedSearcher(local, remote).Search(
		t.Context(),
		Request{Source: SourceGlobal, Limit: 10},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 1 ||
		len(resp.PartialFailures) != 1 ||
		resp.PartialFailures[0].Reason != federatedRemoteSearchFailed {
		t.Fatalf("response = %#v", resp)
	}
}

func TestFederatedSearcherUsesDefaultWindowForEmptyLimit(t *testing.T) {
	local := &fakeCoreSearcher{response: Response{
		Results: []Result{{URL: "https://local.example/hit"}},
	}}
	remote := &fakeCoreSearcher{response: Response{
		Results: []Result{{URL: "https://peer.example/hit"}},
	}}

	response, err := NewFederatedSearcher(local, remote).Search(
		t.Context(),
		Request{Source: SourceGlobal},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if local.got.Limit != DefaultPublicLimit || remote.got.Limit != DefaultPublicLimit {
		t.Fatalf("window local=%#v remote=%#v", local.got, remote.got)
	}
	if len(response.Results) != 2 {
		t.Fatalf("response = %#v", response)
	}
}

func TestFederatedSearcherUsesDefaultLimitForSingleBranchAnswers(t *testing.T) {
	for _, test := range []struct {
		name   string
		local  Response
		remote Response
		want   string
	}{
		{
			name:  "local",
			local: Response{Results: []Result{{URL: "https://local.example/hit"}}},
			want:  "https://local.example/hit",
		},
		{
			name:   "peer",
			remote: Response{Results: []Result{{URL: "https://peer.example/hit"}}},
			want:   "https://peer.example/hit",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			response, err := NewFederatedSearcher(
				&fakeCoreSearcher{response: test.local},
				&fakeCoreSearcher{response: test.remote},
			).Search(t.Context(), Request{Source: SourceGlobal})
			if err != nil || len(response.Results) != 1 ||
				response.Results[0].URL != test.want {
				t.Fatalf("response = %#v, error = %v", response, err)
			}
		})
	}
}

func TestFederatedSearcherReturnsHonestEmptyGlobalAnswer(t *testing.T) {
	response, err := NewFederatedSearcher(
		&fakeCoreSearcher{},
		&fakeCoreSearcher{},
	).Search(t.Context(), Request{Source: SourceGlobal, Limit: 10})
	if err != nil || len(response.Results) != 0 || len(response.PartialFailures) != 0 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestFederatedSearcherReturnsLocalError(t *testing.T) {
	response, err := NewFederatedSearcher(
		&fakeCoreSearcher{err: errors.New("local failed")},
		&fakeCoreSearcher{},
	).Search(t.Context(), Request{Source: SourceGlobal, Limit: 10})
	if !errors.Is(err, errFederatedSearchUnavailable) ||
		len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Reason != federatedLocalSearchFailed ||
		response.PartialFailures[0].Reason == "local failed" {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestFederatedSearcherKeepsPeerHitAfterLocalFailure(t *testing.T) {
	response, err := NewFederatedSearcher(
		&fakeCoreSearcher{err: errors.New("private local failure")},
		&fakeCoreSearcher{response: Response{
			Results: []Result{{URL: "https://peer.example/hit", Source: SourceRemote}},
		}},
	).Search(t.Context(), Request{Source: SourceGlobal, Limit: 10})
	if err != nil || len(response.Results) != 1 ||
		response.Results[0].URL != "https://peer.example/hit" ||
		response.TotalResults != 1 || len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Reason != federatedLocalSearchFailed {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestFederatedSearcherKeepsBothRowsAfterLocalOperationalFailure(t *testing.T) {
	response, err := NewFederatedSearcher(
		&fakeCoreSearcher{
			response: Response{Results: []Result{{URL: "https://local.example/hit"}}},
			err:      errors.New("private local failure"),
		},
		&fakeCoreSearcher{response: Response{
			Results: []Result{{URL: "https://peer.example/hit", Source: SourceRemote}},
		}},
	).Search(t.Context(), Request{Source: SourceGlobal, Limit: 10})
	if err != nil || len(response.Results) != 2 || response.TotalResults != 2 ||
		len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Reason != federatedLocalSearchFailed {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestFederatedSearcherKeepsRemoteRowsReturnedWithError(t *testing.T) {
	response, err := NewFederatedSearcher(
		&fakeCoreSearcher{},
		&fakeCoreSearcher{
			response: Response{Results: []Result{{
				URL: "https://peer.example/partial", Source: SourceRemote,
			}}},
			err: errors.New("private remote failure"),
		},
	).Search(t.Context(), Request{Source: SourceGlobal, Limit: 10})
	if err != nil || len(response.Results) != 1 || response.TotalResults != 1 ||
		len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Reason != federatedRemoteSearchFailed {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestFederatedSearcherReturnsStableFailureWhenBothBranchesFail(t *testing.T) {
	response, err := NewFederatedSearcher(
		&fakeCoreSearcher{err: errors.New("private local failure")},
		&fakeCoreSearcher{err: errors.New("private remote failure")},
	).Search(t.Context(), Request{Source: SourceGlobal, Limit: 10})
	if !errors.Is(err, errFederatedSearchUnavailable) ||
		len(response.PartialFailures) != 2 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
	for _, failure := range response.PartialFailures {
		if failure.Reason == "private local failure" ||
			failure.Reason == "private remote failure" {
			t.Fatalf("private failure escaped: %#v", response.PartialFailures)
		}
	}
}

func TestResultIdentityFallsBackToURL(t *testing.T) {
	if got := resultIdentity(Result{URL: "https://example.org"}); got != "url:https://example.org" {
		t.Fatalf("identity = %q", got)
	}
}

func TestOffsetResultsEmptyWindow(t *testing.T) {
	if got := offsetResults([]Result{{URL: "a"}}, 2, 1); got != nil {
		t.Fatalf("results = %#v", got)
	}
}

// TestFederatedSearchRunsBranchesConcurrently proves PERF-02: the local and
// remote branches overlap in time — each branch blocks until the other has
// started, which deadlocks (and times out) under sequential execution.
func TestFederatedSearchRunsBranchesConcurrently(t *testing.T) {
	localStarted := make(chan struct{})
	remoteStarted := make(chan struct{})
	barrier := func(mine, other chan struct{}) error {
		close(mine)
		select {
		case <-other:
			return nil
		case <-time.After(5 * time.Second):
			return errors.New("fan-out is sequential")
		}
	}
	local := searchFunc(func(context.Context, Request) (Response, error) {
		if err := barrier(localStarted, remoteStarted); err != nil {
			return Response{}, err
		}

		return Response{Results: []Result{{URL: "https://local.example/"}}}, nil
	})
	remote := searchFunc(func(context.Context, Request) (Response, error) {
		if err := barrier(remoteStarted, localStarted); err != nil {
			return Response{}, err
		}

		return Response{Results: []Result{{URL: "https://remote.example/"}}}, nil
	})

	resp, err := NewFederatedSearcher(local, remote).Search(
		context.Background(),
		Request{Source: SourceGlobal, Limit: 10},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("results = %d, want both branches merged", len(resp.Results))
	}
}

func TestFederatedSearchKeepsCompletedLocalResultsAtRemoteDeadline(t *testing.T) {
	remoteStarted := make(chan struct{})
	releaseRemote := make(chan struct{})
	remoteFinished := make(chan struct{})
	local := searchFunc(func(context.Context, Request) (Response, error) {
		return Response{
			TotalResults: 2,
			Results: []Result{
				{URL: "https://local.example/first"},
				{URL: "https://local.example/second"},
			},
			PartialFailures: []PartialFailure{{Source: "local", Reason: "degraded"}},
			Facets:          []FacetGroup{{Name: "host"}},
		}, nil
	})
	remote := searchFunc(func(context.Context, Request) (Response, error) {
		close(remoteStarted)
		<-releaseRemote
		close(remoteFinished)

		return Response{}, nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	response, err := NewFederatedSearcher(local, remote).Search(
		ctx,
		Request{Source: SourceGlobal, Offset: 1, Limit: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results) != 1 ||
		response.Results[0].URL != "https://local.example/second" ||
		response.TotalResults != 2 ||
		len(response.PartialFailures) != 2 ||
		response.PartialFailures[1].Source != PartialFailureSourceRemoteYaCy ||
		len(response.Facets) != 1 {
		t.Fatalf("response = %#v", response)
	}
	select {
	case <-remoteStarted:
	default:
		t.Fatal("remote search did not start")
	}
	close(releaseRemote)
	select {
	case <-remoteFinished:
	case <-time.After(time.Second):
		t.Fatal("remote search did not finish")
	}
}

func TestFederatedSearchPreservesParentCancellation(t *testing.T) {
	remoteStarted := make(chan struct{})
	releaseRemote := make(chan struct{})
	remote := searchFunc(func(context.Context, Request) (Response, error) {
		close(remoteStarted)
		<-releaseRemote

		return Response{}, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := NewFederatedSearcher(&fakeCoreSearcher{}, remote).Search(
			ctx,
			Request{Source: SourceGlobal, Limit: 1},
		)
		done <- err
	}()
	<-remoteStarted
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	close(releaseRemote)
}

func TestDrainRemoteOutcomeKeepsQueuedCompletion(t *testing.T) {
	outcomes := make(chan searchOutcome, 1)
	want := searchOutcome{resp: Response{Results: []Result{{URL: "https://peer.example/"}}}}
	outcomes <- want
	got, ready := remoteOutcomeAfterCancellation(outcomes, Response{Results: []Result{{
		URL: "https://local.example/",
	}}})
	if !ready || len(got.resp.Results) != 1 ||
		got.resp.Results[0].URL != want.resp.Results[0].URL {
		t.Fatalf("outcome = %#v, ready = %v", got, ready)
	}
	if got, ready = availableRemoteOutcome(outcomes); ready || len(got.resp.Results) != 0 {
		t.Fatalf("empty outcome = %#v, ready = %v", got, ready)
	}
	outcomes <- want
	if got, ready = drainRemoteOutcome(outcomes); !ready ||
		len(got.resp.Results) != 1 || got.resp.Results[0].URL != want.resp.Results[0].URL {
		t.Fatalf("drained outcome = %#v, ready = %v", got, ready)
	}
}

func TestFederatedSearchKeepsPeerCompletionAtCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	peerCompleted := make(chan struct{})
	remote := searchFunc(func(context.Context, Request) (Response, error) {
		close(peerCompleted)

		return Response{Results: []Result{{URL: "https://peer.example/hit"}}}, nil
	})
	local := searchFunc(func(ctx context.Context, _ Request) (Response, error) {
		<-ctx.Done()

		return Response{}, ctx.Err()
	})
	outcomes := make(chan searchOutcome, 1)
	go func() {
		response, err := NewFederatedSearcher(local, remote).Search(
			ctx,
			Request{Source: SourceGlobal, Limit: 10},
		)
		outcomes <- searchOutcome{resp: response, err: err}
	}()
	<-peerCompleted
	cancel()
	outcome := <-outcomes
	if outcome.err != nil || len(outcome.resp.Results) != 1 ||
		outcome.resp.Results[0].URL != "https://peer.example/hit" {
		t.Fatalf("response = %#v, error = %v", outcome.resp, outcome.err)
	}
}

func TestFederatedSearchCancellationDrainRemainsBounded(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	releaseRemote := make(chan struct{})
	defer close(releaseRemote)
	remote := searchFunc(func(context.Context, Request) (Response, error) {
		<-releaseRemote

		return Response{}, nil
	})
	started := time.Now()
	response, err := NewFederatedSearcher(&fakeCoreSearcher{}, remote).Search(
		ctx,
		Request{Source: SourceGlobal, Limit: 10},
	)
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		t.Fatalf("elapsed = %s", elapsed)
	}
	if !errors.Is(err, context.Canceled) || len(response.Results) != 0 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

type searchFunc func(context.Context, Request) (Response, error)

func (f searchFunc) Search(ctx context.Context, req Request) (Response, error) {
	return f(ctx, req)
}
