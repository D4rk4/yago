package boltvault_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
	"github.com/D4rk4/yago/yagonode/internal/vaulttest"
)

type stringCodec struct{}

func (stringCodec) Encode(value string) ([]byte, error) { return []byte(value), nil }
func (stringCodec) Decode(raw []byte) (string, error)   { return string(raw), nil }

func TestConformance(t *testing.T) {
	dir := t.TempDir()
	var seq atomic.Int64

	vaulttest.RunConformance(t, func(quotaBytes int64) (*vault.Vault, error) {
		path := filepath.Join(dir, fmt.Sprintf("node-%d.db", seq.Add(1)))

		return boltvault.Open(path, quotaBytes)
	})
}

func TestDurabilityAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")

	first, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	words, err := vault.Register(first, vault.Name("words"), stringCodec{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := first.Update(ctx, func(tx *vault.Txn) error {
		return words.Put(tx, vault.Key("a"), "alpha")
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	words, err = vault.Register(reopened, vault.Name("words"), stringCodec{})
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}

	if err := reopened.View(ctx, func(tx *vault.Txn) error {
		got, ok, err := words.Get(tx, vault.Key("a"))
		if err != nil {
			return fmt.Errorf("get: %w", err)
		}
		if !ok || got != "alpha" {
			t.Fatalf("after reopen Get(a) = %q, %v", got, ok)
		}

		return nil
	}); err != nil {
		t.Fatalf("View: %v", err)
	}
}

func TestWritesAreNotGatedByQuota(t *testing.T) {
	ctx := context.Background()
	store, err := boltvault.Open(filepath.Join(t.TempDir(), "node.db"), 1)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})
	words, err := vault.Register(store, vault.Name("words"), stringCodec{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if err := store.Update(ctx, func(tx *vault.Txn) error {
		return words.Put(tx, vault.Key("a"), "alpha")
	}); err != nil {
		t.Fatalf("Update over quota = %v, want nil (kernel does not gate writes)", err)
	}
}
