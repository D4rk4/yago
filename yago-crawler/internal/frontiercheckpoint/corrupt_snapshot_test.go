package frontiercheckpoint

import (
	"encoding/binary"
	"errors"
	"testing"

	bolt "go.etcd.io/bbolt"
)

func mutateCheckpoint(
	t *testing.T,
	checkpoint *FrontierCheckpoint,
	mutate func(*bolt.Tx) error,
) {
	t.Helper()
	if err := checkpoint.writeTransaction(testContext, mutate); err != nil {
		t.Fatalf("mutate checkpoint: %v", err)
	}
}

func admittedCheckpoint(t *testing.T) (*FrontierCheckpoint, []byte, Page) {
	t.Helper()
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	provenance := []byte("corrupt-run")
	beginTestRun(t, checkpoint, provenance, []byte("corrupt-identity"))
	page := testPage("https://example.com/page", "example.com", "observation", 0)
	if admitted, err := checkpoint.Admit(
		testContext, provenance, []Page{page},
	); err != nil || admitted != 1 {
		t.Fatalf("admit corruption fixture = %d, %v", admitted, err)
	}
	return checkpoint, provenance, page
}

func requireCorruptLoad(t *testing.T, checkpoint *FrontierCheckpoint, provenance []byte) {
	t.Helper()
	if _, err := checkpoint.Load(testContext, provenance); !errors.Is(err, ErrCorruptCheckpoint) {
		t.Fatalf("load error = %v, want corrupt checkpoint", err)
	}
}

func TestCorruptRunRowsAreRejected(t *testing.T) {
	t.Run("encoding", func(t *testing.T) {
		checkpoint, provenance, _ := admittedCheckpoint(t)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(runsBucket).Put(provenance, []byte("{"))
		})
		if err := checkpoint.Begin(
			testContext,
			provenance,
			[]byte("identity"),
			"",
		); !errors.Is(
			err,
			ErrCorruptCheckpoint,
		) {
			t.Fatalf("begin corrupt run error = %v", err)
		}
		if _, err := checkpoint.Load(
			testContext,
			provenance,
		); !errors.Is(
			err,
			ErrCorruptCheckpoint,
		) {
			t.Fatalf("load corrupt run error = %v", err)
		}
	})
	t.Run("identity", func(t *testing.T) {
		checkpoint, provenance, _ := admittedCheckpoint(t)
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return writeRunRecord(transaction, provenance, runRecord{Seeding: true})
		})
		if _, err := checkpoint.Status(
			testContext,
			provenance,
			[]byte("identity"),
		); !errors.Is(
			err,
			ErrCorruptCheckpoint,
		) {
			t.Fatalf("empty run identity error = %v", err)
		}
	})
}

func TestLoadRejectsCorruptVisitedRows(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*bolt.Tx, []byte, Page) error
	}{
		{
			name: "marker",
			mutate: func(transaction *bolt.Tx, prefix []byte, page Page) error {
				return transaction.Bucket(visitedBucket).
					Put(childRowKey(prefix, page.URL), []byte{2})
			},
		},
		{
			name: "empty url",
			mutate: func(transaction *bolt.Tx, prefix []byte, _ Page) error {
				return transaction.Bucket(visitedBucket).Put(prefix, visitedMarker)
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, page := admittedCheckpoint(t)
			prefix, _ := provenancePrefix(provenance)
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return testCase.mutate(transaction, prefix, page)
			})
			requireCorruptLoad(t, checkpoint, provenance)
		})
	}
}

func TestLoadRejectsCorruptOutstandingRows(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*bolt.Tx, []byte, Page) error
	}{
		{name: "key", mutate: putBadPageKey},
		{name: "encoding", mutate: putBadPageEncoding},
		{name: "page fields", mutate: putBadPageFields},
		{name: "position", mutate: putBadPagePosition},
		{name: "visited relationship", mutate: deletePageVisitedRow},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, page := admittedCheckpoint(t)
			prefix, _ := provenancePrefix(provenance)
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return testCase.mutate(transaction, prefix, page)
			})
			requireCorruptLoad(t, checkpoint, provenance)
		})
	}
}

func putBadPageKey(transaction *bolt.Tx, prefix []byte, _ Page) error {
	return wrapDatabaseError(
		"write corrupt page key",
		transaction.Bucket(pagesBucket).Put(childRowKey(prefix, "bad"), []byte("{}")),
	)
}

func putBadPageEncoding(transaction *bolt.Tx, prefix []byte, _ Page) error {
	return wrapDatabaseError(
		"write corrupt page encoding",
		transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), []byte("{")),
	)
}

func putBadPageFields(transaction *bolt.Tx, prefix []byte, page Page) error {
	page.URL = ""
	encoded, err := encodeRow("page", page)
	if err != nil {
		return err
	}
	return wrapDatabaseError(
		"write corrupt page fields",
		transaction.Bucket(pagesBucket).Put(sequenceRowKey(prefix, 1), encoded),
	)
}

func putBadPagePosition(transaction *bolt.Tx, prefix []byte, page Page) error {
	return wrapDatabaseError(
		"write corrupt page position",
		transaction.Bucket(pagePositionsBucket).Put(
			childRowKey(prefix, page.URL), sequenceValue(2),
		),
	)
}

func deletePageVisitedRow(transaction *bolt.Tx, prefix []byte, page Page) error {
	return wrapDatabaseError(
		"delete visited test row",
		transaction.Bucket(visitedBucket).Delete(childRowKey(prefix, page.URL)),
	)
}

func TestLoadRejectsCorruptHostRows(t *testing.T) {
	cases := []struct {
		name  string
		key   func([]byte) []byte
		value []byte
	}{
		{
			name:  "empty host",
			key:   func(prefix []byte) []byte { return prefix },
			value: []byte("{}"),
		},
		{
			name:  "encoding",
			key:   func(prefix []byte) []byte { return childRowKey(prefix, "bad") },
			value: []byte("{"),
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, _ := admittedCheckpoint(t)
			prefix, _ := provenancePrefix(provenance)
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return transaction.Bucket(hostsBucket).Put(testCase.key(prefix), testCase.value)
			})
			requireCorruptLoad(t, checkpoint, provenance)
		})
	}
}

func TestLoadRejectsInconsistentRunCounters(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*runRecord)
	}{
		{name: "pending rows", mutate: func(record *runRecord) { record.Pending = 2 }},
		{name: "page total", mutate: func(record *runRecord) { record.Pages = 0 }},
		{name: "completion", mutate: func(record *runRecord) { record.Completed = true }},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint, provenance, _ := admittedCheckpoint(t)
			mutateRunRecord(t, checkpoint, provenance, testCase.mutate)
			requireCorruptLoad(t, checkpoint, provenance)
		})
	}
}

func TestPagePositionEncodingIsBigEndian(t *testing.T) {
	value := sequenceValue(0x0102030405060708)
	if got := binary.BigEndian.Uint64(value); got != 0x0102030405060708 {
		t.Fatalf("sequence value = %x", got)
	}
}
