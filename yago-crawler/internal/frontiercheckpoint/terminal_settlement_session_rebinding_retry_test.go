package frontiercheckpoint

import (
	"testing"

	bolt "go.etcd.io/bbolt"
)

func TestWorkerSessionRebindingBatchCallbackClearsResultBeforeRetry(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	settlement := completedTerminalSettlement(t, checkpoint, "lease-rebind-retry")
	if err := checkpoint.Stage(testContext, settlement); err != nil {
		t.Fatalf("stage terminal settlement: %v", err)
	}

	var result workerSessionRebindingResult
	rebind := rebindTerminalSettlementWorkerSession(settlement, "replacement-session", &result)
	if err := checkpoint.database.Update(rebind); err != nil {
		t.Fatalf("first worker session rebind callback: %v", err)
	}
	if !result.found || result.settlement.WorkerSessionID != "replacement-session" {
		t.Fatalf("first worker session rebind result = %+v", result)
	}
	if err := checkpoint.database.Update(func(transaction *bolt.Tx) error {
		return deleteRow(
			transaction.Bucket(terminalOutboxBucket),
			[]byte(settlement.LeaseID),
			"terminal settlement",
		)
	}); err != nil {
		t.Fatalf("remove terminal settlement before callback retry: %v", err)
	}
	if err := checkpoint.database.Update(rebind); err != nil {
		t.Fatalf("retried worker session rebind callback: %v", err)
	}
	if result.found || result.settlement.LeaseID != "" {
		t.Fatalf("retried missing worker session rebind result = %+v", result)
	}
}
