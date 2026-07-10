package shardvault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"
)

// errCov is the sentinel a coverage seam returns to force a failure branch.
var errCov = errors.New("boom")

func assertErr(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: want error, got nil", what)
	}
}

// mustNonEmptyDir creates path as a directory holding one file, so a later
// os.Remove or os.Rename over path fails with a not-empty error.
func mustNonEmptyDir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o750); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	if err := os.WriteFile(filepath.Join(path, "occupant"), []byte("x"), 0o600); err != nil {
		t.Fatalf("occupy %s: %v", path, err)
	}
}

func swapOpenBolt(t *testing.T, fn func(string, os.FileMode, *bolt.Options) (*bolt.DB, error)) {
	t.Helper()
	saved := openBolt
	openBolt = fn
	t.Cleanup(func() { openBolt = saved })
}

func swapCloseDB(t *testing.T, fn func(*bolt.DB) error) {
	t.Helper()
	saved := closeDB
	closeDB = fn
	t.Cleanup(func() { closeDB = saved })
}

func swapCommitTx(t *testing.T, fn func(*bolt.Tx) error) {
	t.Helper()
	saved := commitTx
	commitTx = fn
	t.Cleanup(func() { commitTx = saved })
}

func swapCreateBucket(t *testing.T, fn func(*bolt.Tx, []byte) (*bolt.Bucket, error)) {
	t.Helper()
	saved := createBucketIfNotExists
	createBucketIfNotExists = fn
	t.Cleanup(func() { createBucketIfNotExists = saved })
}

func smallSplitTx(t *testing.T) {
	t.Helper()
	saved := splitTxMaxBytes
	splitTxMaxBytes = 1
	t.Cleanup(func() { splitTxMaxBytes = saved })
}

// newSourceShard opens a standalone, writable bbolt file provisioned with the
// test bucket and one record, standing in for a split's source shard.
func newSourceShard(t *testing.T) *bolt.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "src.db")
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("open source shard: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Update(func(tx *bolt.Tx) error {
		b, createErr := tx.CreateBucketIfNotExists([]byte(testBucket))
		if createErr != nil {
			return fmt.Errorf("create bucket: %w", createErr)
		}
		if putErr := b.Put([]byte("k"), []byte("v")); putErr != nil {
			return fmt.Errorf("put: %w", putErr)
		}

		return nil
	}); err != nil {
		t.Fatalf("fill source shard: %v", err)
	}

	return db
}

// newClosedShard returns a bbolt handle that has already been closed, so any
// transaction on it fails.
func newClosedShard(t *testing.T) *bolt.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "closed.db")
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("open closed shard: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close shard: %v", err)
	}

	return db
}

