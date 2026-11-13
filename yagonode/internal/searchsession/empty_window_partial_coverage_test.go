package searchsession

import (
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestStableWindowKeepsEmptyPartialCoverageUnexhausted(t *testing.T) {
	failures := []searchcore.PartialFailure{
		{
			Source: searchcore.PartialFailureSourceFuzzyStage,
			Reason: "recovery deadline",
			Diagnostic: searchcore.PartialFailureDiagnostic{
				Stage: searchcore.FailureStageFuzzySearch,
				Cause: searchcore.FailureCauseDeadline,
			},
		},
		{
			Source: "failed-peer",
			Reason: "peer unavailable",
			Diagnostic: searchcore.PartialFailureDiagnostic{
				Stage:          searchcore.FailureStageRemotePeer,
				Cause:          searchcore.FailureCauseBackend,
				SourceAnswered: true,
			},
		},
	}
	for _, test := range []struct {
		name     string
		failures []searchcore.PartialFailure
	}{
		{name: "partial peers stored with incomplete coverage", failures: failures[1:]},
		{name: "fuzzy recovery skips complete storage", failures: failures},
	} {
		t.Run(test.name, func(t *testing.T) {
			search := WithStableWindow(&payloadSearcher{
				response: searchcore.Response{PartialFailures: test.failures},
			})
			request := searchcore.Request{Query: "orbital instruments", Limit: 5}
			for _, offset := range []int{0, 5} {
				request.Offset = offset
				response, err := search.Search(t.Context(), request)
				if err != nil {
					t.Fatal(err)
				}
				assertEmptyPartialCoverage(t, response, test.failures)
			}
		})
	}
}

func assertEmptyPartialCoverage(
	t *testing.T,
	response searchcore.Response,
	failures []searchcore.PartialFailure,
) {
	t.Helper()
	if response.Availability.Exhausted || !response.UnprovenZero() {
		t.Fatalf("partial coverage became exhaustive: %+v", response.Availability)
	}
	if response.LostSourceFailures() != len(failures) {
		t.Fatalf("strict losses=%d, want %d", response.LostSourceFailures(), len(failures))
	}
	if !slices.Equal(response.PartialFailures, failures) {
		t.Fatalf("diagnostics changed: %+v", response.PartialFailures)
	}
}
