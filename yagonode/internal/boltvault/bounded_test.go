package boltvault_test

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestOpenWithLockTimeoutRejectsUnboundedTimeout(t *testing.T) {
	for _, timeout := range []time.Duration{0, -time.Second} {
		storage, err := boltvault.OpenWithLockTimeout(
			filepath.Join(t.TempDir(), "state.db"), timeout,
		)
		if err == nil {
			_ = storage.Close()
			t.Fatalf("OpenWithLockTimeout(%v) accepted an unbounded timeout", timeout)
		}
	}
}

func TestOpenWithLockTimeoutStopsOnHeldFileAndReopensAfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	first, err := boltvault.OpenWithLockTimeout(path, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("open first state: %v", err)
	}
	started := time.Now()
	second, err := boltvault.OpenWithLockTimeout(path, 20*time.Millisecond)
	if err == nil {
		_ = second.Close()
		t.Fatal("second open acquired held state file")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("held state open took %v", elapsed)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("close first state: %v", err)
	}
	reopened, err := boltvault.OpenWithLockTimeout(path, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("reopen state: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened state: %v", err)
	}
}

func TestBoundedBoltVaultPersistsCompletedRetainedMigration(t *testing.T) {
	source, err := boltvault.OpenWithLockTimeout(
		filepath.Join(t.TempDir(), "source.db"),
		time.Second,
	)
	if err != nil {
		t.Fatalf("open source: %v", err)
	}
	orders, err := vault.Register(source, "orders", stringCodec{})
	if err != nil {
		t.Fatalf("register source orders: %v", err)
	}
	if err := source.Update(t.Context(), func(tx *vault.Txn) error {
		return orders.Put(tx, vault.Key("order-1"), "payload")
	}); err != nil {
		t.Fatalf("seed source: %v", err)
	}
	path := filepath.Join(t.TempDir(), "state.db")
	target, err := boltvault.OpenWithLockTimeout(path, time.Second)
	if err != nil {
		t.Fatalf("open target: %v", err)
	}
	if err := vault.MigrateRetainedBuckets(
		t.Context(), source, target, "migration", "1", []vault.Name{"orders"},
	); err != nil {
		t.Fatalf("migrate retained orders: %v", err)
	}
	if err := target.Close(); err != nil {
		t.Fatalf("close target: %v", err)
	}
	if err := source.Close(); err != nil {
		t.Fatalf("close source: %v", err)
	}
	reopened, err := boltvault.OpenWithLockTimeout(path, time.Second)
	if err != nil {
		t.Fatalf("reopen target: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := vault.MigrateRetainedBuckets(
		context.Background(), source, reopened, "migration", "1", []vault.Name{"orders"},
	); err != nil {
		t.Fatalf("completed migration touched source: %v", err)
	}
	reopenedOrders, err := vault.Register(reopened, "orders", stringCodec{})
	if err != nil {
		t.Fatalf("register reopened orders: %v", err)
	}
	if err := reopened.View(t.Context(), func(tx *vault.Txn) error {
		value, found, err := reopenedOrders.Get(tx, vault.Key("order-1"))
		if err != nil {
			return fmt.Errorf("read reopened order: %w", err)
		}
		if !found || value != "payload" {
			t.Fatalf("reopened order = %q found=%t", value, found)
		}

		return nil
	}); err != nil {
		t.Fatalf("read reopened orders: %v", err)
	}
}
