package documentstore

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func refreshMarkedTargets(t *testing.T, documents documentVault) []string {
	t.Helper()
	var page vault.BucketKeyPage
	if err := documents.vault.View(t.Context(), func(tx *vault.Txn) error {
		var err error
		page, err = tx.ReadBucketKeyPage(inboundAnchorRefreshBucket, nil, 16)
		if err != nil {
			return fmt.Errorf("page refresh marks: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	targets := make([]string, 0, len(page.Keys))
	for _, key := range page.Keys {
		targets = append(targets, string(key))
	}

	return targets
}

func markInboundAnchorRefresh(t *testing.T, documents documentVault, targetURL string) {
	t.Helper()
	if err := documents.vault.Update(t.Context(), func(tx *vault.Txn) error {
		return documents.storeInboundAnchorRefresh(
			tx,
			targetURL,
			inboundAnchorRefreshFor(map[string]struct{}{"https://source.example/": {}}),
		)
	}); err != nil {
		t.Fatal(err)
	}
}

func TestInboundAnchorRefreshSurfacesAdmissionAndLockFailures(t *testing.T) {
	_, receiver := openDocuments(t)
	documents := receiver.(documentVault)
	reindex := func([]Document) error { return nil }
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := documents.refreshInboundAnchorDocuments(
		canceled,
		[]string{"https://target.example/canceled-refresh"},
		reindex,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled refresh admission = %v", err)
	}
	targetURL := "https://target.example/contended-refresh"
	markInboundAnchorRefresh(t, documents, targetURL)
	release, err := documents.urlBoundaries.lockWrites(t.Context(), []string{targetURL})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	deadline, deadlineCancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
	defer deadlineCancel()
	if _, err := documents.RefreshInboundAnchorDocuments(
		deadline,
		outboundAnchorTargetPageSize,
		reindex,
	); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("contended refresh = %v", err)
	}
	if marked := refreshMarkedTargets(t, documents); len(marked) != 1 || marked[0] != targetURL {
		t.Fatalf("contended refresh marks = %#v", marked)
	}
}

func TestInboundAnchorRefreshKeepsMarkWhenTargetStateIsUnreadable(t *testing.T) {
	tests := []struct {
		name   string
		bucket vault.Name
		raw    []byte
	}{
		{name: "document location", bucket: documentLocationBucketName, raw: []byte{1}},
		{name: "refresh mark", bucket: inboundAnchorRefreshBucket, raw: []byte("{")},
		{name: "inbound anchors", bucket: inboundAnchorBucket, raw: []byte("{")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, documents, engine := openDocumentStorageFaultVault(t)
			targetURL := "https://target.example/unreadable"
			markInboundAnchorRefresh(t, documents, targetURL)
			engine.putRaw(test.bucket, vault.Key(targetURL), test.raw)
			reindexed := false
			_, err := documents.RefreshInboundAnchorDocuments(
				t.Context(),
				outboundAnchorTargetPageSize,
				func([]Document) error {
					reindexed = true

					return nil
				},
			)
			if err == nil || reindexed {
				t.Fatalf("unreadable target refresh = %v, reindexed %t", err, reindexed)
			}
			if marked := refreshMarkedTargets(t, documents); len(marked) != 1 {
				t.Fatalf("unreadable target marks = %#v", marked)
			}
		})
	}
}

func TestOutboundAnchorTargetMutationsRefuseNegativeSizeBeforeStoring(t *testing.T) {
	_, receiver := openDocuments(t)
	documents := receiver.(documentVault)
	targetURL := "https://target.example/negative-size"
	err := documents.storeOutboundAnchorTargetMutations(
		t.Context(),
		[]outboundAnchorTargetMutation{{
			targetURL:    targetURL,
			storeAnchors: true,
			anchors:      []AnchorText{{URL: "https://source.example/", Text: "kept out"}},
			encodedBytes: -1,
		}},
	)
	if err == nil {
		t.Fatal("negative mutation size was accepted")
	}
	var stored bool
	if err := documents.vault.View(t.Context(), func(tx *vault.Txn) error {
		var err error
		_, stored, err = documents.inboundAnchors.Get(tx, vault.Key(targetURL))
		if err != nil {
			return fmt.Errorf("read refused anchors: %w", err)
		}

		return nil
	}); err != nil || stored {
		t.Fatalf("refused mutation stored anchors: %t/%v", stored, err)
	}
}

func TestOpenRefusesDuplicateInboundAnchorRefreshCollection(t *testing.T) {
	storage, err := vault.New(newDocumentStorageFaultEngine())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = storage.Close() }()
	if _, err := registerInboundAnchorRefreshes(storage); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Open(storage); err == nil {
		t.Fatal("Open accepted a duplicate inbound anchor refresh collection")
	}
}
