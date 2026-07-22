package shardvault

import (
	"context"
	"errors"
	"fmt"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	shardRWIRecoveryLiveBucket    vault.Name = "rwi-recovery-live"
	shardRWIRecoveryJournalBucket vault.Name = "rwi-recovery-journal"
)

type rwiRecoveryCodec struct{}

func (rwiRecoveryCodec) Encode(value string) ([]byte, error) {
	return []byte(value), nil
}

func (rwiRecoveryCodec) Decode(raw []byte) (string, error) {
	return string(raw), nil
}

type rwiRecoveryCollections struct {
	live    *vault.Keyspace[string]
	journal *vault.Keyspace[string]
}

type rwiRecoveryRow struct {
	key          vault.Key
	value        string
	liveShard    int
	journalShard int
}

func TestShardRWIRecoverySurvivesLaterLiveShardCommitFailureAndReopen(t *testing.T) {
	directory := t.TempDir()
	shards, storage, collections := rwiRecoveryOpenStorage(t, directory)
	rows := rwiRecoveryRowsForDistinctRoute(
		t,
		shards,
		shardRWIRecoveryLiveBucket,
		"live-failure",
	)
	rwiRecoveryPutRows(t, storage, collections.live, rows)
	rwiRecoveryPutRows(t, storage, collections.journal, rows)

	realCommit := commitTx
	t.Cleanup(func() { commitTx = realCommit })
	commits := 0
	commitFailure := errors.New("injected later live-shard commit failure")
	commitTx = func(tx *bolt.Tx) error {
		commits++
		if tx.DB() == shards.shards[rows[1].liveShard] {
			return commitFailure
		}

		return realCommit(tx)
	}
	if err := rwiRecoveryDeleteLiveRows(storage, collections.live, rows); !errors.Is(
		err,
		commitFailure,
	) {
		t.Fatalf("delete live rows error = %v, want %v", err, commitFailure)
	}
	if commits != 2 {
		t.Fatalf("live deletion commits = %d, want 2", commits)
	}
	commitTx = realCommit
	if err := storage.Close(); err != nil {
		t.Fatalf("close failed storage: %v", err)
	}

	_, reopened, reopenedCollections := rwiRecoveryOpenStorage(t, directory)
	t.Cleanup(func() { _ = reopened.Close() })
	if stored := rwiRecoveryStoredRows(t, reopened, reopenedCollections.live, rows); stored != 1 {
		t.Fatalf("live rows before recovery = %d, want 1", stored)
	}
	if stored := rwiRecoveryStoredRows(
		t,
		reopened,
		reopenedCollections.journal,
		rows,
	); stored != len(rows) {
		t.Fatalf("journal rows before recovery = %d, want %d", stored, len(rows))
	}
	recovered, err := rwiRecoveryRestore(reopened, reopenedCollections, rows)
	if err != nil {
		t.Fatalf("recover rows: %v", err)
	}
	if recovered != len(rows) {
		t.Fatalf("recovered rows = %d, want %d", recovered, len(rows))
	}
	rwiRecoveryAssertConverged(t, reopened, reopenedCollections, rows)
	again, err := rwiRecoveryRestore(reopened, reopenedCollections, rows)
	if err != nil || again != 0 {
		t.Fatalf("second recovery = %d, %v; want 0, nil", again, err)
	}
}

