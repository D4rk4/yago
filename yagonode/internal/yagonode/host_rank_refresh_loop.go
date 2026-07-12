package yagonode

import (
	"context"
	"log/slog"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
	"github.com/D4rk4/yago/yagonode/internal/hosttrust"
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
	trust     hostTrustPolicySource
}

type hostTrustPolicySource interface {
	Current() hosttrust.Policy
	Changes() <-chan struct{}
}

var newHostRankRefreshTicks = func(interval time.Duration) (<-chan time.Time, func()) {
	ticker := time.NewTicker(interval)

	return ticker.C, ticker.Stop
}

func runHostRankRefreshLoop(ctx context.Context, sweeper hostRankSweeper) {
	sweeper.refreshOnce(ctx)

	ticks, stop := newHostRankRefreshTicks(defaultHostRankRefreshInterval)
	defer stop()
	var trustChanges <-chan struct{}
	if sweeper.trust != nil {
		trustChanges = sweeper.trust.Changes()
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			sweeper.refreshOnce(ctx)
		case <-trustChanges:
			sweeper.refreshOnce(ctx)
		}
	}
}

func (s hostRankSweeper) refreshOnce(ctx context.Context) {
	if s.documents == nil {
		return
	}

	citations := hostrank.NewCitationSample()
	err := s.documents.StoredDocuments(ctx, func(doc documentstore.Document) (bool, error) {
		visitDocumentAuthorityCitations(doc, func(citation hostrank.Citation) {
			citations.Add(citation)
		})

		return true, nil
	})
	if err != nil {
		slog.WarnContext(ctx, hostRankRefreshFailedMessage, slog.Any("error", err))

		return
	}

	options := hostrank.DomainOptions{}
	if s.trust != nil {
		policy := s.trust.Current()
		options.TrustedDomains = policy.Domains
		options.TrustBlend = policy.Blend
	}
	table, err := hostrank.ComputeDomainAuthority(ctx, citations.Citations(), options)
	if err != nil {
		slog.WarnContext(ctx, hostRankRefreshFailedMessage, slog.Any("error", err))

		return
	}
	s.holder.Store(table)
}
