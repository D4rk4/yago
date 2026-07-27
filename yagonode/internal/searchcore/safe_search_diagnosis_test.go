package searchcore

import (
	"context"
	"errors"
	"testing"
)

type failingDiagnosticSearcher struct{}

func (failingDiagnosticSearcher) Search(_ context.Context, _ Request) (Response, error) {
	return Response{
			PartialFailures: []PartialFailure{
				{Source: "peer-a", Reason: "peer transport failed"},
				{Source: "peer-b", Reason: "status 503"},
			},
		},
		errors.New("exact search deadline exceeded")
}

// The federation returns its partial failures alongside its error on purpose.
// Replacing them with a zero Response turned four named peer failures into one
// opaque stage timeout, so the operator lost the diagnosis at the exact moment
// the answer went empty.
func TestSafeSearchKeepsPartialFailuresWhenTheInnerSearchFails(t *testing.T) {
	req := Request{}
	response, err := NewSafeSearchSearcher(failingDiagnosticSearcher{}).
		Search(context.Background(), req)
	if err == nil {
		t.Fatal("inner failure was swallowed")
	}
	if len(response.PartialFailures) != 2 {
		t.Fatalf("partial failures = %v, want both peers named", response.PartialFailures)
	}
	if response.PartialFailures[0].Source != "peer-a" {
		t.Fatalf("first failure = %+v", response.PartialFailures[0])
	}
	if len(response.Results) != 0 {
		t.Fatalf("results survived a failed search: %v", response.Results)
	}
}
