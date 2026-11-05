package rwi

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestRestoreOutboundKeepsExistingRow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	word := yagomodel.Hash("AAAAAAAAAAAA")
	url := yagomodel.Hash("CCCCCCCCCCCC")
	entry := postingWithHashes(word, url)
	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	store := outboundStore(t, h.rwi.Index)
	if _, err := store.SelectOutbound(ctx, OutboundSelectionConfig{}); err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}

	replacement := postingWithHashes(word, url)
	replacement.Properties[yagomodel.ColHitCount] = "7"
	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{replacement}); err != nil {
		t.Fatalf("Receive newer row: %v", err)
	}

	storedBefore := len(h.observer.stored)

	restored, err := store.RestoreOutbound(ctx, []yagomodel.WordPostings{{
		WordHash: word,
		Postings: []yagomodel.RWIPosting{entry},
	}})
	if err != nil {
		t.Fatalf("RestoreOutbound: %v", err)
	}
	if restored != 1 {
		t.Fatalf("restored = %d, want 1", restored)
	}
	assertStoredHitCount(t, ctx, h.rwi.Index, hitExpectation{word: word, url: url, want: "7"})
	if len(h.observer.stored) != storedBefore {
		t.Fatalf(
			"stored observers = %v, want unchanged at %d when restore keeps an existing row",
			h.observer.stored,
			storedBefore,
		)
	}
}

func TestRestoreOutboundKeepsExistingRowThroughRecoverOutbound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	word := yagomodel.Hash("AAAAAAAAAAAA")
	url := yagomodel.Hash("CCCCCCCCCCCC")
	entry := postingWithHashes(word, url)
	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	store := outboundStore(t, h.rwi.Index)
	if _, err := store.SelectOutbound(ctx, OutboundSelectionConfig{}); err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}

	replacement := postingWithHashes(word, url)
	replacement.Properties[yagomodel.ColHitCount] = "7"
	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{replacement}); err != nil {
		t.Fatalf("Receive newer row: %v", err)
	}

	storedBefore := len(h.observer.stored)

	recovered, err := store.RecoverOutbound(ctx)
	if err != nil {
		t.Fatalf("RecoverOutbound: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	assertStoredHitCount(t, ctx, h.rwi.Index, hitExpectation{word: word, url: url, want: "7"})
	if len(h.observer.stored) != storedBefore {
		t.Fatalf(
			"stored observers = %v, want unchanged at %d when recovery keeps an existing row",
			h.observer.stored,
			storedBefore,
		)
	}
	if _, err := store.RecoverOutbound(ctx); err != nil {
		t.Fatalf("RecoverOutbound second pass: %v", err)
	}
	if count, err := journalEntryCount(ctx, h.vault, h.rwi.Index); err != nil {
		t.Fatalf("journalEntryCount: %v", err)
	} else if count != 0 {
		t.Fatalf("journal entries = %d, want empty after recovery", count)
	}
}

func TestRecoverOutboundCrashBetweenTransactionsKeepsRow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	word := yagomodel.Hash("AAAAAAAAAAAA")
	url := yagomodel.Hash("CCCCCCCCCCCC")
	entry := postingWithHashes(word, url)
	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}

	store := outboundStore(t, h.rwi.Index)
	storedBefore := len(h.observer.stored)

	if err := h.vault.Update(ctx, func(tx *vault.Txn) error {
		return h.rwi.Index.(postingDirectory).journalOutboundSelection(
			tx,
			[]selectedPosting{{
				key:  postingKey(word, url),
				word: word,
				url:  url,
				row:  entry,
			}},
		)
	}); err != nil {
		t.Fatalf("journal: %v", err)
	}

	recovered, err := store.RecoverOutbound(ctx)
	if err != nil {
		t.Fatalf("RecoverOutbound: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	assertStoredHitCount(t, ctx, h.rwi.Index, hitExpectation{word: word, url: url, want: "1"})
	if len(h.observer.stored) != storedBefore {
		t.Fatalf(
			"stored observers = %v, want unchanged at %d when recovery keeps a journaled row that is already live",
			h.observer.stored,
			storedBefore,
		)
	}
	if _, err := store.RecoverOutbound(ctx); err != nil {
		t.Fatalf("RecoverOutbound second pass: %v", err)
	}
	if count, err := journalEntryCount(ctx, h.vault, h.rwi.Index); err != nil {
		t.Fatalf("journalEntryCount: %v", err)
	} else if count != 0 {
		t.Fatalf("journal entries = %d, want empty after recovery", count)
	}
}

func TestRestoreOutboundReinsertsAbsentRow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	word := yagomodel.Hash("AAAAAAAAAAAA")
	url := yagomodel.Hash("CCCCCCCCCCCC")
	entry := postingWithHashes(word, url)
	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	store := outboundStore(t, h.rwi.Index)
	if _, err := store.SelectOutbound(ctx, OutboundSelectionConfig{}); err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}

	storedBefore := len(h.observer.stored)

	restored, err := store.RestoreOutbound(ctx, []yagomodel.WordPostings{{
		WordHash: word,
		Postings: []yagomodel.RWIPosting{entry},
	}})
	if err != nil {
		t.Fatalf("RestoreOutbound: %v", err)
	}
	if restored != 1 {
		t.Fatalf("restored = %d, want 1", restored)
	}
	assertStoredHitCount(t, ctx, h.rwi.Index, hitExpectation{word: word, url: url, want: "1"})
	if len(h.observer.stored) != storedBefore+1 || h.observer.stored[storedBefore] != word {
		t.Fatalf(
			"stored observers = %v, want one new notification for %q when restore reinserts an absent row",
			h.observer.stored,
			word,
		)
	}
}

