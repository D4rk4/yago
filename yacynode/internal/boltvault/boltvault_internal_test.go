package boltvault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yacynode/internal/vault"
)

func openTestBolt(t *testing.T) *bolt.DB {
	t.Helper()
	db, err := bolt.Open(filepath.Join(t.TempDir(), "node.db"), 0o600, nil)
	if err != nil {
		t.Fatalf("open bolt: %v", err)
	}
	t.Cleanup(func() {
		if db != nil {
			_ = db.Close()
		}
	})
	if err := db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists([]byte("bucket"))
		if err != nil {
			return fmt.Errorf("create bucket: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	return db
}

func TestOpenReturnsDirectoryCreationError(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if _, err := Open(filepath.Join(file, "node.db"), 0); err == nil {
		t.Fatal("expected directory creation error")
	}
}

func TestOpenReturnsBoltOpenError(t *testing.T) {
	if _, err := Open(t.TempDir(), 0); err == nil {
		t.Fatal("expected bolt open error for directory path")
	}
}

func TestOpenReturnsVaultInitError(t *testing.T) {
	saved := newVault
	t.Cleanup(func() { newVault = saved })
	sentinel := errors.New("new vault failed")
	newVault = func(vault.Engine) (*vault.Vault, error) {
		return nil, sentinel
	}

	if _, err := Open(filepath.Join(t.TempDir(), "node.db"), 0); !errors.Is(err, sentinel) {
		t.Fatalf("Open error = %v, want %v", err, sentinel)
	}
}

func TestProvisionReturnsClosedDatabaseError(t *testing.T) {
	db := openTestBolt(t)
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	err := (&engine{db: db}).Provision(vault.Name("bucket"))
	if err == nil {
		t.Fatal("expected provision error on closed database")
	}
}

func TestProvisionReturnsBucketCreationError(t *testing.T) {
	engine := &engine{db: openTestBolt(t)}

	err := engine.Provision(vault.Name(""))
	if err == nil {
		t.Fatal("expected bucket creation error")
	}
}

func TestUpdateReturnsFunctionError(t *testing.T) {
	sentinel := errors.New("update failed")
	engine := &engine{db: openTestBolt(t)}

	err := engine.Update(context.Background(), func(vault.EngineTxn) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Update error = %v, want %v", err, sentinel)
	}
}

func TestUpdateMapsCapacityError(t *testing.T) {
	engine := &engine{db: openTestBolt(t)}

	err := engine.Update(context.Background(), func(vault.EngineTxn) error {
		return syscall.ENOSPC
	})
	if !errors.Is(err, vault.ErrAtCapacity) {
		t.Fatalf("Update error = %v, want ErrAtCapacity", err)
	}
}

func TestViewReturnsFunctionError(t *testing.T) {
	sentinel := errors.New("view failed")
	engine := &engine{db: openTestBolt(t)}

	err := engine.View(context.Background(), func(vault.EngineTxn) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("View error = %v, want %v", err, sentinel)
	}
}

func TestCloseClosesDatabase(t *testing.T) {
	db := openTestBolt(t)
	engine := &engine{db: db}
	if err := engine.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestWrapCloseError(t *testing.T) {
	if err := wrapCloseError(nil); err != nil {
		t.Fatalf("nil close error wrapped to %v", err)
	}
	sentinel := errors.New("close failed")
	if err := wrapCloseError(sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("wrapped close error = %v, want %v", err, sentinel)
	}
}

func TestUsedBytesRejectsCanceledContext(t *testing.T) {
	engine := &engine{db: openTestBolt(t)}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := engine.UsedBytes(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("UsedBytes error = %v, want context.Canceled", err)
	}
}

func TestUsedBytesReturnsClosedDatabaseError(t *testing.T) {
	db := openTestBolt(t)
	engine := &engine{db: db}
	if err := db.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	if _, err := engine.UsedBytes(context.Background()); err == nil {
		t.Fatal("expected UsedBytes error on closed database")
	}
}

func TestBoltBucketRejectsReadOnlyMutation(t *testing.T) {
	db := openTestBolt(t)
	if err := db.View(func(tx *bolt.Tx) error {
		bucket := boltBucket{bucket: tx.Bucket([]byte("bucket"))}
		if err := bucket.Put(vault.Key("key"), []byte("value")); err == nil {
			t.Fatal("expected read-only put error")
		}
		if err := bucket.Delete(vault.Key("key")); err == nil {
			t.Fatal("expected read-only delete error")
		}
		return nil
	}); err != nil {
		t.Fatalf("view: %v", err)
	}
}

func TestBoltBucketScanReturnsCallbackError(t *testing.T) {
	sentinel := errors.New("scan failed")
	db := openTestBolt(t)
	if err := db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("bucket")).Put([]byte("prefix-key"), []byte("value"))
	}); err != nil {
		t.Fatalf("put: %v", err)
	}

	if err := db.View(func(tx *bolt.Tx) error {
		bucket := boltBucket{bucket: tx.Bucket([]byte("bucket"))}
		return bucket.Scan(vault.Key("prefix"), func(vault.Key, []byte) (bool, error) {
			return false, sentinel
		})
	}); !errors.Is(err, sentinel) {
		t.Fatalf("Scan error = %v, want %v", err, sentinel)
	}
}

func TestStorageAtCapacityErrorRecognizesMessages(t *testing.T) {
	for _, err := range []error{
		syscall.EDQUOT,
		syscall.EFBIG,
		errors.New("disk quota exceeded"),
		errors.New("file too large"),
		errors.New("not enough space"),
	} {
		if !storageAtCapacityError(err) {
			t.Fatalf("storageAtCapacityError(%v) = false", err)
		}
	}
	if storageAtCapacityError(errors.New("permission denied")) {
		t.Fatal("permission denied should not be treated as capacity error")
	}
}
