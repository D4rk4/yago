package crawlformats

import (
	"context"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagocrawlcontract"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
	"github.com/D4rk4/yago/yagonode/internal/vault"
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

	defaults, err := store.Current(ctx)
	if err != nil {
		t.Fatalf("current defaults: %v", err)
	}
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
	got, err := store.Current(ctx)
	if err != nil {
		t.Fatalf("current custom: %v", err)
	}
	if got != custom {
		t.Fatalf("current = %+v, want %+v", got, custom)
	}
}

func TestTogglesCodecDecodeError(t *testing.T) {
	if _, err := (togglesCodec{}).Decode([]byte("{not json")); err == nil {
		t.Fatal("expected a decode error for malformed JSON")
	}
}

func TestOpenRejectsDuplicateBucket(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	if _, err := Open(v); err != nil {
		t.Fatalf("first open: %v", err)
	}
	if _, err := Open(v); err == nil {
		t.Fatal("expected an error registering the formats bucket twice")
	}
}

func TestSetSurfacesVaultError(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	store, err := Open(v)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.Set(ctx, yagocrawlcontract.FormatToggles{Text: true}); err == nil {
		t.Fatal("Set must surface the vault error on a cancelled context")
	}
}

func TestCurrentDoesNotWaitForStorageWriter(t *testing.T) {
	v, err := memvault.Open(0)
	if err != nil {
		t.Fatalf("memvault: %v", err)
	}
	store, err := Open(v)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- v.Update(context.Background(), func(*vault.Txn) error {
			close(started)
			<-release

			return nil
		})
	}()
	<-started

	currentDone := make(chan error, 1)
	go func() {
		_, currentErr := store.Current(context.Background())
		currentDone <- currentErr
	}()
	select {
	case currentErr := <-currentDone:
		if currentErr != nil {
			t.Fatalf("current during storage write: %v", currentErr)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("cached format read waited for storage writer")
	}
	close(release)
	if err := <-writerDone; err != nil {
		t.Fatalf("release storage writer: %v", err)
	}
}
