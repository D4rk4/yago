package documentstore

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	maximumOutboundAnchors        = 1024
	maximumAnchorsPerSourceTarget = 2
	maximumInboundAnchors         = 256
	maximumAnchorTextRunes        = 256
)

func (d documentVault) ReplaceOutboundAnchors(
	ctx context.Context,
	sets []OutboundAnchorSet,
) (AnchorUpdate, error) {
	var err error
	sets, err = canonicalOutboundAnchorSets(sets)
	if err != nil {
		return AnchorUpdate{}, err
	}
	if len(sets) == 0 {
		return AnchorUpdate{}, nil
	}
	sources := make([]string, 0, len(sets))
	for _, set := range sets {
		sources = append(sources, set.SourceURL)
	}
	reservation, err := d.ReserveDocumentLineages(ctx, sources)
	if err != nil {
		return AnchorUpdate{}, err
	}

	return d.replaceReservedOutboundAnchors(ctx, reservation, sets, true)
}

func (d documentVault) ReplaceReservedOutboundAnchors(
	ctx context.Context,
	reservation DocumentLineageReservation,
	sets []OutboundAnchorSet,
) (AnchorUpdate, error) {
	var err error
	sets, err = canonicalOutboundAnchorSets(sets)
	if err != nil {
		return AnchorUpdate{}, err
	}
	if len(sets) == 0 {
		err := d.activeDocumentLineageLease(reservation, nil)

		return AnchorUpdate{}, err
	}

	return d.replaceReservedOutboundAnchors(ctx, reservation, sets, false)
}

func (d documentVault) replaceReservedOutboundAnchors(
	ctx context.Context,
	reservation DocumentLineageReservation,
	sets []OutboundAnchorSet,
	releaseReservation bool,
) (AnchorUpdate, error) {
	sources := make([]string, 0, len(sets))
	for _, set := range sets {
		sources = append(sources, set.SourceURL)
	}
	if err := d.activeDocumentLineageLease(reservation, sources); err != nil {
		d.releaseOwnedDocumentLineage(releaseReservation, reservation)

		return AnchorUpdate{}, err
	}
	atCapacity, err := d.outboundAnchorUpdateAtCapacity(ctx, sets)
	if err != nil {
		d.releaseOwnedDocumentLineage(releaseReservation, reservation)

		return AnchorUpdate{}, err
	}
	if atCapacity {
		d.releaseOwnedDocumentLineage(releaseReservation, reservation)

		return AnchorUpdate{Busy: true}, nil
	}
	releaseWrite, err := d.enterStoredDocumentWrite(ctx)
	if err != nil {
		d.releaseOwnedDocumentLineage(releaseReservation, reservation)

		return AnchorUpdate{}, err
	}
	defer releaseWrite()
	targetURLs, err := d.outboundAnchorTargetURLs(ctx, sets)
	if err != nil {
		d.releaseOwnedDocumentLineage(releaseReservation, reservation)

		return AnchorUpdate{}, err
	}
	releaseTargets, err := d.urlBoundaries.lockWrites(ctx, targetURLs)
	if err != nil {
		d.releaseOwnedDocumentLineage(releaseReservation, reservation)

		return AnchorUpdate{}, err
	}
	releaseBoundaries := d.outboundAnchorBoundaryRelease(
		releaseTargets,
		releaseReservation,
		reservation,
	)
	defer releaseCurrentOutboundAnchorBoundaries(&releaseBoundaries)
	urls, finalizations, err := d.replaceOutboundAnchorSets(ctx, sets)
	if err != nil {
		return AnchorUpdate{}, err
	}
	if len(finalizations) > 0 {
		lease := newOutboundAnchorLease(releaseBoundaries, urls)
		for index := range finalizations {
			finalizations[index].lease = lease
		}
		releaseBoundaries = nil
	}

	return AnchorUpdate{Finalizations: finalizations}, nil
}

func (d documentVault) replaceOutboundAnchorSet(
	ctx context.Context,
	tx *vault.Txn,
	set OutboundAnchorSet,
	affected map[string]struct{},
) (OutboundAnchorFinalization, bool, error) {
	sourceURL := strings.TrimSpace(set.SourceURL)
	incoming, incomingTargets := canonicalOutboundAnchors(sourceURL, set.Anchors)
	previous, err := d.readOutboundAnchorPublication(tx, sourceURL)
	if err != nil {
		return OutboundAnchorFinalization{}, false, fmt.Errorf("read outbound targets: %w", err)
	}
	desired := desiredOutboundAnchorPublication(incoming, incomingTargets)
	if outboundAnchorPublicationsEqual(previous, desired) {
		return OutboundAnchorFinalization{}, false, nil
	}
	targets := uniqueSortedStrings(append(previous.Targets, incomingTargets...))
	for _, targetURL := range targets {
		if err := ctx.Err(); err != nil {
			return OutboundAnchorFinalization{}, false, fmt.Errorf("context: %w", err)
		}
		if err := d.replaceTargetAnchors(
			tx,
			sourceURL,
			targetURL,
			incoming[targetURL],
			affected,
		); err != nil {
			return OutboundAnchorFinalization{}, false, err
		}
	}

	return OutboundAnchorFinalization{
		sourceURL: sourceURL,
		expected:  previous,
		desired:   desired,
	}, true, nil
}

