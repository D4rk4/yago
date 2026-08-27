package shardvault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cespare/xxhash/v2"
	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestRootRecordRecoveryResumesAfterReplayBeforeRelease(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "vault")
	initial, err := openEngine(directory, 1<<20)
	if err != nil {
		t.Fatalf("open fixture engine: %v", err)
	}
	key := []byte("gggggggggggggggggggggggg")
	payload := rootRecordFixturePayload("valid-replay")
	stored := encodeValue(payload)
	createRootRecordBuckets(t, initial.shards[0], [][]byte{key})
	closeShards(initial.shards)
	turnRootBucketsIntoRecords(t, shardPath(directory, 0), [][]byte{key}, [][]byte{stored})

	isolated, err := openEngine(directory, 1<<20)
	if err != nil {
		t.Fatalf("isolate fixture: %v", err)
	}
	isolated.rootRecordRecovery = recognizedRootRecordRecovery(payload)
	records, err := readQuarantinedRootRecords(isolated.shards[0])
	if err != nil || len(records) != 1 {
		t.Fatalf("isolated records = %d, %v", len(records), err)
	}
	destination := isolated.route(testBucket, key)
	if restored, present, conflicts, replayErr := isolated.replayRootRecords(
		isolated.shards[destination],
		[]*quarantinedRootRecord{&records[0]},
	); replayErr != nil || restored != 1 || present != 0 || conflicts != 0 {
		t.Fatalf("replay = %d/%d/%d, %v", restored, present, conflicts, replayErr)
	}
	closeShards(isolated.shards)

	recovered, err := openEngine(
		directory,
		1<<20,
		WithRootRecordRecovery(testBucket, recognizedRootRecordRecovery(payload).accept),
	)
	if err != nil {
		t.Fatalf("resume after replay: %v", err)
	}
	t.Cleanup(func() { closeShards(recovered.shards) })
	assertStoredFixture(t, recovered, key, stored)
	assertQuarantinedKeys(t, recovered)
	if additions := collectionAdditions(t, recovered); additions != 1 {
		t.Fatalf("collection additions = %d, want 1", additions)
	}
}

func TestRootRecordRecoveryUpdatesLiveWordFilter(t *testing.T) {
	payload := rootRecordFixturePayload("valid-filter")
	engine, err := openEngine(
		filepath.Join(t.TempDir(), "vault"),
		1<<20,
		WithWordFilter(testBucket, 4),
		WithRootRecordRecovery(testBucket, recognizedRootRecordRecovery(payload).accept),
	)
	if err != nil {
		t.Fatalf("open filtered engine: %v", err)
	}
	engine.startWordFilterMaintenance()
	waitForWordFilterMaintenance(t, engine)
	t.Cleanup(func() { _ = engine.Close() })
	key := []byte("hhhhhhhhhhhhhhhhhhhhhhhh")
	stored := encodeValue(payload)
	destination := engine.route(testBucket, key)
	word := xxhash.Sum64(key[:4])
	if engine.wordFilters[destination].mayContain(word) {
		t.Fatal("empty filter admitted the recovered word before replay")
	}
	putRootRecordQuarantine(t, engine.shards[0], rootRecord{key: key, stored: stored})
	if err := engine.recoverRootRecordsLocked(
		context.Background(),
		discardLogger(),
	); err != nil {
		t.Fatalf("recover with live filter: %v", err)
	}
	if !engine.wordFilters[destination].mayContain(word) {
		t.Fatal("live filter hid a restored posting")
	}
	assertStoredFixture(t, engine, key, stored)
}

func TestRootRecordRecoveryReportsIsolationFailure(t *testing.T) {
	database, _, _ := openCorruptRootRecordFixture(t)
	swapDeleteStructuralRootRecord(t, func(*bolt.Cursor) error { return errCov })
	engine := rootRecoveryEngine([]*bolt.DB{database}, func(vault.Key, []byte) bool { return true })
	err := engine.recoverRootRecordsLocked(context.Background(), discardLogger())
	assertErrorContains(t, err, "isolate shard 1 root records")
	assertRootRecordCount(t, database, 1)
}

