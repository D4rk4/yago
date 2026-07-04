package rwi

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func outboundStore(tb testing.TB, index PostingIndex) OutboundPostingStore {
	tb.Helper()

	store, ok := index.(OutboundPostingStore)
	if !ok {
		tb.Fatal("PostingIndex does not expose outbound store")
	}

	return store
}

func postingWithHashes(word, url yagomodel.Hash) yagomodel.RWIPosting {
	return yagomodel.RWIPosting{
		WordHash: word,
		Properties: map[string]string{
			yagomodel.ColURLHash:        url.String(),
			yagomodel.ColLocalLinkCount: "1",
			yagomodel.ColHitCount:       "1",
			yagomodel.ColWordDistance:   "0",
		},
	}
}

func TestSelectOutboundUsesDefaultBoundsAndDeletesSelectedPosting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	firstWord := yagomodel.Hash("AAAAAAAAAAAA")
	secondWord := yagomodel.Hash("BBBBBBBBBBBB")
	firstURL := yagomodel.Hash("CCCCCCCCCCCC")
	secondURL := yagomodel.Hash("DDDDDDDDDDDD")

	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{
		postingWithHashes(firstWord, firstURL),
		postingWithHashes(secondWord, secondURL),
	}); err != nil {
		t.Fatalf("Receive: %v", err)
	}

	selection, err := outboundStore(t, h.rwi.Index).SelectOutbound(
		ctx,
		OutboundSelectionConfig{},
	)
	if err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}
	if selection.PostingCount() != 1 ||
		len(selection.Words) != 1 ||
		selection.Words[0].WordHash != firstWord ||
		selection.Words[0].Postings[0].WordHash != firstWord {
		t.Fatalf("selection = %#v", selection)
	}
	count, err := h.rwi.Index.RWICount(ctx)
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}
	if count != 1 {
		t.Fatalf("RWICount = %d, want one posting left", count)
	}
	if len(h.observer.purged) != 1 || h.observer.purged[0] != firstWord {
		t.Fatalf("purged = %v, want first word", h.observer.purged)
	}
}

func TestSelectOutboundHonorsWordAndPostingLimits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	firstWord := yagomodel.Hash("AAAAAAAAAAAA")
	secondWord := yagomodel.Hash("BBBBBBBBBBBB")
	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{
		postingWithHashes(firstWord, yagomodel.Hash("CCCCCCCCCCCC")),
		postingWithHashes(firstWord, yagomodel.Hash("DDDDDDDDDDDD")),
		postingWithHashes(secondWord, yagomodel.Hash("EEEEEEEEEEEE")),
	}); err != nil {
		t.Fatalf("Receive: %v", err)
	}

	selection, err := outboundStore(t, h.rwi.Index).SelectOutbound(
		ctx,
		OutboundSelectionConfig{MaxWords: 1, MaxPostings: 10},
	)
	if err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}
	if selection.PostingCount() != 2 ||
		len(selection.Words) != 1 ||
		selection.Words[0].WordHash != firstWord {
		t.Fatalf("selection = %#v", selection)
	}
	count, err := h.rwi.Index.RWICount(ctx)
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}
	if count != 1 {
		t.Fatalf("RWICount = %d, want second word retained", count)
	}
}

func TestRestoreOutboundReinsertsSelectedPostings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	word := yagomodel.Hash("AAAAAAAAAAAA")
	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{
		postingWithHashes(word, yagomodel.Hash("CCCCCCCCCCCC")),
	}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	store := outboundStore(t, h.rwi.Index)
	selection, err := store.SelectOutbound(ctx, OutboundSelectionConfig{})
	if err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}

	restored, err := store.RestoreOutbound(ctx, selection.Words)
	if err != nil {
		t.Fatalf("RestoreOutbound: %v", err)
	}
	if restored != 1 {
		t.Fatalf("restored = %d, want 1", restored)
	}
	var visited int
	if err := h.rwi.Index.ScanWord(ctx, word, func(yagomodel.RWIPosting) (bool, error) {
		visited++

		return true, nil
	}); err != nil {
		t.Fatalf("ScanWord: %v", err)
	}
	if visited != 1 {
		t.Fatalf("visited = %d, want restored posting", visited)
	}
}

