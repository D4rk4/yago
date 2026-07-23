package tavilyapi

import (
	"context"
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

func searchAvailabilityEndpointCases() []searchAvailabilityEndpointCase {
	return []searchAvailabilityEndpointCase{
		{
			name:     "honest miss",
			response: searchcore.Response{},
			status:   http.StatusOK,
		},
		{
			name: "incomplete miss",
			response: searchcore.Response{PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceLocalSearch,
				Reason: "deadline",
			}}},
			status: http.StatusServiceUnavailable,
		},
		{
			name: "incomplete miss with source error",
			response: searchcore.Response{PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceLocalSearch,
				Reason: "unavailable",
			}}},
			err:    errors.New("source unavailable"),
			status: http.StatusServiceUnavailable,
		},
		{
			name: "source deadline incomplete miss",
			response: searchcore.Response{PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceLocalSearch,
				Reason: "deadline",
			}}},
			err:    context.DeadlineExceeded,
			status: http.StatusServiceUnavailable,
		},
		{
			name: "expired caller incomplete miss",
			response: searchcore.Response{PartialFailures: []searchcore.PartialFailure{{
				Source: searchcore.PartialFailureSourceLocalSearch,
				Reason: "deadline",
			}}},
			err:            context.DeadlineExceeded,
			expiredRequest: true,
			status:         http.StatusInternalServerError,
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
					Source: searchcore.PartialFailureSourceRemoteYaCy,
					Reason: "deadline",
				}},
			},
			status: http.StatusOK,
		},
	}
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
	if test.status == http.StatusServiceUnavailable &&
		!strings.Contains(rec.Body.String(), `"search unavailable`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if test.status == http.StatusServiceUnavailable &&
		rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After = %q", rec.Header().Get("Retry-After"))
	}
}
