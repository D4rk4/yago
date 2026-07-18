package memvault

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestEngineUpdateRejectsCanceledContext(t *testing.T) {
	engine := &engine{buckets: map[vault.Name]map[string][]byte{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := engine.Update(
		ctx,
		func(vault.EngineTxn) error { return nil },
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("Update error = %v, want context.Canceled", err)
	}
}

func TestEngineViewRejectsCanceledContext(t *testing.T) {
	engine := &engine{buckets: map[vault.Name]map[string][]byte{}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := engine.View(
		ctx,
		func(vault.EngineTxn) error { return nil },
	); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("View error = %v, want context.Canceled", err)
	}
}

func TestMemBucketScanReturnsCallbackError(t *testing.T) {
	sentinel := errors.New("stop")
	bucket := memBucket{entries: map[string][]byte{"prefix-a": []byte("value")}}

	err := bucket.Scan(vault.Key("prefix"), func(vault.Key, []byte) (bool, error) {
		return false, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("Scan error = %v, want %v", err, sentinel)
	}
}

func TestMemBucketContains(t *testing.T) {
	bucket := memBucket{entries: map[string][]byte{"key": nil}}
	if !bucket.Contains(vault.Key("key")) || bucket.Contains(vault.Key("missing")) {
		t.Fatal("memory presence mismatch")
	}
}

func TestEngineSetQuotaBytes(t *testing.T) {
	e := &engine{}
	e.SetQuotaBytes(42)

	if got := e.QuotaBytes(); got != 42 {
		t.Fatalf("QuotaBytes after SetQuotaBytes = %d, want 42", got)
	}
}

func TestBucketProvisionedReportsPresenceAndCancellation(t *testing.T) {
	engine := &engine{buckets: map[vault.Name]map[string][]byte{
		vault.Name("present"): {},
	}}
	present, err := engine.BucketProvisioned(context.Background(), vault.Name("present"))
	if err != nil || !present {
		t.Fatalf("provisioned bucket presence=%t error=%v", present, err)
	}
	present, err = engine.BucketProvisioned(context.Background(), vault.Name("absent"))
	if err != nil || present {
		t.Fatalf("absent bucket presence=%t error=%v", present, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = engine.BucketProvisioned(ctx, vault.Name("present"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled bucket presence error = %v", err)
	}
}

func TestSnapshotCopiesValues(t *testing.T) {
	source := map[vault.Name]map[string][]byte{
		vault.Name("bucket"): {"key": []byte("value")},
	}

	copied := snapshot(source)
	source[vault.Name("bucket")]["key"][0] = 'X'

	if string(copied[vault.Name("bucket")]["key"]) != "value" {
		t.Fatalf("snapshot value = %q", copied[vault.Name("bucket")]["key"])
	}
}
