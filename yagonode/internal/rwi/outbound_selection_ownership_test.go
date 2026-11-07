package rwi

import (
	"context"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var (
	ownershipWord      = yagomodel.Hash("AAAAAAAAAAAA")
	ownershipOtherWord = yagomodel.Hash("BBBBBBBBBBBB")
	ownershipURL       = yagomodel.Hash("CCCCCCCCCCCC")
	ownershipOtherURL  = yagomodel.Hash("DDDDDDDDDDDD")
)

func journalScriptedOutboundSelection(
	t *testing.T,
	engine *scriptedEngine,
	entry yagomodel.RWIPosting,
) {
	t.Helper()

	raw, err := (postingCodec{}).Encode(entry)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	key := postingKey(entry.WordHash, referencedHash(t, entry))
	engine.buckets[OutboundSelectionBucket][string(key)] = raw
}

func selectReceivedPostings(
	t *testing.T,
	ctx context.Context,
	h harness,
	entries ...yagomodel.RWIPosting,
) OutboundSelection {
	t.Helper()

	if _, err := h.rwi.Receiver.Receive(ctx, entries); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	selection, err := outboundStore(t, h.rwi.Index).SelectOutbound(
		ctx,
		OutboundSelectionConfig{MaxWords: 2, MaxPostings: 10},
	)
	if err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}
	if selection.PostingCount() != len(entries) {
		t.Fatalf("selected = %d, want %d", selection.PostingCount(), len(entries))
	}

	return selection
}

