package searchsession

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type completedDeadlineSearcher struct {
	cancel context.CancelCauseFunc
}

func (s completedDeadlineSearcher) Search(
	context.Context,
	searchcore.Request,
) (searchcore.Response, error) {
	s.cancel(context.DeadlineExceeded)

	return searchcore.Response{
		TotalResults: 3,
		Results: []searchcore.Result{
			{URL: "https://example.org/first"},
			{URL: "https://example.org/second"},
			{URL: "https://example.org/third"},
		},
	}, nil
}

func TestStableWindowRetainsCompletedPageAtDeadlineWithoutCaching(t *testing.T) {
	for _, offset := range []int{0, 1, 3} {
		ctx, cancel := context.WithCancelCause(t.Context())
		defer cancel(nil)
		stable := NewStableWindow(completedDeadlineSearcher{cancel: cancel})
		request := searchcore.Request{Query: "synthetic", Offset: offset, Limit: 1}
		response, err := stable.Search(ctx, request)
		if !errors.Is(err, context.DeadlineExceeded) || response.Availability.Exhausted {
			t.Fatalf("offset=%d response=%+v error=%v", offset, response, err)
		}
		want := min(1, 3-offset)
		if len(response.Results) != want || response.TotalResults != 3 ||
			!reflect.DeepEqual(response.Request, request) {
			t.Fatalf("offset=%d response=%+v", offset, response)
		}
		if offset == 1 && response.Results[0].URL != "https://example.org/second" {
			t.Fatalf("page=%+v", response.Results)
		}
		if _, cached := stable.Recent(request); cached {
			t.Fatal("deadline response was cached")
		}
	}
}