func TestRecoverOutboundRestoresPendingSelectedPostings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	word := yagomodel.Hash("AAAAAAAAAAAA")
	url := yagomodel.Hash("CCCCCCCCCCCC")
	storePosting := postingWithHashes(word, url)
	if _, err := h.rwi.Receiver.Receive(ctx, []yagomodel.RWIPosting{storePosting}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	store := outboundStore(t, h.rwi.Index)
	if _, err := store.SelectOutbound(ctx, OutboundSelectionConfig{}); err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}

	recovered, err := store.RecoverOutbound(ctx)
	if err != nil {
		t.Fatalf("RecoverOutbound: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered = %d, want 1", recovered)
	}
	assertStoredPosting(t, ctx, h.rwi.Index, word, url)
	again, err := store.RecoverOutbound(ctx)
	if err != nil {
		t.Fatalf("second RecoverOutbound: %v", err)
	}
	if again != 0 {
		t.Fatalf("second recovered = %d, want 0", again)
	}
}

func TestConfirmOutboundClearsPendingSelection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	word := yagomodel.Hash("AAAAAAAAAAAA")
	url := yagomodel.Hash("CCCCCCCCCCCC")
	if _, err := h.rwi.Receiver.Receive(
		ctx,
		[]yagomodel.RWIPosting{postingWithHashes(word, url)},
	); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	store := outboundStore(t, h.rwi.Index)
	selection, err := store.SelectOutbound(ctx, OutboundSelectionConfig{})
	if err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}

	confirmed, err := store.ConfirmOutbound(ctx, selection.Words[0].Postings)
	if err != nil {
		t.Fatalf("ConfirmOutbound: %v", err)
	}
	if confirmed != 1 {
		t.Fatalf("confirmed = %d, want 1", confirmed)
	}
	recovered, err := store.RecoverOutbound(ctx)
	if err != nil {
		t.Fatalf("RecoverOutbound: %v", err)
	}
	if recovered != 0 {
		t.Fatalf("recovered = %d, want no confirmed rows", recovered)
	}
	count, err := h.rwi.Index.RWICount(ctx)
	if err != nil {
		t.Fatalf("RWICount: %v", err)
	}
	if count != 0 {
		t.Fatalf("RWICount = %d, want confirmed row deleted", count)
	}
}