func TestShardRWIRecoverySurvivesPartialJournalCommitAndReopen(t *testing.T) {
	directory := t.TempDir()
	shards, storage, collections := rwiRecoveryOpenStorage(t, directory)
	rows := rwiRecoveryRowsForDistinctRoute(
		t,
		shards,
		shardRWIRecoveryJournalBucket,
		"journal-failure",
	)
	rwiRecoveryPutRows(t, storage, collections.live, rows)

	realCommit := commitTx
	t.Cleanup(func() { commitTx = realCommit })
	commits := 0
	commitFailure := errors.New("injected later journal-shard commit failure")
	commitTx = func(tx *bolt.Tx) error {
		commits++
		if tx.DB() == shards.shards[rows[1].journalShard] {
			return commitFailure
		}

		return realCommit(tx)
	}
	err := rwiRecoveryPut(storage, collections.journal, rows)
	if !errors.Is(err, commitFailure) {
		t.Fatalf("journal rows error = %v, want %v", err, commitFailure)
	}
	if commits != 2 {
		t.Fatalf("journal commits = %d, want 2", commits)
	}
	commitTx = realCommit
	if err := storage.Close(); err != nil {
		t.Fatalf("close failed storage: %v", err)
	}

	_, reopened, reopenedCollections := rwiRecoveryOpenStorage(t, directory)
	t.Cleanup(func() { _ = reopened.Close() })
	stored := rwiRecoveryStoredRows(t, reopened, reopenedCollections.live, rows)
	if stored != len(rows) {
		t.Fatalf("live rows after partial journal = %d, want %d", stored, len(rows))
	}
	if stored := rwiRecoveryStoredRows(
		t,
		reopened,
		reopenedCollections.journal,
		rows,
	); stored != 1 {
		t.Fatalf("journal rows after partial commit = %d, want 1", stored)
	}
	recovered, err := rwiRecoveryRestore(reopened, reopenedCollections, rows)
	if err != nil {
		t.Fatalf("recover partial journal: %v", err)
	}
	if recovered != 1 {
		t.Fatalf("recovered partial journal rows = %d, want 1", recovered)
	}
	rwiRecoveryAssertConverged(t, reopened, reopenedCollections, rows)
}

func rwiRecoveryOpenStorage(
	t *testing.T,
	directory string,
) (*engine, *vault.Vault, rwiRecoveryCollections) {
	t.Helper()
	shards, err := openEngine(directory, 1<<30)
	if err != nil {
		t.Fatalf("open shard engine: %v", err)
	}
	storage, err := vaultOverEngine(shards)
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}
	live, err := vault.RegisterKeyspace[string](
		storage,
		shardRWIRecoveryLiveBucket,
		rwiRecoveryCodec{},
	)
	if err != nil {
		_ = storage.Close()
		t.Fatalf("register live rows: %v", err)
	}
	journal, err := vault.RegisterKeyspace[string](
		storage,
		shardRWIRecoveryJournalBucket,
		rwiRecoveryCodec{},
	)
	if err != nil {
		_ = storage.Close()
		t.Fatalf("register journal rows: %v", err)
	}

	return shards, storage, rwiRecoveryCollections{live: live, journal: journal}
}

func rwiRecoveryRowsForDistinctRoute(
	t *testing.T,
	shards *engine,
	routedBucket vault.Name,
	prefix string,
) []rwiRecoveryRow {
	t.Helper()
	rows := make([]rwiRecoveryRow, 0, 2)
	routes := make(map[int]struct{}, 2)
	for candidate := 0; candidate < 10_000 && len(rows) < 2; candidate++ {
		key := vault.Key(fmt.Sprintf("%s-%04d", prefix, candidate))
		row := rwiRecoveryRow{
			key:          key,
			value:        fmt.Sprintf("posting-%04d", candidate),
			liveShard:    shards.route(shardRWIRecoveryLiveBucket, key),
			journalShard: shards.route(shardRWIRecoveryJournalBucket, key),
		}
		if row.liveShard == row.journalShard {
			continue
		}
		route := rwiRecoveryRoute(row, routedBucket)
		if _, found := routes[route]; found {
			continue
		}
		routes[route] = struct{}{}
		rows = append(rows, row)
	}
	if len(rows) != 2 {
		t.Fatalf("found %d distinct-shard rows, want 2", len(rows))
	}
	if rwiRecoveryRoute(rows[0], routedBucket) > rwiRecoveryRoute(rows[1], routedBucket) {
		rows[0], rows[1] = rows[1], rows[0]
	}

	return rows
}

func rwiRecoveryRoute(row rwiRecoveryRow, bucket vault.Name) int {
	if bucket == shardRWIRecoveryLiveBucket {
		return row.liveShard
	}

	return row.journalShard
}

