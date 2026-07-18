package frontiercheckpoint

import (
	"errors"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestWorkerIdentityRejectsOversizedAndInvalidPrefixesBeforePersistence(t *testing.T) {
	cases := []string{
		strings.Repeat("w", yagocrawlcontract.MaximumCrawlerWorkerIdentityBytes),
		string([]byte{0xff}),
	}
	for index, prefix := range cases {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		if _, err := checkpoint.WorkerID(prefix); !errors.Is(err, ErrInvalidWorkerPrefix) {
			t.Fatalf("invalid worker prefix %d error = %v", index, err)
		}
		if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
			metadata := transaction.Bucket(metadataBucket)
			if metadata.Get(workerIdentityKey) != nil || metadata.Get(workerSuffixKey) != nil {
				t.Fatalf("invalid worker prefix %d was persisted", index)
			}

			return nil
		}); err != nil {
			t.Fatalf("inspect invalid worker prefix %d: %v", index, err)
		}
	}
}

func TestWorkerIdentityRejectsInvalidPersistedIdentityAndSuffix(t *testing.T) {
	cases := []struct {
		key   []byte
		value []byte
	}{
		{key: workerIdentityKey, value: []byte{0xff}},
		{
			key: workerSuffixKey,
			value: []byte(strings.Repeat(
				"s",
				yagocrawlcontract.MaximumCrawlerWorkerIdentityBytes,
			)),
		},
	}
	for index, testCase := range cases {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(metadataBucket).Put(testCase.key, testCase.value)
		})
		if _, err := checkpoint.WorkerID("crawler"); !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("invalid persisted worker identity %d error = %v", index, err)
		}
	}
}
