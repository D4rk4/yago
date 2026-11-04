package documentstore

import (
	"fmt"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOutboundAnchorReplacementKeepsLegacyInlinksOfATargetWithoutAnchorRecord(t *testing.T) {
	directory, receiver, _ := openPagedDocuments(t)
	replacer := receiver.(OutboundAnchorDocumentReplacer)
	target := "https://target.example/legacy"
	legacy := AnchorText{URL: "https://legacy.example/", Text: "legacy"}
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: target,
		Inlinks:       []AnchorText{legacy},
	}}); err != nil {
		t.Fatal(err)
	}
	if _, found := storedInboundAnchorRecord(t, receiver.(documentVault), target); found {
		t.Fatal("a received document must not create an inbound anchor record")
	}

	source := "https://source.example/page"
	refreshed := replaceAndRefreshInboundAnchors(t, replacer, source, target)
	if len(refreshed) != 1 || !documentHasInboundAnchorSource(refreshed[0], source) {
		t.Fatalf("legacy target refreshes = %#v", refreshed)
	}
	want := []AnchorText{legacy, {URL: source, Text: "hub"}}
	if got := storedInlinks(t, directory, target); !slices.Equal(got, want) {
		t.Fatalf("legacy target inlinks = %#v, want %#v", got, want)
	}
}

func TestOutboundAnchorReplacementRecordsLegacyInlinksItConfirms(t *testing.T) {
	directory, receiver, _ := openPagedDocuments(t)
	replacer := receiver.(OutboundAnchorDocumentReplacer)
	target := "https://target.example/confirmed"
	source := "https://source.example/page"
	confirmed := []AnchorText{{URL: source, Text: "hub"}}
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: target,
		Inlinks:       confirmed,
	}}); err != nil {
		t.Fatal(err)
	}

	replaceAndRefreshInboundAnchors(t, replacer, source, target)
	record, found := storedInboundAnchorRecord(t, receiver.(documentVault), target)
	if !found || !slices.Equal(record, confirmed) {
		t.Fatalf("confirmed legacy anchor record = %#v/%t", record, found)
	}
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: target}}); err != nil {
		t.Fatal(err)
	}
	if got := storedInlinks(t, directory, target); !slices.Equal(got, confirmed) {
		t.Fatalf("recrawled target inlinks = %#v, want %#v", got, confirmed)
	}
}

func storedInboundAnchorRecord(
	t *testing.T,
	documents documentVault,
	targetURL string,
) ([]AnchorText, bool) {
	t.Helper()
	var (
		anchors []AnchorText
		found   bool
	)
	err := documents.vault.View(t.Context(), func(tx *vault.Txn) error {
		var err error
		anchors, found, err = documents.inboundAnchors.Get(tx, vault.Key(targetURL))
		if err != nil {
			return fmt.Errorf("read inbound anchor record: %w", err)
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return anchors, found
}
