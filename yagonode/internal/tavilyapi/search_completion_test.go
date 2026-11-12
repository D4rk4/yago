package tavilyapi

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestSearchCompletionUsesAvailabilityFailureForLostSources(t *testing.T) {
	completion := searchCompletion{
		response: searchcore.Response{PartialFailures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceRemoteYaCy,
			Reason: "peer unavailable",
		}}},
		err: errors.New("search returned partial results"),
	}
	if err := completion.errorForCaller(
		nil,
	); err == nil ||
		err.Error() == "search returned partial results" {
		t.Fatalf("lost-source availability error=%v", err)
	}
}

func TestSearchCompletionReturnsPartialRowsAfterCallerDeadline(t *testing.T) {
	completion := searchCompletion{
		response: searchcore.Response{
			Results: []searchcore.Result{{URL: "https://example.org/partial"}},
			PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceLocalSearch,
				Reason: "deadline",
			}},
		},
		err: context.DeadlineExceeded,
	}
	if err := completion.errorForCaller(context.DeadlineExceeded); err != nil {
		t.Fatalf("partial rows after caller deadline returned error: %v", err)
	}
}