func TestRootRecordRecoveryReportsQuarantineReadFailure(t *testing.T) {
	first := newSourceShard(t)
	second := newSourceShard(t)
	payload := rootRecordFixturePayload("valid-read")
	putRootRecordQuarantine(t, first, rootRecord{
		key:    []byte("iiiiiiiiiiiiiiiiiiiiiiii"),
		stored: encodeValue(payload),
	})
	engine := rootRecoveryEngine([]*bolt.DB{first, second}, func(vault.Key, []byte) bool {
		_ = second.Close()

		return true
	})
	err := engine.recoverRootRecordsLocked(context.Background(), discardLogger())
	assertErrorContains(t, err, "read shard 2 root quarantine")
}

func TestRootRecordRecoveryHonorsCancellationBeforeReplay(t *testing.T) {
	database := newSourceShard(t)
	payload := rootRecordFixturePayload("valid-cancel")
	key := []byte("jjjjjjjjjjjjjjjjjjjjjjjj")
	putRootRecordQuarantine(t, database, rootRecord{key: key, stored: encodeValue(payload)})
	engine := rootRecoveryEngine([]*bolt.DB{database}, recognizedRootRecordRecovery(payload).accept)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := engine.recoverRootRecordsLocked(ctx, discardLogger())
	assertErrorContains(t, err, "context: context canceled")
	assertQuarantinedKeys(t, engine, key)
}

func TestRootRecordRecoveryReportsReplayFailures(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*testing.T)
	}{
		{"destination", func(t *testing.T) {
			swapCreateRootRecordDestination(t, func(*bolt.Tx, []byte) (*bolt.Bucket, error) {
				return nil, errCov
			})
		}},
		{"record", func(t *testing.T) {
			swapStoreRecoveredRootRecord(t, func(*bolt.Bucket, []byte, []byte) error {
				return errCov
			})
		}},
		{"length", func(t *testing.T) {
			swapCreateCollectionLengthChangesBucket(
				t,
				func(*bolt.Tx, []byte) (*bolt.Bucket, error) {
					return nil, errCov
				},
			)
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database := newSourceShard(t)
			payload := rootRecordFixturePayload("valid-" + test.name)
			key := bytes.Repeat([]byte{test.name[0]}, 24)
			putRootRecordQuarantine(t, database, rootRecord{key: key, stored: encodeValue(payload)})
			test.configure(t)
			engine := rootRecoveryEngine(
				[]*bolt.DB{database},
				recognizedRootRecordRecovery(payload).accept,
			)
			err := engine.recoverRootRecordsLocked(context.Background(), discardLogger())
			assertErrorContains(t, err, "replay root records to shard 1")
			assertQuarantinedKeys(t, engine, key)
		})
	}
}

func TestRootRecordRecoveryRetriesReplaySyncFailureWithoutDoubleCounting(t *testing.T) {
	database := newSourceShard(t)
	database.NoSync = true
	payload := rootRecordFixturePayload("valid-sync")
	key := []byte("kkkkkkkkkkkkkkkkkkkkkkkk")
	putRootRecordQuarantine(t, database, rootRecord{key: key, stored: encodeValue(payload)})
	engine := rootRecoveryEngine([]*bolt.DB{database}, recognizedRootRecordRecovery(payload).accept)
	realSync := syncDB
	syncDB = func(*bolt.DB) error { return errCov }
	t.Cleanup(func() { syncDB = realSync })
	err := engine.recoverRootRecordsLocked(context.Background(), discardLogger())
	assertErrorContains(t, err, "sync recovered root records")
	syncDB = func(*bolt.DB) error { return nil }
	if err := engine.recoverRootRecordsLocked(
		context.Background(),
		discardLogger(),
	); err != nil {
		t.Fatalf("retry replay sync: %v", err)
	}
	assertQuarantinedKeys(t, engine)
	if additions := collectionAdditions(t, engine); additions != 1 {
		t.Fatalf("collection additions = %d, want 1", additions)
	}
}

func TestRootRecordRecoveryRetriesReleaseFailureWithoutDoubleCounting(t *testing.T) {
	database := newSourceShard(t)
	payload := rootRecordFixturePayload("valid-release")
	key := []byte("llllllllllllllllllllllll")
	putRootRecordQuarantine(t, database, rootRecord{key: key, stored: encodeValue(payload)})
	engine := rootRecoveryEngine([]*bolt.DB{database}, recognizedRootRecordRecovery(payload).accept)
	realDelete := deleteRootRecordQuarantine
	deleteRootRecordQuarantine = func(*bolt.Bucket, []byte) error { return errCov }
	t.Cleanup(func() { deleteRootRecordQuarantine = realDelete })
	err := engine.recoverRootRecordsLocked(context.Background(), discardLogger())
	assertErrorContains(t, err, "release shard 1 root records")
	deleteRootRecordQuarantine = realDelete
	if err := engine.recoverRootRecordsLocked(
		context.Background(),
		discardLogger(),
	); err != nil {
		t.Fatalf("retry release: %v", err)
	}
	assertQuarantinedKeys(t, engine)
	if additions := collectionAdditions(t, engine); additions != 1 {
		t.Fatalf("collection additions = %d, want 1", additions)
	}
}

