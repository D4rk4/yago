package documentstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func anchorReceiver(t *testing.T, receiver DocumentReceiver) InboundAnchorReceiver {
	t.Helper()
	anchors, ok := receiver.(InboundAnchorReceiver)
	if !ok {
		t.Fatal("document receiver has no inbound anchor capability")
	}

	return anchors
}

func TestReplaceOutboundAnchorsUpdatesStoredAndFutureTargets(t *testing.T) {
	directory, receiver := openDocuments(t)
	targetA := "https://target.example/a"
	targetB := "https://target.example/b"
	source := "https://source.example/page"
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: targetA,
		Title:         "Target A",
		Inlinks:       []AnchorText{{URL: "https://legacy.example/", Text: "legacy"}},
	}}); err != nil {
		t.Fatalf("receive target: %v", err)
	}

	update, err := anchorReceiver(t, receiver).ReplaceOutboundAnchors(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: source,
			Anchors: []OutboundAnchor{
				{TargetURL: targetA, Text: "  trusted   title  "},
				{TargetURL: targetA, Text: "trusted title"},
				{TargetURL: targetA, Text: "community", UserGenerated: true},
				{TargetURL: targetA, Text: "ignored third", Sponsored: true},
				{TargetURL: targetB, Text: strings.Repeat("x", 300), NoFollow: true},
				{TargetURL: source, Text: "self"},
				{Text: "missing target"},
			},
		}},
	)
	if err != nil {
		t.Fatalf("replace anchors: %v", err)
	}
	if update.Busy || len(update.Documents) != 1 || update.Documents[0].NormalizedURL != targetA {
		t.Fatalf("update = %#v", update)
	}
	storedA, found, err := directory.Document(t.Context(), targetA)
	if err != nil || !found {
		t.Fatalf("target A = %#v/%v/%v", storedA, found, err)
	}
	if len(storedA.Inlinks) != 3 || storedA.Inlinks[1].Text != "trusted title" ||
		!storedA.Inlinks[2].Sponsored {
		t.Fatalf("target A anchors = %#v", storedA.Inlinks)
	}

	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: targetB}}); err != nil {
		t.Fatalf("receive future target: %v", err)
	}
	storedB, found, err := directory.Document(t.Context(), targetB)
	if err != nil || !found || len(storedB.Inlinks) != 1 ||
		!storedB.Inlinks[0].NoFollow || len([]rune(storedB.Inlinks[0].Text)) != 256 {
		t.Fatalf("target B = %#v/%v/%v", storedB, found, err)
	}

	update, err = anchorReceiver(t, receiver).ReplaceOutboundAnchors(
		t.Context(),
		[]OutboundAnchorSet{{SourceURL: source, Anchors: []OutboundAnchor{{
			TargetURL: targetB,
			Text:      "replacement",
		}}}},
	)
	if err != nil || len(update.Documents) != 2 {
		t.Fatalf("replace recrawl = %#v/%v", update, err)
	}
	storedA, _, _ = directory.Document(t.Context(), targetA)
	storedB, _, _ = directory.Document(t.Context(), targetB)
	if len(storedA.Inlinks) != 1 || len(storedB.Inlinks) != 1 ||
		storedB.Inlinks[0].Text != "replacement" {
		t.Fatalf("recrawl anchors = %#v / %#v", storedA.Inlinks, storedB.Inlinks)
	}

	update, err = anchorReceiver(t, receiver).ReplaceOutboundAnchors(
		t.Context(),
		[]OutboundAnchorSet{{SourceURL: source}},
	)
	if err != nil || len(update.Documents) != 1 {
		t.Fatalf("clear anchors = %#v/%v", update, err)
	}
	storedB, _, _ = directory.Document(t.Context(), targetB)
	if len(storedB.Inlinks) != 0 {
		t.Fatalf("cleared target B anchors = %#v", storedB.Inlinks)
	}
}

func TestReplaceOutboundAnchorsSkipsUnchangedTargetDocuments(t *testing.T) {
	_, receiver := openDocuments(t)
	target := "https://target.example/page"
	set := OutboundAnchorSet{
		SourceURL: "https://source.example/page",
		Anchors:   []OutboundAnchor{{TargetURL: target, Text: "stable"}},
	}
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: target}}); err != nil {
		t.Fatalf("receive target: %v", err)
	}
	anchors := anchorReceiver(t, receiver)
	first, err := anchors.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{set})
	if err != nil || len(first.Documents) != 1 {
		t.Fatalf("first update = %#v/%v", first, err)
	}
	second, err := anchors.ReplaceOutboundAnchors(t.Context(), []OutboundAnchorSet{set})
	if err != nil || len(second.Documents) != 0 {
		t.Fatalf("unchanged update = %#v/%v, want no documents", second, err)
	}
}

