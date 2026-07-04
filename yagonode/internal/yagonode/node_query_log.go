package yagonode

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
)

// queryLogMode governs how much of a search query is recorded to the node's logs.
type queryLogMode string

const (
	queryLogOff       queryLogMode = "off"
	queryLogAggregate queryLogMode = "aggregate"
	queryLogFull      queryLogMode = "full"
)

func parseQueryLogMode(raw string) (queryLogMode, error) {
	switch queryLogMode(raw) {
	case "", queryLogOff:
		return queryLogOff, nil
	case queryLogAggregate, queryLogFull:
		return queryLogMode(raw), nil
	default:
		return "", fmt.Errorf("unknown query log mode %q", raw)
	}
}

// queryLoggingSearcher decorates a searcher to record search queries per the
// operator's privacy mode: off records nothing, aggregate records only the query
// length and result count (never the text), and full records the query text.
type queryLoggingSearcher struct {
	next   searchcore.Searcher
	mode   queryLogMode
	logger *slog.Logger
}

func withQueryLogging(next searchcore.Searcher, mode queryLogMode) searchcore.Searcher {
	if mode == queryLogOff || mode == "" {
		return next
	}

	return queryLoggingSearcher{next: next, mode: mode, logger: slog.Default()}
}

func (s queryLoggingSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	resp, err := s.next.Search(ctx, req)
	s.record(ctx, req, resp)

	return resp, err //nolint:wrapcheck // pass the wrapped searcher's error through unchanged.
}

func (s queryLoggingSearcher) record(
	ctx context.Context,
	req searchcore.Request,
	resp searchcore.Response,
) {
	if s.mode == queryLogFull {
		s.logger.InfoContext(ctx, "search query",
			slog.String("query", req.Query),
			slog.Int("results", resp.TotalResults),
		)

		return
	}

	s.logger.InfoContext(ctx, "search query",
		slog.Int("queryLength", len(req.Query)),
		slog.Int("results", resp.TotalResults),
	)
}