func TestRootRecordIsolationRollsBackMutationFailures(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*testing.T)
	}{
		{"delete", func(t *testing.T) {
			swapDeleteStructuralRootRecord(t, func(*bolt.Cursor) error { return errCov })
		}},
		{"create", func(t *testing.T) {
			swapCreateRootRecordQuarantine(t, func(*bolt.Tx, []byte) (*bolt.Bucket, error) {
				return nil, errCov
			})
		}},
		{"store", func(t *testing.T) {
			swapStoreRootRecordQuarantine(t, func(*bolt.Bucket, []byte, []byte) error {
				return errCov
			})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			database, _, _ := openCorruptRootRecordFixture(t)
			test.configure(t)
			if _, err := isolateRootRecords(database); err == nil {
				t.Fatal("isolation mutation failure was ignored")
			}
			assertRootRecordCount(t, database, 1)
			records, err := readQuarantinedRootRecords(database)
			if err != nil || len(records) != 0 {
				t.Fatalf("rolled-back quarantine = %d, %v", len(records), err)
			}
		})
	}
}

func TestRootRecordIsolationRejectsQuarantineIdentityCollision(t *testing.T) {
	database, key, stored := openCorruptRootRecordFixture(t)
	envelope := encodeRootRecordEnvelope(rootRecord{key: key, stored: stored})
	identity := sha256.Sum256(envelope)
	if err := database.Update(func(tx *bolt.Tx) error {
		quarantine, err := tx.CreateBucketIfNotExists([]byte(rootRecordQuarantineBucket))
		if err != nil {
			return fmt.Errorf("create collision quarantine: %w", err)
		}

		return quarantine.Put(identity[:], []byte("different"))
	}); err != nil {
		t.Fatalf("seed collision: %v", err)
	}
	if _, err := isolateRootRecords(database); err == nil {
		t.Fatal("quarantine identity collision was accepted")
	}
	assertRootRecordCount(t, database, 1)
}

func TestRootRecordIsolationReportsDeferredSyncFailure(t *testing.T) {
	database, _, _ := openCorruptRootRecordFixture(t)
	database.NoSync = true
	realSync := syncDB
	syncDB = func(*bolt.DB) error { return errCov }
	t.Cleanup(func() { syncDB = realSync })
	if _, err := isolateRootRecords(database); err == nil {
		t.Fatal("isolation sync failure was ignored")
	}
	assertRootRecordCount(t, database, 0)
	records, err := readQuarantinedRootRecords(database)
	if err != nil || len(records) != 1 {
		t.Fatalf("committed quarantine = %d, %v", len(records), err)
	}
	syncDB = func(*bolt.DB) error { return nil }
	if err := syncRecoveryDatabase(database); err != nil {
		t.Fatalf("retry sync: %v", err)
	}
}

func TestRootRecordQuarantineEnvelopeValidation(t *testing.T) {
	valid := encodeRootRecordEnvelope(rootRecord{key: []byte("key"), stored: []byte("stored")})
	validIdentity := sha256.Sum256(valid)
	zeroLengthKey := make([]byte, rootRecordEnvelopeHeader)
	zeroLengthKey[0] = rootRecordEnvelopeVersion
	longKey := append([]byte(nil), zeroLengthKey...)
	binary.BigEndian.PutUint64(longKey[1:rootRecordEnvelopeHeader], 1)
	binaryIdentity := func(envelope []byte) []byte {
		identity := sha256.Sum256(envelope)

		return identity[:]
	}
	tests := []struct {
		name     string
		identity []byte
		envelope []byte
		valid    bool
	}{
		{"valid", validIdentity[:], valid, true},
		{"identity", []byte("short"), valid, false},
		{"short", validIdentity[:], []byte{rootRecordEnvelopeVersion}, false},
		{"version", validIdentity[:], append([]byte{2}, valid[1:]...), false},
		{"checksum", bytes.Repeat([]byte{1}, sha256.Size), valid, false},
		{"zero-key", binaryIdentity(zeroLengthKey), zeroLengthKey, false},
		{"long-key", binaryIdentity(longKey), longKey, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record, err := decodeRootRecordEnvelope(test.identity, test.envelope)
			if (err == nil) != test.valid {
				t.Fatalf("decode valid = %t, error = %v", test.valid, err)
			}
			if test.valid && (!bytes.Equal(record.key, []byte("key")) ||
				!bytes.Equal(record.stored, []byte("stored"))) {
				t.Fatalf("decoded record = %q/%q", record.key, record.stored)
			}
		})
	}
}

