package infrastructure

import (
	"context"
	"errors"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/core/ports"
	"github.com/nikitakarpei/yacy-rwi-node/yacymodel"
)

func TestBboltStorageRejectsAtCapacity(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")
	store := openTestStorage(t, path, 1)
	defer closeTestStorage(t, store)

	_, err := store.StoreURLs(ctx, []yacymodel.URIMetadataRow{urlRowForStorageTest("url-a")})
	if !errors.Is(err, ports.ErrAtCapacity) {
		t.Fatalf("StoreURLs error = %v, want ErrAtCapacity", err)
	}

	_, err = store.AppendRWI(
		ctx,
		[]yacymodel.RWIPosting{rwiPostingForStorageTest(hashForStorageTest("word"), "url-a", 1)},
	)
	if !errors.Is(err, ports.ErrAtCapacity) {
		t.Fatalf("AppendRWI error = %v, want ErrAtCapacity", err)
	}
	assertCount(t, "rwi count", store.RWICount, 0)
	assertCount(t, "url count", store.URLCount, 0)
}

func TestBboltStorageMapsCapacityWriteErrors(t *testing.T) {
	err := wrapStorageError("write storage", syscall.ENOSPC)
	if !errors.Is(err, ports.ErrAtCapacity) {
		t.Fatalf("mapped error = %v, want ErrAtCapacity", err)
	}

	err = wrapStorageError(
		"write storage",
		errors.New("file resize error: no space left on device"),
	)
	if !errors.Is(err, ports.ErrAtCapacity) {
		t.Fatalf("string mapped error = %v, want ErrAtCapacity", err)
	}
}
