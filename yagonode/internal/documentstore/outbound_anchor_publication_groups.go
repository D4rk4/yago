package documentstore

import (
	"context"
	"fmt"
)

func (d documentVault) replaceReservedOutboundAnchorDocumentsWithin(
	ctx context.Context,
	reservation DocumentLineageReservation,
	sets []OutboundAnchorSet,
	visit func([]Document) error,
	publicationMaximumEncodedBytes int,
) (AnchorReplacementReceipt, error) {
	sources := make([]string, 0, len(sets))
	for _, set := range sets {
		sources = append(sources, set.SourceURL)
	}
	if err := d.activeDocumentLineageLease(reservation, sources); err != nil {
		return AnchorReplacementReceipt{}, err
	}
	atCapacity, err := d.outboundAnchorUpdateAtCapacity(ctx, sets)
	if err != nil {
		return AnchorReplacementReceipt{}, err
	}
	if atCapacity {
		return AnchorReplacementReceipt{Busy: true}, nil
	}
	replacement, err := d.prepareOutboundAnchorDocumentReplacement(ctx, sets)
	if err != nil {
		return AnchorReplacementReceipt{}, err
	}
	groups, err := outboundAnchorPublicationGroups(
		replacement.finalizations,
		publicationMaximumEncodedBytes,
	)
	if err != nil {
		return AnchorReplacementReceipt{}, err
	}
	if len(replacement.finalizations) == 0 {
		return AnchorReplacementReceipt{}, nil
	}
	if err := d.replaceOutboundAnchorDocumentTargets(ctx, replacement, visit); err != nil {
		return AnchorReplacementReceipt{}, err
	}
	if err := d.activeDocumentLineageLease(reservation, sources); err != nil {
		return AnchorReplacementReceipt{}, err
	}
	for _, group := range groups {
		if err := d.activeDocumentLineageLease(reservation, sources); err != nil {
			return AnchorReplacementReceipt{}, err
		}
		if err := d.publishOutboundAnchorFinalizations(
			ctx,
			group,
		); err != nil {
			return AnchorReplacementReceipt{}, fmt.Errorf(
				"publish outbound anchor documents: %w",
				err,
			)
		}
	}

	return AnchorReplacementReceipt{}, nil
}

func outboundAnchorPublicationGroups(
	finalizations []OutboundAnchorFinalization,
	maximumEncodedBytes int,
) ([][]OutboundAnchorFinalization, error) {
	if maximumEncodedBytes < 1 {
		return nil, fmt.Errorf("outbound anchor publication byte limit must be positive")
	}
	groups := make([][]OutboundAnchorFinalization, 0)
	first := 0
	encodedBytes := 0
	for index, finalization := range finalizations {
		finalizationEncodedBytes := outboundAnchorFinalizationEncodedByteCeiling(finalization)
		if finalizationEncodedBytes > maximumEncodedBytes {
			return nil, fmt.Errorf("outbound anchor source publication byte limit exceeded")
		}
		if index > first && finalizationEncodedBytes > maximumEncodedBytes-encodedBytes {
			groups = append(groups, finalizations[first:index])
			first = index
			encodedBytes = 0
		}
		encodedBytes += finalizationEncodedBytes
	}
	if first < len(finalizations) {
		groups = append(groups, finalizations[first:])
	}

	return groups, nil
}