func TestReadQuarantinedRootRecordsRetainsMalformedEnvelope(t *testing.T) {
	database := newSourceShard(t)
	if err := database.Update(func(tx *bolt.Tx) error {
		quarantine, err := tx.CreateBucketIfNotExists([]byte(rootRecordQuarantineBucket))
		if err != nil {
			return fmt.Errorf("create malformed quarantine: %w", err)
		}

		return quarantine.Put([]byte("malformed"), []byte("evidence"))
	}); err != nil {
		t.Fatalf("seed malformed quarantine: %v", err)
	}
	records, err := readQuarantinedRootRecords(database)
	if err != nil || len(records) != 1 || records[0].key != nil ||
		!bytes.Equal(records[0].identity, []byte("malformed")) ||
		!bytes.Equal(records[0].envelope, []byte("evidence")) {
		t.Fatalf("malformed quarantine = %+v, %v", records, err)
	}
}

func TestRootRecordAcceptanceRequiresConfiguredDecoder(t *testing.T) {
	record := &quarantinedRootRecord{key: []byte("key"), stored: []byte("bad")}
	if (&engine{}).acceptsRootRecord(record) {
		t.Fatal("unconfigured recovery accepted a record")
	}
	configured := &engine{rootRecordRecovery: &rootRecordRecovery{}}
	if configured.acceptsRootRecord(record) {
		t.Fatal("recovery without a classifier accepted a record")
	}
	configured.rootRecordRecovery.accept = func(vault.Key, []byte) bool { return true }
	if configured.acceptsRootRecord(record) {
		t.Fatal("recovery accepted an undecodable stored value")
	}
}

func TestReleaseResolvedRootRecordsReportsReadAndDestinationFailures(t *testing.T) {
	closed := newClosedShard(t)
	engine := rootRecoveryEngine([]*bolt.DB{closed}, func(vault.Key, []byte) bool { return true })
	assertErrorContains(
		t,
		engine.releaseResolvedRootRecords(),
		"read shard 1 resolved root records",
	)

	source := newSourceShard(t)
	engine = rootRecoveryEngine(
		[]*bolt.DB{source, closed},
		func(vault.Key, []byte) bool { return true },
	)
	key := rootRecordKeyForDestination(t, engine, 1)
	putRootRecordQuarantine(t, source, rootRecord{
		key:    key,
		stored: encodeValue(rootRecordFixturePayload("valid-destination")),
	})
	assertErrorContains(t, engine.releaseResolvedRootRecords(), "verify recovered root record")
}

func TestDeleteResolvedRootRecordsHandlesChangedQuarantine(t *testing.T) {
	database := newSourceShard(t)
	if err := deleteResolvedRootRecords(database, []quarantinedRootRecord{{
		identity: []byte("absent"),
	}}); err != nil {
		t.Fatalf("absent quarantine: %v", err)
	}
	record := putRootRecordQuarantine(t, database, rootRecord{
		key:    []byte("mmmmmmmmmmmmmmmmmmmmmmmm"),
		stored: encodeValue(rootRecordFixturePayload("valid-delete")),
	})
	if err := deleteResolvedRootRecords(database, []quarantinedRootRecord{{
		identity: []byte("missing"),
	}}); err != nil {
		t.Fatalf("missing record: %v", err)
	}
	changed := record
	changed.envelope = []byte("changed")
	if err := deleteResolvedRootRecords(database, []quarantinedRootRecord{changed}); err == nil {
		t.Fatal("changed quarantine was deleted")
	}
}

