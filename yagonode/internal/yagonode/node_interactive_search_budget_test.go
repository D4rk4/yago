package yagonode

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type deadlineSearch struct {
	cause error
}

func (s *deadlineSearch) Search(
	ctx context.Context,
	_ searchcore.Request,
) (searchcore.Response, error) {
	<-ctx.Done()
	s.cause = context.Cause(ctx)

	return searchcore.Response{}, fmt.Errorf("deadline search: %w", ctx.Err())
}

func TestInteractiveSearchBudgetCancelsSlowPipeline(t *testing.T) {
	previous := interactiveSearchBudget
	interactiveSearchBudget = 10 * time.Millisecond
	t.Cleanup(func() { interactiveSearchBudget = previous })

	inner := &deadlineSearch{}
	_, err := withInteractiveSearchBudget(inner).Search(t.Context(), searchcore.Request{})
	if !errors.Is(err, context.DeadlineExceeded) ||
		!errors.Is(inner.cause, context.DeadlineExceeded) {
		t.Fatalf("deadline = error %v cause %v", err, inner.cause)
	}
}

func TestInteractiveSearchBudgetKeepsCompletedLocalResults(t *testing.T) {
	previous := interactiveSearchBudget
	interactiveSearchBudget = 10 * time.Millisecond
	t.Cleanup(func() { interactiveSearchBudget = previous })

	local := staticSearcher{resp: searchcore.Response{Results: []searchcore.Result{{
		Title: "local", URL: "https://local.example/",
	}}}}
	response, err := withInteractiveSearchBudget(searchcore.NewFederatedSearcher(
		local,
		&deadlineSearch{},
	)).Search(t.Context(), searchcore.Request{Source: searchcore.SourceGlobal, Limit: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(response.Results) != 1 || response.Results[0].Title != "local" ||
		len(response.PartialFailures) != 1 {
		t.Fatalf("tail-tolerant response = %#v", response)
	}
}
