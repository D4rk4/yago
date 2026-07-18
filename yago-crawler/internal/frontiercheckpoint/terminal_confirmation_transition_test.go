package frontiercheckpoint

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"
)

func TestTerminalConfirmationTransitionHandlesConcurrentDurableState(t *testing.T) {
	testTerminalConfirmationMissingCorruptAndChanged(t)
	testTerminalConfirmationPhaseAndWriteState(t)
}

func testTerminalConfirmationMissingCorruptAndChanged(t *testing.T) {
	t.Helper()
	t.Run("missing", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		if err := checkpoint.writeTransaction(
			testContext,
			advanceTerminalConfirmation(
				"missing",
				bytes.Repeat([]byte{1}, sha256.Size),
				bytes.Repeat([]byte{2}, sha256.Size),
			),
		); err != nil {
			t.Fatalf("missing confirmation transition: %v", err)
		}
	})
	t.Run("corrupt", func(t *testing.T) {
		checkpoint, settlement, token := acknowledgedTerminalSettlement(t, "confirmation-corrupt")
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			return transaction.Bucket(terminalOutboxBucket).
				Put([]byte(settlement.LeaseID), []byte("{"))
		})
		err := checkpoint.writeTransaction(
			testContext,
			advanceTerminalConfirmation(settlement.LeaseID, settlement.OrderIdentity, token),
		)
		if !errors.Is(err, ErrCorruptCheckpoint) {
			t.Fatalf("corrupt confirmation transition error = %v", err)
		}
	})
	t.Run("token changed", func(t *testing.T) {
		checkpoint, settlement, _ := acknowledgedTerminalSettlement(t, "confirmation-token")
		err := checkpoint.writeTransaction(
			testContext,
			advanceTerminalConfirmation(
				settlement.LeaseID,
				settlement.OrderIdentity,
				bytes.Repeat([]byte{9}, sha256.Size),
			),
		)
		if !errors.Is(err, crawlsettlement.ErrDefinitionConflict) {
			t.Fatalf("changed confirmation token error = %v", err)
		}
	})
}

func testTerminalConfirmationPhaseAndWriteState(t *testing.T) {
	t.Helper()
	t.Run("already confirming", func(t *testing.T) {
		checkpoint, settlement, token := acknowledgedTerminalSettlement(t, "confirmation-current")
		mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
			outbox, current, found, err := readTerminalSettlement(
				transaction, settlement.LeaseID, settlement.OrderIdentity,
			)
			if err != nil || !found {
				return err
			}
			current.Phase = crawlsettlement.Confirming
			return writeTerminalSettlement(outbox, current)
		})
		if err := checkpoint.writeTransaction(
			testContext,
			advanceTerminalConfirmation(settlement.LeaseID, settlement.OrderIdentity, token),
		); err != nil {
			t.Fatalf("replayed confirming transition: %v", err)
		}
	})
	t.Run("wrong phase", func(t *testing.T) {
		checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
		settlement := completedTerminalSettlement(t, checkpoint, "confirmation-phase")
		if err := checkpoint.Stage(testContext, settlement); err != nil {
			t.Fatalf("stage wrong-phase fixture: %v", err)
		}
		err := checkpoint.writeTransaction(
			testContext,
			advanceTerminalConfirmation(settlement.LeaseID, settlement.OrderIdentity, nil),
		)
		if !errors.Is(err, crawlsettlement.ErrDefinitionConflict) {
			t.Fatalf("wrong confirmation phase error = %v", err)
		}
	})
	t.Run("read-only write", func(t *testing.T) {
		checkpoint, settlement, token := acknowledgedTerminalSettlement(t, "confirmation-read-only")
		if err := checkpoint.readTransaction(
			testContext,
			advanceTerminalConfirmation(settlement.LeaseID, settlement.OrderIdentity, token),
		); err == nil {
			t.Fatal("read-only confirmation transition succeeded")
		}
	})
}

func TestWorkerSessionRebindingPropagatesDurableWriteFailure(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	settlement := completedTerminalSettlement(t, checkpoint, "rebind-read-only")
	if err := checkpoint.Stage(testContext, settlement); err != nil {
		t.Fatalf("stage rebind write fixture: %v", err)
	}
	var result workerSessionRebindingResult
	if err := checkpoint.readTransaction(
		testContext,
		rebindTerminalSettlementWorkerSession(settlement, "replacement-session", &result),
	); err == nil {
		t.Fatal("read-only worker-session rebinding succeeded")
	}
}

func acknowledgedTerminalSettlement(
	t *testing.T,
	leaseID string,
) (*FrontierCheckpoint, crawlsettlement.Settlement, []byte) {
	t.Helper()
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	settlement := completedTerminalSettlement(t, checkpoint, leaseID)
	if err := checkpoint.Stage(testContext, settlement); err != nil {
		t.Fatalf("stage acknowledged settlement fixture: %v", err)
	}
	token := bytes.Repeat([]byte{3}, sha256.Size)
	if err := checkpoint.RecordAcknowledgment(
		testContext, settlement.LeaseID, settlement.OrderIdentity, token,
	); err != nil {
		t.Fatalf("acknowledge settlement fixture: %v", err)
	}

	return checkpoint, settlement, token
}
