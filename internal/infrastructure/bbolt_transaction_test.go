package infrastructure

import (
	"errors"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
)

func TestBboltStorageHidesImplementationWriteErrors(t *testing.T) {
	err := wrapStorageError("write storage", errors.New("bbolt internal detail"))
	if !errors.Is(err, ports.ErrStoreFailure) {
		t.Fatalf("mapped error = %v, want ErrStoreFailure", err)
	}
	if errors.Is(err, ports.ErrAtCapacity) {
		t.Fatalf("mapped error = %v, did not want ErrAtCapacity", err)
	}
}
