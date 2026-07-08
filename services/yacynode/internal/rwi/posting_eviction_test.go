package rwi

import (
	"context"
	"fmt"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

type recordingObserver struct {
	purged []yacymodel.Hash
}

func (o *recordingObserver) PostingStored(_ *vault.Txn, _, _ yacymodel.Hash) error {
	return nil
}

func (o *recordingObserver) PostingPurged(_ *vault.Txn, word, _ yacymodel.Hash) error {
	o.purged = append(o.purged, word)

	return nil
}

func TestPurgePostingDropsPostingAndNotifies(t *testing.T) {
	ctx := context.Background()
	h := openHarness(t, 0, 100)

	if _, err := h.rwi.Receiver.Receive(ctx, []yacymodel.RWIPosting{
		posting("w1", "u1"),
		posting("w1", "u2"),
		posting("w2", "u1"),
	}); err != nil {
		t.Fatalf("Intake: %v", err)
	}

	word := yacymodel.WordHash("w1")
	url := referencedHash(t, posting("w1", "u1"))
	var deleted bool
	if err := h.vault.Update(ctx, func(tx *vault.Txn) error {
		dropped, err := h.rwi.Purger.PurgePosting(tx, word, url)
		if err != nil {
			return fmt.Errorf("purge posting: %w", err)
		}
		deleted = dropped

		return nil
	}); err != nil {
		t.Fatalf("PurgePosting: %v", err)
	}
	if !deleted {
		t.Fatal("PurgePosting reported nothing deleted, want the posting dropped")
	}

	rwiCount, err := h.rwi.Index.RWICount(ctx)
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}
	if rwiCount != 2 {
		t.Fatalf("RWICount = %d, want 2", rwiCount)
	}
	if len(h.observer.purged) != 1 || h.observer.purged[0] != word {
		t.Fatalf("purged observers = %v, want one notification for %q", h.observer.purged, word)
	}
}
