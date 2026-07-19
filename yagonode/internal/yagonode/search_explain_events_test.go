package yagonode

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/events"
	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

func TestSearchExplanationRecordsBoundedOperatorEvents(t *testing.T) {
	t.Parallel()

	recorder := events.NewRecorder(4)
	endpoint := newSearchExplainEndpoint(
		&searchExplainScript{}, nil, nil, nil, nil,
	).withEvents(recorder)

	response, status, err := endpoint.observedExplanation(
		context.Background(),
		searchExplainRequest{Query: "private operator query", Scope: searchcore.SourceLocal},
	)
	if err != nil || status != http.StatusOK || response.Scope != searchcore.SourceLocal {
		t.Fatalf("response/status/error = %#v/%d/%v", response, status, err)
	}
	_, status, err = endpoint.observedExplanation(
		context.Background(),
		searchExplainRequest{},
	)
	if err == nil || status != http.StatusBadRequest {
		t.Fatalf("failure status/error = %d/%v", status, err)
	}

	recent := recorder.Recent(2)
	if len(recent) != 2 ||
		recent[0].Category != events.CategorySearch ||
		recent[0].Severity != events.SeverityWarn ||
		recent[0].Name != "search.explain.failed" ||
		recent[0].Message != "search explanation failed with status 400" ||
		recent[1].Category != events.CategorySearch ||
		recent[1].Severity != events.SeverityInfo ||
		recent[1].Name != "search.explain.completed" ||
		recent[1].Message != "search explanation completed for local scope with 0 results" {
		t.Fatalf("events = %#v", recent)
	}
	for _, event := range recent {
		if strings.Contains(event.Message, "private operator query") {
			t.Fatalf("event leaks query: %#v", event)
		}
	}
}

func TestSearchExplanationWithoutEventSinkKeepsResponse(t *testing.T) {
	t.Parallel()

	response, status, err := newSearchExplainEndpoint(
		&searchExplainScript{}, nil, nil, nil, nil,
	).observedExplanation(
		context.Background(),
		searchExplainRequest{Query: "query", Scope: searchcore.SourceLocal},
	)
	if err != nil || status != http.StatusOK || response.Scope != searchcore.SourceLocal {
		t.Fatalf("response/status/error = %#v/%d/%v", response, status, err)
	}
}
