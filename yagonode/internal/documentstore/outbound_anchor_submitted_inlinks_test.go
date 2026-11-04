package documentstore

import (
	"slices"
	"testing"
)

type targetAnchorReplacement func(t *testing.T, receiver DocumentReceiver, sourceURL, targetURL string)

func targetAnchorReplacements() map[string]targetAnchorReplacement {
	return map[string]targetAnchorReplacement{
		"document replacement": func(t *testing.T, receiver DocumentReceiver, sourceURL, targetURL string) {
			t.Helper()
			replaceAndRefreshInboundAnchors(
				t,
				receiver.(OutboundAnchorDocumentReplacer),
				sourceURL,
				targetURL,
			)
		},
		"anchor update": func(t *testing.T, receiver DocumentReceiver, sourceURL, targetURL string) {
			t.Helper()
			replaceAndFinalizeOutboundAnchors(t, receiver, []OutboundAnchorSet{{
				SourceURL: sourceURL,
				Anchors:   []OutboundAnchor{{TargetURL: targetURL, Text: "hub"}},
			}})
		},
	}
}

func TestOutboundAnchorReplacementKeepsSubmittedInlinksOfOtherSources(t *testing.T) {
	for name, replace := range targetAnchorReplacements() {
		t.Run(name, func(t *testing.T) {
			directory, receiver, _ := openPagedDocuments(t)
			target := "https://target.example/submitted"
			legacy := AnchorText{URL: "https://legacy.example/", Text: "legacy"}
			submitted := AnchorText{URL: "https://submitted.example/", Text: "submitted"}
			first := AnchorText{URL: "https://first.example/", Text: "hub"}
			second := AnchorText{URL: "https://second.example/", Text: "hub"}
			receiveTargetWithInlinks(t, receiver, target, legacy)
			replace(t, receiver, first.URL, target)
			receiveTargetWithInlinks(t, receiver, target, submitted)

			replace(t, receiver, second.URL, target)
			want := []AnchorText{first, legacy, second, submitted}
			if got := storedInlinks(t, directory, target); !slices.Equal(got, want) {
				t.Fatalf("target inlinks after replacement = %#v, want %#v", got, want)
			}

			receiveTargetWithInlinks(t, receiver, target)
			want = []AnchorText{first, legacy, second}
			if got := storedInlinks(t, directory, target); !slices.Equal(got, want) {
				t.Fatalf("target inlinks after resubmission = %#v, want %#v", got, want)
			}
		})
	}
}

func TestOutboundAnchorReplacementSupersedesSubmittedInlinksOfTheReplacedSource(t *testing.T) {
	for name, replace := range targetAnchorReplacements() {
		t.Run(name, func(t *testing.T) {
			directory, receiver, _ := openPagedDocuments(t)
			target := "https://target.example/superseded"
			first := AnchorText{URL: "https://first.example/", Text: "hub"}
			second := AnchorText{URL: "https://second.example/", Text: "hub"}
			receiveTargetWithInlinks(t, receiver, target)
			replace(t, receiver, first.URL, target)
			receiveTargetWithInlinks(
				t,
				receiver,
				target,
				AnchorText{URL: second.URL, Text: "stale"},
			)

			replace(t, receiver, second.URL, target)
			want := []AnchorText{first, second}
			if got := storedInlinks(t, directory, target); !slices.Equal(got, want) {
				t.Fatalf("target inlinks after replacement = %#v, want %#v", got, want)
			}
		})
	}
}

func receiveTargetWithInlinks(
	t *testing.T,
	receiver DocumentReceiver,
	targetURL string,
	inlinks ...AnchorText,
) {
	t.Helper()
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: targetURL,
		Inlinks:       inlinks,
	}}); err != nil {
		t.Fatal(err)
	}
}
