package yagonode

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestInteractiveHardDeadlineKeepsQueuedCompletion(t *testing.T) {
	hardContext, cancel := context.WithCancelCause(t.Context())
	cancel(context.DeadlineExceeded)
	want := searchcore.Response{Results: []searchcore.Result{{
		URL: "https://local.example/result", Source: searchcore.SourceLocal,
	}}}
	outcomes := make(chan interactiveSearchOutcome, 1)
	outcomes <- interactiveSearchOutcome{response: want}
	response, err := completedOrExpiredInteractiveSearch(
		t.Context(),
		hardContext,
		searchcore.Request{Query: "query"},
		outcomes,
	)
	if err != nil || len(response.Results) != 1 ||
		response.Results[0].URL != want.Results[0].URL ||
		len(response.PartialFailures) != 0 {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}

func TestInteractiveHardDeadlineRefusesUnfinishedWork(t *testing.T) {
	hardContext, cancel := context.WithCancelCause(t.Context())
	cancel(context.DeadlineExceeded)
	response, err := completedOrExpiredInteractiveSearch(
		t.Context(),
		hardContext,
		searchcore.Request{Query: "query"},
		make(chan interactiveSearchOutcome),
	)
	if err != nil || len(response.Results) != 0 ||
		len(response.PartialFailures) != 1 ||
		response.PartialFailures[0] != (searchcore.PartialFailure{
			Source: interactiveSearchFailureSource,
			Reason: interactiveSearchTimeoutFailure,
		}) {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
}
