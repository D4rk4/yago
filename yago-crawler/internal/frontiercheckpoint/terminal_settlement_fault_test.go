package frontiercheckpoint

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	bolt "go.etcd.io/bbolt"

	"github.com/D4rk4/yago/yago-crawler/internal/crawlsettlement"
)

func TestTerminalSettlementStageRejectsInvalidDefinitionsAndRunState(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	valid := completedTerminalSettlement(t, checkpoint, "valid-stage")
	for _, testCase := range []struct {
		name       string
		settlement crawlsettlement.Settlement
		want       error
	}{
		{name: "phase", settlement: func() crawlsettlement.Settlement {
			changed := valid
			changed.Phase = crawlsettlement.Confirming
			return changed
		}(), want: crawlsettlement.ErrDefinitionConflict},
		{name: "definition", settlement: crawlsettlement.Settlement{}, want: crawlsettlement.ErrDefinitionConflict},
		{name: "lease key", settlement: func() crawlsettlement.Settlement {
			changed := valid
			changed.LeaseID = strings.Repeat("l", bolt.MaxKeySize+1)
			return changed
		}(), want: crawlsettlement.ErrDefinitionConflict},
		{name: "provenance key", settlement: func() crawlsettlement.Settlement {
			changed := valid
			changed.Provenance = []byte(strings.Repeat("p", (bolt.MaxKeySize-2)/2+1))
			return changed
		}(), want: crawlsettlement.ErrDefinitionConflict},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := checkpoint.Stage(
				testContext,
				testCase.settlement,
			); !errors.Is(
				err,
				testCase.want,
			) {
				t.Fatalf("terminal stage error = %v, want %v", err, testCase.want)
			}
		})
	}
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx, crawlsettlement.Settlement) error
	}{
		{name: "missing outbox", mutate: func(transaction *bolt.Tx, _ crawlsettlement.Settlement) error {
			return transaction.DeleteBucket(terminalOutboxBucket)
		}},
		{name: "missing run", mutate: func(transaction *bolt.Tx, settlement crawlsettlement.Settlement) error {
			return transaction.Bucket(runsBucket).Delete(settlement.Provenance)
		}},
		{name: "run encoding", mutate: func(transaction *bolt.Tx, settlement crawlsettlement.Settlement) error {
			return transaction.Bucket(runsBucket).Put(settlement.Provenance, []byte("{"))
		}},
		{name: "deleting run", mutate: func(transaction *bolt.Tx, settlement crawlsettlement.Settlement) error {
			record, _, err := readRunRecord(transaction, settlement.Provenance)
			if err != nil {
				return err
			}
			record.Deleting = true
			return writeRunRecord(transaction, settlement.Provenance, record)
		}},
		{name: "identity", mutate: func(transaction *bolt.Tx, settlement crawlsettlement.Settlement) error {
			record, _, err := readRunRecord(transaction, settlement.Provenance)
			if err != nil {
				return err
			}
			record.OrderIdentity = bytes.Repeat([]byte{9}, sha256.Size)
			return writeRunRecord(transaction, settlement.Provenance, record)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			settlement := completedTerminalSettlement(t, checkpoint, "stage-"+testCase.name)
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return testCase.mutate(transaction, settlement)
			})
			if err := checkpoint.Stage(testContext, settlement); err == nil {
				t.Fatal("terminal stage against corrupt run succeeded")
			}
		})
	}
}

func TestTerminalSettlementExistingRowRejectsCorruptionAndDefinitionChanges(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx, crawlsettlement.Settlement) error
		change func(crawlsettlement.Settlement) crawlsettlement.Settlement
	}{
		{name: "encoding", mutate: func(transaction *bolt.Tx, settlement crawlsettlement.Settlement) error {
			return transaction.Bucket(terminalOutboxBucket).Put([]byte(settlement.LeaseID), []byte("{"))
		}, change: func(settlement crawlsettlement.Settlement) crawlsettlement.Settlement { return settlement }},
		{name: "invalid record", mutate: func(transaction *bolt.Tx, settlement crawlsettlement.Settlement) error {
			return transaction.Bucket(terminalOutboxBucket).Put([]byte(settlement.LeaseID), []byte("{}"))
		}, change: func(settlement crawlsettlement.Settlement) crawlsettlement.Settlement { return settlement }},
		{name: "definition", mutate: func(*bolt.Tx, crawlsettlement.Settlement) error { return nil }, change: func(settlement crawlsettlement.Settlement) crawlsettlement.Settlement {
			settlement.Tally.Failed++
			return settlement
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			settlement := completedTerminalSettlement(t, checkpoint, "existing-"+testCase.name)
			if err := checkpoint.Stage(testContext, settlement); err != nil {
				t.Fatalf("stage existing fixture: %v", err)
			}
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return testCase.mutate(transaction, settlement)
			})
			if err := checkpoint.Stage(testContext, testCase.change(settlement)); err == nil {
				t.Fatal("changed terminal settlement succeeded")
			}
		})
	}
}