func TestDeleteResolvedRootRecordsReportsDeleteAndSyncFailures(t *testing.T) {
	t.Run("delete", func(t *testing.T) {
		database := newSourceShard(t)
		record := putRootRecordQuarantine(t, database, rootRecord{
			key:    []byte("nnnnnnnnnnnnnnnnnnnnnnnn"),
			stored: encodeValue(rootRecordFixturePayload("valid-delete")),
		})
		swapDeleteRootRecordQuarantine(t, func(*bolt.Bucket, []byte) error { return errCov })
		if err := deleteResolvedRootRecords(database, []quarantinedRootRecord{record}); err == nil {
			t.Fatal("quarantine delete failure was ignored")
		}
	})
	t.Run("sync", func(t *testing.T) {
		database := newSourceShard(t)
		record := putRootRecordQuarantine(t, database, rootRecord{
			key:    []byte("oooooooooooooooooooooooo"),
			stored: encodeValue(rootRecordFixturePayload("valid-sync")),
		})
		database.NoSync = true
		realSync := syncDB
		syncDB = func(*bolt.DB) error { return errCov }
		t.Cleanup(func() { syncDB = realSync })
		if err := deleteResolvedRootRecords(database, []quarantinedRootRecord{record}); err == nil {
			t.Fatal("quarantine sync failure was ignored")
		}
	})
}

func TestOpenSplitAndMigrationRejectUnrecoverableRootRecords(t *testing.T) {
	t.Run("open", func(t *testing.T) {
		_, err := openEngine(filepath.Join(t.TempDir(), "vault"), 1<<20, func(engine *engine) {
			_ = engine.shards[0].Close()
		})
		assertErrorContains(t, err, "recover vault root records")
	})
	t.Run("split", func(t *testing.T) {
		engine := openTestEngine(t)
		_ = engine.shards[0].Close()
		_, err := engine.splitLocked(context.Background())
		assertErrorContains(t, err, "recover root records before split")
	})
	t.Run("migration", func(t *testing.T) {
		database, _, _ := openCorruptRootRecordFixture(t)
		path := database.Path()
		if err := database.Close(); err != nil {
			t.Fatalf("close legacy fixture: %v", err)
		}
		target := openTestEngine(t)
		assertErrorContains(t, migrateLegacy(path, target), errStructuralRootRecord.Error())
	})
}

type closeShardOnContextCheck struct {
	context.Context
	database *bolt.DB
	closed   bool
}

func (ctx *closeShardOnContextCheck) Err() error {
	if !ctx.closed {
		ctx.closed = true
		_ = ctx.database.Close()
	}

	return nil
}

func TestCompactReportsShardFailureAfterRecovery(t *testing.T) {
	engine := openTestEngine(t)
	ctx := &closeShardOnContextCheck{Context: context.Background(), database: engine.shards[0]}
	_, err := engine.Compact(ctx)
	assertErrorContains(t, err, "compact shard 0")
}

func TestCompactShardReportsMeasureFailure(t *testing.T) {
	engine := openTestEngine(t)
	_ = engine.shards[0].Close()
	_, _, err := engine.compactShard(0)
	assertErrorContains(t, err, "database not open")
}

func TestCompactIntoReportsCopyFailureAfterRootValidation(t *testing.T) {
	closed := newClosedShard(t)
	swapOpenBolt(t, func(string, os.FileMode, *bolt.Options) (*bolt.DB, error) {
		return closed, nil
	})
	err := compactInto(newSourceShard(t), filepath.Join(t.TempDir(), "compact.db"))
	assertErrorContains(t, err, "compact")
}

func recognizedRootRecordRecovery(payload []byte) *rootRecordRecovery {
	return &rootRecordRecovery{
		destination: testBucket,
		accept: func(_ vault.Key, raw []byte) bool {
			return bytes.Equal(raw, payload)
		},
	}
}

func rootRecoveryEngine(
	databases []*bolt.DB,
	accept func(vault.Key, []byte) bool,
) *engine {
	level := 0
	for 1<<level < len(databases) {
		level++
	}

	return &engine{
		shards: databases,
		level:  level,
		rootRecordRecovery: &rootRecordRecovery{
			destination: testBucket,
			accept:      accept,
		},
	}
}

