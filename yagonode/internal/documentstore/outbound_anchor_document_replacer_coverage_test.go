package documentstore

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOutboundAnchorDocumentReplacerValidatesPublicInputs(t *testing.T) {
	_, receiver := openDocuments(t)
	documents := receiver.(documentVault)
	tooMany := make([]OutboundAnchorSet, MaximumOutboundAnchorSourcesPerReplacement+1)
	for index := range tooMany {
		tooMany[index].SourceURL = fmt.Sprintf("https://source.example/%02d", index)
	}
	if _, err := documents.ReplaceOutboundAnchorDocuments(
		t.Context(),
		tooMany,
		nil,
	); err == nil {
		t.Fatal("oversized replacement was accepted")
	}
	if receipt, err := documents.ReplaceOutboundAnchorDocuments(
		t.Context(),
		nil,
		nil,
	); err != nil || receipt.Busy {
		t.Fatalf("empty replacement = %#v/%v", receipt, err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := documents.ReplaceOutboundAnchorDocuments(
		canceled,
		[]OutboundAnchorSet{{SourceURL: "https://source.example/canceled"}},
		nil,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled replacement = %v", err)
	}
	if _, err := documents.ReplaceReservedOutboundAnchorDocuments(
		t.Context(),
		nil,
		tooMany,
		nil,
	); err == nil {
		t.Fatal("oversized reserved replacement was accepted")
	}
	if _, err := documents.ReplaceReservedOutboundAnchorDocuments(
		t.Context(),
		nil,
		nil,
		nil,
	); err == nil {
		t.Fatal("empty replacement accepted a nil reservation")
	}
	if _, err := documents.ReplaceReservedOutboundAnchorDocuments(
		t.Context(),
		nil,
		[]OutboundAnchorSet{{SourceURL: "https://source.example/unreserved"}},
		nil,
	); err == nil {
		t.Fatal("replacement accepted a nil reservation")
	}
}

func TestReservedOutboundAnchorDocumentReplacementHandlesEmptyAndRepeatedSets(t *testing.T) {
	_, receiver := openDocuments(t)
	documents := receiver.(documentVault)
	source := "https://source.example/reserved"
	target := "https://target.example/absent"
	reservation, err := documents.ReserveDocumentLineages(t.Context(), []string{source})
	if err != nil {
		t.Fatal(err)
	}
	defer documents.ReleaseDocumentLineages(reservation)
	reservation.(*documentLineageLease).documentLineageReservation()
	if receipt, err := documents.ReplaceReservedOutboundAnchorDocuments(
		t.Context(),
		reservation,
		nil,
		nil,
	); err != nil || receipt.Busy {
		t.Fatalf("empty reserved replacement = %#v/%v", receipt, err)
	}
	sets := []OutboundAnchorSet{{
		SourceURL: source,
		Anchors:   []OutboundAnchor{{TargetURL: target, Text: "target"}},
	}}
	for attempt := 0; attempt < 2; attempt++ {
		if receipt, err := documents.ReplaceReservedOutboundAnchorDocuments(
			t.Context(),
			reservation,
			sets,
			nil,
		); err != nil || receipt.Busy {
			t.Fatalf("reserved replacement %d = %#v/%v", attempt, receipt, err)
		}
	}
}

func TestOutboundAnchorDocumentReplacementSurfacesCapacityPlanningAndPublicationFailures(
	t *testing.T,
) {
	t.Run("capacity", func(t *testing.T) {
		_, receiver, engine := openScriptedDocuments(t)
		documents := receiver.(documentVault)
		want := errors.New("capacity failed")
		engine.quotaBytes = 1
		engine.usedBytesErr = want
		_, err := documents.ReplaceOutboundAnchorDocuments(
			t.Context(),
			outboundAnchorReplacementSet("capacity"),
			nil,
		)
		if !errors.Is(err, want) {
			t.Fatalf("capacity failure = %v", err)
		}
	})
	t.Run("view", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		want := errors.New("view failed")
		engine.viewError = want
		_, err := documents.ReplaceOutboundAnchorDocuments(
			t.Context(),
			outboundAnchorReplacementSet("view"),
			nil,
		)
		if !errors.Is(err, want) {
			t.Fatalf("planning view failure = %v", err)
		}
	})
	t.Run("publication read", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		source := "https://source.example/publication-read"
		engine.putRaw(outboundAnchorPublicationBucket, vault.Key(source), []byte("invalid"))
		_, err := documents.ReplaceOutboundAnchorDocuments(
			t.Context(),
			[]OutboundAnchorSet{{SourceURL: source}},
			nil,
		)
		if err == nil {
			t.Fatal("malformed source publication was accepted")
		}
	})
	t.Run("publication write", func(t *testing.T) {
		_, documents, engine := openDocumentStorageFaultVault(t)
		want := errors.New("publication write failed")
		engine.putErrors[outboundAnchorPublicationBucket] = want
		_, err := documents.ReplaceOutboundAnchorDocuments(
			t.Context(),
			outboundAnchorReplacementSet("publication-write"),
			nil,
		)
		if !errors.Is(err, want) {
			t.Fatalf("publication write failure = %v", err)
		}
	})
}

func TestOutboundAnchorDocumentReplacementAllowsNilVisitorForStoredDocument(t *testing.T) {
	_, receiver := openDocuments(t)
	target := "https://target.example/nil-visitor"
	if _, err := receiver.Receive(
		t.Context(),
		[]Document{{NormalizedURL: target}},
	); err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.(OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
		t.Context(),
		outboundAnchorReplacementSet("nil-visitor"),
		nil,
	); err != nil {
		t.Fatal(err)
	}
}

func outboundAnchorReplacementSet(identity string) []OutboundAnchorSet {
	return []OutboundAnchorSet{{
		SourceURL: "https://source.example/" + identity,
		Anchors: []OutboundAnchor{{
			TargetURL: "https://target.example/" + identity,
			Text:      identity,
		}},
	}}
}
