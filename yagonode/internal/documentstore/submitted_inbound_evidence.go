package documentstore

func retainSubmittedInlinks(document Document) Document {
	if document.submittedInlinksKnown {
		return document
	}
	document.Inlinks = canonicalAnchorTexts(document.Inlinks)
	document.submittedInlinks = append([]AnchorText(nil), document.Inlinks...)
	document.submittedInlinksKnown = true

	return document
}

func restoreSubmittedInlinks(document Document) Document {
	if !document.submittedInlinksKnown {
		return document
	}
	document.Inlinks = append([]AnchorText(nil), document.submittedInlinks...)

	return document
}

func retainDocumentRevisionInlinks(
	document Document,
	materialized []AnchorText,
) Document {
	document.Inlinks = canonicalAnchorTexts(document.Inlinks)
	materialized = canonicalAnchorTexts(materialized)
	materializedEvidence := make(map[AnchorText]struct{}, len(materialized))
	for _, anchor := range materialized {
		materializedEvidence[anchor] = struct{}{}
	}
	document.submittedInlinks = make([]AnchorText, 0, len(document.Inlinks))
	for _, anchor := range document.Inlinks {
		if _, found := materializedEvidence[anchor]; found {
			continue
		}
		document.submittedInlinks = append(document.submittedInlinks, anchor)
	}
	document.submittedInlinksKnown = true

	return document
}