func openCorruptRootRecordFixture(t *testing.T) (*bolt.DB, []byte, []byte) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "corrupt.db")
	database, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("open corrupt fixture: %v", err)
	}
	key := []byte("pppppppppppppppppppppppp")
	stored := encodeValue(rootRecordFixturePayload("valid-corrupt"))
	createRootRecordBuckets(t, database, [][]byte{key})
	if err := database.Close(); err != nil {
		t.Fatalf("close root fixture: %v", err)
	}
	turnRootBucketsIntoRecords(t, path, [][]byte{key}, [][]byte{stored})
	database, err = bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("reopen corrupt fixture: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	return database, key, stored
}

func putRootRecordQuarantine(
	t *testing.T,
	database *bolt.DB,
	record rootRecord,
) quarantinedRootRecord {
	t.Helper()
	envelope := encodeRootRecordEnvelope(record)
	identity := sha256.Sum256(envelope)
	if err := database.Update(func(tx *bolt.Tx) error {
		quarantine, err := tx.CreateBucketIfNotExists([]byte(rootRecordQuarantineBucket))
		if err != nil {
			return fmt.Errorf("create fixture quarantine: %w", err)
		}

		return quarantine.Put(identity[:], envelope)
	}); err != nil {
		t.Fatalf("put root record quarantine: %v", err)
	}
	decoded, err := decodeRootRecordEnvelope(identity[:], envelope)
	if err != nil {
		t.Fatalf("decode fixture quarantine: %v", err)
	}

	return decoded
}

func assertRootRecordCount(t *testing.T, database *bolt.DB, want int) {
	t.Helper()
	records, err := readRootRecords(database)
	if err != nil || len(records) != want {
		t.Fatalf("root records = %d, %v, want %d", len(records), err, want)
	}
}

func rootRecordKeyForDestination(t *testing.T, engine *engine, destination int) []byte {
	t.Helper()
	for candidate := range 256 {
		key := bytes.Repeat([]byte{byte(candidate)}, 24)
		if engine.route(testBucket, key) == destination {
			return key
		}
	}
	t.Fatalf("no key routes to shard %d", destination)

	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func assertErrorContains(t *testing.T, err error, fragment string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), fragment) {
		t.Fatalf("error = %v, want fragment %q", err, fragment)
	}
}

func swapDeleteStructuralRootRecord(t *testing.T, fn func(*bolt.Cursor) error) {
	t.Helper()
	real := deleteStructuralRootRecord
	deleteStructuralRootRecord = fn
	t.Cleanup(func() { deleteStructuralRootRecord = real })
}

func swapCreateRootRecordQuarantine(
	t *testing.T,
	fn func(*bolt.Tx, []byte) (*bolt.Bucket, error),
) {
	t.Helper()
	real := createRootRecordQuarantine
	createRootRecordQuarantine = fn
	t.Cleanup(func() { createRootRecordQuarantine = real })
}

func swapStoreRootRecordQuarantine(
	t *testing.T,
	fn func(*bolt.Bucket, []byte, []byte) error,
) {
	t.Helper()
	real := storeRootRecordQuarantine
	storeRootRecordQuarantine = fn
	t.Cleanup(func() { storeRootRecordQuarantine = real })
}

func swapCreateRootRecordDestination(
	t *testing.T,
	fn func(*bolt.Tx, []byte) (*bolt.Bucket, error),
) {
	t.Helper()
	real := createRootRecordDestination
	createRootRecordDestination = fn
	t.Cleanup(func() { createRootRecordDestination = real })
}

func swapStoreRecoveredRootRecord(
	t *testing.T,
	fn func(*bolt.Bucket, []byte, []byte) error,
) {
	t.Helper()
	real := storeRecoveredRootRecord
	storeRecoveredRootRecord = fn
	t.Cleanup(func() { storeRecoveredRootRecord = real })
}

func swapDeleteRootRecordQuarantine(
	t *testing.T,
	fn func(*bolt.Bucket, []byte) error,
) {
	t.Helper()
	real := deleteRootRecordQuarantine
	deleteRootRecordQuarantine = fn
	t.Cleanup(func() { deleteRootRecordQuarantine = real })
}

func swapCreateCollectionLengthChangesBucket(
	t *testing.T,
	fn func(*bolt.Tx, []byte) (*bolt.Bucket, error),
) {
	t.Helper()
	real := createCollectionLengthChangesBucket
	createCollectionLengthChangesBucket = fn
	t.Cleanup(func() { createCollectionLengthChangesBucket = real })
}
