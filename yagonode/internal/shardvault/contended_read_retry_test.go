package shardvault

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestContendedReadOperationsRetryBeforeCommitting(t *testing.T) {
	tests := map[string]struct {
		access     func(*vault.Collection[string], *vault.Txn, vault.Key) (bool, error)
		wantStored bool
	}{
		"get": {
			access: func(values *vault.Collection[string], tx *vault.Txn, key vault.Key) (bool, error) {
				value, found, err := values.Get(tx, key)
				if err != nil {
					return false, fmt.Errorf("get held value: %w", err)
				}

				return found && value == "held", nil
			},
			wantStored: true,
		},
		"contains": {
			access: func(values *vault.Collection[string], tx *vault.Txn, key vault.Key) (bool, error) {
				return values.Contains(tx, key), nil
			},
			wantStored: true,
		},
		"delete": {
			access: func(values *vault.Collection[string], tx *vault.Txn, key vault.Key) (bool, error) {
				deleted, err := values.Delete(tx, key)
				if err != nil {
					return false, fmt.Errorf("delete held value: %w", err)
				}

				return deleted, nil
			},
			wantStored: false,
		},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assertContendedReadRetry(t, test.access, test.wantStored)
		})
	}
}

func assertContendedReadRetry(
	t *testing.T,
	access func(*vault.Collection[string], *vault.Txn, vault.Key) (bool, error),
	wantStored bool,
) {
	t.Helper()
	vaulted, _ := openTestVault(t)
	values, err := vault.Register[string](vaulted, "retrydocs", stringCodec{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	heldKey := vault.Key("held")
	otherKey := keyOnDifferentShard(t, vault.Name("retrydocs"), heldKey)
	seedContendedReadValues(t, vaulted, values, heldKey, otherKey)
	release, holder := holdContendedReadValue(vaulted, values, heldKey)
	attempts, observed, second := startContendedReadUpdate(
		vaulted,
		values,
		heldKey,
		otherKey,
		access,
	)

	select {
	case err := <-second:
		close(release)
		holderErr := <-holder
		t.Fatalf("contended read committed before retry: %v; holder: %v", err, holderErr)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-holder; err != nil {
		t.Fatalf("holder: %v", err)
	}
	if err := <-second; err != nil {
		t.Fatalf("retried update: %v", err)
	}
	if attempts.Load() != 2 || !observed.Load() {
		t.Fatalf("attempts=%d observed=%v, want 2/true", attempts.Load(), observed.Load())
	}
	verifyContendedReadValue(t, vaulted, values, heldKey, wantStored)
}

func seedContendedReadValues(
	t *testing.T,
	vaulted *vault.Vault,
	values *vault.Collection[string],
	heldKey vault.Key,
	otherKey vault.Key,
) {
	t.Helper()
	if err := vaulted.Update(context.Background(), func(tx *vault.Txn) error {
		if err := values.Put(tx, heldKey, "seed"); err != nil {
			return fmt.Errorf("seed held value: %w", err)
		}
		if err := values.Put(tx, otherKey, "seed"); err != nil {
			return fmt.Errorf("seed other value: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func holdContendedReadValue(
	vaulted *vault.Vault,
	values *vault.Collection[string],
	heldKey vault.Key,
) (chan struct{}, chan error) {
	holding := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- vaulted.Update(context.Background(), func(tx *vault.Txn) error {
			if err := values.Put(tx, heldKey, "held"); err != nil {
				return fmt.Errorf("hold value: %w", err)
			}
			close(holding)
			<-release

			return nil
		})
	}()
	<-holding

	return release, done
}

func startContendedReadUpdate(
	vaulted *vault.Vault,
	values *vault.Collection[string],
	heldKey vault.Key,
	otherKey vault.Key,
	access func(*vault.Collection[string], *vault.Txn, vault.Key) (bool, error),
) (*atomic.Int32, *atomic.Bool, chan error) {
	attempts := &atomic.Int32{}
	observed := &atomic.Bool{}
	done := make(chan error, 1)
	go func() {
		done <- vaulted.Update(context.Background(), func(tx *vault.Txn) error {
			attempts.Add(1)
			found, err := access(values, tx, heldKey)
			if err != nil {
				return err
			}
			if found {
				observed.Store(true)
			}
			if err := values.Put(tx, otherKey, "updated"); err != nil {
				return fmt.Errorf("update other value: %w", err)
			}

			return nil
		})
	}()

	return attempts, observed, done
}

func verifyContendedReadValue(
	t *testing.T,
	vaulted *vault.Vault,
	values *vault.Collection[string],
	heldKey vault.Key,
	wantStored bool,
) {
	t.Helper()
	if err := vaulted.View(context.Background(), func(tx *vault.Txn) error {
		_, stored, err := values.Get(tx, heldKey)
		if err != nil {
			return fmt.Errorf("get final held value: %w", err)
		}
		if stored != wantStored {
			return fmt.Errorf("stored=%v, want %v", stored, wantStored)
		}

		return nil
	}); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func keyOnDifferentShard(t *testing.T, bucket vault.Name, first vault.Key) vault.Key {
	t.Helper()
	level, err := exactLog2(minShards)
	if err != nil {
		t.Fatalf("level: %v", err)
	}
	probe := engine{shards: make([]*bolt.DB, minShards), level: level}
	firstShard := probe.route(bucket, first)
	for index := range 4096 {
		key := vault.Key(fmt.Sprintf("other-%d", index))
		if probe.route(bucket, key) != firstShard {
			return key
		}
	}
	t.Fatal("different shard key not found")

	return nil
}
