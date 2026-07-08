// Package vaulttest holds the storage-contract suite every vault Engine driver
// runs against itself. A driver passes its own opener to RunConformance and the
// suite exercises the guarantees a backend must honour: durable round trips,
// scan ordering, transaction atomicity, bucket isolation, and byte accounting.
// Engine-independent behaviour the port enforces lives with the port's own
// tests, not here.
package vaulttest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/nikitakarpei/yacy-rwi-node/yacynode/internal/vault"
)

type stringCodec struct{}

func (stringCodec) Encode(value string) ([]byte, error) { return []byte(value), nil }
func (stringCodec) Decode(raw []byte) (string, error)   { return string(raw), nil }

func RunConformance(t *testing.T, open func(quotaBytes int64) (*vault.Vault, error)) {
	t.Helper()

	t.Run("RoundTripAndLength", func(t *testing.T) { roundTripAndLength(t, open) })
	t.Run("MissingKeyReportsAbsent", func(t *testing.T) { missingKeyReportsAbsent(t, open) })
	t.Run(
		"LengthAfterDeleteAndOverwrite",
		func(t *testing.T) { lengthAfterDeleteAndOverwrite(t, open) },
	)
	t.Run("ScanVisitsPrefixInOrder", func(t *testing.T) { scanVisitsPrefixInOrder(t, open) })
	t.Run("ScanStopsWhenAsked", func(t *testing.T) { scanStopsWhenAsked(t, open) })
	t.Run(
		"CrossCollectionAtomicRollback",
		func(t *testing.T) { crossCollectionAtomicRollback(t, open) },
	)
	t.Run("BucketOwnershipIsolation", func(t *testing.T) { bucketOwnershipIsolation(t, open) })
	t.Run("AtCapacityTracksQuota", func(t *testing.T) { atCapacityTracksQuota(t, open) })
	t.Run("UsedBytesGrowsWithData", func(t *testing.T) { usedBytesGrowsWithData(t, open) })
}

func openVault(
	t *testing.T,
	open func(int64) (*vault.Vault, error),
	quotaBytes int64,
) *vault.Vault {
	t.Helper()

	v, err := open(quotaBytes)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
	})

	return v
}

func register(t *testing.T, v *vault.Vault, name string) *vault.Collection[string] {
	t.Helper()

	collection, err := vault.Register(v, vault.Name(name), stringCodec{})
	if err != nil {
		t.Fatalf("Register %s: %v", name, err)
	}

	return collection
}

func wrapTest(err error) error {
	return fmt.Errorf("vault op: %w", err)
}

