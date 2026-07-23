package crawlbroker

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestAutomaticDiscoverySettlementIntentCodecBoundaries(t *testing.T) {
	codec := automaticDiscoverySettlementIntentCodec{}
	for _, raw := range [][]byte{
		nil,
		{automaticDiscoverySettlementIntentFormat, '{'},
	} {
		if _, err := codec.Decode(raw); err == nil {
			t.Fatal("malformed automatic discovery settlement intent was accepted")
		}
	}
	for _, intent := range []automaticDiscoverySettlementIntent{
		{},
		{
			Lease:      leaseRecord{DiscoveryKey: "https://codec.example/"},
			Settlement: leaseSettlementRecord{Outcome: leaseSettlementRequeued},
		},
		{
			Lease:      leaseRecord{DiscoveryKey: "https://codec.example/"},
			Target:     leaseControlTarget{WorkerID: "worker"},
			Settlement: leaseSettlementRecord{Outcome: leaseSettlementAcknowledged},
		},
		{
			Lease: leaseRecord{DiscoveryKey: "https://codec.example/"},
			Settlement: leaseSettlementRecord{
				Outcome:       leaseSettlementAcknowledged,
				OrderIdentity: []byte{1},
			},
		},
	} {
		raw, err := codec.Encode(intent)
		if err != nil {
			t.Fatalf("encode invalid settlement intent: %v", err)
		}
		if _, err := codec.Decode(raw); err == nil {
			t.Fatal("invalid automatic discovery settlement intent was accepted")
		}
	}
}

func TestAutomaticDiscoverySettlementPersistenceBoundaries(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		fixture := scriptedQueue(t)
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.persistAutomaticDiscoverySettlementTx(
				tx,
				"lease",
				automaticDiscoverySettlementIntent{},
			)
		}); err != nil {
			t.Fatalf("ignore empty settlement intent: %v", err)
		}
	})
	t.Run("read", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.readErrors[discoverySettlementBucket] = errors.New("read failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			return fixture.queue.persistAutomaticDiscoverySettlementTx(
				tx,
				"lease",
				automaticDiscoverySettlementIntentFor("https://read.example/", 1),
			)
		})
		if err == nil {
			t.Fatal("settlement intent read failure was hidden")
		}
	})
}

func TestAutomaticDiscoverySettlementStageIntentReadFailuresPropagate(t *testing.T) {
	t.Run("acknowledgment", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.readErrors[discoverySettlementBucket] = errors.New("read failed")
		if err := fixture.queue.stageAutomaticDiscoveryAcknowledgment(
			t.Context(),
			"missing",
			"worker",
			"session",
			true,
		); err == nil {
			t.Fatal("acknowledgment intent read failure was hidden")
		}
	})
	t.Run("terminal", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.readErrors[discoverySettlementBucket] = errors.New("read failed")
		if err := fixture.queue.stageAutomaticDiscoveryTerminalSettlement(
			t.Context(),
			"missing",
			terminalLeaseRequest{Outcome: leaseSettlementAcknowledged},
		); err == nil {
			t.Fatal("terminal intent read failure was hidden")
		}
	})
}

func TestAutomaticDiscoverySettlementRecoveryBoundaries(t *testing.T) {
	target := "https://recovery-boundary.example/"
	t.Run("lease identity", func(t *testing.T) {
		fixture := automaticDiscoverySettlementFixture(t, target, true)
		if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			record, _, err := fixture.queue.leases.Get(tx, vault.Key("lease"))
			if err != nil {
				return fmt.Errorf("read automatic discovery lease: %w", err)
			}
			record.WorkerID = "replacement"

			if err := fixture.queue.leases.Put(tx, vault.Key("lease"), record); err != nil {
				return fmt.Errorf("replace automatic discovery lease: %w", err)
			}

			return nil
		}); err != nil {
			t.Fatalf("replace settlement lease: %v", err)
		}
		if _, err := fixture.queue.completeAutomaticDiscoverySettlement(
			t.Context(),
			"lease",
		); !errors.Is(err, errLeaseDispositionConflict) {
			t.Fatalf("replacement lease error = %v", err)
		}
	})
	for _, boundary := range []struct {
		name string
		seed func(*scriptedQueueFixture)
	}{
		{
			name: "settlement index read",
			seed: func(fixture *scriptedQueueFixture) {
				fixture.engine.buckets[leaseSettlementOrderBucket][string(orderKey(7))] = []byte{1}
			},
		},
		{
			name: "settlement sequence read",
			seed: func(fixture *scriptedQueueFixture) {
				fixture.engine.buckets[seqBucket][string(leaseSettlementNextKey)] = []byte{1}
			},
		},
		{
			name: "settlement migration read",
			seed: func(fixture *scriptedQueueFixture) {
				raw, _ := (sequenceCodec{}).Encode(8)
				fixture.engine.buckets[seqBucket][string(leaseSettlementNextKey)] = raw
				fixture.engine.buckets[seqBucket][string(leaseSettlementMigrationNextKey)] = []byte{1}
			},
		},
	} {
		t.Run(boundary.name, func(t *testing.T) {
			fixture := automaticDiscoverySettlementFixture(t, target, false)
			seedPartialAutomaticDiscoverySettlementHistory(t, &fixture, "lease")
			boundary.seed(&fixture)
			if _, err := fixture.queue.completeAutomaticDiscoverySettlement(
				t.Context(),
				"lease",
			); err == nil {
				t.Fatal("settlement recovery boundary failure was hidden")
			}
		})
	}
}

