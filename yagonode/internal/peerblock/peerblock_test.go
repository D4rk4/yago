package peerblock_test

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagomodel"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/peerblock"
)

func openStore(t *testing.T) *peerblock.Store {
	t.Helper()
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	now := func() time.Time { return time.Date(2026, 7, 4, 10, 0, 0, 0, time.UTC) }
	store, err := peerblock.Open(v, now)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	return store
}

func mustHash(t *testing.T, s string) yagomodel.Hash {
	t.Helper()
	hash, err := yagomodel.ParseHash(s)
	if err != nil {
		t.Fatalf("ParseHash(%q): %v", s, err)
	}

	return hash
}

func TestBlockThenIsBlocked(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	hash := mustHash(t, "AAAAAAAAAAAA")

	if blocked, err := store.IsBlocked(ctx, hash); err != nil || blocked {
		t.Fatalf("IsBlocked before block = %v, %v; want false", blocked, err)
	}
	if err := store.Block(ctx, hash); err != nil {
		t.Fatalf("Block: %v", err)
	}
	if blocked, err := store.IsBlocked(ctx, hash); err != nil || !blocked {
		t.Fatalf("IsBlocked after block = %v, %v; want true", blocked, err)
	}
}

func TestUnblockRemoves(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	hash := mustHash(t, "BBBBBBBBBBBB")

	if err := store.Block(ctx, hash); err != nil {
		t.Fatalf("Block: %v", err)
	}
	if err := store.Unblock(ctx, hash); err != nil {
		t.Fatalf("Unblock: %v", err)
	}
	if blocked, err := store.IsBlocked(ctx, hash); err != nil || blocked {
		t.Fatalf("IsBlocked after unblock = %v, %v; want false", blocked, err)
	}
}

func TestUnblockUnknownIsNoError(t *testing.T) {
	if err := openStore(t).Unblock(context.Background(), mustHash(t, "CCCCCCCCCCCC")); err != nil {
		t.Fatalf("Unblock of a peer that is not blocked should be a no-op: %v", err)
	}
}

func TestBlockedListsAllWithTimes(t *testing.T) {
	ctx := context.Background()
	store := openStore(t)
	first := mustHash(t, "AAAAAAAAAAAA")
	second := mustHash(t, "BBBBBBBBBBBB")

	if err := store.Block(ctx, first); err != nil {
		t.Fatalf("Block first: %v", err)
	}
	if err := store.Block(ctx, second); err != nil {
		t.Fatalf("Block second: %v", err)
	}

	blocked, err := store.Blocked(ctx)
	if err != nil {
		t.Fatalf("Blocked: %v", err)
	}
	if len(blocked) != 2 {
		t.Fatalf("blocked = %+v, want 2", blocked)
	}
	seen := map[yagomodel.Hash]bool{}
	for _, b := range blocked {
		seen[b.Hash] = true
		if b.BlockedAt.IsZero() {
			t.Fatalf("blocked %s has no recorded time", b.Hash)
		}
	}
	if !seen[first] || !seen[second] {
		t.Fatalf("blocked set = %v, want both hashes", seen)
	}
}