func rwiRecoveryPutRows(
	t *testing.T,
	storage *vault.Vault,
	keyspace *vault.Keyspace[string],
	rows []rwiRecoveryRow,
) {
	t.Helper()
	if err := rwiRecoveryPut(storage, keyspace, rows); err != nil {
		t.Fatalf("put recovery rows: %v", err)
	}
}

func rwiRecoveryPut(
	storage *vault.Vault,
	keyspace *vault.Keyspace[string],
	rows []rwiRecoveryRow,
) error {
	err := storage.Update(context.Background(), func(tx *vault.Txn) error {
		for _, row := range rows {
			if err := keyspace.Put(tx, row.key, row.value); err != nil {
				return fmt.Errorf("put recovery row: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("write recovery rows: %w", err)
	}

	return nil
}

func rwiRecoveryDeleteLiveRows(
	storage *vault.Vault,
	live *vault.Keyspace[string],
	rows []rwiRecoveryRow,
) error {
	err := storage.Update(context.Background(), func(tx *vault.Txn) error {
		for _, row := range rows {
			if _, err := live.Delete(tx, row.key); err != nil {
				return fmt.Errorf("delete live recovery row: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("remove live recovery rows: %w", err)
	}

	return nil
}

func rwiRecoveryRestore(
	storage *vault.Vault,
	collections rwiRecoveryCollections,
	rows []rwiRecoveryRow,
) (int, error) {
	pending, err := rwiRecoveryPending(storage, collections.journal, rows)
	if err != nil || len(pending) == 0 {
		return 0, err
	}
	if err := rwiRecoveryPut(storage, collections.live, pending); err != nil {
		return 0, fmt.Errorf("restore live recovery rows: %w", err)
	}
	if err := storage.Update(context.Background(), func(tx *vault.Txn) error {
		for _, row := range pending {
			if _, err := collections.journal.Delete(tx, row.key); err != nil {
				return fmt.Errorf("release recovery journal row: %w", err)
			}
		}

		return nil
	}); err != nil {
		return 0, fmt.Errorf("release recovery journal: %w", err)
	}

	return len(pending), nil
}

func rwiRecoveryPending(
	storage *vault.Vault,
	journal *vault.Keyspace[string],
	rows []rwiRecoveryRow,
) ([]rwiRecoveryRow, error) {
	pending := make([]rwiRecoveryRow, 0, len(rows))
	err := storage.View(context.Background(), func(tx *vault.Txn) error {
		for _, row := range rows {
			value, found, err := journal.Get(tx, row.key)
			if err != nil {
				return fmt.Errorf("read recovery journal row: %w", err)
			}
			if found && value == row.value {
				pending = append(pending, row)
			}
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read pending recovery rows: %w", err)
	}

	return pending, nil
}

func rwiRecoveryStoredRows(
	t *testing.T,
	storage *vault.Vault,
	keyspace *vault.Keyspace[string],
	rows []rwiRecoveryRow,
) int {
	t.Helper()
	stored := 0
	err := storage.View(t.Context(), func(tx *vault.Txn) error {
		for _, row := range rows {
			value, found, err := keyspace.Get(tx, row.key)
			if err != nil {
				return fmt.Errorf("read recovery row: %w", err)
			}
			if found {
				if value != row.value {
					return fmt.Errorf("recovery row %s has value %q", row.key, value)
				}
				stored++
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("read stored recovery rows: %v", err)
	}

	return stored
}

func rwiRecoveryAssertConverged(
	t *testing.T,
	storage *vault.Vault,
	collections rwiRecoveryCollections,
	rows []rwiRecoveryRow,
) {
	t.Helper()
	if stored := rwiRecoveryStoredRows(t, storage, collections.live, rows); stored != len(rows) {
		t.Fatalf("live rows after recovery = %d, want %d", stored, len(rows))
	}
	if stored := rwiRecoveryStoredRows(t, storage, collections.journal, rows); stored != 0 {
		t.Fatalf("journal rows after recovery = %d, want 0", stored)
	}
}
