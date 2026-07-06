package crawlformats

import (
	"context"
	"testing"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func TestStoreDefaultsAndRoundTrip(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	store, err := Open(v)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx := context.Background()

	defaults := store.Current(ctx)
	if defaults != yagocrawlcontract.DefaultFormatToggles() {
		t.Fatalf("defaults = %+v", defaults)
	}
	if defaults.Archives {
		t.Fatal("archives must default off")
	}

	custom := yagocrawlcontract.FormatToggles{Text: true, Archives: true}
	if err := store.Set(ctx, custom); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := store.Current(ctx); got != custom {
		t.Fatalf("current = %+v, want %+v", got, custom)
	}
}
