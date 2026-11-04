package documentstore

import "slices"

func replacedTargetAnchors(
	replacement outboundAnchorDocumentReplacement,
	targetURL string,
	previousAnchors []AnchorText,
	previousAnchorsStored bool,
) outboundAnchorTargetMutation {
	kept := make([]AnchorText, 0, len(previousAnchors))
	for _, anchor := range previousAnchors {
		if _, replaced := replacement.sources[anchor.URL]; !replaced {
			kept = append(kept, anchor)
		}
	}
	anchors := canonicalAnchorTexts(append(kept, replacement.contributions[targetURL]...))
	mutation := outboundAnchorTargetMutation{anchors: anchors, targetURL: targetURL}
	switch {
	case len(anchors) == 0:
		mutation.deleteAnchors = previousAnchorsStored
	case !previousAnchorsStored || !slices.Equal(previousAnchors, anchors):
		mutation.storeAnchors = true
	}

	return mutation
}
