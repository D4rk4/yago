package searchsession

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestGlobalIncompleteRefreshUsesUnexpiredLocalCoverage(t *testing.T) {
	previousClock := clock
	now := time.Unix(1_700_000_000, 0)
	clock = func() time.Time { return now }
	t.Cleanup(func() { clock = previousClock })

	localRequest := searchcore.Request{
		Query: "drunklab", Source: searchcore.SourceLocal, Limit: 10,
	}
	stable := NewStableWindow(&refreshSequenceSearcher{responses: []searchcore.Response{{
		TotalResults: 148,
		Results: []searchcore.Result{{
			URL: "https://about.me/drunklab", Source: searchcore.SourceLocal,
		}},
	}}})
	if _, err := stable.Search(t.Context(), localRequest); err != nil {
		t.Fatal(err)
	}
	now = now.Add(sessionTTL - time.Second)
	failure := searchcore.PartialFailure{
		Source: searchcore.PartialFailureSourceLocalSearch,
		Reason: "local search deadline exceeded",
	}
	globalRequest := localRequest
	globalRequest.Source = searchcore.SourceGlobal
	inner := &refreshSequenceSearcher{
		responses: []searchcore.Response{{PartialFailures: []searchcore.PartialFailure{failure}}},
		errors:    []error{context.DeadlineExceeded},
	}
	response, err := WithRecentSuccessOnIncompleteRefresh(inner, stable).Search(
		t.Context(),
		globalRequest,
	)
	if err != nil || response.TotalResults != 148 || len(response.Results) != 1 ||
		!reflect.DeepEqual(response.Request, globalRequest) ||
		!reflect.DeepEqual(response.PartialFailures, []searchcore.PartialFailure{failure}) {
		t.Fatalf("response = %#v, error = %v", response, err)
	}
	if recent, found := stable.Recent(globalRequest); found || len(recent.Results) != 0 {
		t.Fatalf("global session was stored: %#v", recent)
	}
	now = now.Add(2 * time.Second)
	if recent, found := stable.Recent(localRequest); found || len(recent.Results) != 0 {
		t.Fatalf("local session expiry was extended: %#v", recent)
	}
}

func TestGlobalHonestMissDoesNotUseLocalCoverage(t *testing.T) {
	localRequest := searchcore.Request{
		Query: "drunklab", Source: searchcore.SourceLocal, Limit: 10,
	}
	stable := NewStableWindow(&refreshSequenceSearcher{responses: []searchcore.Response{{
		Results: []searchcore.Result{{URL: "https://about.me/drunklab"}},
	}}})
	if _, err := stable.Search(t.Context(), localRequest); err != nil {
		t.Fatal(err)
	}
	globalRequest := localRequest
	globalRequest.Source = searchcore.SourceGlobal

	honest := &refreshSequenceSearcher{responses: []searchcore.Response{{}}}
	response, err := WithRecentSuccessOnIncompleteRefresh(honest, stable).Search(
		t.Context(),
		globalRequest,
	)
	if err != nil || len(response.Results) != 0 {
		t.Fatalf("honest response = %#v, error = %v", response, err)
	}

	incomplete := &refreshSequenceSearcher{
		responses: []searchcore.Response{{PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceLocalSearch,
			Reason: "deadline",
		}}}},
		errors: []error{errors.New("deadline")},
	}
	missingRequest := globalRequest
	missingRequest.Query = "missing"
	response, err = WithRecentSuccessOnIncompleteRefresh(incomplete, stable).Search(
		t.Context(),
		missingRequest,
	)
	if err == nil || len(response.Results) != 0 || len(response.PartialFailures) != 1 {
		t.Fatalf("missing response = %#v, error = %v", response, err)
	}
}
