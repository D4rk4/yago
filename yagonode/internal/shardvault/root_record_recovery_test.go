package shardvault

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestRootRecordRecoveryRestoresOnlyRecognizedNonConflictingRows(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "vault")
	engine, err := openEngine(directory, 1<<20)
	if err != nil {
		t.Fatalf("open fixture engine: %v", err)
	}
	keys := [][]byte{
		[]byte("aaaaaaaaaaaaaaaaaaaaaaaa"),
		[]byte("bbbbbbbbbbbbbbbbbbbbbbbb"),
		[]byte("cccccccccccccccccccccccc"),
		[]byte("dddddddddddddddddddddddd"),
	}
	stored := [][]byte{
		encodeValue(rootRecordFixturePayload("valid-miss1")),
		encodeValue(rootRecordFixturePayload("valid-exact")),
		encodeValue(rootRecordFixturePayload("valid-confl")),
		encodeValue(rootRecordFixturePayload("invalid-row")),
	}
	createRootRecordBuckets(t, engine.shards[0], keys)
	putStoredFixture(t, engine, testBucket, keys[1], stored[1])
	conflict := encodeValue(rootRecordFixturePayload("other-value"))
	putStoredFixture(t, engine, testBucket, keys[2], conflict)
	closeShards(engine.shards)
	turnRootBucketsIntoRecords(t, shardPath(directory, 0), keys, stored)

	var output bytes.Buffer
	recovered, err := openEngineWithStartupProgress(
		directory,
		1<<20,
		startupProgress{logger: slog.New(slog.NewJSONHandler(&output, nil)), clock: time.Now},
		WithRootRecordRecovery(testBucket, func(_ vault.Key, raw []byte) bool {
			return bytes.HasPrefix(raw, []byte("valid-"))
		}),
	)
	if err != nil {
		t.Fatalf("recover engine: %v", err)
	}
	assertNoRootRecords(t, recovered)
	assertStoredFixture(t, recovered, keys[0], stored[0])
	assertStoredFixture(t, recovered, keys[1], stored[1])
	assertStoredFixture(t, recovered, keys[2], conflict)
	assertAbsentStoredFixture(t, recovered, testBucket, keys[3])
	assertQuarantinedKeys(t, recovered, keys[2], keys[3])
	if additions := collectionAdditions(t, recovered); additions != 1 {
		t.Fatalf("recovered collection additions = %d, want 1", additions)
	}
	logText := output.String()
	for _, fragment := range []string{
		rootRecordRecoveryMessage,
		`"isolated":4`,
		`"restored":1`,
		`"alreadyPresent":1`,
		`"conflicts":1`,
		`"retained":2`,
	} {
		if !strings.Contains(logText, fragment) {
			t.Fatalf("recovery log %q lacks %q", logText, fragment)
		}
	}
	closeShards(recovered.shards)

	again, err := openEngine(
		directory,
		1<<20,
		WithRootRecordRecovery(testBucket, func(_ vault.Key, raw []byte) bool {
			return bytes.HasPrefix(raw, []byte("valid-"))
		}),
	)
	if err != nil {
		t.Fatalf("reopen recovered engine: %v", err)
	}
	t.Cleanup(func() { closeShards(again.shards) })
	assertNoRootRecords(t, again)
	assertStoredFixture(t, again, keys[0], stored[0])
	assertStoredFixture(t, again, keys[1], stored[1])
	assertStoredFixture(t, again, keys[2], conflict)
	assertQuarantinedKeys(t, again, keys[2], keys[3])
	if additions := collectionAdditions(t, again); additions != 1 {
		t.Fatalf("reopened collection additions = %d, want unchanged 1", additions)
	}
}

func TestRootRecordRecoveryResumesAfterIsolation(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "vault")
	engine, err := openEngine(directory, 1<<20)
	if err != nil {
		t.Fatalf("open fixture engine: %v", err)
	}
	key := []byte("eeeeeeeeeeeeeeeeeeeeeeee")
	payload := rootRecordFixturePayload("valid-crash")
	stored := encodeValue(payload)
	createRootRecordBuckets(t, engine.shards[0], [][]byte{key})
	closeShards(engine.shards)
	path := shardPath(directory, 0)
	turnRootBucketsIntoRecords(t, path, [][]byte{key}, [][]byte{stored})
	database, err := openBolt(path, 0o600, openTimeoutOptions())
	if err != nil {
		t.Fatalf("open corrupt shard: %v", err)
	}
	if isolated, isolateErr := isolateRootRecords(database); isolateErr != nil || isolated != 1 {
		t.Fatalf("isolate root records = %d, %v", isolated, isolateErr)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close isolated shard: %v", err)
	}

	recovered, err := openEngine(
		directory,
		1<<20,
		WithRootRecordRecovery(testBucket, func(_ vault.Key, raw []byte) bool {
			return bytes.Equal(raw, payload)
		}),
	)
	if err != nil {
		t.Fatalf("resume recovery: %v", err)
	}
	t.Cleanup(func() { closeShards(recovered.shards) })
	assertStoredFixture(t, recovered, key, stored)
	assertQuarantinedKeys(t, recovered)
}

