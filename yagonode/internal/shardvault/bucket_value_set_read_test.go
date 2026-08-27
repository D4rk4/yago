package shardvault

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestShardKeySetReadPreservesValuesAndPresenceOrder(t *testing.T) {
	vaulted, shards := openCollectionLengthTestVault(t)
	values, err := vault.RegisterKeyspace(vaulted, testBucket, stringCodec{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	keys := keysAcrossEveryShard(shards, testBucket, 2)
	wantValues := make([]string, len(keys))
	wantPresence := make([]bool, len(keys))
	err = vaulted.Update(t.Context(), func(tx *vault.Txn) error {
		for index, key := range keys {
			if index%2 != 0 {
				continue
			}
			wantValues[index] = fmt.Sprintf("value-%02d", index)
			wantPresence[index] = true
			if err := values.Put(tx, key, wantValues[index]); err != nil {
				return fmt.Errorf("put %d: %w", index, err)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	err = vaulted.View(t.Context(), func(tx *vault.Txn) error {
		gotValues, gotPresence, err := values.Values(t.Context(), tx, keys)
		if err != nil {
			return fmt.Errorf("values: %w", err)
		}
		if !slices.Equal(gotValues, wantValues) || !slices.Equal(gotPresence, wantPresence) {
			t.Fatalf("values=%v/%v, want=%v/%v", gotValues, gotPresence, wantValues, wantPresence)
		}
		gotPresence, err = values.Presence(t.Context(), tx, keys)
		if err != nil {
			return fmt.Errorf("presence: %w", err)
		}
		if !slices.Equal(gotPresence, wantPresence) {
			t.Fatalf("presence=%v, want=%v", gotPresence, wantPresence)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
}

func TestShardKeySetReadRunsDistinctShardsConcurrently(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	sets := []shardKeySet{
		{positions: []shardKeyPosition{{key: vault.Key("first"), position: 1}}},
		{positions: []shardKeyPosition{{key: vault.Key("second"), position: 0}}},
	}
	arrived := make(chan struct{}, len(sets))
	release := make(chan struct{})
	type outcome struct {
		values []string
		err    error
	}
	completed := make(chan outcome, 1)
	go func() {
		values, err := readShardKeySets(ctx, sets, 2, func(
			_ *bolt.Bucket,
			key vault.Key,
		) (string, error) {
			arrived <- struct{}{}
			select {
			case <-release:
				return string(key), nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		})
		completed <- outcome{values: values, err: err}
	}()
	for range sets {
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatal("shard readers did not overlap")
		}
	}
	close(release)
	result := <-completed
	if result.err != nil || !slices.Equal(result.values, []string{"second", "first"}) {
		t.Fatalf("values=%v error=%v", result.values, result.err)
	}
}

func TestShardKeySetReadRejectsReadFailureAndCancellation(t *testing.T) {
	failure := errors.New("read failed")
	sets := []shardKeySet{{positions: []shardKeyPosition{{key: vault.Key("key")}}}}
	if _, err := readShardKeySets(
		t.Context(),
		sets,
		1,
		func(*bolt.Bucket, vault.Key) (string, error) { return "", failure },
	); !errors.Is(err, failure) {
		t.Fatalf("read failure=%v, want=%v", err, failure)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readShardKeySets(
		canceled,
		sets,
		1,
		func(*bolt.Bucket, vault.Key) (string, error) { return "value", nil },
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("read cancellation=%v", err)
	}
	values, err := readShardKeySets(
		t.Context(),
		[]shardKeySet{{}},
		0,
		func(*bolt.Bucket, vault.Key) (string, error) { return "value", nil },
	)
	if err != nil || len(values) != 0 {
		t.Fatalf("empty values=%v error=%v", values, err)
	}
}

func TestShardKeySetReadReturnsLowestShardFailure(t *testing.T) {
	lowestFailure := errors.New("lowest shard failed")
	higherFailure := errors.New("higher shard failed")
	higherFinished := make(chan struct{})
	sets := []shardKeySet{
		{positions: []shardKeyPosition{{key: vault.Key("lowest")}}},
		{positions: []shardKeyPosition{{key: vault.Key("higher")}}},
	}
	_, err := readShardKeySets(
		t.Context(),
		sets,
		2,
		func(_ *bolt.Bucket, key vault.Key) (string, error) {
			if string(key) == "higher" {
				close(higherFinished)

				return "", higherFailure
			}
			<-higherFinished

			return "", lowestFailure
		},
	)
	if !errors.Is(err, lowestFailure) || errors.Is(err, higherFailure) {
		t.Fatalf("failure=%v, want=%v", err, lowestFailure)
	}
}

func TestShardKeySetReadRejectsCorruptValueAndUnavailableBucket(t *testing.T) {
	shards := openTestEngine(t)
	key := vault.Key("corrupt")
	shard := shards.route(testBucket, key)
	if err := shards.shards[shard].Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(testBucket)).Put(key, []byte{0xff})
	}); err != nil {
		t.Fatalf("seed corruption: %v", err)
	}
	err := shards.View(t.Context(), func(tx vault.EngineTxn) error {
		_, _, err := tx.Bucket(testBucket).(*shardBucket).ReadValues(
			t.Context(),
			[]vault.Key{key},
		)

		return err
	})
	if !errors.Is(err, vault.ErrCorruptValue) {
		t.Fatalf("corrupt value error=%v", err)
	}
	missing := vault.Name("missing")
	err = shards.View(t.Context(), func(tx vault.EngineTxn) error {
		_, err := tx.Bucket(missing).(*shardBucket).ReadPresence(
			t.Context(),
			[]vault.Key{vault.Key("key")},
		)

		return err
	})
	if err == nil {
		t.Fatal("missing bucket accepted")
	}
}

func TestShardKeySetReadRejectsCanceledAndClosedShard(t *testing.T) {
	shards := openTestEngine(t)
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	err := shards.View(t.Context(), func(tx vault.EngineTxn) error {
		bucket := tx.Bucket(testBucket).(*shardBucket)
		if _, _, err := bucket.ReadValues(
			canceled,
			[]vault.Key{vault.Key("key")},
		); !errors.Is(err, context.Canceled) {
			return fmt.Errorf("value cancellation: %w", err)
		}
		_, err := bucket.ReadPresence(canceled, []vault.Key{vault.Key("key")})

		return err
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("presence cancellation=%v", err)
	}
	staged := &shardSetCancellationContext{Context: t.Context(), openChecks: 1}
	err = shards.View(t.Context(), func(tx vault.EngineTxn) error {
		_, err := tx.Bucket(testBucket).(*shardBucket).ReadPresence(
			staged,
			[]vault.Key{vault.Key("key")},
		)

		return err
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight presence cancellation=%v", err)
	}
	key := vault.Key("closed")
	shard := shards.route(testBucket, key)
	if err := shards.shards[shard].Close(); err != nil {
		t.Fatalf("close shard: %v", err)
	}
	err = shards.View(t.Context(), func(tx vault.EngineTxn) error {
		_, err := tx.Bucket(testBucket).(*shardBucket).ReadPresence(
			t.Context(),
			[]vault.Key{key},
		)

		return err
	})
	if err == nil {
		t.Fatal("closed shard accepted")
	}
}

type shardSetCancellationContext struct {
	context.Context
	checks     atomic.Int64
	openChecks int64
}

func (c *shardSetCancellationContext) Err() error {
	if c.checks.Add(1) > c.openChecks {
		return context.Canceled
	}

	return nil
}

func keysAcrossEveryShard(shards *engine, bucket vault.Name, perShard int) []vault.Key {
	keys := make([][]vault.Key, len(shards.shards))
	for candidate := 0; ; candidate++ {
		key := vault.Key(fmt.Sprintf("set-key-%06d", candidate))
		shard := shards.route(bucket, key)
		if len(keys[shard]) < perShard {
			keys[shard] = append(keys[shard], key)
		}
		complete := true
		for _, shardKeys := range keys {
			if len(shardKeys) != perShard {
				complete = false

				break
			}
		}
		if complete {
			break
		}
	}
	ordered := make([]vault.Key, 0, len(keys)*perShard)
	for index := range perShard {
		for shard := range keys {
			ordered = append(ordered, keys[shard][index])
		}
	}

	return ordered
}
