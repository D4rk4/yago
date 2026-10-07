package yagonode

import (
	"context"
	"errors"
	"log/slog"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/tracectx"
)

const searchExecutionDiagnosticMessage = "search execution incomplete"

type searchExecutionDiagnosticsSearcher struct {
	next searchcore.Searcher
}

func withSearchExecutionDiagnostics(next searchcore.Searcher) searchcore.Searcher {
	return searchExecutionDiagnosticsSearcher{next: next}
}

func (s searchExecutionDiagnosticsSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (response searchcore.Response, err error) {
	defer func() {
		logSearchExecution(ctx, response, err)
	}()
	response, err = s.next.Search(ctx, req)

	return
}

func logSearchExecution(ctx context.Context, response searchcore.Response, searchErr error) {
	lostSources := response.LostSourceFailures()
	if lostSources == 0 && searchErr == nil {
		return
	}

	level := slog.LevelDebug
	if len(response.Results) == 0 || searchErr != nil {
		level = slog.LevelWarn
	}
	attributes := []slog.Attr{
		slog.Int("retrievedRows", len(response.Results)),
		slog.Int("totalResults", response.TotalResults),
		slog.Any("failureCounts", searchcore.FailureDiagnosticCounts(response.PartialFailures)),
	}
	attributes = append(attributes, tracectx.ServerSpanAttribute(ctx))
	if searchErr != nil {
		attributes = append(attributes, slog.String(
			"errorCategory",
			searchcore.FailureCauseLabel(searchExecutionErrorCause(searchErr)),
		))
	}
	slog.Default().LogAttrs(ctx, level, searchExecutionDiagnosticMessage, attributes...)
}

func searchExecutionErrorCause(err error) searchcore.FailureCause {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return searchcore.FailureCauseDeadline
	case errors.Is(err, context.Canceled):
		return searchcore.FailureCauseCanceled
	case errors.Is(err, errInteractiveSearchCapacity):
		return searchcore.FailureCauseCapacity
	default:
		return searchcore.FailureCauseUnknown
	}
}
