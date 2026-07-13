package documentstore

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

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
	if len(sets) == 0 {
		return AnchorUpdate{}, nil
	}
	if outboundAnchorSetsCarryEdges(sets) {
		atCapacity, err := d.vault.AtCapacity(ctx)
		if err != nil {
			return AnchorUpdate{}, fmt.Errorf("check capacity: %w", err)
		}
		if atCapacity {
			return AnchorUpdate{Busy: true}, nil
		}
	}

	affected := make(map[string]Document)
	err := d.vault.Update(ctx, func(tx *vault.Txn) error {
		affected = make(map[string]Document)
		for _, set := range sets {
			if err := d.replaceOutboundAnchorSet(ctx, tx, set, affected); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return AnchorUpdate{}, fmt.Errorf("replace outbound anchors: %w", err)
	}
	urls := slices.Sorted(maps.Keys(affected))
	documents := make([]Document, 0, len(urls))
	for _, targetURL := range urls {
		documents = append(documents, affected[targetURL])
	}

	return AnchorUpdate{Documents: documents}, nil
}

func (d documentVault) replaceOutboundAnchorSet(
	ctx context.Context,
	tx *vault.Txn,
	set OutboundAnchorSet,
	affected map[string]Document,
) error {
	sourceURL := strings.TrimSpace(set.SourceURL)
	if sourceURL == "" {
		return nil
	}
	incoming, incomingTargets := canonicalOutboundAnchors(sourceURL, set.Anchors)
	previousTargets, _, err := d.outboundTargets.Get(tx, vault.Key(sourceURL))
	if err != nil {
		return fmt.Errorf("read outbound targets: %w", err)
	}
	targets := uniqueSortedStrings(append(previousTargets, incomingTargets...))
	for _, targetURL := range targets {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("context: %w", err)
		}
		if err := d.replaceTargetAnchors(
			tx,
			sourceURL,
			targetURL,
			incoming[targetURL],
			affected,
		); err != nil {
			return err
		}
	}
	if len(incomingTargets) == 0 {
		if _, err := d.outboundTargets.Delete(tx, vault.Key(sourceURL)); err != nil {
			return fmt.Errorf("delete outbound targets: %w", err)
		}

		return nil
	}
	if err := d.outboundTargets.Put(tx, vault.Key(sourceURL), incomingTargets); err != nil {
		return fmt.Errorf("store outbound targets: %w", err)
	}

	return nil
}

func (d documentVault) replaceTargetAnchors(
	tx *vault.Txn,
	sourceURL string,
	targetURL string,
	incoming []AnchorText,
	affected map[string]Document,
) error {
	key := vault.Key(targetURL)
	doc, documentFound, err := d.collection.Get(tx, key)
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
	if slices.Equal(previousAnchors, anchors) {
		return nil
	}
	if len(anchors) == 0 {
		if _, err := d.inboundAnchors.Delete(tx, key); err != nil {
			return fmt.Errorf("delete target anchors: %w", err)
		}
	} else if err := d.inboundAnchors.Put(tx, key, anchors); err != nil {
		return fmt.Errorf("store target anchors: %w", err)
	}
	if !documentFound {
		return nil
	}
	doc.Inlinks = append([]AnchorText(nil), anchors...)
	if err := d.collection.Put(tx, key, doc); err != nil {
		return fmt.Errorf("store anchor target document: %w", err)
	}
	affected[targetURL] = normalizedDocument(doc)

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
		if targetURL == "" || targetURL == sourceURL {
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
		if value == "" {
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

func outboundAnchorSetsCarryEdges(sets []OutboundAnchorSet) bool {
	for _, set := range sets {
		if len(set.Anchors) > 0 {
			return true
		}
	}

	return false
}