func TestDeleteOutboundSkipsReplacedRow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	word := yagomodel.Hash("AAAAAAAAAAAA")
	url := yagomodel.Hash("CCCCCCCCCCCC")
	entry := postingWithHashes(word, url)
	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	store := outboundStore(t, h.rwi.Index)
	if _, err := store.SelectOutbound(ctx, OutboundSelectionConfig{}); err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}

	purgedBefore := len(h.observer.purged)

	replacement := postingWithHashes(word, url)
	replacement.Properties[yagomodel.ColHitCount] = "9"
	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{replacement}); err != nil {
		t.Fatalf("Receive newer row: %v", err)
	}

	selected := selectedPosting{
		key:  postingKey(word, url),
		word: word,
		url:  url,
		row:  entry,
	}
	if err := h.vault.Update(ctx, func(tx *vault.Txn) error {
		return h.rwi.Index.(postingDirectory).deleteOutboundPosting(tx, selected)
	}); err != nil {
		t.Fatalf("deleteOutboundPosting: %v", err)
	}
	count, err := h.rwi.Index.RWICount(ctx)
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}
	if count != 1 {
		t.Fatalf("RWICount = %d, want replacement row retained by guard", count)
	}
	assertStoredHitCount(t, ctx, h.rwi.Index, hitExpectation{word: word, url: url, want: "9"})
	if len(h.observer.purged) != purgedBefore {
		t.Fatalf(
			"purged observers = %v, want unchanged at %d when removal skips a replaced row",
			h.observer.purged,
			purgedBefore,
		)
	}
}

func TestDeleteOutboundSkipsRemovedRow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	word := yagomodel.Hash("AAAAAAAAAAAA")
	url := yagomodel.Hash("CCCCCCCCCCCC")
	entry := postingWithHashes(word, url)
	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if err := h.vault.Update(ctx, func(tx *vault.Txn) error {
		return h.rwi.Index.(postingDirectory).journalOutboundSelection(
			tx,
			[]selectedPosting{{
				key:  postingKey(word, url),
				word: word,
				url:  url,
				row:  entry,
			}},
		)
	}); err != nil {
		t.Fatalf("journal: %v", err)
	}
	if err := h.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := h.rwi.Purger.PurgePosting(tx, word, url); err != nil {
			return fmt.Errorf("purge: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("purge row: %v", err)
	}

	purgedBefore := len(h.observer.purged)

	selected := selectedPosting{
		key:  postingKey(word, url),
		word: word,
		url:  url,
		row:  entry,
	}
	if err := h.vault.Update(ctx, func(tx *vault.Txn) error {
		return h.rwi.Index.(postingDirectory).deleteOutboundPosting(tx, selected)
	}); err != nil {
		t.Fatalf("deleteOutboundPosting: %v", err)
	}
	assertStoredHitCountAbsent(t, ctx, h.rwi.Index, word, url)
	if len(h.observer.purged) != purgedBefore {
		t.Fatalf(
			"purged observers = %v, want unchanged at %d when removal skips an absent row",
			h.observer.purged,
			purgedBefore,
		)
	}
}