func TestTerminalSettlementAwaitingRejectsCorruptRowsAndWrapsOnce(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	for index := range 3 {
		settlement := completedTerminalSettlement(t, checkpoint, "wrap-"+string(rune('a'+index)))
		if err := checkpoint.Stage(testContext, settlement); err != nil {
			t.Fatalf("stage wrap settlement %d: %v", index, err)
		}
	}
	first, err := checkpoint.Awaiting(testContext)
	if err != nil || len(first) != 3 {
		t.Fatalf("first wrapped batch = %d, %v", len(first), err)
	}
	second, err := checkpoint.Awaiting(testContext)
	if err != nil || len(second) != 3 {
		t.Fatalf("second wrapped batch = %d, %v", len(second), err)
	}
	for _, testCase := range []struct {
		name   string
		mutate func(*bolt.Tx, crawlsettlement.Settlement) error
	}{
		{name: "missing outbox", mutate: func(transaction *bolt.Tx, _ crawlsettlement.Settlement) error {
			return transaction.DeleteBucket(terminalOutboxBucket)
		}},
		{name: "encoding", mutate: func(transaction *bolt.Tx, settlement crawlsettlement.Settlement) error {
			return transaction.Bucket(terminalOutboxBucket).Put([]byte(settlement.LeaseID), []byte("{"))
		}},
		{name: "key mismatch", mutate: func(transaction *bolt.Tx, settlement crawlsettlement.Settlement) error {
			encoded, err := encodeRow("settlement", settlement)
			if err != nil {
				return err
			}
			return transaction.Bucket(terminalOutboxBucket).Put([]byte("different-key"), encoded)
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
			settlement := completedTerminalSettlement(t, checkpoint, "awaiting-"+testCase.name)
			if err := checkpoint.Stage(testContext, settlement); err != nil {
				t.Fatalf("stage awaiting fixture: %v", err)
			}
			mutateCheckpoint(t, checkpoint, func(transaction *bolt.Tx) error {
				return testCase.mutate(transaction, settlement)
			})
			if _, err := checkpoint.Awaiting(testContext); !errors.Is(err, ErrCorruptCheckpoint) {
				t.Fatalf("corrupt awaiting error = %v", err)
			}
		})
	}
}

func TestTerminalSettlementAcknowledgmentValidatesReplayTokens(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	settlement := completedTerminalSettlement(t, checkpoint, "ack-errors")
	if err := checkpoint.Stage(testContext, settlement); err != nil {
		t.Fatalf("stage acknowledgment fixture: %v", err)
	}
	token := bytes.Repeat([]byte{1}, sha256.Size)
	for _, testCase := range []struct {
		name     string
		leaseID  string
		identity []byte
		token    []byte
	}{
		{name: "lease", identity: settlement.OrderIdentity, token: token},
		{name: "lease length", leaseID: strings.Repeat("l", bolt.MaxKeySize+1), identity: settlement.OrderIdentity, token: token},
		{name: "identity", leaseID: settlement.LeaseID, token: token},
		{name: "token", leaseID: settlement.LeaseID, identity: settlement.OrderIdentity, token: []byte{1}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := checkpoint.RecordAcknowledgment(
				testContext, testCase.leaseID, testCase.identity, testCase.token,
			); !errors.Is(err, crawlsettlement.ErrDefinitionConflict) {
				t.Fatalf("invalid acknowledgment error = %v", err)
			}
		})
	}
	if err := checkpoint.RecordAcknowledgment(
		testContext,
		"missing",
		settlement.OrderIdentity,
		token,
	); err != nil {
		t.Fatalf("missing acknowledgment replay: %v", err)
	}
	if err := checkpoint.RecordAcknowledgment(
		testContext,
		settlement.LeaseID,
		settlement.OrderIdentity,
		token,
	); err != nil {
		t.Fatalf("record acknowledgment: %v", err)
	}
	if err := checkpoint.RecordAcknowledgment(
		testContext,
		settlement.LeaseID,
		settlement.OrderIdentity,
		token,
	); err != nil {
		t.Fatalf("replay acknowledgment: %v", err)
	}
	other := bytes.Repeat([]byte{2}, sha256.Size)
	if err := checkpoint.RecordAcknowledgment(
		testContext, settlement.LeaseID, settlement.OrderIdentity, other,
	); !errors.Is(err, crawlsettlement.ErrDefinitionConflict) {
		t.Fatalf("conflicting acknowledgment token error = %v", err)
	}
}

