package yagonode

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestRemoteSearchRetentionRejectsWorkAtCapacity(t *testing.T) {
	admission := make(chan struct{}, 1)
	admission <- struct{}{}
	inner := &localExactCountingSearcher{}
	response, err := (remoteSearchRetentionSearcher{
		inner: inner, admission: admission,
	}).Search(t.Context(), searchcore.Request{Query: "busy"})
	if err != nil || response.Request.Query != "busy" || len(response.Results) != 0 ||
		len(response.PartialFailures) != 1 ||
		response.PartialFailures[0].Source != searchcore.PartialFailureSourceRemoteStage ||
		response.PartialFailures[0].Reason != remoteSearchCapacityFailure || inner.calls != 0 {
		t.Fatalf("response = %#v, error = %v, calls = %d", response, err, inner.calls)
	}
}

func TestRemoteSearchRetentionReleasesCapacityAfterCompletion(t *testing.T) {
	admission := make(chan struct{}, 1)
	inner := &localExactCountingSearcher{response: searchcore.Response{
		Results: []searchcore.Result{{URL: "https://remote.example/"}},
	}}
	searcher := remoteSearchRetentionSearcher{inner: inner, admission: admission}
	for range 2 {
		response, err := searcher.Search(t.Context(), searchcore.Request{Query: "remote"})
		if err != nil || len(response.Results) != 1 || len(admission) != 0 {
			t.Fatalf("response = %#v, error = %v, retained = %d", response, err, len(admission))
		}
	}
}

func TestRemoteSearchRetentionHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	inner := &localExactCountingSearcher{}
	_, err := (remoteSearchRetentionSearcher{
		inner: inner, admission: make(chan struct{}, 1),
	}).Search(ctx, searchcore.Request{Query: "canceled"})
	if !errors.Is(err, context.Canceled) || inner.calls != 0 {
		t.Fatalf("error = %v, calls = %d", err, inner.calls)
	}
}

func TestRemoteSearchRetentionKeepsNilRemoteDisabled(t *testing.T) {
	if withRemoteSearchRetention(nil) != nil {
		t.Fatal("nil remote searcher changed")
	}
}

func TestRemoteSearchRetentionUsesProcessAdmission(t *testing.T) {
	previous := processRemoteSearchAdmission
	processRemoteSearchAdmission = make(chan struct{}, 1)
	t.Cleanup(func() { processRemoteSearchAdmission = previous })

	wrapped, ok := withRemoteSearchRetention(&localExactCountingSearcher{}).(remoteSearchRetentionSearcher)
	if !ok || cap(wrapped.admission) != 1 {
		t.Fatalf("wrapped = %T, capacity = %d", wrapped, cap(wrapped.admission))
	}
}
