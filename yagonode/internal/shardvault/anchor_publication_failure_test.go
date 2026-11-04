package shardvault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	anchorPublicationContributions vault.Name = "document_inbound_anchors"
	anchorPublicationSources       vault.Name = "document_outbound_anchor_publications"
	anchorPublicationSource                   = "https://source.example/publication"
)

func TestOutboundAnchorPublicationReplaysPartialShardCommitsAfterReopen(t *testing.T) {
	for _, replacement := range []struct {
		name     string
		previous string
		desired  string
	}{
		{name: "addition", desired: "current anchor"},
		{name: "replacement", previous: "previous anchor", desired: "current anchor"},
		{name: "removal", previous: "previous anchor"},
	} {
		for _, refused := range []vault.Name{anchorPublicationContributions, anchorRefreshMarks} {
			for _, refreshBeforeReplay := range []bool{false, true} {
				name := fmt.Sprintf(
					"%s/%s/refresh_before_replay_%t",
					replacement.name,
					refused,
					refreshBeforeReplay,
				)
				t.Run(name, func(t *testing.T) {
					anchorPublicationFailureReplay(
						t,
						replacement.previous,
						replacement.desired,
						refused,
						refreshBeforeReplay,
					)
				})
			}
		}
	}
}

func anchorPublicationFailureReplay(
	t *testing.T,
	previous string,
	desired string,
	refused vault.Name,
	refreshBeforeReplay bool,
) {
	t.Helper()
	directory := t.TempDir()
	shards, storage, documents, receiver := anchorRefreshOpenStorage(t, directory)
	target := anchorPublicationTarget(t, shards)
	if _, err := receiver.Receive(
		t.Context(),
		[]documentstore.Document{{NormalizedURL: target}},
	); err != nil {
		t.Fatal(err)
	}
	if previous != "" {
		anchorPublicationReplace(t, receiver, target, previous)
		anchorPublicationRefresh(t, receiver)
	}
	before := anchorRefreshDocument(t, documents, target)
	publicationBefore := anchorPublishedSource(t, shards)
	refusedShard := shards.shards[shards.route(refused, vault.Key(target))]
	anchorPublicationRefuseCommit(t, receiver, target, desired, refusedShard)
	anchorPublicationUnchanged(t, shards, publicationBefore)
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	shards, storage, documents, receiver = anchorRefreshOpenStorage(t, directory)
	anchorPublicationUnchanged(t, shards, publicationBefore)
	partial := desired
	if refused == anchorPublicationContributions {
		partial = previous
	}
	wantPartial := anchorPublicationInlinks(partial)
	if anchors := anchorPublicationAnchors(t, shards, target); !slices.Equal(anchors, wantPartial) {
		t.Fatalf("durable partial anchors = %#v, want %#v", anchors, wantPartial)
	}
	pending := anchorRefreshMarkPresent(t, storage, target)
	if pending != (refused == anchorPublicationContributions) {
		t.Fatalf("durable refresh mark = %t, refused %s", pending, refused)
	}
	if current := anchorRefreshDocument(t, documents, target); !reflect.DeepEqual(current, before) {
		t.Fatalf("target document changed before refresh: %#v", current)
	}
	if refreshBeforeReplay {
		anchorPublicationRefresh(t, receiver)
	}
	anchorPublicationReplace(t, receiver, target, desired)
	publicationAfter := anchorPublishedSource(t, shards)
	if bytes.Equal(publicationAfter, publicationBefore) {
		t.Fatal("successful replay did not advance source publication")
	}
	anchorPublicationRefresh(t, receiver)
	want := anchorPublicationInlinks(desired)
	if anchors := anchorPublicationAnchors(t, shards, target); !slices.Equal(anchors, want) {
		t.Fatalf("replayed anchors = %#v, want %#v", anchors, want)
	}
	current := anchorRefreshDocument(t, documents, target)
	if !slices.Equal(current.Inlinks, want) {
		t.Fatalf("replayed document inlinks = %#v, want %#v", current.Inlinks, want)
	}
	anchorPublicationReplace(t, receiver, target, desired)
	anchorPublicationUnchanged(t, shards, publicationAfter)
	if anchorRefreshMarkPresent(t, storage, target) {
		t.Fatal("unchanged replay created a refresh mark")
	}
	if unchanged := anchorRefreshDocument(
		t,
		documents,
		target,
	); !reflect.DeepEqual(
		unchanged,
		current,
	) {
		t.Fatalf("unchanged replay changed target document: %#v", unchanged)
	}
}