func TestDeleteOutboundRemovesUnchangedRow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	word := yagomodel.Hash("AAAAAAAAAAAA")
	url := yagomodel.Hash("CCCCCCCCCCCC")
	entry := postingWithHashes(word, url)
	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	store := outboundStore(t, h.rwi.Index)
	if _, err := store.SelectOutbound(ctx, OutboundSelectionConfig{}); err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}

	if err := h.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := h.rwi.Purger.PurgePosting(tx, word, url); err != nil {
			return fmt.Errorf("purge row: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("purge row: %v", err)
	}
	if err := h.vault.Update(ctx, func(tx *vault.Txn) error {
		if _, err := h.rwi.Purger.PurgePosting(tx, word, url); err != nil {
			return fmt.Errorf("purge again: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("purge again: %v", err)
	}

	if err := h.vault.Update(ctx, func(tx *vault.Txn) error {
		if err := h.rwi.Index.(postingDirectory).postings.Put(
			tx, postingKey(word, url), entry,
		); err != nil {
			return fmt.Errorf("restore row: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("restore row: %v", err)
	}

	purgedBefore := len(h.observer.purged)

	selected := selectedPosting{
		key:  postingKey(word, url),
		word: word,
		url:  url,
		row:  entry,
	}
	if err := h.vault.Update(ctx, func(tx *vault.Txn) error {
		return h.rwi.Index.(postingDirectory).deleteOutboundPosting(tx, selected)
	}); err != nil {
		t.Fatalf("deleteOutboundPosting: %v", err)
	}
	assertStoredHitCountAbsent(t, ctx, h.rwi.Index, word, url)
	if len(h.observer.purged) != purgedBefore+1 || h.observer.purged[purgedBefore] != word {
		t.Fatalf(
			"purged observers = %v, want one new notification for %q when removal deletes an unchanged row",
			h.observer.purged,
			word,
		)
	}
}

func TestRestoreOutboundReadErrorFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	_, index, _, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	word := yagomodel.Hash("AAAAAAAAAAAA")
	url := yagomodel.Hash("CCCCCCCCCCCC")
	injected := errors.New("read failed")
	engine.readErrors[PostingsBucket] = injected
	if _, err := outboundStore(t, index).RestoreOutbound(
		ctx,
		[]yagomodel.WordPostings{{
			WordHash: word,
			Postings: []yagomodel.RWIPosting{postingWithHashes(word, url)},
		}},
	); !errors.Is(err, injected) {
		t.Fatalf("restore error = %v, want %v", err, injected)
	}
}

func TestDeleteOutboundReadErrorFails(t *testing.T) {
	t.Parallel()

	storage, index, _, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	ctx := context.Background()
	word := yagomodel.Hash("AAAAAAAAAAAA")
	url := yagomodel.Hash("CCCCCCCCCCCC")
	injected := errors.New("read failed")
	engine.readErrors[PostingsBucket] = injected
	selected := selectedPosting{
		key:  postingKey(word, url),
		word: word,
		url:  url,
		row:  postingWithHashes(word, url),
	}
	err := storage.Update(ctx, func(tx *vault.Txn) error {
		return index.(postingDirectory).deleteOutboundPosting(tx, selected)
	})
	if !errors.Is(err, injected) {
		t.Fatalf("delete error = %v, want %v", err, injected)
	}
}

func journalEntryCount(ctx context.Context, v *vault.Vault, index PostingIndex) (int, error) {
	directory, ok := index.(postingDirectory)
	if !ok {
		return 0, fmt.Errorf("PostingIndex is not a postingDirectory")
	}
	count := 0
	err := v.View(ctx, func(tx *vault.Txn) error {
		return directory.outboundSelected.Scan(
			tx,
			nil,
			func(vault.Key, yagomodel.RWIPosting) (bool, error) {
				count++

				return true, nil
			},
		)
	})
	if err != nil {
		return 0, fmt.Errorf("scan outbound selected rwi: %w", err)
	}

	return count, nil
}

type hitExpectation struct {
	word yagomodel.Hash
	url  yagomodel.Hash
	want string
}

func assertStoredHitCount(
	t *testing.T,
	ctx context.Context,
	index PostingIndex,
	want hitExpectation,
) {
	t.Helper()

	matches := 0
	if err := index.ScanWord(ctx, want.word, func(posting yagomodel.RWIPosting) (bool, error) {
		got, err := posting.URLHash()
		if err != nil {
			return false, fmt.Errorf("posting url hash: %w", err)
		}
		if got.Hash() != want.url {
			return true, nil
		}
		matches++
		if posting.Properties[yagomodel.ColHitCount] != want.want {
			t.Fatalf(
				"hit count = %q, want %q",
				posting.Properties[yagomodel.ColHitCount],
				want.want,
			)
		}

		return true, nil
	}); err != nil {
		t.Fatalf("ScanWord: %v", err)
	}
	if matches != 1 {
		t.Fatalf("postings for %q = %d, want exactly 1", want.url, matches)
	}
}

func assertStoredHitCountAbsent(
	t *testing.T,
	ctx context.Context,
	index PostingIndex,
	word, url yagomodel.Hash,
) {
	t.Helper()

	matches := 0
	if err := index.ScanWord(ctx, word, func(posting yagomodel.RWIPosting) (bool, error) {
		got, err := posting.URLHash()
		if err != nil {
			return false, fmt.Errorf("posting url hash: %w", err)
		}
		if got.Hash() == url {
			matches++
		}

		return true, nil
	}); err != nil {
		t.Fatalf("ScanWord: %v", err)
	}
	if matches != 0 {
		t.Fatalf("postings for %q = %d, want 0", url, matches)
	}
}
