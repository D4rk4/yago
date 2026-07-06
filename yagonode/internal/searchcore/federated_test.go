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
	if s.err != nil {
		return Response{}, s.err
	}

	return s.response, nil
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
		resp.PartialFailures[0].Reason != "remote down" {
		t.Fatalf("response = %#v", resp)
	}
}

func TestFederatedSearcherUsesDefaultWindowForEmptyLimit(t *testing.T) {
	local := &fakeCoreSearcher{}
	remote := &fakeCoreSearcher{}

	_, err := NewFederatedSearcher(local, remote).Search(
		t.Context(),
		Request{Source: SourceGlobal},
	)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if local.got.Limit != DefaultPublicLimit || remote.got.Limit != DefaultPublicLimit {
		t.Fatalf("window local=%#v remote=%#v", local.got, remote.got)
	}
}

func TestFederatedSearcherReturnsLocalError(t *testing.T) {
	_, err := NewFederatedSearcher(
		&fakeCoreSearcher{err: errors.New("local failed")},
		&fakeCoreSearcher{},
	).Search(t.Context(), Request{Source: SourceGlobal, Limit: 10})
	if err == nil {
		t.Fatal("expected local error")
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

type searchFunc func(context.Context, Request) (Response, error)

func (f searchFunc) Search(ctx context.Context, req Request) (Response, error) {
	return f(ctx, req)
}