func TestRootRecordGuardRefusesMaintenanceAndCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.db")
	database, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	key := []byte("ffffffffffffffffffffffff")
	createRootRecordBuckets(t, database, [][]byte{key})
	if err := database.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}
	turnRootBucketsIntoRecords(
		t,
		path,
		[][]byte{key},
		[][]byte{encodeValue(rootRecordFixturePayload("invalid-now"))},
	)
	database, err = bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("reopen corrupt fixture: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	if err := validateStructuralRoot(database); !errors.Is(err, errStructuralRootRecord) {
		t.Fatalf("validate structural root = %v", err)
	}
	if _, err := bucketNames(database); !errors.Is(err, errStructuralRootRecord) {
		t.Fatalf("bucketNames = %v", err)
	}
	destination, err := bolt.Open(filepath.Join(t.TempDir(), "split.db"), 0o600, nil)
	if err != nil {
		t.Fatalf("open split destination: %v", err)
	}
	t.Cleanup(func() { _ = destination.Close() })
	if err := copyMovedRecords(
		context.Background(),
		database,
		destination,
		3,
		8,
	); !errors.Is(
		err,
		errStructuralRootRecord,
	) {
		t.Fatalf("copy moved records = %v", err)
	}
	if err := compactInto(
		database,
		filepath.Join(t.TempDir(), "compact.db"),
	); !errors.Is(
		err,
		errStructuralRootRecord,
	) {
		t.Fatalf("compact corrupt root = %v", err)
	}

	guarded := &engine{
		shards:     []*bolt.DB{database},
		shardLocks: make([]sync.Mutex, 1),
	}
	err = guarded.runUpdate(func(transaction vault.EngineTxn) error {
		_, shardErr := transaction.(*shardTxn).shard(0)

		return shardErr
	}, false)
	if !errors.Is(err, errStructuralRootRecord) {
		t.Fatalf("guarded commit = %v", err)
	}
}

func rootRecordFixturePayload(prefix string) []byte {
	payload := bytes.Repeat([]byte{'x'}, 27)
	copy(payload, prefix)

	return payload
}

