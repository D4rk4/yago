package services

import (
	"context"
	"errors"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestReceiveURLsReportsDoublesAndRejects(t *testing.T) {
	existing := []yacymodel.Hash{hashFor("a"), hashFor("b")}
	rejected := []yacymodel.Hash{hashFor("c")}
	store := &fakeURLStore{existing: existing, rejected: rejected}
	receiver := NewURLReceiver(store)

	receipt, err := receiver.ReceiveURLs(context.Background(), []yacymodel.URIMetadataRow{{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receipt.Double != 2 {
		t.Errorf("double: got %d, want 2", receipt.Double)
	}
	if len(receipt.ErrorURL) != 1 {
		t.Errorf("error url: got %v, want 1", receipt.ErrorURL)
	}
	if len(store.stored) != 1 {
		t.Errorf("expected one store call, got %d", len(store.stored))
	}
}

func TestReceiveURLsTriggersEvictionAfterStore(t *testing.T) {
	store := &fakeURLStore{}
	triggered := 0
	receiver := NewURLReceiver(store, WithURLEvictionTrigger(func() { triggered++ }))

	if _, err := receiver.ReceiveURLs(
		context.Background(),
		[]yacymodel.URIMetadataRow{{}},
	); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if triggered != 1 {
		t.Fatalf("trigger calls: got %d, want 1", triggered)
	}
}

func TestReceiveURLsSkipsEvictionTriggerOnFailure(t *testing.T) {
	store := &fakeURLStore{storeErr: ports.ErrStoreFailure}
	triggered := 0
	receiver := NewURLReceiver(store, WithURLEvictionTrigger(func() { triggered++ }))

	if _, err := receiver.ReceiveURLs(
		context.Background(),
		[]yacymodel.URIMetadataRow{{}},
	); !errors.Is(err, ports.ErrStoreFailure) {
		t.Fatalf("got %v, want ErrStoreFailure", err)
	}
	if triggered != 0 {
		t.Fatalf("trigger calls: got %d, want 0", triggered)
	}
}

func TestReceiveURLsCapacityBusy(t *testing.T) {
	store := &fakeURLStore{storeErr: ports.ErrAtCapacity}
	receiver := NewURLReceiver(store)

	receipt, err := receiver.ReceiveURLs(context.Background(), []yacymodel.URIMetadataRow{{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !receipt.Busy {
		t.Error("expected busy on capacity error")
	}
}

func TestReceiveURLsWrapsStoreFailure(t *testing.T) {
	store := &fakeURLStore{storeErr: ports.ErrStoreFailure}
	receiver := NewURLReceiver(store)

	if _, err := receiver.ReceiveURLs(
		context.Background(),
		[]yacymodel.URIMetadataRow{{}},
	); !errors.Is(err, ports.ErrStoreFailure) {
		t.Fatalf("got %v, want ErrStoreFailure", err)
	}
}
