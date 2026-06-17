package services

import (
	"context"
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
