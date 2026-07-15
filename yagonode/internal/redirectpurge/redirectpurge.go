// Package redirectpurge sweeps already-indexed search-engine click-tracking
// documents out of the live corpus (SEARCH-29). SEARCH-28 stopped new
// bing.com/ck/a redirect targets from being indexed, but documents ingested
// before that fix keep polluting results until removed; the sweep runs once
// in the background at startup and deletes them from both the search index
// and the document vault.
package redirectpurge

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
)

// Sweeper walks the stored corpus once and removes tracking-redirect pages.
type Sweeper struct {
	docs    documentstore.StoredDocuments
	purge   func(context.Context, []string, []yagomodel.Hash) error
	hashURL func(string) (yagomodel.URLHash, error)
}

// New wires a sweeper; any nil dependency disables it.
func New(
	docs documentstore.StoredDocuments,
	purge func(context.Context, []string, []yagomodel.Hash) error,
) *Sweeper {
	if docs == nil || purge == nil {
		return nil
	}

	return &Sweeper{docs: docs, purge: purge, hashURL: yagomodel.HashURL}
}

// Run performs the sweep. A nil sweeper is a no-op. Per-document failures
// are logged and skipped — a page that resists deletion today is caught by
// the next restart's sweep. Run returns between documents once ctx is
// cancelled, so a shutdown joining the sweep never waits out a full pass.
func (s *Sweeper) Run(ctx context.Context) {
	if s == nil {
		return
	}
	condemned := s.collect(ctx)
	removed := 0
	for _, docURL := range condemned {
		if ctx.Err() != nil {
			return
		}
		hash, err := s.hashURL(docURL)
		if err != nil {
			slog.WarnContext(ctx, "redirect purge: url hash failed",
				slog.String("url", docURL), slog.Any("error", err))

			continue
		}
		if err := s.purge(ctx, []string{docURL}, []yagomodel.Hash{hash.Hash()}); err != nil {
			slog.WarnContext(ctx, "redirect purge: document lineage failed",
				slog.String("url", docURL), slog.Any("error", err))

			continue
		}
		removed++
	}
	if len(condemned) > 0 {
		slog.InfoContext(ctx, "redirect purge complete",
			slog.Int("found", len(condemned)), slog.Int("removed", removed))
	}
}

// collect gathers condemned document URLs first, so deletion never mutates
// the store mid-scan.
func (s *Sweeper) collect(ctx context.Context) []string {
	condemned := make([]string, 0)
	err := s.docs.StoredDocuments(ctx, func(doc documentstore.Document) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, fmt.Errorf("redirect purge scan cancelled: %w", err)
		}
		if IsTrackingRedirect(doc.NormalizedURL) || IsTrackingRedirect(doc.CanonicalURL) {
			condemned = append(condemned, doc.NormalizedURL)
		}

		return true, nil
	})
	if err != nil {
		slog.WarnContext(ctx, "redirect purge: corpus scan failed", slog.Any("error", err))
	}

	return condemned
}

// IsTrackingRedirect reports whether rawURL is a search-engine click-tracking
// address rather than a destination page. Today that means Bing's /ck/
// redirects — the family SEARCH-28 decodes at ingest; a tracking URL is junk
// in the corpus regardless of whether its destination can be recovered.
func IsTrackingRedirect(rawURL string) bool {
	if rawURL == "" {
		return false
	}
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "bing.com" && !strings.HasSuffix(host, ".bing.com") {
		return false
	}

	return strings.HasPrefix(parsed.EscapedPath(), "/ck/")
}
