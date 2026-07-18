package frontiercheckpoint

import (
	"encoding/binary"
	"errors"
	"testing"

	bolt "go.etcd.io/bbolt"
)

type schemaMigrationFault struct {
	name    string
	version uint32
	buckets [][]byte
	extra   []byte
}

var schemaMigrationFaults = []schemaMigrationFault{
	{
		name:    "initial missing",
		version: initialSchemaVersion,
		buckets: initialSchemaBuckets[1 : len(initialSchemaBuckets)-1],
	},
	{
		name:    "initial overlap",
		version: initialSchemaVersion,
		buckets: initialSchemaBuckets[1:],
		extra:   hostPacesBucket,
	},
	{
		name:    "host pace missing",
		version: hostPaceSchemaVersion,
		buckets: append(append([][]byte(nil), initialSchemaBuckets[1:]...), hostPacesBucket),
	},
	{
		name:    "host pace overlap",
		version: hostPaceSchemaVersion,
		buckets: append(
			append(append([][]byte(nil), initialSchemaBuckets[1:]...), hostPacesBucket),
			hostPaceOrderBucket,
		),
		extra: seedManifestBucket,
	},
	{
		name:    "seed missing",
		version: seedManifestSchemaVersion,
		buckets: append(
			append(append([][]byte(nil), initialSchemaBuckets[1:]...), hostPacesBucket),
			hostPaceOrderBucket,
		),
	},
	{
		name:    "seed overlap",
		version: seedManifestSchemaVersion,
		buckets: append(
			append(
				append(append([][]byte(nil), initialSchemaBuckets[1:]...), hostPacesBucket),
				hostPaceOrderBucket,
			),
			seedManifestBucket,
		),
		extra: terminalOutboxBucket,
	},
	{
		name:    "terminal missing",
		version: terminalOutboxSchemaVersion,
		buckets: append(
			append(
				append(append([][]byte(nil), initialSchemaBuckets[1:]...), hostPacesBucket),
				hostPaceOrderBucket,
			),
			seedManifestBucket,
		),
	},
}

func TestSchemaMigrationsRejectMissingAndOverlappingBuckets(t *testing.T) {
	for _, testCase := range schemaMigrationFaults {
		t.Run(testCase.name, func(t *testing.T) {
			runSchemaMigrationFault(t, testCase)
		})
	}
}

func runSchemaMigrationFault(t *testing.T, testCase schemaMigrationFault) {
	t.Helper()
	path := testCheckpointPath(t)
	writeRawCheckpoint(t, path, func(transaction *bolt.Tx) error {
		version := make([]byte, 4)
		binary.BigEndian.PutUint32(version, testCase.version)
		putSchemaVersion(t, transaction, version)
		for _, name := range testCase.buckets {
			if _, err := transaction.CreateBucket(name); err != nil {
				return wrapDatabaseError("create migration fixture bucket", err)
			}
		}
		if testCase.extra != nil {
			_, err := transaction.CreateBucket(testCase.extra)
			return wrapDatabaseError("create overlapping migration fixture bucket", err)
		}
		return nil
	})
	checkpoint, err := Open(path)
	if checkpoint != nil {
		_ = checkpoint.Close()
	}
	if err == nil {
		t.Fatal("invalid migration layout opened")
	}
}

func TestSchemaMigrationFunctionsPropagateReadOnlyWrites(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		for name, migrate := range map[string]func(*bolt.Tx) error{
			"initial":   migrateInitialSchema,
			"host pace": migrateHostPaceSchema,
			"seed":      migrateSeedManifestSchema,
			"terminal":  migrateTerminalOutboxSchema,
		} {
			if err := migrate(transaction); err == nil {
				t.Fatalf("read-only %s migration succeeded", name)
			}
		}
		return nil
	}); err != nil && !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("inspect read-only migrations: %v", err)
	}
}