func anchorPublicationRefuseCommit(
	t *testing.T,
	receiver documentstore.DocumentReceiver,
	target string,
	desired string,
	refused *bolt.DB,
) {
	t.Helper()
	refusal := errors.New("injected anchor publication commit failure")
	realCommit := commitTx
	t.Cleanup(func() { commitTx = realCommit })
	commitTx = func(tx *bolt.Tx) error {
		if tx.DB() == refused {
			return refusal
		}

		return realCommit(tx)
	}
	_, err := receiver.(documentstore.OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
		t.Context(),
		anchorPublicationReplacement(target, desired),
	)
	commitTx = realCommit
	if !errors.Is(err, refusal) {
		t.Fatalf("publication failure = %v, want %v", err, refusal)
	}
}

func anchorPublicationTarget(t *testing.T, shards *engine) string {
	t.Helper()
	publicationShard := shards.route(anchorPublicationSources, vault.Key(anchorPublicationSource))
	for index := range 1000 {
		target := fmt.Sprintf("https://target.example/publication/%d", index)
		anchors := shards.route(anchorPublicationContributions, vault.Key(target))
		refresh := shards.route(anchorRefreshMarks, vault.Key(target))
		if anchors != refresh && anchors != publicationShard && refresh != publicationShard {
			return target
		}
	}
	t.Fatal("no target on distinct anchor, refresh, and source publication shards")

	return ""
}

func anchorPublicationReplacement(target string, text string) []documentstore.OutboundAnchorSet {
	set := documentstore.OutboundAnchorSet{SourceURL: anchorPublicationSource}
	if text != "" {
		set.Anchors = []documentstore.OutboundAnchor{{TargetURL: target, Text: text}}
	}

	return []documentstore.OutboundAnchorSet{set}
}

func anchorPublicationReplace(
	t *testing.T,
	receiver documentstore.DocumentReceiver,
	target string,
	text string,
) {
	t.Helper()
	receipt, err := receiver.(documentstore.OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
		t.Context(),
		anchorPublicationReplacement(target, text),
	)
	if err != nil || receipt.Busy {
		t.Fatalf("replace outbound anchors = %#v/%v", receipt, err)
	}
}

func anchorPublicationRefresh(t *testing.T, receiver documentstore.DocumentReceiver) {
	t.Helper()
	receipt, err := receiver.(documentstore.InboundAnchorDocumentRefresher).RefreshInboundAnchorDocuments(
		t.Context(),
		16,
		func([]documentstore.Document) error { return nil },
	)
	if err != nil || receipt.More {
		t.Fatalf("refresh anchor target = %#v/%v", receipt, err)
	}
}

func anchorPublicationRecord(t *testing.T, shards *engine, bucket vault.Name, key string) []byte {
	t.Helper()
	var record []byte
	if err := shards.View(t.Context(), func(tx vault.EngineTxn) error {
		record = bytes.Clone(tx.Bucket(bucket).Get(vault.Key(key)))

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	return record
}

func anchorPublicationUnchanged(t *testing.T, shards *engine, expected []byte) {
	t.Helper()
	publication := anchorPublishedSource(t, shards)
	if !bytes.Equal(publication, expected) {
		t.Fatalf("source publication = %s, want unchanged %s", publication, expected)
	}
}

func anchorPublishedSource(t *testing.T, shards *engine) []byte {
	t.Helper()

	return anchorPublicationRecord(t, shards, anchorPublicationSources, anchorPublicationSource)
}

func anchorPublicationAnchors(
	t *testing.T,
	shards *engine,
	target string,
) []documentstore.AnchorText {
	t.Helper()
	var anchors []documentstore.AnchorText
	record := anchorPublicationRecord(t, shards, anchorPublicationContributions, target)
	if record != nil {
		if err := json.Unmarshal(record, &anchors); err != nil {
			t.Fatal(err)
		}
	}

	return anchors
}

func anchorPublicationInlinks(text string) []documentstore.AnchorText {
	if text == "" {
		return nil
	}

	return []documentstore.AnchorText{{URL: anchorPublicationSource, Text: text}}
}
