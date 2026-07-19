package yagonode

import (
	"context"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/events"
)

const (
	searchExplanationCompletedEvent = "search.explain.completed"
	searchExplanationFailedEvent    = "search.explain.failed"
)

func (e *searchExplainEndpoint) withEvents(
	sink nodeEventRecorder,
) *searchExplainEndpoint {
	e.events = sink

	return e
}

func (e searchExplainEndpoint) observedExplanation(
	ctx context.Context,
	request searchExplainRequest,
) (searchExplainResponse, int, error) {
	response, status, err := e.explanation(ctx, request)
	if e.events == nil {
		return response, status, err
	}
	if err != nil {
		e.events.Record(
			events.SeverityWarn,
			events.CategorySearch,
			searchExplanationFailedEvent,
			fmt.Sprintf("search explanation failed with status %d", status),
		)

		return response, status, err
	}
	e.events.Record(
		events.SeverityInfo,
		events.CategorySearch,
		searchExplanationCompletedEvent,
		fmt.Sprintf(
			"search explanation completed for %s scope with %d results",
			response.Scope,
			len(response.Results),
		),
	)

	return response, status, nil
}
