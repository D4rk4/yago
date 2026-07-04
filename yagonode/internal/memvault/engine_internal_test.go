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
