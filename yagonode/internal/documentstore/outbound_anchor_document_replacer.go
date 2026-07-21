package documentstore

import (
	"context"
)

type AnchorReplacementReceipt struct {
	Busy bool
}

type OutboundAnchorDocumentReplacer interface {
	ReplaceOutboundAnchorDocuments(
		context.Context,
		[]OutboundAnchorSet,
		func([]Document) error,
	) (AnchorReplacementReceipt, error)
}

type ReservedOutboundAnchorDocumentReplacer interface {
	ReplaceReservedOutboundAnchorDocuments(
		context.Context,
		DocumentLineageReservation,
		[]OutboundAnchorSet,
		func([]Document) error,
	) (AnchorReplacementReceipt, error)
}

func (d documentVault) ReplaceOutboundAnchorDocuments(
	ctx context.Context,
	sets []OutboundAnchorSet,
	visit func([]Document) error,
) (AnchorReplacementReceipt, error) {
	canonical, err := canonicalOutboundAnchorSets(sets)
	if err != nil {
		return AnchorReplacementReceipt{}, err
	}
	if len(canonical) == 0 {
		return AnchorReplacementReceipt{}, nil
	}
	sources := make([]string, 0, len(canonical))
	for _, set := range canonical {
		sources = append(sources, set.SourceURL)
	}
	reservation, err := d.ReserveDocumentLineages(ctx, sources)
	if err != nil {
		return AnchorReplacementReceipt{}, err
	}
	defer d.ReleaseDocumentLineages(reservation)

	return d.replaceReservedOutboundAnchorDocuments(
		ctx,
		reservation,
		canonical,
		visit,
	)
}

func (d documentVault) ReplaceReservedOutboundAnchorDocuments(
	ctx context.Context,
	reservation DocumentLineageReservation,
	sets []OutboundAnchorSet,
	visit func([]Document) error,
) (AnchorReplacementReceipt, error) {
	canonical, err := canonicalOutboundAnchorSets(sets)
	if err != nil {
		return AnchorReplacementReceipt{}, err
	}
	if len(canonical) == 0 {
		if err := d.activeDocumentLineageLease(reservation, nil); err != nil {
			return AnchorReplacementReceipt{}, err
		}

		return AnchorReplacementReceipt{}, nil
	}

	return d.replaceReservedOutboundAnchorDocuments(
		ctx,
		reservation,
		canonical,
		visit,
	)
}

func (d documentVault) replaceReservedOutboundAnchorDocuments(
	ctx context.Context,
	reservation DocumentLineageReservation,
	sets []OutboundAnchorSet,
	visit func([]Document) error,
) (AnchorReplacementReceipt, error) {
	return d.replaceReservedOutboundAnchorDocumentsWithin(
		ctx,
		reservation,
		sets,
		visit,
		outboundAnchorPublicationMaximumEncodedBytes,
	)
}

var (
	_ OutboundAnchorDocumentReplacer         = documentVault{}
	_ ReservedOutboundAnchorDocumentReplacer = documentVault{}
)