func TestSelectOutboundReturnsStorageAndObserverErrors(t *testing.T) {
	t.Parallel()

	entry := postingWithHashes(yagomodel.Hash("AAAAAAAAAAAA"), yagomodel.Hash("CCCCCCCCCCCC"))

	_, index, receiver, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	if _, err := receiver.Receive(t.Context(), []yagomodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	engine.scanErrors[postingsBucket] = errors.New("scan failed")
	if _, err := outboundStore(t, index).SelectOutbound(
		t.Context(),
		OutboundSelectionConfig{},
	); err == nil {
		t.Fatal("expected scan error")
	}

	_, index, receiver, _, engine = openScriptedRWI(t, fakeURLDirectory{})
	if _, err := receiver.Receive(t.Context(), []yagomodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	engine.putErrors[outboundSelectedBucket] = errors.New("journal failed")
	if _, err := outboundStore(t, index).SelectOutbound(
		t.Context(),
		OutboundSelectionConfig{},
	); err == nil {
		t.Fatal("expected journal error")
	}

	_, index, receiver, _, engine = openScriptedRWI(t, fakeURLDirectory{})
	if _, err := receiver.Receive(t.Context(), []yagomodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	engine.deleteErrors[postingsBucket] = errors.New("delete failed")
	if _, err := outboundStore(t, index).SelectOutbound(
		t.Context(),
		OutboundSelectionConfig{},
	); err == nil {
		t.Fatal("expected delete error")
	}

	_, index, receiver, _, _ = openScriptedRWI(
		t,
		fakeURLDirectory{},
		failingObserver{purgeErr: errors.New("observer failed")},
	)
	if _, err := receiver.Receive(t.Context(), []yagomodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if _, err := outboundStore(t, index).SelectOutbound(
		t.Context(),
		OutboundSelectionConfig{},
	); err == nil {
		t.Fatal("expected observer error")
	}
}

func TestConfirmOutboundReturnsPostingAndStorageErrors(t *testing.T) {
	t.Parallel()

	entry := postingWithHashes(yagomodel.Hash("AAAAAAAAAAAA"), yagomodel.Hash("CCCCCCCCCCCC"))
	_, index, receiver, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	if _, err := receiver.Receive(t.Context(), []yagomodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	selection, err := outboundStore(t, index).SelectOutbound(t.Context(), OutboundSelectionConfig{})
	if err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}
	engine.deleteErrors[outboundSelectedBucket] = errors.New("confirm failed")
	if _, err := outboundStore(t, index).ConfirmOutbound(
		t.Context(),
		selection.Words[0].Postings,
	); err == nil {
		t.Fatal("expected confirm delete error")
	}
	if _, err := outboundStore(t, index).ConfirmOutbound(
		t.Context(),
		[]yagomodel.RWIPosting{{WordHash: yagomodel.Hash("bad")}},
	); err == nil {
		t.Fatal("expected bad word hash error")
	}
	if _, err := outboundStore(t, index).ConfirmOutbound(
		t.Context(),
		[]yagomodel.RWIPosting{{WordHash: yagomodel.Hash("AAAAAAAAAAAA")}},
	); err == nil {
		t.Fatal("expected bad url hash error")
	}

	ctx := &errAfterContext{Context: context.Background(), remaining: 2, err: context.Canceled}
	if _, err := outboundStore(t, index).ConfirmOutbound(
		ctx,
		selection.Words[0].Postings,
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("ConfirmOutbound error = %v, want context.Canceled", err)
	}
}

func TestSelectOutboundReturnsContextError(t *testing.T) {
	t.Parallel()

	h := openHarness(t, 0, 100)
	if _, err := h.rwi.Receiver.Receive(t.Context(), []yagomodel.RWIPosting{
		postingWithHashes(yagomodel.Hash("AAAAAAAAAAAA"), yagomodel.Hash("CCCCCCCCCCCC")),
	}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	ctx := &errAfterContext{Context: context.Background(), remaining: 2, err: context.Canceled}
	if _, err := outboundStore(t, h.rwi.Index).SelectOutbound(
		ctx,
		OutboundSelectionConfig{},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("SelectOutbound error = %v, want context.Canceled", err)
	}
}

func TestRestoreOutboundReturnsStorageObserverAndPostingErrors(t *testing.T) {
	t.Parallel()

	good := yagomodel.WordPostings{
		WordHash: yagomodel.Hash("AAAAAAAAAAAA"),
		Postings: []yagomodel.RWIPosting{
			postingWithHashes(yagomodel.Hash("AAAAAAAAAAAA"), yagomodel.Hash("CCCCCCCCCCCC")),
		},
	}
	bad := yagomodel.WordPostings{
		WordHash: yagomodel.Hash("AAAAAAAAAAAA"),
		Postings: []yagomodel.RWIPosting{{Properties: map[string]string{}}},
	}

	_, index, _, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	engine.putErrors[postingsBucket] = errors.New("put failed")
	if _, err := outboundStore(
		t,
		index,
	).RestoreOutbound(t.Context(), []yagomodel.WordPostings{good}); err == nil {
		t.Fatal("expected put error")
	}

	_, index, _, _, _ = openScriptedRWI(
		t,
		fakeURLDirectory{},
		failingObserver{storeErr: errors.New("observer failed")},
	)
	if _, err := outboundStore(
		t,
		index,
	).RestoreOutbound(t.Context(), []yagomodel.WordPostings{good}); err == nil {
		t.Fatal("expected observer error")
	}

	_, index, receiver, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	if _, err := receiver.Receive(t.Context(), good.Postings); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	selection, err := outboundStore(t, index).SelectOutbound(t.Context(), OutboundSelectionConfig{})
	if err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}
	engine.deleteErrors[outboundSelectedBucket] = errors.New("recovery delete failed")
	if _, err := outboundStore(
		t,
		index,
	).RestoreOutbound(t.Context(), selection.Words); err == nil {
		t.Fatal("expected selected delete error")
	}

	if _, err := outboundStore(
		t,
		index,
	).RestoreOutbound(t.Context(), []yagomodel.WordPostings{bad}); err == nil {
		t.Fatal("expected bad posting error")
	}

	ctx := &errAfterContext{Context: context.Background(), remaining: 2, err: context.Canceled}
	if _, err := outboundStore(
		t,
		index,
	).RestoreOutbound(ctx, []yagomodel.WordPostings{good}); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("RestoreOutbound error = %v, want context.Canceled", err)
	}
}

func TestRecoverOutboundReturnsStoragePostingObserverAndContextErrors(t *testing.T) {
	t.Parallel()

	entry := postingWithHashes(yagomodel.Hash("AAAAAAAAAAAA"), yagomodel.Hash("CCCCCCCCCCCC"))

	_, index, receiver, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	if _, err := receiver.Receive(t.Context(), []yagomodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if _, err := outboundStore(
		t,
		index,
	).SelectOutbound(t.Context(), OutboundSelectionConfig{}); err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}
	engine.scanErrors[outboundSelectedBucket] = errors.New("scan failed")
	if _, err := outboundStore(t, index).RecoverOutbound(t.Context()); err == nil {
		t.Fatal("expected recovery scan error")
	}

	_, index, _, _, engine = openScriptedRWI(t, fakeURLDirectory{})
	raw, err := (postingCodec{}).Encode(entry)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	engine.buckets[outboundSelectedBucket]["short"] = raw
	if _, err := outboundStore(t, index).RecoverOutbound(t.Context()); err == nil {
		t.Fatal("expected malformed pending key error")
	}

	_, index, receiver, _, engine = openScriptedRWI(t, fakeURLDirectory{})
	if _, err := receiver.Receive(t.Context(), []yagomodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if _, err := outboundStore(
		t,
		index,
	).SelectOutbound(t.Context(), OutboundSelectionConfig{}); err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}
	engine.putErrors[postingsBucket] = errors.New("put failed")
	if _, err := outboundStore(t, index).RecoverOutbound(t.Context()); err == nil {
		t.Fatal("expected recovery put error")
	}

	_, index, _, _, engine = openScriptedRWI(
		t,
		fakeURLDirectory{},
		failingObserver{storeErr: errors.New("observer failed")},
	)
	engine.buckets[outboundSelectedBucket][string(postingKey(entry.WordHash, referencedHash(t, entry)))] = raw
	if _, err := outboundStore(t, index).RecoverOutbound(t.Context()); err == nil {
		t.Fatal("expected recovery observer error")
	}

	_, index, receiver, _, _ = openScriptedRWI(t, fakeURLDirectory{})
	if _, err := receiver.Receive(t.Context(), []yagomodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if _, err := outboundStore(
		t,
		index,
	).SelectOutbound(t.Context(), OutboundSelectionConfig{}); err != nil {
		t.Fatalf("SelectOutbound: %v", err)
	}
	ctx := &errAfterContext{Context: context.Background(), remaining: 2, err: context.Canceled}
	if _, err := outboundStore(t, index).RecoverOutbound(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("RecoverOutbound error = %v, want context.Canceled", err)
	}
}

func TestSelectOutboundReturnsMalformedStoredKeyError(t *testing.T) {
	t.Parallel()

	_, index, _, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	raw, err := (postingCodec{}).Encode(
		postingWithHashes(yagomodel.Hash("AAAAAAAAAAAA"), yagomodel.Hash("CCCCCCCCCCCC")),
	)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	engine.buckets[postingsBucket]["short"] = raw

	if _, err := outboundStore(t, index).SelectOutbound(
		t.Context(),
		OutboundSelectionConfig{},
	); err == nil {
		t.Fatal("expected malformed stored key error")
	}
}

func TestPostingKeyHashesRejectsMalformedKeys(t *testing.T) {
	t.Parallel()

	if _, _, err := postingKeyHashes(vault.Key("short")); err == nil {
		t.Fatal("expected short key error")
	}
	if _, _, err := postingKeyHashes(vault.Key("!!!!!!!!!!!!AAAAAAAAAAAA")); err == nil {
		t.Fatal("expected word hash error")
	}
	if _, _, err := postingKeyHashes(vault.Key("AAAAAAAAAAAA!!!!!!!!!!!!")); err == nil {
		t.Fatal("expected url hash error")
	}
}

func assertStoredPosting(
	t *testing.T,
	ctx context.Context,
	index PostingIndex,
	word yagomodel.Hash,
	url yagomodel.Hash,
) {
	t.Helper()

	seen := 0
	if err := index.ScanWord(ctx, word, func(posting yagomodel.RWIPosting) (bool, error) {
		seen++
		got, err := posting.URLHash()
		if err != nil {
			return false, fmt.Errorf("posting url hash: %w", err)
		}
		if got.Hash() != url {
			t.Fatalf("url = %s, want %s", got.Hash(), url)
		}

		return true, nil
	}); err != nil {
		t.Fatalf("ScanWord: %v", err)
	}
	if seen != 1 {
		t.Fatalf("seen = %d, want 1", seen)
	}
}
