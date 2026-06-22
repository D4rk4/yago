package boltvault_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/internal/boltvault"
)

type stringCodec struct{}

func (stringCodec) Encode(v string) ([]byte, error) { return []byte(v), nil }
func (stringCodec) Decode(b []byte) (string, error) { return string(b), nil }

func openVault(t *testing.T, quotaBytes int64) *boltvault.Vault {
	t.Helper()

	vault, err := boltvault.Open(filepath.Join(t.TempDir(), "node.db"), quotaBytes)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := vault.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	return vault
}

func register(t *testing.T, vault *boltvault.Vault, name string) *boltvault.Collection[string] {
	t.Helper()

	collection, err := boltvault.Register(vault, boltvault.Name(name), stringCodec{})
	if err != nil {
		t.Fatalf("Register %s: %v", name, err)
	}

	return collection
}

func TestRoundTripAndLength(t *testing.T) {
	ctx := context.Background()
	vault := openVault(t, 0)
	words := register(t, vault, "words")

	if err := vault.Update(ctx, func(tx *boltvault.Txn) error {
		if err := words.Put(tx, boltvault.Key("a"), "alpha"); err != nil {
			return wrapTest(err)
		}

		return words.Put(tx, boltvault.Key("b"), "beta")
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := vault.View(ctx, func(tx *boltvault.Txn) error {
		got, ok, err := words.Get(tx, boltvault.Key("a"))
		if err != nil {
			return wrapTest(err)
		}
		if !ok || got != "alpha" {
			t.Fatalf("Get(a) = %q, %v", got, ok)
		}

		length, err := words.Len(tx)
		if err != nil {
			return wrapTest(err)
		}
		if length != 2 {
			t.Fatalf("Len = %d, want 2", length)
		}

		return nil
	}); err != nil {
		t.Fatalf("View: %v", err)
	}
}

func TestLengthAfterDeleteAndOverwrite(t *testing.T) {
	ctx := context.Background()
	vault := openVault(t, 0)
	words := register(t, vault, "words")

	if err := vault.Update(ctx, func(tx *boltvault.Txn) error {
		if err := words.Put(tx, boltvault.Key("a"), "alpha"); err != nil {
			return wrapTest(err)
		}
		if err := words.Put(tx, boltvault.Key("a"), "again"); err != nil {
			return wrapTest(err)
		}
		deleted, err := words.Delete(tx, boltvault.Key("a"))
		if err != nil {
			return wrapTest(err)
		}
		if !deleted {
			t.Fatal("Delete reported missing key")
		}
		missing, err := words.Delete(tx, boltvault.Key("a"))
		if err != nil {
			return wrapTest(err)
		}
		if missing {
			t.Fatal("second Delete reported a deletion")
		}

		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := vault.View(ctx, func(tx *boltvault.Txn) error {
		length, err := words.Len(tx)
		if err != nil {
			return wrapTest(err)
		}
		if length != 0 {
			t.Fatalf("Len = %d, want 0", length)
		}

		return nil
	}); err != nil {
		t.Fatalf("View: %v", err)
	}
}

func TestScanVisitsPrefix(t *testing.T) {
	ctx := context.Background()
	vault := openVault(t, 0)
	words := register(t, vault, "words")

	if err := vault.Update(ctx, func(tx *boltvault.Txn) error {
		for _, key := range []string{"pa", "pb", "qa"} {
			if err := words.Put(tx, boltvault.Key(key), key); err != nil {
				return wrapTest(err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var visited []string
	if err := vault.View(ctx, func(tx *boltvault.Txn) error {
		return words.Scan(tx, boltvault.Key("p"), func(_ boltvault.Key, v string) (bool, error) {
			visited = append(visited, v)

			return true, nil
		})
	}); err != nil {
		t.Fatalf("View: %v", err)
	}

	if len(visited) != 2 || visited[0] != "pa" || visited[1] != "pb" {
		t.Fatalf("scan visited = %v, want [pa pb]", visited)
	}
}

func TestRejectsAtCapacity(t *testing.T) {
	ctx := context.Background()
	vault := openVault(t, 1)
	words := register(t, vault, "words")

	err := vault.Update(ctx, func(tx *boltvault.Txn) error {
		return words.Put(tx, boltvault.Key("a"), "alpha")
	})
	if !errors.Is(err, boltvault.ErrAtCapacity) {
		t.Fatalf("Update error = %v, want ErrAtCapacity", err)
	}
}

func TestCrossCollectionAtomicRollback(t *testing.T) {
	ctx := context.Background()
	vault := openVault(t, 0)
	left := register(t, vault, "left")
	right := register(t, vault, "right")

	sentinel := errors.New("boom")
	err := vault.Update(ctx, func(tx *boltvault.Txn) error {
		if err := left.Put(tx, boltvault.Key("a"), "alpha"); err != nil {
			return wrapTest(err)
		}
		if err := right.Put(tx, boltvault.Key("b"), "beta"); err != nil {
			return wrapTest(err)
		}

		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Update error = %v, want sentinel", err)
	}

	if err := vault.View(ctx, func(tx *boltvault.Txn) error {
		leftLen, err := left.Len(tx)
		if err != nil {
			return wrapTest(err)
		}
		rightLen, err := right.Len(tx)
		if err != nil {
			return wrapTest(err)
		}
		if leftLen != 0 || rightLen != 0 {
			t.Fatalf("lengths after rollback = %d, %d, want 0, 0", leftLen, rightLen)
		}

		return nil
	}); err != nil {
		t.Fatalf("View: %v", err)
	}
}

func TestBucketOwnershipIsolation(t *testing.T) {
	ctx := context.Background()
	vault := openVault(t, 0)
	left := register(t, vault, "left")
	right := register(t, vault, "right")

	if err := vault.Update(ctx, func(tx *boltvault.Txn) error {
		return left.Put(tx, boltvault.Key("a"), "alpha")
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := vault.View(ctx, func(tx *boltvault.Txn) error {
		_, ok, err := right.Get(tx, boltvault.Key("a"))
		if err != nil {
			return wrapTest(err)
		}
		if ok {
			t.Fatal("right collection saw left collection's key")
		}

		return nil
	}); err != nil {
		t.Fatalf("View: %v", err)
	}
}

func TestRejectsDuplicateRegistration(t *testing.T) {
	vault := openVault(t, 0)
	register(t, vault, "words")

	if _, err := boltvault.Register(vault, boltvault.Name("words"), stringCodec{}); err == nil {
		t.Fatal("duplicate Register succeeded, want error")
	}
}

func TestWriteInsideViewReturnsError(t *testing.T) {
	ctx := context.Background()
	vault := openVault(t, 0)
	words := register(t, vault, "words")

	err := vault.View(ctx, func(tx *boltvault.Txn) error {
		return words.Put(tx, boltvault.Key("a"), "alpha")
	})
	if err == nil {
		t.Fatal("Put inside View succeeded, want error")
	}

	deleteErr := vault.View(ctx, func(tx *boltvault.Txn) error {
		_, delErr := words.Delete(tx, boltvault.Key("a"))
		if delErr != nil {
			return wrapTest(delErr)
		}

		return nil
	})
	if deleteErr == nil {
		t.Fatal("Delete inside View succeeded, want error")
	}
}

func TestDurabilityAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "node.db")

	vault, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	words, err := boltvault.Register(vault, boltvault.Name("words"), stringCodec{})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := vault.Update(ctx, func(tx *boltvault.Txn) error {
		return words.Put(tx, boltvault.Key("a"), "alpha")
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := vault.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer func() {
		if err := reopened.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	}()
	words, err = boltvault.Register(reopened, boltvault.Name("words"), stringCodec{})
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}

	if err := reopened.View(ctx, func(tx *boltvault.Txn) error {
		got, ok, err := words.Get(tx, boltvault.Key("a"))
		if err != nil {
			return wrapTest(err)
		}
		if !ok || got != "alpha" {
			t.Fatalf("after reopen Get(a) = %q, %v", got, ok)
		}

		return nil
	}); err != nil {
		t.Fatalf("View: %v", err)
	}
}

func wrapTest(err error) error {
	return fmt.Errorf("vault op: %w", err)
}
