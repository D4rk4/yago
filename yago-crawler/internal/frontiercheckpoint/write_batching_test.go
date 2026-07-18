package frontiercheckpoint

import (
	"fmt"
	"sync"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yagocrawlcontract"
)

func TestOpenConfiguresBoundedGroupCommit(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	if checkpoint.database.MaxBatchDelay != checkpointBatchDelay {
		t.Fatalf(
			"batch delay = %v, want %v",
			checkpoint.database.MaxBatchDelay,
			checkpointBatchDelay,
		)
	}
	if checkpoint.database.MaxBatchSize != checkpointBatchSize {
		t.Fatalf(
			"batch size = %d, want %d",
			checkpoint.database.MaxBatchSize,
			checkpointBatchSize,
		)
	}
}

func TestConcurrentWritesShareOneTransaction(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	const writes = 32
	checkpoint.database.MaxBatchDelay = time.Second
	checkpoint.database.MaxBatchSize = writes
	before := currentTransactionID(t, checkpoint)
	start := make(chan struct{})
	errorsByWrite := make(chan error, writes)
	var writers sync.WaitGroup
	for index := 0; index < writes; index++ {
		writers.Add(1)
		go func() {
			defer writers.Done()
			<-start
			errorsByWrite <- checkpoint.Begin(
				testContext,
				[]byte(fmt.Sprintf("batch-%02d", index)),
				[]byte(fmt.Sprintf("identity-%02d", index)),
				yagocrawlcontract.CrawlOrderPriorityNormal,
			)
		}()
	}
	close(start)
	writers.Wait()
	close(errorsByWrite)
	for err := range errorsByWrite {
		if err != nil {
			t.Fatalf("batched write: %v", err)
		}
	}
	after := currentTransactionID(t, checkpoint)
	if after-before != 1 {
		t.Fatalf("write transaction delta = %d, want 1", after-before)
	}
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		runs := transaction.Bucket(runsBucket)
		if runs.Stats().KeyN != writes {
			t.Fatalf("persisted runs = %d, want %d", runs.Stats().KeyN, writes)
		}
		return nil
	}); err != nil {
		t.Fatalf("inspect batched writes: %v", err)
	}
}

func currentTransactionID(t *testing.T, checkpoint *FrontierCheckpoint) int {
	t.Helper()
	identifier := 0
	if err := checkpoint.readTransaction(testContext, func(transaction *bolt.Tx) error {
		identifier = transaction.ID()
		return nil
	}); err != nil {
		t.Fatalf("read transaction identity: %v", err)
	}
	return identifier
}
