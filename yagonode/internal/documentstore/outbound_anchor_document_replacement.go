package documentstore

import (
	"context"
	"fmt"
	"sort"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const outboundAnchorPublicationMaximumEncodedBytes = 64 << 20

type outboundAnchorDocumentReplacement struct {
	contributions map[string][]AnchorText
	finalizations []OutboundAnchorFinalization
	sources       map[string]struct{}
	targets       []string
}

func (d documentVault) prepareOutboundAnchorDocumentReplacement(
	ctx context.Context,
	sets []OutboundAnchorSet,
) (outboundAnchorDocumentReplacement, error) {
	replacement := outboundAnchorDocumentReplacement{
		contributions: make(map[string][]AnchorText),
		finalizations: make([]OutboundAnchorFinalization, 0, len(sets)),
		sources:       make(map[string]struct{}, len(sets)),
	}
	err := d.vault.View(ctx, func(tx *vault.Txn) error {
		for _, set := range sets {
			incoming, incomingTargets := canonicalOutboundAnchors(
				set.SourceURL,
				set.Anchors,
			)
			previous, err := d.readOutboundAnchorPublication(tx, set.SourceURL)
			if err != nil {
				return err
			}
			desired := desiredOutboundAnchorPublication(incoming, incomingTargets)
			if outboundAnchorPublicationsEqual(previous, desired) {
				continue
			}
			replacement.sources[set.SourceURL] = struct{}{}
			replacement.finalizations = append(
				replacement.finalizations,
				OutboundAnchorFinalization{
					sourceURL: set.SourceURL,
					expected:  previous,
					desired:   desired,
				},
			)
			replacement.targets = append(
				replacement.targets,
				previous.Targets...,
			)
			replacement.targets = append(
				replacement.targets,
				incomingTargets...,
			)
			for targetURL, anchors := range incoming {
				replacement.contributions[targetURL] = append(
					replacement.contributions[targetURL],
					anchors...,
				)
			}
		}

		return nil
	})
	if err != nil {
		return outboundAnchorDocumentReplacement{}, fmt.Errorf(
			"prepare outbound anchor document replacement: %w",
			err,
		)
	}
	replacement.targets = uniqueSortedStrings(replacement.targets)
	for targetURL, anchors := range replacement.contributions {
		replacement.contributions[targetURL] = canonicalAnchorTexts(anchors)
	}
	sort.Slice(replacement.finalizations, func(left, right int) bool {
		return replacement.finalizations[left].sourceURL <
			replacement.finalizations[right].sourceURL
	})
	return replacement, nil
}

func outboundAnchorFinalizationEncodedByteCeiling(
	finalization OutboundAnchorFinalization,
) int {
	return len(finalization.sourceURL) +
		outboundAnchorPublicationEncodedByteCeiling(finalization.desired)
}

func outboundAnchorPublicationEncodedByteCeiling(
	publication outboundAnchorPublication,
) int {
	encodedBytes := len(`{"Targets":[],"Revision":""}`) + 6*len(publication.Revision)
	for index, targetURL := range publication.Targets {
		if index > 0 {
			encodedBytes++
		}
		encodedBytes += 2 + 6*len(targetURL)
	}

	return encodedBytes
}
