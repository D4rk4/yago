package vaulttest

import (
	"context"
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func RunBucketInspectionConformance(
	t *testing.T,
	open func(int64) (*vault.Vault, error),
) {
	t.Helper()

	t.Run("ExclusiveKeyPages", func(t *testing.T) {
		exclusiveKeyPages(t, open)
	})
	t.Run("EncodedValueSize", func(t *testing.T) {
		encodedValueSize(t, open)
	})
}

func exclusiveKeyPages(t *testing.T, open func(int64) (*vault.Vault, error)) {
	ctx := context.Background()
	v := openVault(t, open, 0)
	words := register(t, v, "words")
	if err := v.Update(ctx, func(tx *vault.Txn) error {
		for _, key := range []string{"d", "a", "c", "b"} {
			if err := words.Put(tx, vault.Key(key), key); err != nil {
				return wrapTest(err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := v.View(ctx, func(tx *vault.Txn) error {
		assertKeyPage(t, readKeyPage(t, tx, nil, 2), []string{"a", "b"}, true)
		assertKeyPage(t, readKeyPage(t, tx, vault.Key("b"), 2), []string{"c", "d"}, false)
		assertKeyPage(t, readKeyPage(t, tx, vault.Key("bb"), 1), []string{"c"}, true)
		assertKeyPage(t, readKeyPage(t, tx, vault.Key("d"), 2), nil, false)

		return nil
	}); err != nil {
		t.Fatalf("View: %v", err)
	}
}

func readKeyPage(
	t *testing.T,
	tx *vault.Txn,
	after vault.Key,
	limit int,
) vault.BucketKeyPage {
	t.Helper()
	page, err := tx.ReadBucketKeyPage(vault.Name("words"), after, limit)
	if err != nil {
		t.Fatalf("ReadBucketKeyPage: %v", err)
	}

	return page
}

func assertKeyPage(t *testing.T, page vault.BucketKeyPage, want []string, more bool) {
	t.Helper()
	got := make([]string, len(page.Keys))
	for index, key := range page.Keys {
		got[index] = string(key)
	}
	if page.More != more || !slices.Equal(got, want) {
		t.Fatalf("key page = %#v, want %v more=%t", page, want, more)
	}
}

func encodedValueSize(t *testing.T, open func(int64) (*vault.Vault, error)) {
	ctx := context.Background()
	v := openVault(t, open, 0)
	words := register(t, v, "words")
	if err := v.Update(ctx, func(tx *vault.Txn) error {
		if err := words.Put(tx, vault.Key("a"), "alpha"); err != nil {
			return wrapTest(err)
		}
		if err := words.Put(tx, vault.Key("empty"), ""); err != nil {
			return wrapTest(err)
		}

		return nil
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := v.View(ctx, func(tx *vault.Txn) error {
		assertEncodedSize(
			t,
			words,
			tx,
			encodedSizeExpectation{key: vault.Key("a"), size: 5, found: true},
		)
		assertEncodedSize(
			t,
			words,
			tx,
			encodedSizeExpectation{key: vault.Key("empty"), found: true},
		)
		assertEncodedSize(
			t,
			words,
			tx,
			encodedSizeExpectation{key: vault.Key("missing")},
		)

		return nil
	}); err != nil {
		t.Fatalf("View: %v", err)
	}
}

type encodedSizeExpectation struct {
	key   vault.Key
	size  int
	found bool
}

func assertEncodedSize(
	t *testing.T,
	words *vault.Collection[string],
	tx *vault.Txn,
	want encodedSizeExpectation,
) {
	t.Helper()
	size, present, err := words.EncodedSize(tx, want.key)
	if err != nil || size != want.size || present != want.found {
		t.Fatalf(
			"EncodedSize(%q) = %d/%t/%v, want %d/%t",
			want.key,
			size,
			present,
			err,
			want.size,
			want.found,
		)
	}
}