func (d documentVault) replaceTargetAnchors(
	tx *vault.Txn,
	sourceURL string,
	targetURL string,
	incoming []AnchorText,
	affected map[string]struct{},
) error {
	key := vault.Key(targetURL)
	doc, location, documentFound, err := d.readStoredDocument(tx, targetURL)
	if err != nil {
		return fmt.Errorf("read anchor target document: %w", err)
	}
	anchors, anchorsFound, err := d.inboundAnchors.Get(tx, key)
	if err != nil {
		return fmt.Errorf("read target anchors: %w", err)
	}
	if !anchorsFound && documentFound {
		anchors = doc.Inlinks
	}
	previousAnchors := append([]AnchorText(nil), anchors...)
	kept := anchors[:0]
	for _, anchor := range anchors {
		if anchor.URL != sourceURL {
			kept = append(kept, anchor)
		}
	}
	anchors = canonicalAnchorTexts(append(kept, incoming...))
	inboundAnchorsChanged := !anchorsFound || !slices.Equal(previousAnchors, anchors)
	if len(anchors) == 0 {
		if anchorsFound {
			if _, err := d.inboundAnchors.Delete(tx, key); err != nil {
				return fmt.Errorf("delete target anchors: %w", err)
			}
		}
	} else if inboundAnchorsChanged {
		if err := d.inboundAnchors.Put(tx, key, anchors); err != nil {
			return fmt.Errorf("store target anchors: %w", err)
		}
	}
	if !documentFound {
		return nil
	}
	if slices.Equal(doc.Inlinks, anchors) {
		affected[targetURL] = struct{}{}

		return nil
	}
	doc.Inlinks = append([]AnchorText(nil), anchors...)
	if err := d.putStoredDocument(tx, location, doc); err != nil {
		return fmt.Errorf("store anchor target document: %w", err)
	}
	affected[targetURL] = struct{}{}

	return nil
}

func canonicalOutboundAnchors(
	sourceURL string,
	anchors []OutboundAnchor,
) (map[string][]AnchorText, []string) {
	grouped := make(map[string][]AnchorText)
	accepted := 0
	for _, anchor := range anchors {
		if accepted >= maximumOutboundAnchors {
			break
		}
		targetURL := strings.TrimSpace(anchor.TargetURL)
		if !validOutboundAnchorIdentity(targetURL) || targetURL == sourceURL {
			continue
		}
		grouped[targetURL] = append(grouped[targetURL], AnchorText{
			URL:           sourceURL,
			Text:          boundedAnchorText(anchor.Text),
			NoFollow:      anchor.NoFollow,
			UserGenerated: anchor.UserGenerated,
			Sponsored:     anchor.Sponsored,
		})
		accepted++
	}
	targets := make([]string, 0, len(grouped))
	for targetURL, values := range grouped {
		values = canonicalAnchorTexts(values)
		if len(values) > maximumAnchorsPerSourceTarget {
			values = values[:maximumAnchorsPerSourceTarget]
		}
		grouped[targetURL] = values
		targets = append(targets, targetURL)
	}
	sort.Strings(targets)

	return grouped, targets
}

func canonicalAnchorTexts(anchors []AnchorText) []AnchorText {
	canonical := make([]AnchorText, 0, min(len(anchors), maximumInboundAnchors))
	seen := make(map[string]struct{}, len(anchors))
	for _, anchor := range anchors {
		anchor.URL = strings.TrimSpace(anchor.URL)
		anchor.Text = boundedAnchorText(anchor.Text)
		key := fmt.Sprintf(
			"%s\x00%s\x00%t\x00%t\x00%t",
			anchor.URL,
			anchor.Text,
			anchor.NoFollow,
			anchor.UserGenerated,
			anchor.Sponsored,
		)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		canonical = append(canonical, anchor)
	}
	sort.Slice(canonical, func(left, right int) bool {
		return anchorTextOrder(canonical[left]) < anchorTextOrder(canonical[right])
	})
	if len(canonical) > maximumInboundAnchors {
		canonical = canonical[:maximumInboundAnchors]
	}

	return canonical
}

func anchorTextOrder(anchor AnchorText) string {
	return fmt.Sprintf(
		"%s\x00%t\x00%t\x00%t\x00%s",
		anchor.URL,
		anchor.NoFollow,
		anchor.UserGenerated,
		anchor.Sponsored,
		anchor.Text,
	)
}

func boundedAnchorText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > maximumAnchorTextRunes {
		return string(runes[:maximumAnchorTextRunes])
	}

	return text
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if !validOutboundAnchorIdentity(value) {
			continue
		}
		if _, found := seen[value]; found {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)

	return unique
}

func validOutboundAnchorIdentity(value string) bool {
	return value != "" && len(value) <= yagomodel.MaximumURLIdentityBytes
}

func outboundAnchorSetsCarryEdges(sets []OutboundAnchorSet) bool {
	for _, set := range sets {
		_, targets := canonicalOutboundAnchors(set.SourceURL, set.Anchors)
		if len(targets) > 0 {
			return true
		}
	}

	return false
}
