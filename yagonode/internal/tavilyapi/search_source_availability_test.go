package tavilyapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestSearchEndpointAcceptsOptionalSearchFailures(t *testing.T) {
	failures := optionalAvailabilityFailures()
	for _, test := range []struct {
		name     string
		failures []searchcore.PartialFailure
	}{
		{name: "complete empty answer"},
		{name: "fuzzy deadline", failures: failures[:1]},
		{name: "partial peer loss", failures: failures[1:]},
		{name: "fuzzy deadline and partial peer loss", failures: failures},
		{name: "query shape", failures: []searchcore.PartialFailure{{
			Source: searchcore.PartialFailureSourceQueryShape,
		}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := searchcore.Response{PartialFailures: test.failures}
			original := slices.Clone(response.PartialFailures)
			recorder := optionalAvailabilityRequest(t, &fakeSearcher{response: response})
			if recorder.Code != http.StatusOK || recorder.Header().Get("Retry-After") != "" {
				t.Fatalf("status=%d retry=%q body=%s", recorder.Code,
					recorder.Header().Get("Retry-After"), recorder.Body.String())
			}
			var answer SearchResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
				t.Fatal(err)
			}
			if answer.Results == nil || len(answer.Results) != 0 {
				t.Fatalf("results = %#v, want empty array", answer.Results)
			}
			if !slices.Equal(response.PartialFailures, original) {
				t.Fatal("availability changed failure diagnostics")
			}
		})
	}
}

func TestSearchEndpointRetainsRequiredSourceFailures(t *testing.T) {
	for _, source := range []string{
		searchcore.PartialFailureSourceExactStage,
		searchcore.PartialFailureSourceLocalExactStage,
		searchcore.PartialFailureSourceLocalSearch,
		searchcore.PartialFailureSourceLocalEvidence,
		searchcore.PartialFailureSourceRemoteYaCy,
		searchcore.PartialFailureSourceRemoteStage,
		searchcore.PartialFailureSourcePeerReputation,
		searchcore.PartialFailureSourceWeb,
		"unknown-source",
	} {
		t.Run(source, func(t *testing.T) {
			failures := append(optionalAvailabilityFailures(), searchcore.PartialFailure{
				Source: source,
				Diagnostic: searchcore.PartialFailureDiagnostic{
					Cause: searchcore.FailureCauseDeadline,
				},
			})
			assertSearchAvailabilityEndpointCase(t, searchAvailabilityEndpointCase{
				response: searchcore.Response{PartialFailures: failures},
				status:   http.StatusConflict,
			})
		})
	}
	for _, cause := range []searchcore.FailureCause{
		searchcore.FailureCauseUnknown,
		searchcore.FailureCauseCapacity,
		searchcore.FailureCauseBackend,
		searchcore.FailureCauseCanceled,
	} {
		t.Run(searchcore.FailureCauseLabel(cause), func(t *testing.T) {
			failures := optionalAvailabilityFailures()
			failures[0].Diagnostic.Cause = cause
			assertSearchAvailabilityEndpointCase(t, searchAvailabilityEndpointCase{
				response: searchcore.Response{PartialFailures: failures},
				status:   http.StatusConflict,
			})
		})
	}
}

func TestSearchEndpointRequiresPeerAnswerEvidence(t *testing.T) {
	for _, test := range []struct {
		name    string
		failure searchcore.PartialFailure
	}{
		{name: "all peers failed", failure: searchcore.PartialFailure{
			Source: "failed-peer",
			Diagnostic: searchcore.PartialFailureDiagnostic{
				Stage: searchcore.FailureStageRemotePeer,
				Cause: searchcore.FailureCauseDeadline,
			},
		}},
		{name: "legacy failure without diagnostics", failure: searchcore.PartialFailure{Source: "peer"}},
		{name: "unclassified failure with answer evidence", failure: searchcore.PartialFailure{
			Source: "peer", Diagnostic: searchcore.PartialFailureDiagnostic{SourceAnswered: true},
		}},
		{name: "whole federation failure is never optional", failure: searchcore.PartialFailure{
			Source: searchcore.PartialFailureSourceRemoteYaCy,
			Diagnostic: searchcore.PartialFailureDiagnostic{
				Stage: searchcore.FailureStageRemotePeer, SourceAnswered: true,
			},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			assertSearchAvailabilityEndpointCase(t, searchAvailabilityEndpointCase{
				response: searchcore.Response{
					PartialFailures: append(optionalAvailabilityFailures(), test.failure),
				},
				status: http.StatusConflict,
			})
		})
	}
}

func TestOptionalSearchFailuresDoNotHideExecutionErrors(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		caller error
	}{
		{name: "backend error", err: errors.New("backend failed")},
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "caller cancellation", err: context.Canceled, caller: context.Canceled},
	} {
		t.Run(test.name, func(t *testing.T) {
			completion := searchCompletionFrom(searchcore.Response{
				PartialFailures: optionalAvailabilityFailures(),
			}, test.err)
			err := completion.errorForCaller(test.caller)
			if !errors.Is(err, test.err) || errors.Is(err, errSearchUnavailable) {
				t.Fatalf("error = %v, want original error %v", err, test.err)
			}
		})
	}
}

func optionalAvailabilityFailures() []searchcore.PartialFailure {
	return []searchcore.PartialFailure{
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
}

func optionalAvailabilityRequest(
	t *testing.T,
	search searchcore.Searcher,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, PathSearch,
		strings.NewReader(`{"query":"orbital instruments"}`))
	request.Header.Set("Authorization", "Bearer "+searchTestKey)
	recorder := httptest.NewRecorder()
	newTestSearchEndpoint(search, nil).ServeHTTP(recorder, request)

	return recorder
}
