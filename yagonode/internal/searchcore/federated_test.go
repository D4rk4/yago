package searchcore

import (
	"context"
	"errors"
	"testing"
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

func TestCalibratedRemoteResultsScaleToLocalScores(t *testing.T) {
	local := []Result{{Score: 1.6}, {Score: 0.4}}
	remote := []Result{{URL: "https://a", Score: 1}, {URL: "https://b", Score: 0.5}}

	calibrated := calibratedRemoteResults(local, remote)

	if calibrated[0].Score != 1.6 || calibrated[1].Score != 0.8 {
		t.Fatalf("calibrated = %#v", calibrated)
	}
	if remote[0].Score != 1 {
		t.Fatalf("input mutated: %#v", remote)
	}
}

func TestCalibratedRemoteResultsKeepRemoteScaleWithoutLocalScores(t *testing.T) {
	remote := []Result{{URL: "https://a", Score: 0.9}}
	for _, item := range []struct {
		name  string
		local []Result
	}{
		{name: "no local results", local: nil},
		{name: "zero local scores", local: []Result{{Score: 0}}},
	} {
		calibrated := calibratedRemoteResults(item.local, remote)
		if len(calibrated) != 1 || calibrated[0].Score != 0.9 {
			t.Fatalf("%s: calibrated = %#v", item.name, calibrated)
		}
	}
	if got := calibratedRemoteResults([]Result{{Score: 2}}, nil); len(got) != 0 {
		t.Fatalf("empty remote calibrated = %#v", got)
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
