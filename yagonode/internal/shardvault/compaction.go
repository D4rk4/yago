package shardvault

import (
	"context"
	"fmt"
	"os"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

// compactFreeRatio and compactMinBytes gate which shards are worth rewriting: a
// shard is compacted only when its freelist holds at least a quarter of the file
// (free*4 >= size) and the file is non-trivial, so a healthy shard is skipped
// and the exclusive pause stays bounded to the shards that actually need
// reclaiming. They are vars so a test can lower the size floor.
var (
	compactFreeRatio = int64(4)
	compactMinBytes  = int64(8) << 20
)

const (
	// compactTxMaxSize bounds bolt.Compact's per-transaction copy so a large
	// shard does not build one giant write transaction.
	compactTxMaxSize = int64(16) << 20
	compactingSuffix = ".compacting"
)

// Compact rewrites the shards whose freelist has grown large, returning the
// freed pages to the OS (UsedBytes already excludes them, but the files keep
// their high-water size until compacted). Each shard is compacted under the
// exclusive gate so no transaction touches it while its file is swapped, and
// the gate is released between shards, so the pause is bounded to one
// over-full shard at a time (ADR-0036 C).
func (e *engine) Compact(ctx context.Context) (vault.CompactResult, error) {
	var result vault.CompactResult
	for i := range e.shards {
		if err := ctx.Err(); err != nil {
			return result, fmt.Errorf("context: %w", err)
		}
		reclaimed, compacted, err := e.compactShard(i)
		if err != nil {
			return result, fmt.Errorf("compact shard %d: %w", i, err)
		}
		if compacted {
			result.ShardsCompacted++
			result.BytesReclaimed += reclaimed
		}
	}

	return result, nil
}

// compactShard rewrites one shard under the exclusive gate when its freelist is
// large enough to be worth reclaiming, reporting the bytes reclaimed and
// whether it compacted. A failure before the swap leaves the original serving.
func (e *engine) compactShard(index int) (int64, bool, error) {
	e.globalGate.Lock()
	defer e.globalGate.Unlock()

	db := e.shards[index]
	before, free, err := shardSizeAndFree(db)
	if err != nil {
		return 0, false, err
	}
	if !worthCompacting(before, free) {
		return 0, false, nil
	}

	path := shardPath(e.dir, index)
	tmp := path + compactingSuffix
	// Compact into a fresh file while the original stays open; any failure here
	// leaves e.shards[index] untouched and serving.
	if err := compactInto(db, tmp); err != nil {
		return 0, false, err
	}

	reopened, err := swapShard(db, tmp, path)
	if reopened != nil {
		e.shards[index] = reopened
	}
	if err != nil {
		return 0, false, err
	}

	after, _, err := shardSizeAndFree(reopened)
	if err != nil {
		return 0, false, err
	}

	return max(before-after, 0), true, nil
}

// worthCompacting reports whether a shard's freelist is large enough that a
// rewrite reclaims meaningful space.
func worthCompacting(size, free int64) bool {
	return size >= compactMinBytes && free*compactFreeRatio >= size
}

// compactInto copies the live pages of src into a fresh temporary file at tmp,
// clearing any stale leftover first. It does not touch src beyond reading it.
func compactInto(src *bolt.DB, tmp string) error {
	if err := os.Remove(tmp); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear stale compaction file: %w", err)
	}

	dst, err := openBolt(tmp, 0o600, openTimeoutOptions())
	if err != nil {
		return fmt.Errorf("open compaction target: %w", err)
	}
	if err := bolt.Compact(dst, src, compactTxMaxSize); err != nil {
		_ = dst.Close()
		_ = os.Remove(tmp)

		return fmt.Errorf("compact: %w", err)
	}
	if err := closeDB(dst); err != nil {
		_ = os.Remove(tmp)

		return fmt.Errorf("close compaction target: %w", err)
	}

	return nil
}

// swapShard closes the original shard, renames the compacted temporary over it,
// and reopens it. It returns the handle to install: the compacted file on
// success, or — if the rename fails, leaving the untouched original at path —
// the reopened original alongside the error, so the shard keeps serving.
func swapShard(src *bolt.DB, tmp, path string) (*bolt.DB, error) {
	if err := closeDB(src); err != nil {
		_ = os.Remove(tmp)

		return nil, fmt.Errorf("close shard: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		reopened, reopenErr := openBolt(path, 0o600, openTimeoutOptions())
		if reopenErr != nil {
			return nil, fmt.Errorf("swap shard: %w (reopen failed: %w)", err, reopenErr)
		}

		return reopened, fmt.Errorf("swap shard: %w", err)
	}

	reopened, err := openBolt(path, 0o600, openTimeoutOptions())
	if err != nil {
		return nil, fmt.Errorf("reopen compacted shard: %w", err)
	}

	return reopened, nil
}