func TestReplaceOutboundAnchorsReplayDropsAbortedAffectedDocuments(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	target := "https://target.example/page"
	if _, err := receiver.Receive(t.Context(), []Document{{NormalizedURL: target}}); err != nil {
		t.Fatal(err)
	}
	engine.replayNext = true
	engine.commitFirst = true
	update, err := anchorReceiver(t, receiver).ReplaceOutboundAnchors(
		t.Context(),
		[]OutboundAnchorSet{{
			SourceURL: "https://source.example/page",
			Anchors:   []OutboundAnchor{{TargetURL: target, Text: "stable"}},
		}},
	)
	if err != nil || len(update.Documents) != 0 {
		t.Fatalf("replayed update = %#v/%v, want no stale affected documents", update, err)
	}
}

func TestReplaceOutboundAnchorsBoundsSourcesAndTargets(t *testing.T) {
	anchors := make([]OutboundAnchor, maximumOutboundAnchors+1)
	for index := range anchors {
		anchors[index] = OutboundAnchor{
			TargetURL: "https://target.example/page",
			Text:      strings.Repeat("x", index%3+1),
		}
	}
	grouped, targets := canonicalOutboundAnchors("https://source.example/", anchors)
	if len(targets) != 1 || len(grouped[targets[0]]) != maximumAnchorsPerSourceTarget {
		t.Fatalf("grouped anchors = %#v / %#v", grouped, targets)
	}

	inbound := make([]AnchorText, 0, maximumInboundAnchors+3)
	for index := range maximumInboundAnchors + 2 {
		inbound = append(inbound, AnchorText{
			URL:  fmt.Sprintf("https://source.example/%03d", index),
			Text: " value ",
		})
	}
	inbound = append(inbound, inbound[0])
	bounded := canonicalAnchorTexts(inbound)
	if len(bounded) != maximumInboundAnchors {
		t.Fatalf("bounded inbound anchors = %d", len(bounded))
	}
}

