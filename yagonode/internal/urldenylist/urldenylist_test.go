package urldenylist_test

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
)

func openStore(t *testing.T) *urldenylist.Store {
	t.Helper()

	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault.Open: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })

	store, err := urldenylist.Open(v, func() time.Time { return time.Unix(100, 0).UTC() })
	if err != nil {
		t.Fatalf("urldenylist.Open: %v", err)
	}

	return store
}

func TestAddAndEntries(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	if err := store.Add(ctx, urldenylist.KindDomain, "  Example.COM. "); err != nil {
		t.Fatalf("add domain: %v", err)
	}
	if err := store.Add(ctx, urldenylist.KindURL, "https://b.example/page"); err != nil {
		t.Fatalf("add url b: %v", err)
	}
	if err := store.Add(ctx, urldenylist.KindURL, "https://a.example/page"); err != nil {
		t.Fatalf("add url a: %v", err)
	}

	entries, err := store.Entries(ctx)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %#v", entries)
	}
	// Sorted by kind then value: domain before url, and urls by value (a before b).
	if entries[0].Kind != urldenylist.KindDomain || entries[0].Value != "example.com" {
		t.Fatalf("domain entry = %#v (expected normalized host)", entries[0])
	}
	if entries[1].Value != "https://a.example/page" ||
		entries[2].Value != "https://b.example/page" {
		t.Fatalf("url entries out of order: %#v", entries[1:])
	}
	if entries[0].AddedAt != time.Unix(100, 0).UTC() {
		t.Fatalf("addedAt = %v", entries[0].AddedAt)
	}
}

func TestAddRejectsEmptyValue(t *testing.T) {
	store := openStore(t)
	if err := store.Add(context.Background(), urldenylist.KindDomain, "   "); err == nil {
		t.Fatal("adding an empty value should be rejected")
	}
}

func TestRemove(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	if err := store.Add(ctx, urldenylist.KindDomain, "example.com"); err != nil {
		t.Fatalf("add: %v", err)
	}

	removed, err := store.Remove(ctx, urldenylist.KindDomain, "EXAMPLE.com")
	if err != nil || !removed {
		t.Fatalf("remove = %v, %v; want removed", removed, err)
	}

	absent, err := store.Remove(ctx, urldenylist.KindDomain, "example.com")
	if err != nil || absent {
		t.Fatalf("removing an absent entry = %v, %v; want (false, nil)", absent, err)
	}

	entries, err := store.Entries(ctx)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries after remove = %#v", entries)
	}
}

func TestSnapshotBlocks(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	if err := store.Add(ctx, urldenylist.KindDomain, "blocked.example"); err != nil {
		t.Fatalf("add domain: %v", err)
	}
	if err := store.Add(ctx, urldenylist.KindURL, "https://ok.example/bad-page"); err != nil {
		t.Fatalf("add url: %v", err)
	}

	snap := store.Snapshot()
	if snap.IsEmpty() {
		t.Fatal("snapshot should not be empty")
	}

	cases := []struct {
		url  string
		want bool
	}{
		{"https://blocked.example/anything", true}, // domain match
		{"https://sub.blocked.example/x", true},    // subdomain match
		{"https://USER@DEEP.SUB.BLOCKED.EXAMPLE.:443/x", true},
		{"//deep.sub.blocked.example/x", true},
		{"https://ok.example/bad-page", true},
		{"https://ok.example/good-page", false},
		{"https://notblocked.example/", false},
		{"https://myblocked.example/", false},
		{"https://blocked.example.evil/", false},
		{"blocked.example/path", false},
		{"mailto:someone", false},
		{"http://[", false},
	}
	for _, tc := range cases {
		if got := snap.Blocks(tc.url); got != tc.want {
			t.Fatalf("Blocks(%q) = %v, want %v", tc.url, got, tc.want)
		}
	}
}

func TestSnapshotEmpty(t *testing.T) {
	snap := openStore(t).Snapshot()
	if !snap.IsEmpty() {
		t.Fatal("a fresh store should snapshot empty")
	}
	if snap.Blocks("https://anything.example/") {
		t.Fatal("an empty snapshot should block nothing")
	}
}

func TestAddRefreshesTimestamp(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()
	if err := store.Add(ctx, urldenylist.KindURL, "https://a.example/"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := store.Add(ctx, urldenylist.KindURL, "https://a.example/"); err != nil {
		t.Fatalf("re-add: %v", err)
	}

	entries, err := store.Entries(ctx)
	if err != nil {
		t.Fatalf("entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("re-adding the same entry should not duplicate it: %#v", entries)
	}
}