func seedPartialAutomaticDiscoverySettlementHistory(
	t *testing.T,
	fixture *scriptedQueueFixture,
	leaseID string,
) {
	t.Helper()
	if err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		intent, found, err := fixture.queue.discoverySettlements.Get(
			tx,
			vault.Key(leaseID),
		)
		if err != nil {
			return fmt.Errorf("read staged automatic discovery settlement: %w", err)
		}
		if !found {
			return errors.New("settlement intent is missing")
		}
		settlement := intent.Settlement
		settlement.Sequence = 7
		settlement.SettledAtUnixNano = nowFunc().UnixNano()

		if err := fixture.queue.leaseSettlements.Put(
			tx,
			vault.Key(leaseID),
			settlement,
		); err != nil {
			return fmt.Errorf("seed partial automatic discovery settlement: %w", err)
		}

		return nil
	}); err != nil {
		t.Fatalf("seed partial settlement history: %v", err)
	}
}

func TestAcknowledgmentSettlementInnerBoundaries(t *testing.T) {
	target := "https://ack-inner-boundary.example/"
	for _, boundary := range []struct {
		name   string
		record leaseRecord
		owner  string
	}{
		{name: "deferred", record: leaseRecord{Deferred: true}},
		{
			name: "owner",
			record: leaseRecord{
				WorkerID:          "worker",
				WorkerSessionID:   "session",
				ExpiresAtUnixNano: nowFunc().Add(DefaultLeaseTTL).UnixNano(),
			},
			owner: "other",
		},
		{name: "order", record: leaseRecord{OrderData: []byte("{")}},
	} {
		t.Run(boundary.name, func(t *testing.T) {
			fixture := scriptedQueue(t)
			seedAutomaticDiscoveryLease(t, fixture.queue, "lease", boundary.record, "")
			err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
				_, _, _, err := fixture.queue.acknowledgeLeaseTx(
					tx,
					"lease",
					boundary.owner,
					"session",
					boundary.owner != "",
				)

				return err
			})
			if err == nil {
				t.Fatalf("%s acknowledgment boundary was accepted", boundary.name)
			}
		})
	}
	t.Run("read", func(t *testing.T) {
		fixture := scriptedQueue(t)
		fixture.engine.readErrors[leaseBucket] = errors.New("read failed")
		err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
			_, _, _, err := fixture.queue.acknowledgeLeaseTx(
				tx,
				target,
				"",
				"",
				false,
			)

			return err
		})
		if err == nil {
			t.Fatal("acknowledgment lease read failure was hidden")
		}
	})
}

func TestTerminalSettlementInnerBoundaries(t *testing.T) {
	target := "https://terminal-inner-boundary.example/"
	data := automaticDiscoveryData(t, target)
	request := automaticDiscoveryTerminalRequest(data)
	for _, boundary := range []struct {
		name   string
		record leaseRecord
		mutate func(*terminalLeaseRequest)
	}{
		{
			name: "owner",
			record: leaseRecord{
				OrderData:         data,
				WorkerID:          "worker",
				WorkerSessionID:   "session",
				ExpiresAtUnixNano: nowFunc().Add(DefaultLeaseTTL).UnixNano(),
			},
			mutate: func(request *terminalLeaseRequest) {
				request.WorkerID = "other"
			},
		},
		{
			name: "identity",
			record: leaseRecord{
				OrderData:         data,
				WorkerID:          "worker",
				WorkerSessionID:   "session",
				ExpiresAtUnixNano: nowFunc().Add(DefaultLeaseTTL).UnixNano(),
			},
			mutate: func(request *terminalLeaseRequest) {
				request.OrderIdentity[0] ^= 0xff
			},
		},
		{
			name: "order",
			record: leaseRecord{
				OrderData:         []byte("{"),
				WorkerID:          "worker",
				WorkerSessionID:   "session",
				ExpiresAtUnixNano: nowFunc().Add(DefaultLeaseTTL).UnixNano(),
			},
			mutate: func(request *terminalLeaseRequest) {
				identity := sha256.Sum256([]byte("{"))
				request.OrderIdentity = identity[:]
			},
		},
	} {
		t.Run(boundary.name, func(t *testing.T) {
			fixture := scriptedQueue(t)
			seedAutomaticDiscoveryLease(t, fixture.queue, "lease", boundary.record, "")
			got := request
			got.OrderIdentity = append([]byte(nil), request.OrderIdentity...)
			if boundary.mutate != nil {
				boundary.mutate(&got)
			}
			err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
				_, _, _, err := fixture.queue.prepareTerminalLeaseSettlementTx(
					tx,
					"lease",
					got,
					terminalSettlementRecord(got),
				)

				return err
			})
			if err == nil {
				t.Fatalf("%s terminal boundary was accepted", boundary.name)
			}
		})
	}
	t.Run("read", func(t *testing.T) {
		runTerminalSettlementLeaseReadBoundary(t, request)
	})
}

func runTerminalSettlementLeaseReadBoundary(
	t *testing.T,
	request terminalLeaseRequest,
) {
	t.Helper()
	fixture := scriptedQueue(t)
	fixture.engine.readErrors[leaseBucket] = errors.New("read failed")
	err := fixture.queue.vault.Update(t.Context(), func(tx *vault.Txn) error {
		_, _, _, err := fixture.queue.prepareTerminalLeaseSettlementTx(
			tx,
			"lease",
			request,
			terminalSettlementRecord(request),
		)

		return err
	})
	if err == nil {
		t.Fatal("terminal lease read failure was hidden")
	}
}
