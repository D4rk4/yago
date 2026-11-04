package shardvault

import (
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/documentstore"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const anchorRefreshMarks vault.Name = "document_inbound_anchor_refreshes"

func anchorRefreshOpenStorage(
	t *testing.T,
	directory string,
) (*engine, *vault.Vault, documentstore.DocumentDirectory, documentstore.DocumentReceiver) {
	t.Helper()
	shards, err := openEngine(directory, 1<<30)
	if err != nil {
		t.Fatal(err)
	}
	storage, err := vaultOverEngine(shards)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	documents, receiver, err := documentstore.Open(storage)
	if err != nil {
		t.Fatal(err)
	}

	return shards, storage, documents, receiver
}

func anchorRefreshTarget(t *testing.T, shards *engine) string {
	t.Helper()
	for index := range 1000 {
		target := fmt.Sprintf("https://target.example/refresh/%d", index)
		if shards.route("documents_ordered", anchorRefreshDocumentKey(target)) !=
			shards.route(anchorRefreshMarks, vault.Key(target)) {
			return target
		}
	}
	t.Fatal("no target on distinct document and refresh shards")

	return ""
}

func anchorRefreshDocumentKey(target string) vault.Key {
	return append(binary.BigEndian.AppendUint64(nil, 1), target...)
}

func anchorRefreshMarkPresent(t *testing.T, storage *vault.Vault, target string) bool {
	t.Helper()
	var present bool
	if err := storage.View(t.Context(), func(tx *vault.Txn) error {
		page, err := tx.ReadBucketKeyPage(anchorRefreshMarks, nil, 100)
		if err != nil {
			return fmt.Errorf("read refresh marks: %w", err)
		}
		present = slices.ContainsFunc(page.Keys, func(key vault.Key) bool {
			return string(key) == target
		})

		return nil
	}); err != nil {
		t.Fatal(err)
	}

	return present
}

func anchorRefreshReplace(t *testing.T, receiver documentstore.DocumentReceiver, target string) {
	t.Helper()
	if _, err := receiver.(documentstore.OutboundAnchorDocumentReplacer).ReplaceOutboundAnchorDocuments(
		t.Context(),
		[]documentstore.OutboundAnchorSet{{
			SourceURL: "https://source.example/anchor",
			Anchors: []documentstore.OutboundAnchor{{
				TargetURL: target,
				Text:      "current anchor",
			}},
		}},
	); err != nil {
		t.Fatal(err)
	}
}

func anchorRefreshDocument(
	t *testing.T,
	documents documentstore.DocumentDirectory,
	target string,
) documentstore.Document {
	t.Helper()
	document, found, err := documents.Document(t.Context(), target)
	if err != nil || !found {
		t.Fatalf("stored target = %t/%v", found, err)
	}

	return document
}

func TestInboundAnchorRefreshRetainsRetryAcrossShardCommitFailures(t *testing.T) {
	for _, failure := range []string{"document", "mark", "none"} {
		t.Run(failure, func(t *testing.T) {
			anchorRefreshCommitFailure(t, failure)
		})
	}
}

func anchorRefreshCommitFailure(t *testing.T, failure string) {
	t.Helper()
	directory := t.TempDir()
	shards, storage, documents, receiver := anchorRefreshOpenStorage(t, directory)
	target := anchorRefreshTarget(t, shards)
	if _, err := receiver.Receive(
		t.Context(),
		[]documentstore.Document{{NormalizedURL: target}},
	); err != nil {
		t.Fatal(err)
	}
	before := anchorRefreshDocument(t, documents, target)
	anchorRefreshReplace(t, receiver, target)
	restore, refusal := anchorRefreshRefuseCommit(t, shards, target, failure)
	indexed := anchorRefreshRun(t, receiver, refusal)
	restore()
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	_, storage, documents, receiver = anchorRefreshOpenStorage(t, directory)
	pending := anchorRefreshMarkPresent(t, storage, target)
	if pending != (failure != "none") {
		t.Fatalf("refresh mark after %s commit failure = %t; retry evidence lost", failure, pending)
	}
	after := anchorRefreshDocument(t, documents, target)
	wantedDocument := indexed
	wantedRefreshes := 0
	if failure == "document" {
		wantedDocument = before
		wantedRefreshes = 1
	}
	if !reflect.DeepEqual(after, wantedDocument) {
		t.Fatalf("persisted target = %#v, want %#v", after, wantedDocument)
	}
	anchorRefreshRetry(t, receiver, wantedRefreshes)
	if anchorRefreshMarkPresent(t, storage, target) {
		t.Fatal("successful retry retained refresh mark")
	}
	after = anchorRefreshDocument(t, documents, target)
	if !reflect.DeepEqual(after, indexed) {
		t.Fatalf("retried target = %#v, want %#v", after, indexed)
	}
}

func anchorRefreshRefuseCommit(
	t *testing.T,
	shards *engine,
	target string,
	failure string,
) (func(), error) {
	t.Helper()
	realCommit := commitTx
	restore := func() { commitTx = realCommit }
	t.Cleanup(restore)
	var refused *bolt.DB
	switch failure {
	case "document":
		refused = shards.shards[shards.route("documents_ordered", anchorRefreshDocumentKey(target))]
	case "mark":
		refused = shards.shards[shards.route(anchorRefreshMarks, vault.Key(target))]
	case "none":
		return restore, nil
	}
	refusal := errors.New("injected anchor refresh commit failure")
	commitTx = func(tx *bolt.Tx) error {
		if tx.DB() == refused {
			return refusal
		}

		return realCommit(tx)
	}

	return restore, refusal
}

func anchorRefreshRun(
	t *testing.T,
	receiver documentstore.DocumentReceiver,
	refusal error,
) documentstore.Document {
	t.Helper()
	var indexed []documentstore.Document
	receipt, err := receiver.(documentstore.InboundAnchorDocumentRefresher).RefreshInboundAnchorDocuments(
		t.Context(),
		16,
		func(batch []documentstore.Document) error {
			indexed = append(indexed, batch...)

			return nil
		},
	)
	if !errors.Is(err, refusal) {
		t.Fatalf("refresh error = %v, want %v", err, refusal)
	}
	if refusal == nil && (receipt.Refreshed != 1 || receipt.More) {
		t.Fatalf("successful refresh = %#v", receipt)
	}
	if len(indexed) != 1 || !slices.Equal(indexed[0].Inlinks, []documentstore.AnchorText{{
		URL: "https://source.example/anchor", Text: "current anchor",
	}}) {
		t.Fatalf("indexed targets = %#v", indexed)
	}

	return indexed[0]
}

func anchorRefreshRetry(t *testing.T, receiver documentstore.DocumentReceiver, wanted int) {
	t.Helper()
	reindexed := 0
	receipt, err := receiver.(documentstore.InboundAnchorDocumentRefresher).RefreshInboundAnchorDocuments(
		t.Context(),
		16,
		func(batch []documentstore.Document) error {
			reindexed += len(batch)

			return nil
		},
	)
	if err != nil || receipt.Refreshed != wanted || receipt.More || reindexed != wanted {
		t.Fatalf("retried refresh = %#v/%d/%v, want %d", receipt, reindexed, err, wanted)
	}
}

func TestInboundAnchorRefreshMissingAndUnchangedTargetsOnlyCommitMarkDeletion(t *testing.T) {
	for _, state := range []string{"missing", "unchanged"} {
		t.Run(state, func(t *testing.T) {
			anchorRefreshUnchanged(t, state)
		})
	}
}

func anchorRefreshUnchanged(t *testing.T, state string) {
	t.Helper()
	shards, storage, documents, receiver := anchorRefreshOpenStorage(t, t.TempDir())
	target := anchorRefreshTarget(t, shards)
	if state == "unchanged" {
		if _, err := receiver.Receive(
			t.Context(),
			[]documentstore.Document{{NormalizedURL: target}},
		); err != nil {
			t.Fatal(err)
		}
	}
	before, foundBefore, err := documents.Document(t.Context(), target)
	if err != nil {
		t.Fatal(err)
	}
	if err := shards.Update(t.Context(), func(tx vault.EngineTxn) error {
		return tx.Bucket(anchorRefreshMarks).Put(vault.Key(target), []byte(`{}`))
	}); err != nil {
		t.Fatal(err)
	}
	realCommit := commitTx
	t.Cleanup(func() { commitTx = realCommit })
	var commits atomic.Int32
	commitTx = func(tx *bolt.Tx) error {
		if tx.DB() != shards.shards[shards.route(anchorRefreshMarks, vault.Key(target))] {
			return errors.New("unchanged refresh wrote a non-mark shard")
		}
		commits.Add(1)

		return realCommit(tx)
	}
	anchorRefreshRetry(t, receiver, 0)
	commitTx = realCommit
	if commits.Load() != 1 || anchorRefreshMarkPresent(t, storage, target) {
		t.Fatalf("unchanged refresh did not commit only mark deletion: commits %d", commits.Load())
	}
	after, foundAfter, err := documents.Document(t.Context(), target)
	if err != nil || foundAfter != foundBefore || !reflect.DeepEqual(after, before) {
		t.Fatalf("unchanged target altered: %#v/%t/%v", after, foundAfter, err)
	}
}
