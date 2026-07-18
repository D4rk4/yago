package yagonode

import (
	"context"

	"github.com/D4rk4/yago/yagonode/internal/searchcore"
	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
)

// denylistSnapshotter loads the current denylist as an in-memory snapshot.
type denylistSnapshotter interface {
	Snapshot() urldenylist.Snapshot
}

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

	return filterDenylistedResponse(resp, s.deny), nil
}

func filterDenylistedResponse(
	resp searchcore.Response,
	deny denylistSnapshotter,
) searchcore.Response {
	snapshot := deny.Snapshot()
	if snapshot.IsEmpty() {
		return resp
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

	return resp
}