func TestTerminalSettlementReadAndCompletionRejectInvalidState(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	settlement := completedTerminalSettlement(t, checkpoint, "read-errors")
	if err := checkpoint.Stage(testContext, settlement); err != nil {
		t.Fatalf("stage read fixture: %v", err)
	}
	for _, testCase := range []struct {
		name     string
		leaseID  string
		identity []byte
	}{
		{name: "lease", identity: settlement.OrderIdentity},
		{name: "lease length", leaseID: strings.Repeat("l", bolt.MaxKeySize+1), identity: settlement.OrderIdentity},
		{name: "identity", leaseID: settlement.LeaseID},
		{name: "wrong identity", leaseID: settlement.LeaseID, identity: bytes.Repeat([]byte{9}, sha256.Size)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, _, err := checkpoint.Current(
				testContext, testCase.leaseID, testCase.identity,
			); !errors.Is(err, crawlsettlement.ErrDefinitionConflict) {
				t.Fatalf("invalid terminal read error = %v", err)
			}
		})
	}
	if err := checkpoint.Complete(
		testContext, settlement.LeaseID, settlement.OrderIdentity,
	); !errors.Is(err, crawlsettlement.ErrDefinitionConflict) {
		t.Fatalf("premature terminal completion error = %v", err)
	}
	if err := checkpoint.PrepareConfirmation(
		testContext, settlement.LeaseID, settlement.OrderIdentity,
	); !errors.Is(err, crawlsettlement.ErrDefinitionConflict) {
		t.Fatalf("premature confirmation error = %v", err)
	}
}

func TestTerminalSettlementSessionRebindingRejectsInvalidRequestsAndMissingRows(t *testing.T) {
	checkpoint := openTestCheckpoint(t, testCheckpointPath(t))
	settlement := completedTerminalSettlement(t, checkpoint, "rebind-errors")
	if _, found, err := checkpoint.RebindWorkerSession(
		testContext, settlement, "replacement",
	); found || !errors.Is(err, crawlsettlement.ErrDefinitionConflict) {
		t.Fatalf("invalid unstaged rebinding = %v, %v", found, err)
	}
	settlement.Phase = crawlsettlement.AwaitingAcknowledgment
	if _, found, err := checkpoint.RebindWorkerSession(
		testContext, settlement, "replacement",
	); err != nil || found {
		t.Fatalf("missing staged rebinding = %v, %v", found, err)
	}
	if err := checkpoint.Stage(testContext, settlement); err != nil {
		t.Fatalf("stage rebind fixture: %v", err)
	}
	if _, found, err := checkpoint.RebindWorkerSession(
		testContext, settlement, "",
	); found || !errors.Is(err, crawlsettlement.ErrDefinitionConflict) {
		t.Fatalf("invalid replacement rebinding = %v, %v", found, err)
	}
	changed := settlement
	changed.Tally.Failed++
	if _, found, err := checkpoint.RebindWorkerSession(
		testContext, changed, "replacement",
	); found || !errors.Is(err, crawlsettlement.ErrDefinitionConflict) {
		t.Fatalf("changed definition rebinding = %v, %v", found, err)
	}
}