func TestReplaceOutboundAnchorsHandlesNoopCapacityAndContext(t *testing.T) {
	_, receiver := openDocuments(t)
	anchors := anchorReceiver(t, receiver)
	update, err := anchors.ReplaceOutboundAnchors(t.Context(), nil)
	if err != nil || update.Busy || len(update.Documents) != 0 {
		t.Fatalf("empty update = %#v/%v", update, err)
	}
	update, err = anchors.ReplaceOutboundAnchors(
		t.Context(),
		[]OutboundAnchorSet{{SourceURL: " "}},
	)
	if err != nil || len(update.Documents) != 0 {
		t.Fatalf("blank source update = %#v/%v", update, err)
	}

	v, _, receiver := openDocumentsWithVault(t, 1)
	if _, err := receiver.Receive(t.Context(), []Document{{
		NormalizedURL: "https://target.example/",
	}}); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	update, err = anchorReceiver(t, receiver).ReplaceOutboundAnchors(
		t.Context(),
		[]OutboundAnchorSet{{SourceURL: "https://source.example/", Anchors: []OutboundAnchor{{
			TargetURL: "https://target.example/",
		}}}},
	)
	if err != nil || !update.Busy {
		t.Fatalf("capacity update = %#v/%v", update, err)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := anchorReceiver(t, receiver).ReplaceOutboundAnchors(
		t.Context(),
		[]OutboundAnchorSet{{SourceURL: "https://source.example/", Anchors: []OutboundAnchor{{
			TargetURL: "https://target.example/",
		}}}},
	); err == nil {
		t.Fatal("closed vault should fail capacity check")
	}

	_, receiver = openDocuments(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := anchorReceiver(t, receiver).ReplaceOutboundAnchors(
		ctx,
		[]OutboundAnchorSet{{SourceURL: "https://source.example/"}},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("context error = %v", err)
	}
}

func TestAnchorCollectionsCodecsAndRegistrationErrors(t *testing.T) {
	codec := anchorSliceCodec[[]string]{}
	raw, err := codec.Encode([]string{"a", "b"})
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	decoded, err := codec.Decode(raw)
	if err != nil || len(decoded) != 2 || decoded[1] != "b" {
		t.Fatalf("decode = %#v/%v", decoded, err)
	}
	if _, err := codec.Decode([]byte("{")); err == nil {
		t.Fatal("invalid anchor JSON should fail")
	}

	for _, bucket := range []vault.Name{inboundAnchorBucket, outboundTargetBucket} {
		engine := newScriptedDocumentEngine()
		storage, err := vault.New(engine)
		if err != nil {
			t.Fatalf("new vault: %v", err)
		}
		engine.provisionErrors[bucket] = errors.New("provision failed")
		if _, _, err := Open(storage); err == nil {
			t.Fatalf("bucket %s registration should fail", bucket)
		}
	}
}

func TestReplaceOutboundAnchorsSurfacesStorageFailures(t *testing.T) {
	tests := []anchorStorageFailureCase{
		{
			name: "outbound decode", corruptBucket: outboundTargetBucket,
			corruptKey: anchorFailureSource,
		},
		{
			name: "target document decode", corruptBucket: bucketName,
			corruptKey: anchorFailureTarget,
		},
		{
			name: "inbound decode", corruptBucket: inboundAnchorBucket,
			corruptKey: anchorFailureTarget,
		},
		{name: "inbound put", putBucket: inboundAnchorBucket},
		{name: "outbound put", putBucket: outboundTargetBucket},
		{name: "target document put", seedTarget: true, putBucket: bucketName},
		{
			name: "inbound delete", seedEdge: true,
			deleteBucket: inboundAnchorBucket, clear: true,
		},
		{
			name: "outbound delete", seedEdge: true,
			deleteBucket: outboundTargetBucket, clear: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runAnchorStorageFailureCase(t, test)
		})
	}
}

const (
	anchorFailureSource = "https://source.example/"
	anchorFailureTarget = "https://target.example/"
)

type anchorStorageFailureCase struct {
	name          string
	corruptBucket vault.Name
	corruptKey    string
	putBucket     vault.Name
	deleteBucket  vault.Name
	seedTarget    bool
	seedEdge      bool
	clear         bool
}

func runAnchorStorageFailureCase(t *testing.T, test anchorStorageFailureCase) {
	t.Helper()
	_, receiver, engine := openScriptedDocuments(t)
	anchors := anchorReceiver(t, receiver)
	edge := OutboundAnchorSet{
		SourceURL: anchorFailureSource,
		Anchors:   []OutboundAnchor{{TargetURL: anchorFailureTarget, Text: "anchor"}},
	}
	if test.corruptBucket != "" {
		engine.buckets[test.corruptBucket][test.corruptKey] = []byte("{")
	}
	if test.seedTarget {
		if _, err := receiver.Receive(
			t.Context(),
			[]Document{{NormalizedURL: anchorFailureTarget}},
		); err != nil {
			t.Fatalf("seed target: %v", err)
		}
	}
	if test.seedEdge {
		if _, err := anchors.ReplaceOutboundAnchors(
			t.Context(),
			[]OutboundAnchorSet{edge},
		); err != nil {
			t.Fatalf("seed edge: %v", err)
		}
	}
	if test.putBucket != "" {
		engine.putErrors[test.putBucket] = errors.New("put failed")
	}
	if test.deleteBucket != "" {
		engine.delErrors[test.deleteBucket] = errors.New("delete failed")
	}
	updates := []OutboundAnchorSet{edge}
	if test.clear {
		updates = []OutboundAnchorSet{{SourceURL: anchorFailureSource}}
	}
	if _, err := anchors.ReplaceOutboundAnchors(t.Context(), updates); err == nil {
		t.Fatal("expected storage failure")
	}
}

func TestReplaceOutboundAnchorsChecksContextDuringTargets(t *testing.T) {
	_, receiver := openDocuments(t)
	ctx := &errAfterContext{
		Context:   t.Context(),
		remaining: 2,
		err:       context.Canceled,
	}
	if _, err := anchorReceiver(t, receiver).ReplaceOutboundAnchors(
		ctx,
		[]OutboundAnchorSet{{
			SourceURL: "https://source.example/",
			Anchors: []OutboundAnchor{{
				TargetURL: "https://target.example/",
			}},
		}},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("context error = %v", err)
	}
}

func TestReceiveSurfacesInboundAnchorDecodeError(t *testing.T) {
	_, receiver, engine := openScriptedDocuments(t)
	target := "https://target.example/"
	engine.buckets[inboundAnchorBucket][target] = []byte("{")
	if _, err := receiver.Receive(
		t.Context(), []Document{{NormalizedURL: target}},
	); err == nil {
		t.Fatal("expected inbound anchor decode error")
	}
}

func TestUniqueSortedStringsDropsBlankAndDuplicates(t *testing.T) {
	got := uniqueSortedStrings([]string{" b ", "", "a", "b", " "})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("unique values = %#v", got)
	}
}
