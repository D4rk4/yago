package shardvault

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

type stringCodec struct{}

func (stringCodec) Encode(value string) ([]byte, error) { return []byte(value), nil }

func (stringCodec) Decode(raw []byte) (string, error) { return string(raw), nil }

func openTestVault(t *testing.T) (*vault.Vault, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "vault")
	vaulted, err := Open(dir, 1<<20)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = vaulted.Close() })

	return vaulted, dir
}

func assertOrderedScan(t *testing.T, vaulted *vault.Vault, values *vault.Collection[string]) {
	t.Helper()
	err := vaulted.View(context.Background(), func(txn *vault.Txn) error {
		got, ok, err := values.Get(txn, vault.Key("doc-007"))
		if err != nil || !ok || got != "value of doc-007" {
			t.Fatalf("get = %q %v %v", got, ok, err)
		}
		seen := make([]string, 0, 64)
		err = values.Scan(txn, vault.Key("doc-"), func(key vault.Key, _ string) (bool, error) {
			seen = append(seen, string(key))

			return true, nil
		})
		if err != nil {
			return fmt.Errorf("scan: %w", err)
		}
		if len(seen) != 64 {
			t.Fatalf("scan saw %d keys", len(seen))
		}
		for i := 1; i < len(seen); i++ {
			if seen[i-1] >= seen[i] {
				t.Fatalf("scan out of order: %s >= %s", seen[i-1], seen[i])
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("view: %v", err)
	}
}

func assertDeleteAndEarlyStop(
	t *testing.T,
	vaulted *vault.Vault,
	values *vault.Collection[string],
) {
	t.Helper()
	ctx := context.Background()
	err := vaulted.Update(ctx, func(txn *vault.Txn) error {
		if _, err := values.Delete(txn, vault.Key("doc-007")); err != nil {
			return fmt.Errorf("delete: %w", err)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	err = vaulted.View(ctx, func(txn *vault.Txn) error {
		if _, ok, _ := values.Get(txn, vault.Key("doc-007")); ok {
			t.Fatal("deleted key still present")
		}
		count := 0

		return values.Scan(txn, nil, func(vault.Key, string) (bool, error) {
			count++

			return count < 3, nil
		})
	})
	if err != nil {
		t.Fatalf("post-delete view: %v", err)
	}
}

func TestShardVaultRoundTripAndOrderedScan(t *testing.T) {
	vaulted, dir := openTestVault(t)
	values, err := vault.Register(vaulted, "docs", stringCodec{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx := context.Background()

	keys := make([]string, 0, 64)
	err = vaulted.Update(ctx, func(txn *vault.Txn) error {
		for i := 0; i < 64; i++ {
			key := fmt.Sprintf("doc-%03d", i)
			keys = append(keys, key)
			if err := values.Put(txn, vault.Key(key), "value of "+key); err != nil {
				return fmt.Errorf("put: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	// Records must spread across more than one shard file.
	populated := 0
	for i := 0; i < minShards; i++ {
		info, err := os.Stat(shardPath(dir, i))
		if err == nil && info.Size() > 0 {
			populated++
		}
	}
	if populated < 2 {
		t.Fatalf("records landed on %d shards, want spread", populated)
	}

	// The merged scan yields one ascending key sequence across shards, and
	// deletes remove across shards with early-stop scans intact.
	assertOrderedScan(t, vaulted, values)
	assertDeleteAndEarlyStop(t, vaulted, values)

	used, err := vaulted.UsedBytes(ctx)
	if err != nil || used <= 0 {
		t.Fatalf("used = %d %v", used, err)
	}
	if vaulted.QuotaBytes() != 1<<20 {
		t.Fatalf("quota = %d", vaulted.QuotaBytes())
	}
}

func TestShardVaultCompressesLargeValues(t *testing.T) {
	vaulted, dir := openTestVault(t)
	values, err := vault.Register(vaulted, "blobs", stringCodec{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx := context.Background()
	compressible := strings.Repeat("compress me please ", 4096)
	err = vaulted.Update(ctx, func(txn *vault.Txn) error {
		if err := values.Put(txn, vault.Key("big"), compressible); err != nil {
			return fmt.Errorf("put: %w", err)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	err = vaulted.View(ctx, func(txn *vault.Txn) error {
		got, ok, err := values.Get(txn, vault.Key("big"))
		if err != nil || !ok || got != compressible {
			t.Fatalf("compressed roundtrip broken: ok=%v err=%v", ok, err)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	_ = dir
	encoded := encodeValue([]byte(compressible))
	if encoded[0] != tagZstd || len(encoded) > len(compressible)/4 {
		t.Fatalf("compressible value stored as %d bytes (tag %d) for %d input",
			len(encoded), encoded[0], len(compressible))
	}
}

func TestValueEncodingEdges(t *testing.T) {
	// Small and incompressible values stay raw with a checksum.
	raw := encodeValue([]byte("tiny"))
	if raw[0] != tagRaw {
		t.Fatalf("tiny value tag = %d", raw[0])
	}
	decoded, err := decodeValue(raw)
	if err != nil || string(decoded) != "tiny" {
		t.Fatalf("raw roundtrip = %q %v", decoded, err)
	}
	// Corruption fails the checksum.
	raw[len(raw)-1] ^= 0xFF
	if _, err := decodeValue(raw); err == nil {
		t.Fatal("corrupt raw value must fail")
	}
	compressed := encodeValue(bytes.Repeat([]byte("abcd"), 100))
	if compressed[0] != tagZstd {
		t.Fatalf("compressible tag = %d", compressed[0])
	}
	compressed[len(compressed)-1] ^= 0xFF
	if _, err := decodeValue(compressed); err == nil {
		t.Fatal("corrupt zstd value must fail")
	}
	if _, err := decodeValue([]byte{0x7F}); err == nil {
		t.Fatal("unknown tag must fail")
	}
	if _, err := decodeValue([]byte{}); err == nil {
		t.Fatal("empty stored value must fail")
	}
	if _, err := decodeValue([]byte{tagRaw, 1}); err == nil {
		t.Fatal("short raw value must fail")
	}
	if got, err := decodeValue(nil); got != nil || err != nil {
		t.Fatalf("nil stored = %v %v", got, err)
	}
	// Incompressible content of qualifying size stays raw.
	random := make([]byte, 256)
	for i := range random {
		random[i] = byte(i*7 + 13)
	}
	if encoded := encodeValue(random); encoded[0] != tagRaw {
		t.Fatal("incompressible value must stay raw")
	}
}

func TestShardCountAndManifest(t *testing.T) {
	if got := shardCountForQuota(0); got != minShards {
		t.Fatalf("zero quota shards = %d", got)
	}
	if got := shardCountForQuota(200 << 30); got != 32 {
		t.Fatalf("200GB shards = %d", got)
	}
	if got := shardCountForQuota(1 << 50); got != maxShards {
		t.Fatalf("huge quota shards = %d", got)
	}

	dir := filepath.Join(t.TempDir(), "vault")
	first, err := loadOrCreateManifest(dir, 16)
	if err != nil || first.Shards != 16 {
		t.Fatalf("create manifest = %+v %v", first, err)
	}
	second, err := loadOrCreateManifest(dir, 64)
	if err != nil || second.Shards != 16 {
		t.Fatalf("reload manifest = %+v %v, want the recorded layout", second, err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte("junk"), 0o600); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	if _, err := loadOrCreateManifest(dir, 8); err == nil {
		t.Fatal("corrupt manifest must fail open")
	}
}

func TestOpenAtMigratesLegacyVault(t *testing.T) {
	base := t.TempDir()
	legacyPath := filepath.Join(base, "yago.db")
	legacy, err := bolt.Open(legacyPath, 0o600, nil)
	if err != nil {
		t.Fatalf("legacy open: %v", err)
	}
	err = legacy.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucket([]byte("docs"))
		if err != nil {
			return fmt.Errorf("create: %w", err)
		}
		if err := bucket.Put([]byte("legacy-key"), []byte("legacy value")); err != nil {
			return fmt.Errorf("fill: %w", err)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("legacy fill: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("legacy close: %v", err)
	}

	vaulted, err := OpenAt(legacyPath, 1<<20)
	if err != nil {
		t.Fatalf("open at: %v", err)
	}
	defer func() { _ = vaulted.Close() }()

	values, err := vault.Register(vaulted, "docs", stringCodec{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	err = vaulted.View(context.Background(), func(txn *vault.Txn) error {
		got, ok, err := values.Get(txn, vault.Key("legacy-key"))
		if err != nil || !ok || got != "legacy value" {
			t.Fatalf("migrated get = %q %v %v", got, ok, err)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Fatal("legacy file must be renamed away")
	}
	if _, err := os.Stat(legacyPath + migratedSuffix); err != nil {
		t.Fatalf("migrated backup missing: %v", err)
	}

	// A second open must not re-migrate.
	if err := vaulted.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	again, err := OpenAt(legacyPath, 1<<20)
	if err != nil {
		t.Fatalf("re-open: %v", err)
	}
	_ = again.Close()
}

func TestUpdateErrorRollsBack(t *testing.T) {
	vaulted, _ := openTestVault(t)
	values, err := vault.Register(vaulted, "docs", stringCodec{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx := context.Background()
	failure := fmt.Errorf("boom")
	err = vaulted.Update(ctx, func(txn *vault.Txn) error {
		if err := values.Put(txn, vault.Key("ghost"), "x"); err != nil {
			return fmt.Errorf("put: %w", err)
		}

		return failure
	})
	if err == nil {
		t.Fatal("update must surface the callback error")
	}
	err = vaulted.View(ctx, func(txn *vault.Txn) error {
		if _, ok, _ := values.Get(txn, vault.Key("ghost")); ok {
			t.Fatal("failed update must roll back")
		}

		return nil
	})
	if err != nil {
		t.Fatalf("view: %v", err)
	}
}

func TestOpenFailsOnUnusableDirectory(t *testing.T) {
	base := t.TempDir()
	blocked := filepath.Join(base, "occupied")
	if err := os.WriteFile(blocked, []byte("file"), 0o600); err != nil {
		t.Fatalf("occupy: %v", err)
	}
	if _, err := Open(blocked, 1<<20); err == nil {
		t.Fatal("open over a plain file must fail")
	}
}

func TestEngineErrorPaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	shardEngine, err := openEngine(dir, 0)
	if err != nil {
		t.Fatalf("open engine: %v", err)
	}
	ctx := context.Background()

	assertUnprovisionedBucket(t, shardEngine)

	// ErrAtCapacity surfaces unchanged from the callback.
	err = shardEngine.Update(ctx, func(vault.EngineTxn) error {
		return vault.ErrAtCapacity
	})
	if err == nil || err.Error() != vault.ErrAtCapacity.Error() {
		t.Fatalf("capacity error = %v", err)
	}

	// A View callback error surfaces.
	viewErr := fmt.Errorf("view boom")
	if err := shardEngine.View(ctx, func(vault.EngineTxn) error { return viewErr }); err == nil {
		t.Fatal("view error must surface")
	}

	// A scan over an unprovisioned bucket sees nothing.
	err = shardEngine.View(ctx, func(txn vault.EngineTxn) error {
		return txn.Bucket("ghost").Scan(nil, func(vault.Key, []byte) (bool, error) {
			t.Fatal("unprovisioned scan must be empty")

			return false, nil
		})
	})
	if err != nil {
		t.Fatalf("view: %v", err)
	}

	if err := shardEngine.Provision(""); err == nil {
		t.Fatal("empty bucket name must fail provisioning")
	}
	if err := shardEngine.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := wrapCloseError(fmt.Errorf("boom")); err == nil {
		t.Fatal("close errors must wrap")
	}
	if err := wrapCloseError(nil); err != nil {
		t.Fatalf("nil close error = %v", err)
	}
	// Update after close covers the begin-shard error path.
	err = shardEngine.Update(ctx, func(txn vault.EngineTxn) error {
		return txn.Bucket("docs").Put(vault.Key("k"), []byte("v"))
	})
	if err == nil {
		t.Fatal("update after close must fail")
	}
	if err := shardEngine.Provision("late"); err == nil {
		t.Fatal("provision after close must fail")
	}
	if _, err := shardEngine.UsedBytes(ctx); err != nil {
		t.Fatalf("used bytes after close = %v", err)
	}
	closeShards([]*bolt.DB{nil})
}

func assertUnprovisionedBucket(t *testing.T, shardEngine *engine) {
	t.Helper()
	err := shardEngine.Update(context.Background(), func(txn vault.EngineTxn) error {
		if !txn.Writable() {
			t.Fatal("update txn must be writable")
		}
		bucket := txn.Bucket("ghost")
		if err := bucket.Put(vault.Key("k"), []byte("v")); err == nil {
			t.Fatal("put into unprovisioned bucket must fail")
		}
		if got := bucket.Get(vault.Key("k")); got != nil {
			t.Fatalf("get from unprovisioned bucket = %v", got)
		}
		if err := bucket.Delete(vault.Key("k")); err == nil {
			t.Fatal("delete from unprovisioned bucket must fail")
		}

		return nil
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
}

func TestScanSurfacesCallbackAndDecodeErrors(t *testing.T) {
	vaulted, dir := openTestVault(t)
	values, err := vault.Register(vaulted, "docs", stringCodec{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx := context.Background()
	err = vaulted.Update(ctx, func(txn *vault.Txn) error {
		if err := values.Put(txn, vault.Key("a"), "one"); err != nil {
			return fmt.Errorf("put: %w", err)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	scanErr := fmt.Errorf("scan boom")
	err = vaulted.View(ctx, func(txn *vault.Txn) error {
		return values.Scan(txn, nil, func(vault.Key, string) (bool, error) {
			return false, scanErr
		})
	})
	if err == nil {
		t.Fatal("scan callback error must surface")
	}
	_ = dir
}

func TestOpenAtFailurePaths(t *testing.T) {
	base := t.TempDir()
	// A directory where the shard root should be a file blocks OpenAt.
	blockedLegacy := filepath.Join(base, "blocked.db")
	if err := os.WriteFile(blockedLegacy+".vault", []byte("file"), 0o600); err != nil {
		t.Fatalf("occupy: %v", err)
	}
	if _, err := OpenAt(blockedLegacy, 0); err == nil {
		t.Fatal("blocked shard root must fail")
	}

	// A corrupt legacy file fails migration.
	corruptLegacy := filepath.Join(base, "corrupt.db")
	if err := os.WriteFile(corruptLegacy, []byte("not a bolt file"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	if _, err := OpenAt(corruptLegacy, 0); err == nil {
		t.Fatal("corrupt legacy vault must fail migration")
	}
}

func TestOpenAndCommitFailurePaths(t *testing.T) {
	base := t.TempDir()

	// vault.New failure closes the shards.
	saved := newVault
	newVault = func(vault.Engine) (*vault.Vault, error) { return nil, fmt.Errorf("boom") }
	if _, err := Open(filepath.Join(base, "v1"), 0); err == nil {
		t.Fatal("vault init failure must surface")
	}
	newVault = saved

	// A file where a fanout directory belongs fails MkdirAll.
	blockedDir := filepath.Join(base, "v2")
	if _, err := loadOrCreateManifest(blockedDir, 8); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(blockedDir, "00"), []byte("f"), 0o600); err != nil {
		t.Fatalf("occupy: %v", err)
	}
	if _, err := openEngine(blockedDir, 0); err == nil {
		t.Fatal("blocked fanout dir must fail")
	}

	// A directory where the shard file belongs is quarantined and replaced.
	dirShard := filepath.Join(base, "v3")
	if _, err := loadOrCreateManifest(dirShard, 8); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if err := os.MkdirAll(shardPath(dirShard, 0), 0o750); err != nil {
		t.Fatalf("mk shard dir: %v", err)
	}
	replaced, err := openEngine(dirShard, 0)
	if err != nil {
		t.Fatalf("open with a directory shard: %v", err)
	}
	_ = replaced.Close()
	if _, err := os.Stat(shardPath(dirShard, 0) + quarantineSuffix); err != nil {
		t.Fatalf("quarantined directory missing: %v", err)
	}

	// Commit failures wrap; a capacity-shaped commit error maps.
	shardEngine, err := openEngine(filepath.Join(base, "v4"), 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = shardEngine.Close() }()
	if err := shardEngine.Provision("docs"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	savedCommit := commitTx
	commitTx = func(*bolt.Tx) error { return fmt.Errorf("commit boom") }
	err = shardEngine.Update(context.Background(), func(txn vault.EngineTxn) error {
		return txn.Bucket("docs").Put(vault.Key("k"), []byte("v"))
	})
	if err == nil {
		t.Fatal("commit failure must surface")
	}
	commitTx = func(*bolt.Tx) error { return fmt.Errorf("%s", vault.ErrAtCapacity.Error()) }
	err = shardEngine.Update(context.Background(), func(txn vault.EngineTxn) error {
		return txn.Bucket("docs").Put(vault.Key("k"), []byte("v"))
	})
	if err == nil || err.Error() != vault.ErrAtCapacity.Error() {
		t.Fatalf("capacity commit = %v", err)
	}
	commitTx = savedCommit

	// A missing shard file is skipped by UsedBytes.
	if err := os.Remove(shardPath(filepath.Join(base, "v4"), 0)); err != nil {
		t.Fatalf("remove shard: %v", err)
	}
	if _, err := shardEngine.UsedBytes(context.Background()); err != nil {
		t.Fatalf("used bytes with a missing shard = %v", err)
	}
}

func TestMigrateFailurePaths(t *testing.T) {
	base := t.TempDir()

	// Rename failure: the legacy file's directory is read-only after setup.
	roBase := filepath.Join(base, "ro")
	if err := os.MkdirAll(roBase, 0o750); err != nil {
		t.Fatalf("mk: %v", err)
	}
	legacyPath := filepath.Join(roBase, "yago.db")
	legacy, err := bolt.Open(legacyPath, 0o600, nil)
	if err != nil {
		t.Fatalf("legacy: %v", err)
	}
	err = legacy.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucket([]byte("docs"))
		if err != nil {
			return fmt.Errorf("create: %w", err)
		}
		if err := bucket.Put([]byte("k"), []byte("v")); err != nil {
			return fmt.Errorf("fill: %w", err)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("fill: %v", err)
	}
	_ = legacy.Close()
	// The shard root must live elsewhere so only the rename fails.
	if err := os.Chmod(roBase, 0o500); err != nil { //nolint:gosec // read-only on purpose
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roBase, 0o700) }) //nolint:gosec // test cleanup
	if _, err := OpenAt(legacyPath, 0); err == nil {
		t.Fatal("read-only legacy dir must fail the retire rename")
	}

	// copyLegacyBucket surfaces a provision failure on a closed engine.
	closedEngine, err := openEngine(filepath.Join(base, "closed"), 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = closedEngine.Close()
	if err := copyLegacyBucket(closedEngine, "docs", nil); err == nil {
		t.Fatal("provision on a closed engine must fail")
	}
}

func TestRemainingSeamBranches(t *testing.T) {
	base := t.TempDir()

	// Rename failure: a non-empty directory occupies the backup name.
	legacyPath := filepath.Join(base, "busy.db")
	legacy, err := bolt.Open(legacyPath, 0o600, nil)
	if err != nil {
		t.Fatalf("legacy: %v", err)
	}
	err = legacy.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucket([]byte("docs")); err != nil {
			return fmt.Errorf("create: %w", err)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("fill: %v", err)
	}
	_ = legacy.Close()
	if err := os.MkdirAll(filepath.Join(legacyPath+migratedSuffix, "sub"), 0o750); err != nil {
		t.Fatalf("occupy backup: %v", err)
	}
	if _, err := OpenAt(legacyPath, 0); err == nil {
		t.Fatal("occupied backup name must fail the retire rename")
	}

	// Close failure propagates through the seam.
	engineForClose, err := openEngine(filepath.Join(base, "vc"), 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	savedClose := closeDB
	closeDB = func(*bolt.DB) error { return fmt.Errorf("close boom") }
	if err := engineForClose.Close(); err == nil {
		t.Fatal("close failure must surface")
	}
	closeDB = savedClose
	_ = engineForClose.Close()

	assertMultiShardCommitRollback(t, filepath.Join(base, "vm"))

	// A scan on a closed engine surfaces the begin error.
	closed, err := openEngine(filepath.Join(base, "vx"), 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = closed.Close()
	txn := &shardTxn{engine: closed, open: make([]*bolt.Tx, len(closed.shards))}
	err = txn.Bucket("docs").Scan(nil, func(vault.Key, []byte) (bool, error) {
		return true, nil
	})
	if err == nil {
		t.Fatal("scan on a closed engine must fail")
	}
}

// assertMultiShardCommitRollback proves a commit failure across several
// touched shards rolls the rest back and leaves the shards unlocked.
func assertMultiShardCommitRollback(t *testing.T, dir string) {
	t.Helper()
	ctx := context.Background()
	multi, err := openEngine(dir, 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = multi.Close() }()
	if err := multi.Provision("docs"); err != nil {
		t.Fatalf("provision: %v", err)
	}
	savedCommit := commitTx
	commitTx = func(*bolt.Tx) error { return fmt.Errorf("commit boom") }
	err = multi.Update(ctx, func(txn vault.EngineTxn) error {
		bucket := txn.Bucket("docs")
		for i := 0; i < 32; i++ {
			if err := bucket.Put(vault.Key(fmt.Sprintf("k%02d", i)), []byte("v")); err != nil {
				return fmt.Errorf("put: %w", err)
			}
		}

		return nil
	})
	commitTx = savedCommit
	if err == nil {
		t.Fatal("multi-shard commit failure must surface")
	}
	// The shards are unlocked again: a fresh update succeeds.
	err = multi.Update(ctx, func(txn vault.EngineTxn) error {
		return txn.Bucket("docs").Put(vault.Key("after"), []byte("v"))
	})
	if err != nil {
		t.Fatalf("post-failure update: %v", err)
	}

	assertCorruptAndReadOnly(t, multi)
}

func assertCorruptAndReadOnly(t *testing.T, multi *engine) {
	t.Helper()
	ctx := context.Background()
	// A corrupt stored value fails Get to nil and fails the scan.
	corruptShard := multi.route("docs", vault.Key("after"))
	err := multi.shards[corruptShard].Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte("docs")).Put([]byte("after"), []byte{0x7F, 1, 2})
	})
	if err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	err = multi.View(ctx, func(txn vault.EngineTxn) error {
		if got := txn.Bucket("docs").Get(vault.Key("after")); got != nil {
			t.Fatalf("corrupt get = %v", got)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("view: %v", err)
	}
	err = multi.View(ctx, func(txn vault.EngineTxn) error {
		return txn.Bucket("docs").Scan(vault.Key("after"), func(vault.Key, []byte) (bool, error) {
			return true, nil
		})
	})
	if err == nil {
		t.Fatal("scan over a corrupt value must fail")
	}

	// Writes inside a read-only transaction fail.
	err = multi.View(ctx, func(txn vault.EngineTxn) error {
		bucket := txn.Bucket("docs")
		if err := bucket.Put(vault.Key("ro"), []byte("v")); err == nil {
			t.Fatal("put in a view must fail")
		}
		if err := bucket.Delete(vault.Key("after")); err == nil {
			t.Fatal("delete in a view must fail")
		}

		return nil
	})
	if err != nil {
		t.Fatalf("view: %v", err)
	}
}

func TestQuarantineReplacesDamagedShard(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	vaulted, err := Open(dir, 0)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	values, err := vault.Register(vaulted, "docs", stringCodec{})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx := context.Background()
	err = vaulted.Update(ctx, func(txn *vault.Txn) error {
		for i := 0; i < 64; i++ {
			key := fmt.Sprintf("doc-%03d", i)
			if err := values.Put(txn, vault.Key(key), "v"); err != nil {
				return fmt.Errorf("put: %w", err)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("fill: %v", err)
	}
	if err := vaulted.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Damage one shard file beyond bbolt's tolerance.
	damaged := shardPath(dir, 3)
	if err := os.WriteFile(damaged, bytes.Repeat([]byte{0xFF}, 4096), 0o600); err != nil {
		t.Fatalf("damage: %v", err)
	}

	reopened, err := Open(dir, 0)
	if err != nil {
		t.Fatalf("reopen with damaged shard: %v", err)
	}
	defer func() { _ = reopened.Close() }()
	if _, err := os.Stat(damaged + quarantineSuffix); err != nil {
		t.Fatalf("quarantine file missing: %v", err)
	}
	values, err = vault.Register(reopened, "docs", stringCodec{})
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	survivors := 0
	err = reopened.View(ctx, func(txn *vault.Txn) error {
		return values.Scan(txn, nil, func(vault.Key, string) (bool, error) {
			survivors++

			return true, nil
		})
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if survivors == 0 || survivors >= 64 {
		t.Fatalf("survivors = %d, want a partial keyspace", survivors)
	}
	// The replaced shard accepts writes again.
	err = reopened.Update(ctx, func(txn *vault.Txn) error {
		if err := values.Put(txn, vault.Key("fresh"), "v"); err != nil {
			return fmt.Errorf("put: %w", err)
		}

		return nil
	})
	if err != nil {
		t.Fatalf("write after quarantine: %v", err)
	}
}

func TestManifestWriteFailure(t *testing.T) {
	roDir := filepath.Join(t.TempDir(), "ro")
	if err := os.MkdirAll(roDir, 0o500); err != nil {
		t.Fatalf("mk ro: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o700) }) //nolint:gosec // test cleanup
	if _, err := loadOrCreateManifest(roDir, 8); err == nil {
		t.Fatal("read-only root must fail manifest write")
	}
}

func TestQuarantineFailurePaths(t *testing.T) {
	saved := openBolt
	t.Cleanup(func() { openBolt = saved })

	// An engine-level shard failure closes the already-open shards.
	openBolt = func(string, os.FileMode, *bolt.Options) (*bolt.DB, error) {
		return nil, fmt.Errorf("open boom")
	}
	if _, err := openEngine(filepath.Join(t.TempDir(), "veng"), 0); err == nil {
		t.Fatal("engine open with failing shards must fail")
	}
	openBolt = saved

	// A rename failure surfaces: the shard path does not exist.
	openBolt = func(string, os.FileMode, *bolt.Options) (*bolt.DB, error) {
		return nil, fmt.Errorf("open boom")
	}
	if _, err := openOrQuarantineShard(filepath.Join(t.TempDir(), "missing.vlt"), 0); err == nil {
		t.Fatal("rename of a missing shard must fail")
	}

	// A recreate failure after a successful quarantine surfaces.
	dir := t.TempDir()
	path := filepath.Join(dir, "000000.vlt")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := openOrQuarantineShard(path, 0); err == nil {
		t.Fatal("recreate failure must surface")
	}
}
