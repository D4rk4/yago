package yagonode

import (
	"log/slog"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

const maximumLoggedSearchFailureSources = 8

func queryLogAttributes(
	req searchcore.Request,
	resp searchcore.Response,
	includeQuery bool,
) []any {
	attributes := make([]any, 0, 5)
	if includeQuery {
		attributes = append(attributes, slog.String("query", req.Query))
	} else {
		attributes = append(attributes, slog.Int("queryLength", len(req.Query)))
	}
	attributes = append(attributes, slog.Int("results", resp.TotalResults))
	if len(resp.PartialFailures) > 0 {
		attributes = append(
			attributes,
			slog.Int("partialFailures", len(resp.PartialFailures)),
			slog.Any("failureSources", queryFailureSources(resp.PartialFailures)),
		)
	}

	return attributes
}

func queryFailureSources(failures []searchcore.PartialFailure) []string {
	sources := make([]string, 0, min(len(failures), maximumLoggedSearchFailureSources))
	seen := make(map[string]struct{}, min(len(failures), maximumLoggedSearchFailureSources))
	for _, failure := range failures {
		if _, found := seen[failure.Source]; found {
			continue
		}
		seen[failure.Source] = struct{}{}
		sources = append(sources, failure.Source)
		if len(sources) == maximumLoggedSearchFailureSources {
			break
		}
	}

	return sources
}
