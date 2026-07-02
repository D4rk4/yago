package rwi

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yacymodel"
	"github.com/D4rk4/yago/yacynode/internal/vault"
)

func outboundStore(tb testing.TB, index PostingIndex) OutboundPostingStore {
	tb.Helper()

	store, ok := index.(OutboundPostingStore)
	if !ok {
		tb.Fatal("PostingIndex does not expose outbound store")
	}

	return store
}

func postingWithHashes(word, url yacymodel.Hash) yacymodel.RWIPosting {
	return yacymodel.RWIPosting{
		WordHash: word,
		Properties: map[string]string{
			yacymodel.ColURLHash:        url.String(),
			yacymodel.ColLocalLinkCount: "1",
			yacymodel.ColHitCount:       "1",
			yacymodel.ColWordDistance:   "0",
		},
	}
}

func TestSelectOutboundUsesDefaultBoundsAndDeletesSelectedPosting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := openHarness(t, 0, 100)
	firstWord := yacymodel.Hash("AAAAAAAAAAAA")
	secondWord := yacymodel.Hash("BBBBBBBBBBBB")
	firstURL := yacymodel.Hash("CCCCCCCCCCCC")
	secondURL := yacymodel.Hash("DDDDDDDDDDDD")

	if _, err := h.rwi.Receiver.Receive(ctx, []yacymodel.RWIPosting{
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
	firstWord := yacymodel.Hash("AAAAAAAAAAAA")
	secondWord := yacymodel.Hash("BBBBBBBBBBBB")
	if _, err := h.rwi.Receiver.Receive(ctx, []yacymodel.RWIPosting{
		postingWithHashes(firstWord, yacymodel.Hash("CCCCCCCCCCCC")),
		postingWithHashes(firstWord, yacymodel.Hash("DDDDDDDDDDDD")),
		postingWithHashes(secondWord, yacymodel.Hash("EEEEEEEEEEEE")),
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
	word := yacymodel.Hash("AAAAAAAAAAAA")
	if _, err := h.rwi.Receiver.Receive(ctx, []yacymodel.RWIPosting{
		postingWithHashes(word, yacymodel.Hash("CCCCCCCCCCCC")),
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
	if err := h.rwi.Index.ScanWord(ctx, word, func(yacymodel.RWIPosting) (bool, error) {
		visited++

		return true, nil
	}); err != nil {
		t.Fatalf("ScanWord: %v", err)
	}
	if visited != 1 {
		t.Fatalf("visited = %d, want restored posting", visited)
	}
}

func TestSelectOutboundReturnsStorageAndObserverErrors(t *testing.T) {
	t.Parallel()

	entry := postingWithHashes(yacymodel.Hash("AAAAAAAAAAAA"), yacymodel.Hash("CCCCCCCCCCCC"))

	_, index, receiver, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	if _, err := receiver.Receive(t.Context(), []yacymodel.RWIPosting{entry}); err != nil {
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
	if _, err := receiver.Receive(t.Context(), []yacymodel.RWIPosting{entry}); err != nil {
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
	if _, err := receiver.Receive(t.Context(), []yacymodel.RWIPosting{entry}); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if _, err := outboundStore(t, index).SelectOutbound(
		t.Context(),
		OutboundSelectionConfig{},
	); err == nil {
		t.Fatal("expected observer error")
	}
}

func TestSelectOutboundReturnsContextError(t *testing.T) {
	t.Parallel()

	h := openHarness(t, 0, 100)
	if _, err := h.rwi.Receiver.Receive(t.Context(), []yacymodel.RWIPosting{
		postingWithHashes(yacymodel.Hash("AAAAAAAAAAAA"), yacymodel.Hash("CCCCCCCCCCCC")),
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

	good := yacymodel.WordPostings{
		WordHash: yacymodel.Hash("AAAAAAAAAAAA"),
		Postings: []yacymodel.RWIPosting{
			postingWithHashes(yacymodel.Hash("AAAAAAAAAAAA"), yacymodel.Hash("CCCCCCCCCCCC")),
		},
	}
	bad := yacymodel.WordPostings{
		WordHash: yacymodel.Hash("AAAAAAAAAAAA"),
		Postings: []yacymodel.RWIPosting{{Properties: map[string]string{}}},
	}

	_, index, _, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	engine.putErrors[postingsBucket] = errors.New("put failed")
	if _, err := outboundStore(
		t,
		index,
	).RestoreOutbound(t.Context(), []yacymodel.WordPostings{good}); err == nil {
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
	).RestoreOutbound(t.Context(), []yacymodel.WordPostings{good}); err == nil {
		t.Fatal("expected observer error")
	}

	if _, err := outboundStore(
		t,
		index,
	).RestoreOutbound(t.Context(), []yacymodel.WordPostings{bad}); err == nil {
		t.Fatal("expected bad posting error")
	}

	ctx := &errAfterContext{Context: context.Background(), remaining: 2, err: context.Canceled}
	if _, err := outboundStore(
		t,
		index,
	).RestoreOutbound(ctx, []yacymodel.WordPostings{good}); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("RestoreOutbound error = %v, want context.Canceled", err)
	}
}

func TestSelectOutboundReturnsMalformedStoredKeyError(t *testing.T) {
	t.Parallel()

	_, index, _, _, engine := openScriptedRWI(t, fakeURLDirectory{})
	raw, err := (postingCodec{}).Encode(
		postingWithHashes(yacymodel.Hash("AAAAAAAAAAAA"), yacymodel.Hash("CCCCCCCCCCCC")),
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