func purgeHarnessPosting(t *testing.T, ctx context.Context, h harness, word, url yagomodel.Hash) {
	t.Helper()

	if err := h.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := h.rwi.Purger.PurgePosting(tx, word, url); err != nil {
			return fmt.Errorf("purge posting: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("PurgePosting: %v", err)
	}
}

func journaledHitCount(
	t *testing.T,
	ctx context.Context,
	h harness,
	word, url yagomodel.Hash,
) (string, bool) {
	t.Helper()

	directory, ok := h.rwi.Index.(postingDirectory)
	if !ok {
		t.Fatal("PostingIndex is not a postingDirectory")
	}
	var row yagomodel.RWIPosting
	found := false
	if err := h.vault.View(ctx, func(tx *vault.Txn) error {
		var err error
		row, found, err = directory.outboundSelected.Get(tx, postingKey(word, url))
		if err != nil {
			return fmt.Errorf("get journal entry: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("read journal: %v", err)
	}

	return row.Properties[yagomodel.ColHitCount], found
}

func assertJournalEntries(t *testing.T, ctx context.Context, h harness, want int) {
	t.Helper()

	count, err := journalEntryCount(ctx, h.vault, h.rwi.Index)
	if err != nil {
		t.Fatalf("journalEntryCount: %v", err)
	}
	if count != want {
		t.Fatalf("journal entries = %d, want %d", count, want)
	}
}

func TestRestoreOutboundDropsPurgedWaitingPosting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	selection := selectReceivedPostings(t, ctx, h,
		postingWithHashes(ownershipWord, ownershipURL),
		postingWithHashes(ownershipWord, ownershipOtherURL),
	)
	purgeHarnessPosting(t, ctx, h, ownershipWord, ownershipURL)
	storedBefore := len(h.observer.stored)

	restored, err := outboundStore(t, h.rwi.Index).RestoreOutbound(ctx, selection.Words)
	if err != nil {
		t.Fatalf("RestoreOutbound: %v", err)
	}
	if restored != 1 {
		t.Fatalf("restored = %d, want 1", restored)
	}
	assertStoredHitCountAbsent(t, ctx, h.rwi.Index, ownershipWord, ownershipURL)
	assertStoredHitCount(t, ctx, h.rwi.Index, hitExpectation{
		word: ownershipWord, url: ownershipOtherURL, want: "1",
	})
	if len(h.observer.stored) != storedBefore+1 {
		t.Fatalf("stored observers = %d, want %d", len(h.observer.stored), storedBefore+1)
	}
	assertJournalEntries(t, ctx, h, 0)
}

func TestRecoverOutboundDropsPurgedWaitingPosting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	selectReceivedPostings(t, ctx, h,
		postingWithHashes(ownershipWord, ownershipURL),
		postingWithHashes(ownershipWord, ownershipOtherURL),
	)
	storedBefore := len(h.observer.stored)
	purgeHarnessPosting(t, ctx, h, ownershipWord, ownershipURL)

	recovered, err := outboundStore(t, h.rwi.Index).RecoverOutbound(ctx)
	if err != nil {
		t.Fatalf("RecoverOutbound: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	if len(h.observer.stored) != storedBefore+1 {
		t.Fatalf(
			"stored observers = %v, want exactly one new notification when recovery restores the surviving posting and stays silent for the purged one",
			h.observer.stored,
		)
	}
	assertStoredHitCountAbsent(t, ctx, h.rwi.Index, ownershipWord, ownershipURL)
	assertStoredHitCount(t, ctx, h.rwi.Index, hitExpectation{
		word: ownershipWord, url: ownershipOtherURL, want: "1",
	})
	assertJournalEntries(t, ctx, h, 0)
}

func TestRestoreOutboundDropsPostingPurgedBeforeSelectionRemoval(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	entry := postingWithHashes(ownershipWord, ownershipURL)
	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	directory, ok := h.rwi.Index.(postingDirectory)
	if !ok {
		t.Fatal("PostingIndex is not a postingDirectory")
	}
	selected := []selectedPosting{{
		key:  postingKey(ownershipWord, ownershipURL),
		word: ownershipWord,
		url:  ownershipURL,
		row:  entry,
	}}
	if err := h.vault.Update(ctx, func(tx *vault.Txn) error {
		return directory.journalOutboundSelection(tx, selected)
	}); err != nil {
		t.Fatalf("journal selection: %v", err)
	}
	purgeHarnessPosting(t, ctx, h, ownershipWord, ownershipURL)
	if err := directory.removeDurablySelectedPostings(ctx, selected); err != nil {
		t.Fatalf("remove selected postings: %v", err)
	}

	restored, err := directory.RestoreOutbound(ctx, []yagomodel.WordPostings{{
		WordHash: ownershipWord,
		Postings: []yagomodel.RWIPosting{entry},
	}})
	if err != nil {
		t.Fatalf("RestoreOutbound: %v", err)
	}
	if restored != 0 {
		t.Fatalf("restored = %d, want 0", restored)
	}
	assertStoredHitCountAbsent(t, ctx, h.rwi.Index, ownershipWord, ownershipURL)
	assertJournalEntries(t, ctx, h, 0)
}

func TestDropWaitingOutboundPostingsKeepsOtherURLs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	selectReceivedPostings(t, ctx, h,
		postingWithHashes(ownershipWord, ownershipURL),
		postingWithHashes(ownershipWord, ownershipOtherURL),
		postingWithHashes(ownershipOtherWord, ownershipURL),
	)

	if err := h.vault.Update(ctx, func(tx *vault.Txn) error {
		return h.rwi.Purger.DropWaitingOutboundPostings(tx, []yagomodel.Hash{ownershipURL})
	}); err != nil {
		t.Fatalf("DropWaitingOutboundPostings: %v", err)
	}
	assertJournalEntries(t, ctx, h, 1)
	if hits, found := journaledHitCount(t, ctx, h, ownershipWord, ownershipOtherURL); !found ||
		hits != "1" {
		t.Fatalf("kept journal entry = %q, %v; want \"1\", true", hits, found)
	}

	recovered, err := outboundStore(t, h.rwi.Index).RecoverOutbound(ctx)
	if err != nil || recovered != 1 {
		t.Fatalf("RecoverOutbound = %d, %v; want 1, nil", recovered, err)
	}
	assertStoredHitCountAbsent(t, ctx, h.rwi.Index, ownershipWord, ownershipURL)
	assertStoredHitCountAbsent(t, ctx, h.rwi.Index, ownershipOtherWord, ownershipURL)
	assertStoredHitCount(t, ctx, h.rwi.Index, hitExpectation{
		word: ownershipWord, url: ownershipOtherURL, want: "1",
	})
}

func selectNewerRowAfterSelection(
	t *testing.T,
	ctx context.Context,
	h harness,
) (OutboundSelection, OutboundSelection) {
	t.Helper()

	first := selectReceivedPostings(t, ctx, h, postingWithHashes(ownershipWord, ownershipURL))
	newer := postingWithHashes(ownershipWord, ownershipURL)
	newer.Properties[yagomodel.ColHitCount] = "7"
	second := selectReceivedPostings(t, ctx, h, newer)
	if hits, found := journaledHitCount(t, ctx, h, ownershipWord, ownershipURL); !found ||
		hits != "7" {
		t.Fatalf("journal entry = %q, %v; want \"7\", true", hits, found)
	}

	return first, second
}

func TestConfirmOutboundKeepsJournalEntryOfNewerSelection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	first, second := selectNewerRowAfterSelection(t, ctx, h)
	store := outboundStore(t, h.rwi.Index)

	confirmed, err := store.ConfirmOutbound(ctx, first.Words[0].Postings)
	if err != nil || confirmed != 0 {
		t.Fatalf("ConfirmOutbound first = %d, %v; want 0, nil", confirmed, err)
	}
	if hits, found := journaledHitCount(t, ctx, h, ownershipWord, ownershipURL); !found ||
		hits != "7" {
		t.Fatalf("journal entry = %q, %v; want \"7\", true", hits, found)
	}

	restored, err := store.RestoreOutbound(ctx, second.Words)
	if err != nil || restored != 1 {
		t.Fatalf("RestoreOutbound second = %d, %v; want 1, nil", restored, err)
	}
	assertStoredHitCount(t, ctx, h.rwi.Index, hitExpectation{
		word: ownershipWord, url: ownershipURL, want: "7",
	})
	assertJournalEntries(t, ctx, h, 0)
}

func TestRestoreOutboundIgnoresJournalEntryOfNewerSelection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	first, second := selectNewerRowAfterSelection(t, ctx, h)
	store := outboundStore(t, h.rwi.Index)
	storedBefore := len(h.observer.stored)

	restored, err := store.RestoreOutbound(ctx, first.Words)
	if err != nil || restored != 0 {
		t.Fatalf("RestoreOutbound first = %d, %v; want 0, nil", restored, err)
	}
	assertStoredHitCountAbsent(t, ctx, h.rwi.Index, ownershipWord, ownershipURL)
	if len(h.observer.stored) != storedBefore {
		t.Fatalf("stored observers = %d, want %d", len(h.observer.stored), storedBefore)
	}
	if hits, found := journaledHitCount(t, ctx, h, ownershipWord, ownershipURL); !found ||
		hits != "7" {
		t.Fatalf("journal entry = %q, %v; want \"7\", true", hits, found)
	}

	confirmed, err := store.ConfirmOutbound(ctx, second.Words[0].Postings)
	if err != nil || confirmed != 1 {
		t.Fatalf("ConfirmOutbound second = %d, %v; want 1, nil", confirmed, err)
	}
	assertJournalEntries(t, ctx, h, 0)
}

func TestConfirmOutboundReturnsZeroForPurgedWaitingEntry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	entry := postingWithHashes(ownershipWord, ownershipURL)
	selectReceivedPostings(t, ctx, h, entry)
	purgeHarnessPosting(t, ctx, h, ownershipWord, ownershipURL)

	confirmed, err := outboundStore(
		t,
		h.rwi.Index,
	).ConfirmOutbound(ctx, []yagomodel.RWIPosting{entry})
	if err != nil || confirmed != 0 {
		t.Fatalf("ConfirmOutbound = %d, %v; want 0, nil", confirmed, err)
	}
}
