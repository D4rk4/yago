package shardvault

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/FastFilter/xorfilter"
	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const testWordWidth = 8

// openFilterEngine opens an engine with the per-shard word filter enabled over
// the test bucket, the same wiring the assembly layer supplies in production.
func openFilterEngine(t *testing.T) *engine {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "vault")
	e := openFilterEngineAt(t, dir)

	return e
}

func openFilterEngineAt(t *testing.T, dir string) *engine {
	t.Helper()
	e, err := openEngine(dir, 1<<20, WithWordFilter(testBucket, testWordWidth))
	if err != nil {
		t.Fatalf("openEngine: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	if err := e.Provision(testBucket); err != nil {
		t.Fatalf("provision: %v", err)
	}

	return e
}

// putWord stores one posting whose term prefix is word (exactly testWordWidth
// bytes) followed by url, so a word Scan seeks the prefix.
func putWord(t *testing.T, e *engine, word, url string) {
	t.Helper()
	if len(word) != testWordWidth {
		t.Fatalf("word %q must be %d bytes", word, testWordWidth)
	}
	err := e.Update(context.Background(), func(txn vault.EngineTxn) error {
		return txn.Bucket(testBucket).Put(vault.Key(word+url), []byte("v"))
	})
	if err != nil {
		t.Fatalf("put %s: %v", word, err)
	}
}

// putTo stores one value into an arbitrary bucket, used to exercise writes the
// filter must ignore (a different bucket, or a key shorter than the term width).
func putTo(t *testing.T, e *engine, bucket vault.Name, key string) {
	t.Helper()
	err := e.Update(context.Background(), func(txn vault.EngineTxn) error {
		return txn.Bucket(bucket).Put(vault.Key(key), []byte("v"))
	})
	if err != nil {
		t.Fatalf("put %s/%s: %v", bucket, key, err)
	}
}

// scanWord returns the number of postings a word Scan finds, exercising the
// filter's shard-skip guard.
func scanWord(t *testing.T, e *engine, word string) int {
	t.Helper()
	count := 0
	err := e.View(context.Background(), func(txn vault.EngineTxn) error {
		return txn.Bucket(testBucket).Scan(vault.Key(word), func(vault.Key, []byte) (bool, error) {
			count++

			return true, nil
		})
	})
	if err != nil {
		t.Fatalf("scan %s: %v", word, err)
	}

	return count
}

func TestWordFilterMayContain(t *testing.T) {
	var nilFilter *wordFilter
	if !nilFilter.mayContain(1) {
		t.Fatal("nil filter must admit everything")
	}
	if !(&wordFilter{degraded: true}).mayContain(1) {
		t.Fatal("degraded filter must admit everything")
	}

	static, err := buildFuse([]uint64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	if err != nil {
		t.Fatalf("buildFuse: %v", err)
	}
	if !(&wordFilter{static: static}).mayContain(3) {
		t.Fatal("static filter must admit a key it was built from")
	}

	empty := &wordFilter{}
	if empty.mayContain(42) {
		t.Fatal("empty filter must reject an unseen key")
	}
	empty.add(42)
	if !empty.mayContain(42) {
		t.Fatal("side-set must admit a key added after build")
	}
}

func TestWordFilterAddInitializesSideSet(t *testing.T) {
	f := &wordFilter{}
	f.add(1)
	f.add(2)
	if !f.mayContain(1) || !f.mayContain(2) {
		t.Fatal("both added keys must be admitted")
	}
}

func TestEngineWordFilterSkipsEmptyShardsThenFindsWrites(t *testing.T) {
	e := openFilterEngine(t)

	// Every shard filter is empty, so a word Scan skips them all and finds
	// nothing — the deterministic skip path.
	if got := scanWord(t, e, "word0001"); got != 0 {
		t.Fatalf("empty vault word scan = %d, want 0", got)
	}

	putWord(t, e, "word0001", "urlA")
	putWord(t, e, "word0001", "urlB")
	if got := scanWord(t, e, "word0001"); got != 2 {
		t.Fatalf("word0001 scan = %d, want 2", got)
	}
	// A different, unwritten word is still skipped everywhere.
	if got := scanWord(t, e, "word9999"); got != 0 {
		t.Fatalf("unwritten word scan = %d, want 0", got)
	}
}

func TestEngineWordFilterBuildsStaticOnReopen(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	e := openFilterEngineAt(t, dir)
	for i := range 40 {
		putWord(t, e, "word"+pad(i), "url")
	}
	if err := e.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	reopened := openFilterEngineAt(t, dir)
	if got := scanWord(t, reopened, "word0003"); got != 1 {
		t.Fatalf("reopened word0003 scan = %d, want 1 (static filter)", got)
	}
	if got := scanWord(t, reopened, "word9999"); got != 0 {
		t.Fatalf("reopened absent word scan = %d, want 0", got)
	}
}

func TestEngineWordFilterDegradesOnBuildError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "vault")
	e := openFilterEngineAt(t, dir)
	putWord(t, e, "word0001", "url")
	if err := e.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	restore := buildFuse
	buildFuse = func([]uint64) (*xorfilter.BinaryFuse[uint8], error) {
		return nil, errors.New("build refused")
	}
	t.Cleanup(func() { buildFuse = restore })

	// A degraded filter admits everything, so the written word is still found.
	reopened := openFilterEngineAt(t, dir)
	if got := scanWord(t, reopened, "word0001"); got != 1 {
		t.Fatalf("degraded word scan = %d, want 1 (never skips)", got)
	}
}

