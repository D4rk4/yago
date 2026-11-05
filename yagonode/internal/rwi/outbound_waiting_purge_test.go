package rwi

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func dropScriptedWaitingPostings(
	ctx context.Context,
	storage *vault.Vault,
	purger PostingPurger,
	urls []yagomodel.Hash,
) error {
	if err := storage.Update(ctx, func(tx *vault.Txn) error {
		return purger.DropWaitingOutboundPostings(tx, urls)
	}); err != nil {
		return fmt.Errorf("drop waiting postings: %w", err)
	}

	return nil
}

func TestDropWaitingOutboundPostingsSkipsJournalWithoutURLs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storage, _, _, purger, engine := openScriptedRWI(t, fakeURLDirectory{})
	injected := errors.New("scan failed")
	engine.scanErrors[OutboundSelectionBucket] = injected

	if err := dropScriptedWaitingPostings(ctx, storage, purger, nil); err != nil {
		t.Fatalf("drop without urls = %v, want nil", err)
	}
	err := dropScriptedWaitingPostings(ctx, storage, purger, []yagomodel.Hash{ownershipURL})
	if !errors.Is(err, injected) {
		t.Fatalf("drop with urls = %v, want %v", err, injected)
	}
}

func TestDropWaitingOutboundPostingsRejectsMalformedJournalKey(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storage, _, _, purger, engine := openScriptedRWI(t, fakeURLDirectory{})
	raw, err := (postingCodec{}).Encode(postingWithHashes(ownershipWord, ownershipURL))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	engine.buckets[OutboundSelectionBucket]["short"] = raw

	if err := dropScriptedWaitingPostings(
		ctx,
		storage,
		purger,
		[]yagomodel.Hash{ownershipURL},
	); err == nil {
		t.Fatal("expected malformed journal key error")
	}
}

func TestDropWaitingOutboundPostingsReportsDeleteError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storage, _, _, purger, engine := openScriptedRWI(t, fakeURLDirectory{})
	journalScriptedOutboundSelection(t, engine, postingWithHashes(ownershipWord, ownershipURL))
	injected := errors.New("delete failed")
	engine.deleteErrors[OutboundSelectionBucket] = injected

	err := dropScriptedWaitingPostings(ctx, storage, purger, []yagomodel.Hash{ownershipURL})
	if !errors.Is(err, injected) {
		t.Fatalf("drop = %v, want %v", err, injected)
	}
}

func TestPurgePostingReportsWaitingPostingDeleteError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	storage, _, _, purger, engine := openScriptedRWI(t, fakeURLDirectory{})
	purge := func() error {
		if err := storage.Update(ctx, func(tx *vault.Txn) error {
			if _, err := purger.PurgePosting(tx, ownershipWord, ownershipURL); err != nil {
				return fmt.Errorf("purge posting: %w", err)
			}

			return nil
		}); err != nil {
			return fmt.Errorf("purge update: %w", err)
		}

		return nil
	}
	injected := errors.New("delete failed")
	engine.deleteErrors[OutboundSelectionBucket] = injected
	if err := purge(); err != nil {
		t.Fatalf("purge without waiting posting = %v, want nil", err)
	}

	journalScriptedOutboundSelection(t, engine, postingWithHashes(ownershipWord, ownershipURL))
	if err := purge(); !errors.Is(err, injected) {
		t.Fatalf("purge with waiting posting = %v, want %v", err, injected)
	}
}

func TestOutboundOwnershipReadErrorFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, index, _, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	entry := postingWithHashes(ownershipWord, ownershipURL)
	journalScriptedOutboundSelection(t, engine, entry)
	injected := errors.New("read failed")
	engine.readErrors[OutboundSelectionBucket] = injected
	store := outboundStore(t, index)

	if _, err := store.RestoreOutbound(ctx, []yagomodel.WordPostings{{
		WordHash: ownershipWord,
		Postings: []yagomodel.RWIPosting{entry},
	}}); !errors.Is(err, injected) {
		t.Fatalf("restore error = %v, want %v", err, injected)
	}
	if _, err := store.ConfirmOutbound(
		ctx,
		[]yagomodel.RWIPosting{entry},
	); !errors.Is(err, injected) {
		t.Fatalf("confirm error = %v, want %v", err, injected)
	}
}
