package tavilyapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

type searchAvailabilityEndpointCase struct {
	name            string
	response        searchcore.Response
	err             error
	expiredRequest  bool
	canceledRequest bool
	status          int
}

func TestSearchEndpointDistinguishesIncompleteEmptySearch(t *testing.T) {
	for _, test := range searchAvailabilityEndpointCases() {
		t.Run(test.name, func(t *testing.T) {
			assertSearchAvailabilityEndpointCase(t, test)
		})
	}
}

func TestSearchAvailabilityErrorDoesNotMislabelCallerCancellation(t *testing.T) {
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		if err := searchAvailabilityError(0, 1, cause); err != nil {
			t.Errorf("caller cause %v became source unavailability: %v", cause, err)
		}
	}
	if err := searchAvailabilityError(0, 1, nil); !errors.Is(err, errSearchUnavailable) {
		t.Fatalf("missing source without caller cancellation = %v, want unavailable", err)
	}
}

func searchAvailabilityEndpointCases() []searchAvailabilityEndpointCase {
	cases := append(searchAvailabilityBaseCases(), searchAvailabilityCallerCases()...)

	return append(cases, searchAvailabilityProgressiveCases()...)
}

func searchAvailabilityBaseCases() []searchAvailabilityEndpointCase {
	return []searchAvailabilityEndpointCase{
		{
			name:     "honest miss",
			response: searchcore.Response{},
			status:   http.StatusOK,
		},
		{
			name: "incomplete miss",
			response: searchcore.Response{
				Coverage: &searchcore.SearchCoverage{
					CompletedShards: 2, TotalShards: 8, Complete: false,
				},
				PartialFailures: []searchcore.PartialFailure{{
					Source: searchcore.PartialFailureSourceLocalSearch,
					Reason: "deadline",
				}},
			},
			status: http.StatusConflict,
		},
		{
			name: "incomplete miss with source error",
			response: searchcore.Response{PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceLocalSearch,
				Reason: "unavailable",
			}}},
			err:    errors.New("source unavailable"),
			status: http.StatusConflict,
		},
		{
			name: "source deadline incomplete miss",
			response: searchcore.Response{PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceLocalSearch,
				Reason: "deadline",
			}}},
			err:    context.DeadlineExceeded,
			status: http.StatusConflict,
		},
	}
}

func searchAvailabilityCallerCases() []searchAvailabilityEndpointCase {
	return []searchAvailabilityEndpointCase{
		{
			name: "incomplete coverage without rows",
			response: searchcore.Response{Coverage: &searchcore.SearchCoverage{
				CompletedShards: 2, TotalShards: 8, Complete: false,
			}},
			status: http.StatusConflict,
		},
		{
			name: "expired caller incomplete miss",
			response: searchcore.Response{PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceLocalSearch,
				Reason: "deadline",
			}}},
			err:            context.DeadlineExceeded,
			expiredRequest: true,
			status:         http.StatusConflict,
		},
		{
			name: "expired caller with usable partial response",
			response: searchcore.Response{
				Results: []searchcore.Result{{
					Title: "Partial result", URL: "https://example.org/partial",
				}},
				PartialFailures: []searchcore.PartialFailure{{
					Source: searchcore.PartialFailureSourceLocalSearch,
					Reason: "deadline",
				}},
				Coverage: &searchcore.SearchCoverage{
					CompletedShards: 2, TotalShards: 8, Complete: false,
				},
			},
			err:            context.DeadlineExceeded,
			expiredRequest: true,
			status:         http.StatusOK,
		},
		{
			name: "canceled caller incomplete miss",
			response: searchcore.Response{PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceLocalSearch,
				Reason: "canceled",
			}}},
			err:             context.Canceled,
			canceledRequest: true,
			status:          http.StatusInternalServerError,
		},
		{
			name: "usable partial response",
			response: searchcore.Response{
				Results: []searchcore.Result{{
					Title:   "Result",
					URL:     "https://example.org/result",
					Snippet: "available",
				}},
				PartialFailures: []searchcore.PartialFailure{{
					Source: searchcore.PartialFailureSourceLocalSearch,
					Reason: "private backend failure detail",
				}},
			},
			err:    errors.New("private backend failure detail"),
			status: http.StatusOK,
		},
	}
}

