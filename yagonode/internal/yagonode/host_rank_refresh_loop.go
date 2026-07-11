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
		citations.Add(documentAuthorityCitations(doc)...)

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

func documentAuthorityCitations(doc documentstore.Document) []hostrank.Citation {
	sourceURL := doc.NormalizedURL
	if sourceURL == "" {
		sourceURL = doc.CanonicalURL
	}
	if sourceURL == "" {
		return nil
	}
	if doc.OutboundAnchorEvidenceKnown {
		citations := make([]hostrank.Citation, 0, len(doc.OutboundAnchors))
		for _, anchor := range doc.OutboundAnchors {
			if anchor.NoFollow || anchor.UserGenerated || anchor.Sponsored {
				continue
			}
			citations = append(citations, hostrank.Citation{
				SourceURL: sourceURL, TargetURL: anchor.TargetURL, Confidence: 1,
			})
		}

		return citations
	}

	citations := make([]hostrank.Citation, 0, len(doc.Outlinks))
	for _, targetURL := range doc.Outlinks {
		citations = append(citations, hostrank.Citation{
			SourceURL: sourceURL, TargetURL: targetURL, Confidence: 0.4,
		})
	}

	return citations
}
