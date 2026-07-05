package yagonode

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
)

const (
	defaultHostRankRefreshInterval = 10 * time.Minute
	hostRankRefreshFailedMessage   = "host rank refresh scan failed"
)

// hostRankSweeper recomputes this node's local host-authority table from the
// stored document graph and publishes it for the searcher to read. The graph
// changes only as crawls land, so a coarse refresh interval keeps ranking fresh
// without rescanning the store on the query path.
type hostRankSweeper struct {
	documents documentstore.StoredDocuments
	holder    *hostrank.Holder
}

var newHostRankRefreshTicks = func(interval time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(interval)

	return ticker.C, ticker.Stop
}

func runHostRankRefreshLoop(ctx context.Context, sweeper hostRankSweeper) {
	sweeper.refreshOnce(ctx)

	ticks, stop := newHostRankRefreshTicks(defaultHostRankRefreshInterval)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			sweeper.refreshOnce(ctx)
		}
	}
}

func (s hostRankSweeper) refreshOnce(ctx context.Context) {
	if s.documents == nil {
		return
	}

	incoming := map[string]map[string]hostLinkReference{}
	err := s.documents.StoredDocuments(ctx, func(doc documentstore.Document) (bool, error) {
		collectDocumentHostLinks(incoming, doc)

		return true, nil
	})
	if err != nil {
		slog.WarnContext(ctx, hostRankRefreshFailedMessage, slog.Any("error", err))

		return
	}

	s.holder.Store(hostrank.Compute(hostCitationCounts(incoming)))
}

func hostCitationCounts(
	incoming map[string]map[string]hostLinkReference,
) map[string]map[string]int {
	counts := make(map[string]map[string]int, len(incoming))
	for target, sources := range incoming {
		targetCounts := make(map[string]int, len(sources))
		for source, reference := range sources {
			targetCounts[source] = reference.Count
		}
		counts[target] = targetCounts
	}

	return counts
}
