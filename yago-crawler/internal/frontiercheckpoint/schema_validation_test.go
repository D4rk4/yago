package frontiercheckpoint

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func writeRawCheckpoint(t *testing.T, path string, write func(*bolt.Tx) error) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create raw checkpoint directory: %v", err)
	}
	database, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatalf("open raw checkpoint: %v", err)
	}
	if err := database.Update(write); err != nil {
		_ = database.Close()
		t.Fatalf("write raw checkpoint: %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("close raw checkpoint: %v", err)
	}
}

func putSchemaVersion(t *testing.T, transaction *bolt.Tx, version []byte) {
	t.Helper()
	metadata, err := transaction.CreateBucket(metadataBucket)
	if err != nil {
		t.Fatalf("create metadata bucket: %v", err)
	}
	if err := metadata.Put(schemaVersionKey, version); err != nil {
		t.Fatalf("put schema version: %v", err)
	}
}

func requireOpenError(t *testing.T, path string, target error) {
	t.Helper()
	checkpoint, err := Open(path)
	if checkpoint != nil {
		_ = checkpoint.Close()
	}
	if !errors.Is(err, target) {
		t.Fatalf("open error = %v, want %v", err, target)
	}
}

func TestOpenRejectsMalformedAndFutureSchema(t *testing.T) {
	futureVersion := make([]byte, 4)
	binary.BigEndian.PutUint32(futureVersion, currentSchemaVersion+1)
	cases := []struct {
		name    string
		version []byte
		target  error
	}{
		{name: "malformed", version: []byte{1}, target: ErrCorruptCheckpoint},
		{name: "zero", version: []byte{0, 0, 0, 0}, target: ErrCorruptCheckpoint},
		{name: "future", version: futureVersion, target: ErrFutureSchema},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			path := testCheckpointPath(t)
			writeRawCheckpoint(t, path, func(transaction *bolt.Tx) error {
				putSchemaVersion(t, transaction, testCase.version)
				return nil
			})
			requireOpenError(t, path, testCase.target)
		})
	}
}

func TestOpenMigratesInitialSchemaToHostPaceLedger(t *testing.T) {
	path := testCheckpointPath(t)
	writeRawCheckpoint(t, path, func(transaction *bolt.Tx) error {
		version := make([]byte, 4)
		binary.BigEndian.PutUint32(version, initialSchemaVersion)
		putSchemaVersion(t, transaction, version)
		for _, name := range initialSchemaBuckets[1:] {
			if _, err := transaction.CreateBucket(name); err != nil {
				return fmt.Errorf("create initial schema bucket: %w", err)
			}
		}
		return transaction.Bucket(metadataBucket).Put(workerSuffixKey, []byte("stable"))
	})
	checkpoint := openTestCheckpoint(t, path)
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		version := binary.BigEndian.Uint32(transaction.Bucket(metadataBucket).Get(schemaVersionKey))
		if version != currentSchemaVersion {
			t.Fatalf("migrated version = %d, want %d", version, currentSchemaVersion)
		}
		for _, name := range allSchemaBuckets {
			if transaction.Bucket(name) == nil {
				t.Fatalf("migrated bucket %q is missing", name)
			}
		}

		return nil
	}); err != nil {
		t.Fatalf("inspect migrated schema: %v", err)
	}
	workerID, err := checkpoint.WorkerID("crawler")
	if err != nil || workerID != "crawler-stable" {
		t.Fatalf("migrated worker identity = %q, %v", workerID, err)
	}
}

func TestOpenRejectsMissingSchemaMetadataAndBuckets(t *testing.T) {
	t.Run("metadata", func(t *testing.T) {
		path := testCheckpointPath(t)
		writeRawCheckpoint(t, path, func(transaction *bolt.Tx) error {
			_, err := transaction.CreateBucket([]byte("rogue"))
			return wrapDatabaseError("create rogue bucket", err)
		})
		requireOpenError(t, path, ErrCorruptCheckpoint)
	})
	t.Run("required bucket", func(t *testing.T) {
		path := testCheckpointPath(t)
		writeRawCheckpoint(t, path, func(transaction *bolt.Tx) error {
			if err := initializeSchema(transaction); err != nil {
				return err
			}
			return transaction.DeleteBucket(hostsBucket)
		})
		requireOpenError(t, path, ErrCorruptCheckpoint)
	})
}

func TestSchemaVersionUsesPortableBigEndianEncoding(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		metadata := transaction.Bucket(metadataBucket)
		if got := binary.BigEndian.Uint32(
			metadata.Get(schemaVersionKey),
		); got != currentSchemaVersion {
			t.Fatalf("schema version = %d, want %d", got, currentSchemaVersion)
		}
		return nil
	}); err != nil {
		t.Fatalf("read schema: %v", err)
	}
}
