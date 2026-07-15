package yagonode

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/urldenylist"
)

type fakeDenylistStore struct {
	entries    []urldenylist.Entry
	entered    []urldenylist.Entry
	removed    []urldenylist.Entry
	snapshot   urldenylist.Snapshot
	entriesErr error
	writeErr   error
}

func (f *fakeDenylistStore) Entries(context.Context) ([]urldenylist.Entry, error) {
	return f.entries, f.entriesErr
}

func (f *fakeDenylistStore) Add(_ context.Context, kind urldenylist.Kind, value string) error {
	f.entered = append(f.entered, urldenylist.Entry{Kind: kind, Value: value})

	return f.writeErr
}

func (f *fakeDenylistStore) Remove(
	_ context.Context,
	kind urldenylist.Kind,
	value string,
) (bool, error) {
	f.removed = append(f.removed, urldenylist.Entry{Kind: kind, Value: value})

	return f.writeErr == nil, f.writeErr
}

func (f *fakeDenylistStore) Snapshot() urldenylist.Snapshot {
	return f.snapshot
}

func TestBlacklistEntriesMapsAndFormats(t *testing.T) {
	store := &fakeDenylistStore{entries: []urldenylist.Entry{
		{Kind: urldenylist.KindDomain, Value: "example.com", AddedAt: time.Unix(1000, 0)},
		{Kind: urldenylist.KindURL, Value: "https://a.example/", AddedAt: time.Time{}},
	}}
	views, err := newBlacklistController(store).BlacklistEntries(context.Background())
	if err != nil {
		t.Fatalf("entries: %v", err)
	}

	if len(views) != 2 {
		t.Fatalf("views = %#v", views)
	}
	if views[0].Kind != "domain" || views[0].Value != "example.com" || views[0].AddedAt == "" {
		t.Fatalf("domain view = %#v", views[0])
	}
	if views[1].AddedAt != "" {
		t.Fatalf("a zero time should format to empty, got %q", views[1].AddedAt)
	}
}

func TestBlacklistEntriesReturnsError(t *testing.T) {
	store := &fakeDenylistStore{entriesErr: errors.New("scan failed")}
	views, err := newBlacklistController(store).BlacklistEntries(context.Background())
	if err == nil || views != nil {
		t.Fatalf("read result = %#v, %v", views, err)
	}
}

func TestBlacklistAddAndRemove(t *testing.T) {
	store := &fakeDenylistStore{}
	controller := newBlacklistController(store)
	ctx := context.Background()

	if err := controller.AddBlacklist(ctx, "domain", "example.com"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if len(store.entered) != 1 || store.entered[0].Kind != urldenylist.KindDomain {
		t.Fatalf("entered = %#v", store.entered)
	}
	if err := controller.RemoveBlacklist(ctx, "url", "https://a.example/"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if len(store.removed) != 1 || store.removed[0].Kind != urldenylist.KindURL {
		t.Fatalf("removed = %#v", store.removed)
	}
}

func TestBlacklistRejectsUnknownKind(t *testing.T) {
	store := &fakeDenylistStore{}
	controller := newBlacklistController(store)
	ctx := context.Background()

	if err := controller.AddBlacklist(ctx, "regex", "x"); err == nil {
		t.Fatal("an unknown kind should be rejected on add")
	}
	if err := controller.RemoveBlacklist(ctx, "regex", "x"); err == nil {
		t.Fatal("an unknown kind should be rejected on remove")
	}
	if len(store.entered) != 0 || len(store.removed) != 0 {
		t.Fatal("an unknown kind should never reach the store")
	}
}

func TestBlacklistSurfacesStoreErrors(t *testing.T) {
	store := &fakeDenylistStore{writeErr: errors.New("write failed")}
	controller := newBlacklistController(store)
	ctx := context.Background()

	if err := controller.AddBlacklist(ctx, "domain", "example.com"); err == nil {
		t.Fatal("a store add error should surface")
	}
	if err := controller.RemoveBlacklist(ctx, "domain", "example.com"); err == nil {
		t.Fatal("a store remove error should surface")
	}
}
