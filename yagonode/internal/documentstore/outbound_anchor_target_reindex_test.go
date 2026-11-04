package documentstore

import (
	"fmt"
	"slices"
	"testing"
)

func TestOutboundAnchorReplacementReindexesOnlyTargetsWhoseAnchorsItChanges(t *testing.T) {
	directory, receiver, _ := openPagedDocuments(t)
	replacer := receiver.(OutboundAnchorDocumentReplacer)
	hub := "https://hub.example/"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: hub}}); err != nil {
		t.Fatal(err)
	}
	fillInboundAnchorCapacity(t, replacer, hub)
	before := storedInlinks(t, directory, hub)

	late := "https://z.example/late"
	if refreshed := replaceAndRefreshInboundAnchors(t, replacer, late, hub); len(refreshed) != 0 {
		t.Fatalf("source outside the anchor capacity re-indexed %v", refreshed)
	}
	if after := storedInlinks(t, directory, hub); !slices.Equal(after, before) {
		t.Fatalf(
			"hub anchors changed by a source outside capacity: %d -> %d",
			len(before),
			len(after),
		)
	}
	documents := receiver.(documentVault)
	publication := readOutboundAnchorReplacementPublication(t, documents, late)
	if !slices.Equal(publication.Targets, []string{hub}) {
		t.Fatalf("late source publication = %#v", publication.Targets)
	}

	early := "https://0.example/early"
	refreshed := replaceAndRefreshInboundAnchors(t, replacer, early, hub)
	if len(refreshed) != 1 || !documentHasInboundAnchorSource(refreshed[0], early) {
		t.Fatalf("source inside the anchor capacity re-indexed %#v", refreshed)
	}
}

func fillInboundAnchorCapacity(
	t *testing.T,
	replacer OutboundAnchorDocumentReplacer,
	targetURL string,
) {
	t.Helper()
	fillInboundAnchorCapacityWithoutRefresh(t, replacer, targetURL)
	drainInboundAnchorRefreshes(t, replacer.(DocumentReceiver))
}

func fillInboundAnchorCapacityWithoutRefresh(
	t *testing.T,
	replacer OutboundAnchorDocumentReplacer,
	targetURL string,
) {
	t.Helper()
	for first := 0; first < maximumInboundAnchors; first += MaximumOutboundAnchorSourcesPerReplacement {
		sets := make([]OutboundAnchorSet, 0, MaximumOutboundAnchorSourcesPerReplacement)
		for sequence := first; sequence < first+MaximumOutboundAnchorSourcesPerReplacement; sequence++ {
			sets = append(sets, OutboundAnchorSet{
				SourceURL: fmt.Sprintf("https://a.example/%04d", sequence),
				Anchors:   []OutboundAnchor{{TargetURL: targetURL, Text: "hub"}},
			})
		}
		if _, err := replacer.ReplaceOutboundAnchorDocuments(
			t.Context(),
			sets,
		); err != nil {
			t.Fatal(err)
		}
	}
}

func replaceAndRefreshInboundAnchors(
	t *testing.T,
	replacer OutboundAnchorDocumentReplacer,
	sourceURL string,
	targetURL string,
) []Document {
	t.Helper()
	if _, err := replacer.ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: sourceURL,
			Anchors:   []OutboundAnchor{{TargetURL: targetURL, Text: "hub"}},
		}},
	); err != nil {
		t.Fatal(err)
	}

	return drainInboundAnchorRefreshes(t, replacer.(DocumentReceiver))
}

func storedInlinks(t *testing.T, directory DocumentDirectory, normalizedURL string) []AnchorText {
	t.Helper()
	document, found, err := directory.Document(t.Context(), normalizedURL)
	if err != nil || !found {
		t.Fatalf("stored document %s = %t/%v", normalizedURL, found, err)
	}

	return document.Inlinks
}