func createRootRecordBuckets(t *testing.T, database *bolt.DB, keys [][]byte) {
	t.Helper()
	if err := database.Update(func(tx *bolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(testBucket)); err != nil {
			return fmt.Errorf("create fixture collection: %w", err)
		}
		for _, key := range keys {
			if _, err := tx.CreateBucket(key); err != nil {
				return fmt.Errorf("create fixture root bucket: %w", err)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("create root fixture buckets: %v", err)
	}
}

func putStoredFixture(
	t *testing.T,
	engine *engine,
	bucket vault.Name,
	key vault.Key,
	stored []byte,
) {
	t.Helper()
	destination := engine.route(bucket, key)
	if err := engine.shards[destination].Update(func(tx *bolt.Tx) error {
		created, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return fmt.Errorf("create stored fixture collection: %w", err)
		}

		return created.Put(key, stored)
	}); err != nil {
		t.Fatalf("put stored fixture: %v", err)
	}
}

func assertStoredFixture(
	t *testing.T,
	engine *engine,
	key vault.Key,
	want []byte,
) {
	t.Helper()
	destination := engine.route(testBucket, key)
	if err := engine.shards[destination].View(func(tx *bolt.Tx) error {
		stored := tx.Bucket([]byte(testBucket)).Get(key)
		if !bytes.Equal(stored, want) {
			t.Fatalf("stored %q = %x, want %x", key, stored, want)
		}

		return nil
	}); err != nil {
		t.Fatalf("read stored fixture: %v", err)
	}
}

func assertAbsentStoredFixture(
	t *testing.T,
	engine *engine,
	bucket vault.Name,
	key vault.Key,
) {
	t.Helper()
	destination := engine.route(bucket, key)
	if err := engine.shards[destination].View(func(tx *bolt.Tx) error {
		storedBucket := tx.Bucket([]byte(bucket))
		if storedBucket != nil && storedBucket.Get(key) != nil {
			t.Fatalf("unexpected stored %q", key)
		}

		return nil
	}); err != nil {
		t.Fatalf("read absent fixture: %v", err)
	}
}

func assertNoRootRecords(t *testing.T, engine *engine) {
	t.Helper()
	for shard, database := range engine.shards {
		records, err := readRootRecords(database)
		if err != nil || len(records) != 0 {
			t.Fatalf("shard %d root records = %d, %v", shard, len(records), err)
		}
	}
}

func assertQuarantinedKeys(t *testing.T, engine *engine, want ...[]byte) {
	t.Helper()
	got := make([]string, 0)
	for _, database := range engine.shards {
		records, err := readQuarantinedRootRecords(database)
		if err != nil {
			t.Fatalf("read quarantine: %v", err)
		}
		for _, record := range records {
			got = append(got, string(record.key))
		}
	}
	wantStrings := make([]string, len(want))
	for index := range want {
		wantStrings[index] = string(want[index])
	}
	slices.Sort(got)
	slices.Sort(wantStrings)
	if !slices.Equal(got, wantStrings) {
		t.Fatalf("quarantined keys = %q, want %q", got, wantStrings)
	}
}

func collectionAdditions(t *testing.T, engine *engine) uint64 {
	t.Helper()
	var additions uint64
	for _, database := range engine.shards {
		if err := database.View(func(tx *bolt.Tx) error {
			changes := tx.Bucket([]byte(collectionLengthChangesBucket))
			if changes == nil {
				return nil
			}
			shardAdditions, _, err := decodeCollectionLengthChanges(
				changes.Get([]byte(testBucket)),
			)
			additions += shardAdditions

			return err
		}); err != nil {
			t.Fatalf("read collection additions: %v", err)
		}
	}

	return additions
}

func turnRootBucketsIntoRecords(
	t *testing.T,
	path string,
	keys [][]byte,
	stored [][]byte,
) {
	t.Helper()
	directory, name := filepath.Split(path)
	rootDirectory, err := os.OpenRoot(directory)
	if err != nil {
		t.Fatalf("open root fixture directory: %v", err)
	}
	defer func() { _ = rootDirectory.Close() }()
	file, err := rootDirectory.OpenFile(name, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open root fixture: %v", err)
	}
	defer func() { _ = file.Close() }()
	pageSize, root := activeRootPage(t, file)
	for index, key := range keys {
		record := rootRecord{key: key, stored: stored[index]}
		if err := replaceRootBucket(t, file, pageSize, root, record); err != nil {
			t.Fatalf("replace root bucket %q: %v", key, err)
		}
	}
	if err := file.Sync(); err != nil {
		t.Fatalf("sync root fixture: %v", err)
	}
}

func activeRootPage(t *testing.T, file *os.File) (int64, int64) {
	t.Helper()
	var active []byte
	var activeTransaction uint64
	for page := range 2 {
		raw := make([]byte, 4096)
		if _, err := file.ReadAt(raw, int64(page*4096)); err != nil {
			t.Fatalf("read meta page: %v", err)
		}
		transaction := binary.LittleEndian.Uint64(raw[64:72])
		if active == nil || transaction > activeTransaction {
			active = raw
			activeTransaction = transaction
		}
	}

	if binary.LittleEndian.Uint32(active[36:40]) != 0 {
		t.Fatal("fixture root page exceeds the supported test range")
	}

	return int64(binary.LittleEndian.Uint32(active[24:28])),
		int64(binary.LittleEndian.Uint32(active[32:36]))
}

func replaceRootBucket(
	t *testing.T,
	file *os.File,
	pageSize int64,
	root int64,
	record rootRecord,
) error {
	t.Helper()
	pending := []int64{root}
	for len(pending) > 0 {
		pageID := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		raw := make([]byte, pageSize)
		if _, err := file.ReadAt(raw, pageID*pageSize); err != nil {
			return fmt.Errorf("read fixture root page: %w", err)
		}
		flags := binary.LittleEndian.Uint16(raw[8:10])
		switch flags {
		case 1:
			pages, err := rootFixtureBranchPages(raw)
			if err != nil {
				return err
			}
			pending = append(pending, pages...)
			continue
		case 2:
			replaced, err := replaceRootLeafRecord(file, raw, pageID, record)
			if err != nil {
				return err
			}
			if replaced {
				return nil
			}
		default:
			return fmt.Errorf("page %d flags %d", pageID, flags)
		}
	}

	return errors.New("root bucket not found")
}

func rootFixtureBranchPages(raw []byte) ([]int64, error) {
	count := int(binary.LittleEndian.Uint16(raw[10:12]))
	pages := make([]int64, 0, count)
	for index := range count {
		offset := 16 + index*16
		if binary.LittleEndian.Uint32(raw[offset+12:offset+16]) != 0 {
			return nil, errors.New("fixture branch page exceeds the supported test range")
		}
		pages = append(pages, int64(binary.LittleEndian.Uint32(raw[offset+8:offset+12])))
	}

	return pages, nil
}

func replaceRootLeafRecord(
	file *os.File,
	raw []byte,
	pageID int64,
	record rootRecord,
) (bool, error) {
	count := int(binary.LittleEndian.Uint16(raw[10:12]))
	for index := range count {
		offset := 16 + index*16
		position := int(binary.LittleEndian.Uint32(raw[offset+4 : offset+8]))
		keySize := int(binary.LittleEndian.Uint32(raw[offset+8 : offset+12]))
		valueSize := int(binary.LittleEndian.Uint32(raw[offset+12 : offset+16]))
		start := offset + position
		if !bytes.Equal(raw[start:start+keySize], record.key) {
			continue
		}
		if valueSize != len(record.stored) {
			return false, fmt.Errorf("value size %d, want %d", valueSize, len(record.stored))
		}
		binary.LittleEndian.PutUint32(raw[offset:offset+4], 0)
		copy(raw[start+keySize:start+keySize+valueSize], record.stored)
		if _, err := file.WriteAt(raw, pageID*int64(len(raw))); err != nil {
			return false, fmt.Errorf("write fixture root page: %w", err)
		}

		return true, nil
	}

	return false, nil
}