func roundTripAndLength(t *testing.T, open func(int64) (*vault.Vault, error)) {
	ctx := context.Background()
	v := openVault(t, open, 0)
	words := register(t, v, "words")

	if err := v.Update(ctx, func(tx *vault.Txn) error {
		if err := words.Put(tx, vault.Key("a"), "alpha"); err != nil {
			return wrapTest(err)
		}

		return words.Put(tx, vault.Key("b"), "beta")
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := v.View(ctx, func(tx *vault.Txn) error {
		got, ok, err := words.Get(tx, vault.Key("a"))
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

func missingKeyReportsAbsent(t *testing.T, open func(int64) (*vault.Vault, error)) {
	v := openVault(t, open, 0)
	words := register(t, v, "words")

	if err := v.View(context.Background(), func(tx *vault.Txn) error {
		_, ok, err := words.Get(tx, vault.Key("absent"))
		if err != nil {
			return wrapTest(err)
		}
		if ok {
			t.Fatal("Get reported a missing key as present")
		}

		return nil
	}); err != nil {
		t.Fatalf("View: %v", err)
	}
}

func lengthAfterDeleteAndOverwrite(t *testing.T, open func(int64) (*vault.Vault, error)) {
	ctx := context.Background()
	v := openVault(t, open, 0)
	words := register(t, v, "words")

	if err := v.Update(ctx, func(tx *vault.Txn) error {
		if err := words.Put(tx, vault.Key("a"), "alpha"); err != nil {
			return wrapTest(err)
		}
		if err := words.Put(tx, vault.Key("a"), "again"); err != nil {
			return wrapTest(err)
		}
		deleted, err := words.Delete(tx, vault.Key("a"))
		if err != nil {
			return wrapTest(err)
		}
		if !deleted {
			t.Fatal("Delete reported missing key")
		}
		missing, err := words.Delete(tx, vault.Key("a"))
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

	if err := v.View(ctx, func(tx *vault.Txn) error {
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

func scanVisitsPrefixInOrder(t *testing.T, open func(int64) (*vault.Vault, error)) {
	ctx := context.Background()
	v := openVault(t, open, 0)
	words := register(t, v, "words")

	if err := v.Update(ctx, func(tx *vault.Txn) error {
		for _, key := range []string{"qa", "pb", "pa"} {
			if err := words.Put(tx, vault.Key(key), key); err != nil {
				return wrapTest(err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var visited []string
	if err := v.View(ctx, func(tx *vault.Txn) error {
		return words.Scan(tx, vault.Key("p"), func(_ vault.Key, value string) (bool, error) {
			visited = append(visited, value)

			return true, nil
		})
	}); err != nil {
		t.Fatalf("View: %v", err)
	}

	if len(visited) != 2 || visited[0] != "pa" || visited[1] != "pb" {
		t.Fatalf("scan visited = %v, want [pa pb]", visited)
	}
}

func scanStopsWhenAsked(t *testing.T, open func(int64) (*vault.Vault, error)) {
	ctx := context.Background()
	v := openVault(t, open, 0)
	words := register(t, v, "words")

	if err := v.Update(ctx, func(tx *vault.Txn) error {
		for _, key := range []string{"a", "b", "c"} {
			if err := words.Put(tx, vault.Key(key), key); err != nil {
				return wrapTest(err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var visited []string
	if err := v.View(ctx, func(tx *vault.Txn) error {
		return words.Scan(tx, nil, func(_ vault.Key, value string) (bool, error) {
			visited = append(visited, value)

			return false, nil
		})
	}); err != nil {
		t.Fatalf("View: %v", err)
	}

	if len(visited) != 1 || visited[0] != "a" {
		t.Fatalf("scan visited = %v, want [a]", visited)
	}
}

func crossCollectionAtomicRollback(t *testing.T, open func(int64) (*vault.Vault, error)) {
	ctx := context.Background()
	v := openVault(t, open, 0)
	left := register(t, v, "left")
	right := register(t, v, "right")

	sentinel := errors.New("boom")
	err := v.Update(ctx, func(tx *vault.Txn) error {
		if err := left.Put(tx, vault.Key("a"), "alpha"); err != nil {
			return wrapTest(err)
		}
		if err := right.Put(tx, vault.Key("b"), "beta"); err != nil {
			return wrapTest(err)
		}

		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Update error = %v, want sentinel", err)
	}

	if err := v.View(ctx, func(tx *vault.Txn) error {
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

func bucketOwnershipIsolation(t *testing.T, open func(int64) (*vault.Vault, error)) {
	ctx := context.Background()
	v := openVault(t, open, 0)
	left := register(t, v, "left")
	right := register(t, v, "right")

	if err := v.Update(ctx, func(tx *vault.Txn) error {
		return left.Put(tx, vault.Key("a"), "alpha")
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if err := v.View(ctx, func(tx *vault.Txn) error {
		_, ok, err := right.Get(tx, vault.Key("a"))
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

func atCapacityTracksQuota(t *testing.T, open func(int64) (*vault.Vault, error)) {
	ctx := context.Background()
	v := openVault(t, open, 1)
	words := register(t, v, "words")

	if err := v.Update(ctx, func(tx *vault.Txn) error {
		return words.Put(tx, vault.Key("a"), "alpha")
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	atCapacity, err := v.AtCapacity(ctx)
	if err != nil {
		t.Fatalf("AtCapacity: %v", err)
	}
	if !atCapacity {
		t.Fatal("AtCapacity = false, want true over quota")
	}

	used, err := v.UsedBytes(ctx)
	if err != nil {
		t.Fatalf("UsedBytes: %v", err)
	}
	if used < v.QuotaBytes() {
		t.Fatalf("UsedBytes = %d, want >= quota %d", used, v.QuotaBytes())
	}
}

func usedBytesGrowsWithData(t *testing.T, open func(int64) (*vault.Vault, error)) {
	ctx := context.Background()
	v := openVault(t, open, 4096)
	words := register(t, v, "words")

	before, err := v.UsedBytes(ctx)
	if err != nil {
		t.Fatalf("UsedBytes: %v", err)
	}

	if err := v.Update(ctx, func(tx *vault.Txn) error {
		return words.Put(tx, vault.Key("a"), "alpha")
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	after, err := v.UsedBytes(ctx)
	if err != nil {
		t.Fatalf("UsedBytes: %v", err)
	}
	if after < before {
		t.Fatalf("UsedBytes shrank: before=%d after=%d", before, after)
	}
	if v.QuotaBytes() != 4096 {
		t.Fatalf("QuotaBytes = %d, want 4096", v.QuotaBytes())
	}
}