// newReadOnlyShard provisions the test bucket, then reopens the file read-only
// so writes on it fail while reads succeed.
func newReadOnlyShard(t *testing.T) *bolt.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ro.db")
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		if _, createErr := tx.CreateBucketIfNotExists([]byte(testBucket)); createErr != nil {
			return fmt.Errorf("create bucket: %w", createErr)
		}

		return nil
	}); err != nil {
		t.Fatalf("provision: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	roDB, err := bolt.Open(path, 0o600, &bolt.Options{ReadOnly: true})
	if err != nil {
		t.Fatalf("reopen read-only: %v", err)
	}
	t.Cleanup(func() { _ = roDB.Close() })

	return roDB
}

// flipContext reports no error on its first Err call and cancellation after, so
// a split builds its new shard (the copy checks the context once) yet fails its
// cleanup (the delete checks it once) — reaching splitLocked's cleanup branch.
type flipContext struct {
	context.Context
	calls int
}

func (c *flipContext) Err() error {
	c.calls++
	if c.calls <= 1 {
		return nil
	}

	return context.Canceled
}

func TestGrowShardsUsedBytesError(t *testing.T) {
	e, err := openEngine(filepath.Join(t.TempDir(), "vault"), 1<<20)
	if err != nil {
		t.Fatalf("openEngine: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	_, err = e.GrowShards(context.Background(), 3)
	assertErr(t, err, "grow after close")
}

func TestGrowShardsContextCancelled(t *testing.T) {
	e := openTestEngine(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	splits, err := e.GrowShards(ctx, 3)
	assertErr(t, err, "grow cancelled")
	if splits != 0 {
		t.Fatalf("splits = %d, want 0", splits)
	}
}

func TestGrowShardsStopsAtCap(t *testing.T) {
	e := openTestEngine(t)
	saved := shardBytesTarget
	shardBytesTarget = 1
	t.Cleanup(func() { shardBytesTarget = saved })
	writeRecords(t, e, 50)
	e.level = 10 // n = 1<<10 = maxShards, so SplitStep reports no growth
	splits, err := e.GrowShards(context.Background(), 3)
	if err != nil {
		t.Fatalf("grow at cap: %v", err)
	}
	if splits != 0 {
		t.Fatalf("splits = %d, want 0 at the shard cap", splits)
	}
}

func TestGrowShardsSplitStepError(t *testing.T) {
	e := openTestEngine(t)
	saved := shardBytesTarget
	shardBytesTarget = 1
	t.Cleanup(func() { shardBytesTarget = saved })
	writeRecords(t, e, 100)
	swapOpenBolt(t, func(string, os.FileMode, *bolt.Options) (*bolt.DB, error) {
		return nil, errCov
	})
	_, err := e.GrowShards(context.Background(), 3)
	assertErr(t, err, "grow split-step failure")
}

func TestSplitLockedBuildError(t *testing.T) {
	e := openTestEngine(t)
	writeRecords(t, e, 100)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	grew, err := e.SplitStep(ctx)
	assertErr(t, err, "cancelled split build")
	if grew {
		t.Fatal("cancelled split must not report growth")
	}
}

func TestSplitLockedManifestWriteError(t *testing.T) {
	e := openTestEngine(t)
	writeRecords(t, e, 100)
	if err := os.Chmod(e.dir, 0o500); err != nil { //nolint:gosec // read-only on purpose
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(e.dir, 0o700) }) //nolint:gosec // test cleanup
	grew, err := e.SplitStep(context.Background())
	assertErr(t, err, "manifest flip")
	if grew {
		t.Fatal("failed manifest flip must not report growth")
	}
}

func TestSplitLockedCleanupError(t *testing.T) {
	e := openTestEngine(t)
	writeRecords(t, e, 100)
	grew, err := e.SplitStep(&flipContext{Context: context.Background()})
	if !grew {
		t.Fatal("a cleanup-only failure still grew the pool")
	}
	assertErr(t, err, "split cleanup")
}

func TestBuildSplitShardStaleTmpError(t *testing.T) {
	dir := t.TempDir()
	mustNonEmptyDir(t, shardPath(dir, 8)+splittingSuffix)
	e := &engine{dir: dir}
	_, err := e.buildSplitShard(context.Background(), newSourceShard(t), 3, 8)
	assertErr(t, err, "clear stale split file")
}

func TestBuildSplitShardMkdirError(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Dir(filepath.Dir(shardPath(dir, 8)))
	if err := os.MkdirAll(parent, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chmod(parent, 0o500); err != nil { //nolint:gosec // read-only on purpose
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) }) //nolint:gosec // test cleanup
	e := &engine{dir: dir}
	_, err := e.buildSplitShard(context.Background(), newSourceShard(t), 3, 8)
	assertErr(t, err, "create shard directory")
}

func TestBuildSplitShardOpenTargetError(t *testing.T) {
	dir := t.TempDir()
	src := newSourceShard(t)
	swapOpenBolt(t, func(string, os.FileMode, *bolt.Options) (*bolt.DB, error) {
		return nil, errCov
	})
	e := &engine{dir: dir}
	_, err := e.buildSplitShard(context.Background(), src, 3, 8)
	assertErr(t, err, "open split target")
}

func TestBuildSplitShardCloseTargetError(t *testing.T) {
	dir := t.TempDir()
	src := newSourceShard(t)
	swapCloseDB(t, func(*bolt.DB) error { return errCov })
	e := &engine{dir: dir}
	_, err := e.buildSplitShard(context.Background(), src, 3, 8)
	assertErr(t, err, "close split target")
}

func TestBuildSplitShardRenameError(t *testing.T) {
	dir := t.TempDir()
	mustNonEmptyDir(t, shardPath(dir, 8))
	e := &engine{dir: dir}
	_, err := e.buildSplitShard(context.Background(), newSourceShard(t), 3, 8)
	assertErr(t, err, "install split shard")
}

func TestBuildSplitShardReopenError(t *testing.T) {
	dir := t.TempDir()
	src := newSourceShard(t)
	real := openBolt
	swapOpenBolt(t, func(p string, m os.FileMode, o *bolt.Options) (*bolt.DB, error) {
		if strings.HasSuffix(p, splittingSuffix) {
			return real(p, m, o)
		}

		return nil, errCov
	})
	e := &engine{dir: dir}
	_, err := e.buildSplitShard(context.Background(), src, 3, 8)
	assertErr(t, err, "open split shard")
}

func TestCopyMovedRecordsNewCopierError(t *testing.T) {
	err := copyMovedRecords(context.Background(), newSourceShard(t), newClosedShard(t), 3, 8)
	assertErr(t, err, "new copier on closed dst")
}

func TestCopyMovedRecordsStartBucketError(t *testing.T) {
	swapCreateBucket(t, func(*bolt.Tx, []byte) (*bolt.Bucket, error) {
		return nil, errCov
	})
	err := copyMovedRecords(context.Background(), newSourceShard(t), newSourceShard(t), 3, 8)
	assertErr(t, err, "start bucket during copy")
}

func TestSplitCopierPutEmptyKey(t *testing.T) {
	copier, err := newSplitCopier(newSourceShard(t))
	if err != nil {
		t.Fatalf("new copier: %v", err)
	}
	defer copier.abort()
	if err := copier.startBucket([]byte(testBucket)); err != nil {
		t.Fatalf("start bucket: %v", err)
	}
	assertErr(t, copier.put(nil, []byte("v")), "empty-key put")
}

func TestSplitCopierPutCommitError(t *testing.T) {
	smallSplitTx(t)
	swapCommitTx(t, func(*bolt.Tx) error { return errCov })
	copier, err := newSplitCopier(newSourceShard(t))
	if err != nil {
		t.Fatalf("new copier: %v", err)
	}
	defer copier.abort()
	if err := copier.startBucket([]byte(testBucket)); err != nil {
		t.Fatalf("start bucket: %v", err)
	}
	assertErr(t, copier.put([]byte("k"), []byte("v")), "commit split batch")
}

func TestSplitCopierPutBeginError(t *testing.T) {
	smallSplitTx(t)
	dst := newSourceShard(t)
	realCommit := commitTx
	swapCommitTx(t, func(tx *bolt.Tx) error {
		_ = realCommit(tx)
		_ = dst.Close()

		return nil
	})
	copier, err := newSplitCopier(dst)
	if err != nil {
		t.Fatalf("new copier: %v", err)
	}
	if err := copier.startBucket([]byte(testBucket)); err != nil {
		t.Fatalf("start bucket: %v", err)
	}
	assertErr(t, copier.put([]byte("k"), []byte("v")), "begin split write")
}

func TestSplitCopierCommitError(t *testing.T) {
	swapCommitTx(t, func(*bolt.Tx) error { return errCov })
	copier, err := newSplitCopier(newSourceShard(t))
	if err != nil {
		t.Fatalf("new copier: %v", err)
	}
	defer copier.abort()
	assertErr(t, copier.commit(), "commit split")
}

func TestDeleteMovedRecordsBucketNamesError(t *testing.T) {
	err := deleteMovedRecords(context.Background(), newClosedShard(t), 3, 8)
	assertErr(t, err, "bucket names on closed src")
}

func TestDeleteMovedRecordsContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := deleteMovedRecords(ctx, newSourceShard(t), 3, 8)
	assertErr(t, err, "cancelled cleanup")
}

func TestDeleteMovedRecordsBucketDeleteError(t *testing.T) {
	err := deleteMovedRecords(context.Background(), newReadOnlyShard(t), 3, 8)
	assertErr(t, err, "delete on read-only src")
}

func TestDeleteMovedBatchAbsentBucket(t *testing.T) {
	resume, err := deleteMovedBatch(newSourceShard(t), []byte("absent"), 3, 8, nil)
	if err != nil {
		t.Fatalf("absent bucket: %v", err)
	}
	if resume != nil {
		t.Fatalf("resume = %v, want nil", resume)
	}
}

func TestDeleteKeysReadOnlyTx(t *testing.T) {
	src := newSourceShard(t)
	err := src.View(func(tx *bolt.Tx) error {
		return deleteKeys(tx.Bucket([]byte(testBucket)), [][]byte{[]byte("k")})
	})
	assertErr(t, err, "delete in a read-only tx")
}