func TestBuildWordFilterReportsCollectError(t *testing.T) {
	db, err := openBolt(filepath.Join(t.TempDir(), "shard.db"), 0o600, openTimeoutOptions())
	if err != nil {
		t.Fatalf("openBolt: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	e := &engine{wordFilterBucket: testBucket, wordFilterWidth: testWordWidth}
	if filter := e.buildWordFilter(db); !filter.degraded {
		t.Fatal("a shard whose keys cannot be read must degrade to matching everything")
	}
	e.shards = []*bolt.DB{db}
	if degraded := e.initWordFilters(); degraded != 1 {
		t.Fatalf("degraded filters = %d, want 1", degraded)
	}
}

func TestNoteWordKeyIgnoresOtherBucketAndShortKeys(t *testing.T) {
	e := openFilterEngine(t)
	otherBucket := vault.Name("other")
	if err := e.Provision(otherBucket); err != nil {
		t.Fatalf("provision other: %v", err)
	}

	// A write to a different bucket, or a key shorter than the term width, must
	// not touch any shard's side-set.
	putTo(t, e, otherBucket, "word0001url")
	putTo(t, e, testBucket, "short")
	if got := scanWord(t, e, "word0001"); got != 0 {
		t.Fatalf("other-bucket write leaked into the filter: scan = %d, want 0", got)
	}
}

func TestCanSkipShardGuards(t *testing.T) {
	e := openFilterEngine(t)
	putWord(t, e, "word0001", "url")

	// Wrong bucket never skips.
	if e.canSkipShard(0, vault.Name("other"), vault.Key("word0001")) {
		t.Fatal("a non-filter bucket must never skip")
	}
	// A prefix that is not a full term width never skips.
	if e.canSkipShard(0, testBucket, vault.Key("word")) {
		t.Fatal("a partial prefix must never skip")
	}
	// Feature-off engine never skips.
	if (&engine{}).canSkipShard(0, testBucket, vault.Key("word0001")) {
		t.Fatal("a filterless engine must never skip")
	}
}

func TestRebuildWordFilterFoldsSideSet(t *testing.T) {
	e := openFilterEngine(t)
	putWord(t, e, "word0001", "url")
	idx := e.route(testBucket, vault.Key("word0001url"))

	// A feature-off engine's rebuild is a no-op and must not panic.
	(&engine{}).rebuildWordFilter(0)

	e.rebuildWordFilter(idx)
	if got := scanWord(t, e, "word0001"); got != 1 {
		t.Fatalf("after rebuild word0001 scan = %d, want 1", got)
	}
}

func TestAppendWordFilterKeepsInvariantOnSplit(t *testing.T) {
	e := openFilterEngine(t)
	for i := range 20 {
		putWord(t, e, "word"+pad(i), "url"+pad(i))
	}

	grew, err := e.SplitStep(context.Background())
	if err != nil {
		t.Fatalf("SplitStep: %v", err)
	}
	if !grew {
		t.Fatal("SplitStep did not grow the pool")
	}
	if len(e.wordFilters) != len(e.shards) {
		t.Fatalf("len(wordFilters)=%d != len(shards)=%d after split",
			len(e.wordFilters), len(e.shards))
	}
}

// pad renders i as a 4-digit suffix so "word"+pad(i) is exactly testWordWidth.
func pad(i int) string {
	const digits = "0123456789"

	return string([]byte{
		digits[(i/1000)%10],
		digits[(i/100)%10],
		digits[(i/10)%10],
		digits[i%10],
	})
}
