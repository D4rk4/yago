package rwi

import (
	"context"
	"fmt"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/boltvault"
)

func TestScanWordVisitsMatchingPostings(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)

	if _, err := h.rwi.Receiver.Receive(ctx, []yacymodel.RWIPosting{
		posting("w1", "u1"),
		posting("w1", "u2"),
		posting("w2", "u3"),
	}); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	word := yacymodel.WordHash("w1")
	var visited []yacymodel.RWIPosting
	err := h.rwi.Index.ScanWord(ctx, word, func(entry yacymodel.RWIPosting) (bool, error) {
		visited = append(visited, entry)

		return true, nil
	})
	if err != nil {
		t.Fatalf("ScanWord: %v", err)
	}
	if len(visited) != 2 {
		t.Fatalf("visited %d postings, want 2", len(visited))
	}
	for _, entry := range visited {
		if entry.WordHash != word {
			t.Fatalf("entry word hash = %q, want %q", entry.WordHash, word)
		}
	}
}

func TestScanWordStopsWhenVisitorStops(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)

	if _, err := h.rwi.Receiver.Receive(ctx, []yacymodel.RWIPosting{
		posting("w1", "u1"),
		posting("w1", "u2"),
	}); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	visited := 0
	err := h.rwi.Index.ScanWord(
		ctx,
		yacymodel.WordHash("w1"),
		func(yacymodel.RWIPosting) (bool, error) {
			visited++

			return false, nil
		},
	)
	if err != nil {
		t.Fatalf("ScanWord: %v", err)
	}
	if visited != 1 {
		t.Fatalf("visited %d postings, want 1 before stop", visited)
	}
}

func TestPurgeReferencesDropsPostingsAndReferences(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)

	if _, err := h.rwi.Receiver.Receive(ctx, []yacymodel.RWIPosting{
		posting("w1", "u1"),
		posting("w1", "u2"),
		posting("w2", "u1"),
	}); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	target := referencedHash(t, posting("w1", "u1"))
	var result PurgeResult
	err := h.vault.Update(ctx, func(tx *boltvault.Txn) error {
		var purgeErr error
		result, purgeErr = h.rwi.Directory.PurgeReferences(tx, []yacymodel.Hash{target})
		if purgeErr != nil {
			return fmt.Errorf("purge references: %w", purgeErr)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("PurgeReferences: %v", err)
	}
	if result.PostingsDeleted != 2 || result.ReferencesDeleted != 1 {
		t.Fatalf("result = %+v, want 2 postings and 1 reference", result)
	}

	rwiCount, err := h.rwi.Directory.RWICount(ctx)
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}
	if rwiCount != 1 {
		t.Fatalf("RWICount = %d, want 1", rwiCount)
	}
	refCount, err := h.rwi.Directory.ReferencedURLCount(ctx)
	if err != nil {
		t.Fatalf("ReferencedURLCount: %v", err)
	}
	if refCount != 1 {
		t.Fatalf("ReferencedURLCount = %d, want 1", refCount)
	}
}
