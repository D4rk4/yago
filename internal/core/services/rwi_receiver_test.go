package services

import (
	"context"
	"errors"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func rwiEntries(n int) []yacymodel.RWIEntry {
	entries := make([]yacymodel.RWIEntry, n)
	for i := range entries {
		entries[i] = postingEntry(hashFor("word"), "url", 0)
	}

	return entries
}

func TestReceiveRWIPersistsAndReports(t *testing.T) {
	unknown := []yacymodel.Hash{hashFor("miss")}
	rejected := []yacymodel.Hash{hashFor("bad")}
	rwi := &fakeRWIStore{rejected: rejected}
	urls := &fakeURLStore{missing: unknown}
	receiver := NewRWIReceiver(rwi, urls, 10, 30)

	receipt, err := receiver.ReceiveRWI(context.Background(), rwiEntries(2))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rwi.appended) != 1 {
		t.Fatalf("expected one append, got %d", len(rwi.appended))
	}
	if len(receipt.UnknownURL) != 1 || receipt.UnknownURL[0] != unknown[0] {
		t.Errorf("unknown url: got %v, want %v", receipt.UnknownURL, unknown)
	}
	if len(receipt.ErrorURL) != 1 || receipt.ErrorURL[0] != rejected[0] {
		t.Errorf("error url: got %v, want %v", receipt.ErrorURL, rejected)
	}
	if receipt.Busy {
		t.Error("did not expect busy")
	}
}

func TestReceiveRWIBatchCap(t *testing.T) {
	store := &fakeRWIStore{}
	receiver := NewRWIReceiver(store, &fakeURLStore{}, 1, 30)

	receipt, err := receiver.ReceiveRWI(context.Background(), rwiEntries(2))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !receipt.Busy || receipt.Pause != 30 {
		t.Errorf("got busy=%v pause=%d, want busy pause=30", receipt.Busy, receipt.Pause)
	}
	if len(store.appended) != 0 {
		t.Error("over-cap batch must not be persisted")
	}
}

func TestReceiveRWICapacityBackpressure(t *testing.T) {
	store := &fakeRWIStore{appendErr: ports.ErrAtCapacity}
	receiver := NewRWIReceiver(store, &fakeURLStore{}, 10, 15)

	receipt, err := receiver.ReceiveRWI(context.Background(), rwiEntries(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !receipt.Busy || receipt.Pause != 15 {
		t.Errorf("got busy=%v pause=%d, want busy pause=15", receipt.Busy, receipt.Pause)
	}
}

func TestReceiveRWIPropagatesError(t *testing.T) {
	wantErr := errors.New("boom")
	store := &fakeRWIStore{appendErr: wantErr}
	receiver := NewRWIReceiver(store, &fakeURLStore{}, 10, 15)

	if _, err := receiver.ReceiveRWI(
		context.Background(),
		rwiEntries(1),
	); !errors.Is(
		err,
		wantErr,
	) {
		t.Fatalf("got %v, want %v", err, wantErr)
	}
}

func TestReceiveRWIPropagatesMissingURLsError(t *testing.T) {
	wantErr := errors.New("boom")
	receiver := NewRWIReceiver(&fakeRWIStore{}, &fakeURLStore{missingErr: wantErr}, 10, 15)

	if _, err := receiver.ReceiveRWI(
		context.Background(),
		rwiEntries(1),
	); !errors.Is(err, wantErr) {
		t.Fatalf("got %v, want %v", err, wantErr)
	}
}
