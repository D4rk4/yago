package documentstore

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOpenSurfacesAdmissionRecoveryReadFailure(t *testing.T) {
	engine := newDocumentStorageFaultEngine()
	storage, err := vault.New(engine)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	sentinel := errors.New("admission read failed")
	engine.viewError = sentinel
	if _, _, err := Open(storage); !errors.Is(err, sentinel) {
		t.Fatalf("open error = %v", err)
	}
}

func TestStoredDocumentURLWriteBoundaryRejectsCanceledEntry(t *testing.T) {
	boundary := &storedDocumentURLBoundary{}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := boundary.enterWrite(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("write boundary error = %v", err)
	}
}
