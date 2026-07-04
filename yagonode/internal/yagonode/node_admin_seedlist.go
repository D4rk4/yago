package yagonode

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/events"
)

// errUnknownSeedlist rejects a refresh for a URL that is not one of the node's
// configured seed lists, so the operator action cannot be turned into an
// arbitrary outbound fetch.
var errUnknownSeedlist = errors.New("seedlist url is not configured")

// seedlistImporter fetches and decodes the seeds one seed-list URL advertises.
type seedlistImporter interface {
	Import(ctx context.Context, url string) ([]yagomodel.Seed, error)
}

// seedlistRosterSink ingests freshly imported seeds into the peer roster.
type seedlistRosterSink interface {
	Discover(ctx context.Context, seeds ...yagomodel.Seed)
}

// seedImportRecorder persists the outcome of a seed-list import.
type seedImportRecorder interface {
	Record(ctx context.Context, url string, seeds int, importErr error) error
}

// eventRecorder records a structured node event.
type eventRecorder interface {
	Record(severity events.Severity, category events.Category, name, message string)
}

// seedlistRefreshSource re-imports a configured seed list on operator demand,
// feeding the discovered seeds to the roster and recording the outcome both in
// the durable status store and the event log.
type seedlistRefreshSource struct {
	importer seedlistImporter
	roster   seedlistRosterSink
	store    seedImportRecorder
	events   eventRecorder
	allowed  map[string]struct{}
}

func newSeedlistRefreshSource(
	importer seedlistImporter,
	roster seedlistRosterSink,
	store seedImportRecorder,
	recorder eventRecorder,
	urls []string,
) seedlistRefreshSource {
	allowed := make(map[string]struct{}, len(urls))
	for _, url := range urls {
		allowed[url] = struct{}{}
	}

	return seedlistRefreshSource{
		importer: importer,
		roster:   roster,
		store:    store,
		events:   recorder,
		allowed:  allowed,
	}
}

// RefreshSeedlist imports one configured seed list, ingests its seeds, and records
// the result. A URL outside the configured set is rejected before any fetch.
func (s seedlistRefreshSource) RefreshSeedlist(ctx context.Context, url string) error {
	if _, ok := s.allowed[url]; !ok {
		return errUnknownSeedlist
	}

	seeds, importErr := s.importer.Import(ctx, url)
	if err := s.store.Record(ctx, url, len(seeds), importErr); err != nil {
		slog.WarnContext(ctx, "record seedlist import status failed",
			slog.String("url", url), slog.Any("error", err))
	}

	if importErr != nil {
		s.events.Record(events.SeverityWarn, events.CategoryP2P, "seedlist.refresh.failed",
			fmt.Sprintf("seedlist %s import failed: %v", url, importErr))

		return fmt.Errorf("import seedlist %s: %w", url, importErr)
	}

	if s.roster != nil {
		s.roster.Discover(ctx, seeds...)
	}
	s.events.Record(events.SeverityInfo, events.CategoryP2P, "seedlist.refreshed",
		fmt.Sprintf("seedlist %s imported %d seeds", url, len(seeds)))

	return nil
}
