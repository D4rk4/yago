package yagonode

import (
	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/hostrank"
)

func visitDocumentAuthorityCitations(
	doc documentstore.Document,
	visit func(hostrank.Citation),
) {
	sourceURL := doc.NormalizedURL
	if sourceURL == "" {
		sourceURL = doc.CanonicalURL
	}
	if sourceURL == "" {
		return
	}
	if doc.OutboundAnchorEvidenceKnown {
		for _, anchor := range doc.OutboundAnchors {
			if anchor.NoFollow || anchor.UserGenerated || anchor.Sponsored {
				continue
			}
			visit(hostrank.Citation{
				SourceURL: sourceURL, TargetURL: anchor.TargetURL, Confidence: 1,
			})
		}

		return
	}

	for _, targetURL := range doc.Outlinks {
		visit(hostrank.Citation{
			SourceURL: sourceURL, TargetURL: targetURL, Confidence: 0.4,
		})
	}
}
