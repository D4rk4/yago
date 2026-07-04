package yagonode

import (
	"context"
	"log/slog"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
)

// denylistSnapshotter loads the current denylist as an in-memory snapshot.
type denylistSnapshotter interface {
	Snapshot(ctx context.Context) (urldenylist.Snapshot, error)
}

// denylistFilterSearcher drops results whose URL is on the operator denylist, so
// blocked content never reaches the caller regardless of which searcher produced
// it (local index, remote peers, or the web fallback). A snapshot load failure
// fails open — search stays available — with a warning.
type denylistFilterSearcher struct {
	next searchcore.Searcher
	deny denylistSnapshotter
}

// withDenylistFilter wraps a searcher with denylist result filtering. A nil
// denylist leaves the searcher unchanged.
func withDenylistFilter(next searchcore.Searcher, deny denylistSnapshotter) searchcore.Searcher {
	if deny == nil {
		return next
	}

	return denylistFilterSearcher{next: next, deny: deny}
}

func (s denylistFilterSearcher) Search(
	ctx context.Context,
	req searchcore.Request,
) (searchcore.Response, error) {
	resp, err := s.next.Search(ctx, req)
	if err != nil {
		return resp, err //nolint:wrapcheck // pass the wrapped searcher's error through unchanged.
	}

	snapshot, err := s.deny.Snapshot(ctx)
	if err != nil {
		slog.WarnContext(ctx, "denylist snapshot failed; serving unfiltered results",
			slog.Any("error", err))

		return resp, nil
	}
	if snapshot.IsEmpty() {
		return resp, nil
	}

	kept := make([]searchcore.Result, 0, len(resp.Results))
	removed := 0
	for _, result := range resp.Results {
		if snapshot.Blocks(result.URL) {
			removed++

			continue
		}
		kept = append(kept, result)
	}

	resp.Results = kept
	resp.TotalResults -= removed
	if resp.TotalResults < 0 {
		resp.TotalResults = 0
	}

	return resp, nil
}