func searchAvailabilityProgressiveCases() []searchAvailabilityEndpointCase {
	return []searchAvailabilityEndpointCase{{
		name: "sampled results without a source failure",
		response: searchcore.Response{
			TotalResults: 7,
			Results: []searchcore.Result{{
				Title: "Sampled result", URL: "https://sample.example/result",
			}},
			Coverage: &searchcore.SearchCoverage{
				CompletedShards: 8, SampledShards: 8, TotalShards: 8, Complete: false,
			},
		},
		status: http.StatusOK,
	}}
}

func assertSearchAvailabilityEndpointCase(
	t *testing.T,
	test searchAvailabilityEndpointCase,
) {
	t.Helper()
	rec := httptest.NewRecorder()
	ctx := t.Context()
	if test.expiredRequest {
		expired, cancel := context.WithDeadline(ctx, time.Unix(0, 0))
		defer cancel()
		ctx = expired
	}
	if test.canceledRequest {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		ctx = canceled
	}
	req := httptest.NewRequestWithContext(
		ctx,
		http.MethodPost,
		PathSearch,
		strings.NewReader(`{"query":"topic"}`),
	)
	req.Header.Set("Authorization", "Bearer "+searchTestKey)
	newTestSearchEndpoint(
		&fakeSearcher{response: test.response, err: test.err},
		&fakeDocuments{rows: map[string]documentstore.Document{}},
	).ServeHTTP(rec, req)
	if rec.Code != test.status {
		t.Fatalf("status = %d, want %d body=%s",
			rec.Code, test.status, rec.Body.String())
	}
	if test.status == http.StatusConflict {
		assertSearchUnavailableResponse(t, rec)
		return
	}
	if test.status == http.StatusOK {
		assertSearchAvailabilityResponse(t, test, rec)
	}
}

func assertSearchUnavailableResponse(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if !strings.Contains(rec.Body.String(), `"search unavailable`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After = %q", rec.Header().Get("Retry-After"))
	}
	for _, internal := range []string{
		"ddgs", "deadline", "peer timed out", "incomplete shard coverage", "remote-yacy",
	} {
		if strings.Contains(strings.ToLower(rec.Body.String()), internal) {
			t.Fatalf("Tavily error exposed internal detail %q: %s", internal, rec.Body.String())
		}
	}
}

func assertSearchAvailabilityResponse(
	t *testing.T,
	test searchAvailabilityEndpointCase,
	rec *httptest.ResponseRecorder,
) {
	t.Helper()
	response := decodeSearchResponse(t, rec)
	if test.name == "honest miss" && len(response.Results) != 0 {
		t.Fatalf("honest empty response = %+v", response)
	}
	if len(test.response.Results) > 0 && len(response.Results) == 0 {
		t.Fatalf("row-bearing partial response = %+v", response)
	}
	if test.name == "sampled results without a source failure" && len(response.Results) != 1 {
		t.Fatalf("sampled response = %+v", response)
	}
	assertTavilyResponseHasNoSearchInternals(t, rec)
}

func assertTavilyResponseHasNoSearchInternals(
	t *testing.T,
	rec *httptest.ResponseRecorder,
) {
	t.Helper()
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	for _, field := range []string{"complete", "partial_failures", "coverage"} {
		if _, found := payload[field]; found {
			t.Fatalf("Tavily response exposed internal %q field: %s", field, rec.Body.String())
		}
	}
	var results []map[string]json.RawMessage
	if err := json.Unmarshal(payload["results"], &results); err != nil {
		t.Fatalf("decode response results: %v", err)
	}
	for _, result := range results {
		for _, field := range []string{"source", "reason", "partial_failures"} {
			if _, found := result[field]; found {
				t.Fatalf("Tavily result exposed internal %q field: %#v", field, result)
			}
		}
	}
}
