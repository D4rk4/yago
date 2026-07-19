package remotecrawl

import (
	"encoding/binary"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func remoteCrawlRawJSON[Value any](t *testing.T, value Value) []byte {
	t.Helper()
	raw, err := (jsonCodec[Value]{}).Encode(value)
	if err != nil {
		t.Fatal(err)
	}

	return raw
}

func remoteCrawlRawUint64(value uint64) []byte {
	raw := make([]byte, 8)
	binary.BigEndian.PutUint64(raw, value)

	return raw
}

func TestRemoteCrawlQueueStateRejectsInvalidRecords(t *testing.T) {
	snapshot := newQueueStateSnapshot()
	tests := []struct {
		name   string
		key    vault.Key
		record queueRecord
	}{
		{
			name: "sequence mismatch", key: sequenceKey(2),
			record: queueRecord{Sequence: 1, State: queueStatePending},
		},
		{
			name: "unsupported state", key: sequenceKey(1),
			record: queueRecord{Sequence: 1, State: "invalid"},
		},
		{
			name: "missing lease peer", key: sequenceKey(1),
			record: queueRecord{Sequence: 1, State: queueStateLeased, LeaseUntil: 1},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := addDesiredQueueRecord(snapshot, test.key, test.record); err == nil {
				t.Fatal("invalid queue record accepted")
			}
		})
	}
}

func TestRemoteCrawlQueueStateVersionBoundaries(t *testing.T) {
	registered, storage, _ := registerRemoteCrawlFaultCollections(t)
	if err := reconcileQueueState(storage, registered); err != nil {
		t.Fatal(err)
	}
	if err := reconcileQueueState(storage, registered); err != nil {
		t.Fatalf("current version reconciliation: %v", err)
	}

	registered, storage, _ = registerRemoteCrawlFaultCollections(t)
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return registered.schema.Put(tx, queueStateVersionKey, currentQueueStateVersion+1)
	}); err != nil {
		t.Fatal(err)
	}
	if err := reconcileQueueState(storage, registered); err == nil {
		t.Fatal("future queue state version accepted")
	}
}

func TestRemoteCrawlQueueStateReadFailures(t *testing.T) {
	t.Run("queue length", func(t *testing.T) {
		registered, storage, engine := registerRemoteCrawlFaultCollections(t)
		engine.putRaw(vault.Name("__lengths__"), vault.Key(remoteCrawlOrderBucket), []byte{1})
		if _, _, err := readQueueState(storage, registered); err == nil {
			t.Fatal("corrupt queue length accepted")
		}
	})
	t.Run("queue capacity", func(t *testing.T) {
		registered, storage, engine := registerRemoteCrawlFaultCollections(t)
		engine.putRaw(
			vault.Name("__lengths__"),
			vault.Key(remoteCrawlOrderBucket),
			remoteCrawlRawUint64(MaximumQueueCapacity+1),
		)
		if _, _, err := readQueueState(storage, registered); err == nil {
			t.Fatal("oversized queue accepted")
		}
	})
	t.Run("queue scan", func(t *testing.T) {
		registered, storage, engine := registerRemoteCrawlFaultCollections(t)
		engine.scanFailure = remoteCrawlOrderBucket
		if _, _, err := readQueueState(storage, registered); err == nil {
			t.Fatal("queue scan failure ignored")
		}
	})
	t.Run("invalid queue record", func(t *testing.T) {
		registered, storage, engine := registerRemoteCrawlFaultCollections(t)
		engine.putRaw(
			remoteCrawlOrderBucket,
			sequenceKey(2),
			remoteCrawlRawJSON(t, queueRecord{Sequence: 1, State: queueStatePending}),
		)
		if _, _, err := readQueueState(storage, registered); err == nil {
			t.Fatal("invalid queue record accepted")
		}
	})
	t.Run("pending scan", func(t *testing.T) {
		registered, storage, engine := registerRemoteCrawlFaultCollections(t)
		engine.scanFailure = remoteCrawlPendingBucket
		if _, _, err := readQueueState(storage, registered); err == nil {
			t.Fatal("pending scan failure ignored")
		}
	})
	t.Run("expiry scan", func(t *testing.T) {
		registered, storage, engine := registerRemoteCrawlFaultCollections(t)
		engine.scanFailure = remoteCrawlLeaseExpiryBucket
		if _, _, err := readQueueState(storage, registered); err == nil {
			t.Fatal("expiry scan failure ignored")
		}
	})
	t.Run("lease scan", func(t *testing.T) {
		registered, storage, engine := registerRemoteCrawlFaultCollections(t)
		engine.scanFailure = remoteCrawlLeaseCountBucket
		if _, _, err := readQueueState(storage, registered); err == nil {
			t.Fatal("lease scan failure ignored")
		}
	})
	t.Run("view", func(t *testing.T) {
		registered, storage, engine := registerRemoteCrawlFaultCollections(t)
		engine.viewFailure = true
		if _, _, err := readQueueState(storage, registered); err == nil {
			t.Fatal("view failure ignored")
		}
	})
}

func TestRemoteCrawlQueueStateIndexCapacityBounds(t *testing.T) {
	registered, storage, engine := registerRemoteCrawlFaultCollections(t)
	tests := []struct {
		name   string
		bucket vault.Name
		raw    []byte
		scan   func(*vault.Txn, collections, queueStateSnapshot) error
		fill   func(queueStateSnapshot)
	}{
		{
			name: "pending", bucket: remoteCrawlPendingBucket,
			raw:  remoteCrawlRawJSON(t, pendingRecord{Sequence: 1}),
			scan: scanPendingIndex,
			fill: func(snapshot queueStateSnapshot) {
				for index := range MaximumQueueCapacity {
					snapshot.pendingRecords[string(sequenceKey(uint64(index)))] = pendingRecord{}
				}
			},
		},
		{
			name: "expiry", bucket: remoteCrawlLeaseExpiryBucket,
			raw:  remoteCrawlRawJSON(t, leaseExpiryRecord{Sequence: 1}),
			scan: scanLeaseExpiryIndex,
			fill: func(snapshot queueStateSnapshot) {
				for index := range MaximumQueueCapacity {
					snapshot.leaseExpiries[string(sequenceKey(uint64(index)))] = leaseExpiryRecord{}
				}
			},
		},
		{
			name: "lease count", bucket: remoteCrawlLeaseCountBucket,
			raw:  remoteCrawlRawUint64(1),
			scan: scanLeaseCountIndex,
			fill: func(snapshot queueStateSnapshot) {
				for index := range MaximumQueueCapacity {
					snapshot.leasesByPeer[string(sequenceKey(uint64(index)))] = 1
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := newQueueStateSnapshot()
			test.fill(snapshot)
			engine.putRaw(test.bucket, vault.Key("entry"), test.raw)
			err := storage.View(t.Context(), func(tx *vault.Txn) error {
				return test.scan(tx, registered, snapshot)
			})
			if err == nil {
				t.Fatal("oversized queue index accepted")
			}
			engine.deleteRaw(test.bucket, vault.Key("entry"))
		})
	}
}

func TestRemoteCrawlQueueStateMutationStorageFailures(t *testing.T) {
	tests := []struct {
		name     string
		mutation queueStateMutation
		bucket   vault.Name
		remove   bool
	}{
		{
			name:   "delete pending",
			bucket: remoteCrawlPendingBucket,
			remove: true,
			mutation: queueStateMutation{
				part:   queuePendingPart,
				key:    vault.Key("key"),
				remove: true,
			},
		},
		{
			name:   "store pending",
			bucket: remoteCrawlPendingBucket,
			mutation: queueStateMutation{
				part:     queuePendingPart,
				key:      vault.Key("key"),
				sequence: 1,
			},
		},
		{
			name:   "delete expiry",
			bucket: remoteCrawlLeaseExpiryBucket,
			remove: true,
			mutation: queueStateMutation{
				part:   queueExpiryPart,
				key:    vault.Key("key"),
				remove: true,
			},
		},
		{
			name: "store expiry", bucket: remoteCrawlLeaseExpiryBucket,
			mutation: queueStateMutation{part: queueExpiryPart, key: vault.Key("key"), sequence: 1},
		},
		{
			name:   "delete peer lease",
			bucket: remoteCrawlLeaseCountBucket,
			remove: true,
			mutation: queueStateMutation{
				part:   queuePeerLeasePart,
				key:    vault.Key("key"),
				remove: true,
			},
		},
		{
			name:   "store peer lease",
			bucket: remoteCrawlLeaseCountBucket,
			mutation: queueStateMutation{
				part:       queuePeerLeasePart,
				key:        vault.Key("key"),
				leaseTotal: 1,
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			registered, storage, engine := registerRemoteCrawlFaultCollections(t)
			if test.remove {
				engine.putRaw(test.bucket, test.mutation.key, []byte("present"))
				engine.deleteFailure = test.bucket
			} else {
				engine.putFailure = test.bucket
			}
			err := storage.Update(t.Context(), func(tx *vault.Txn) error {
				return applyQueueStateMutation(tx, registered, test.mutation)
			})
			if err == nil {
				t.Fatal("queue mutation storage failure ignored")
			}
		})
	}
}

func TestRemoteCrawlQueueStateRejectsInvalidMutation(t *testing.T) {
	registered, storage, _ := registerRemoteCrawlFaultCollections(t)
	if err := storage.Update(t.Context(), func(tx *vault.Txn) error {
		return applyQueueStateMutation(tx, registered, queueStateMutation{})
	}); err == nil {
		t.Fatal("invalid queue mutation accepted")
	}
}

func TestRemoteCrawlQueueStateReconciliationStorageFailures(t *testing.T) {
	t.Run("schema decode", func(t *testing.T) {
		registered, storage, engine := registerRemoteCrawlFaultCollections(t)
		engine.putRaw(remoteCrawlSchemaBucket, queueStateVersionKey, []byte{1})
		if err := reconcileQueueState(storage, registered); err == nil {
			t.Fatal("corrupt schema accepted")
		}
	})
	t.Run("schema view", func(t *testing.T) {
		registered, storage, engine := registerRemoteCrawlFaultCollections(t)
		engine.viewFailure = true
		if err := reconcileQueueState(storage, registered); err == nil {
			t.Fatal("schema view failure ignored")
		}
	})
	t.Run("mutation update", func(t *testing.T) {
		registered, storage, engine := registerRemoteCrawlFaultCollections(t)
		engine.putRaw(
			remoteCrawlOrderBucket,
			sequenceKey(1),
			remoteCrawlRawJSON(t, queueRecord{Sequence: 1, State: queueStatePending}),
		)
		engine.putRaw(
			vault.Name("__lengths__"),
			vault.Key(remoteCrawlOrderBucket),
			remoteCrawlRawUint64(1),
		)
		engine.putFailure = remoteCrawlPendingBucket
		if err := reconcileQueueState(storage, registered); err == nil {
			t.Fatal("mutation update failure ignored")
		}
	})
	t.Run("schema update", func(t *testing.T) {
		registered, storage, engine := registerRemoteCrawlFaultCollections(t)
		engine.putFailure = remoteCrawlSchemaBucket
		if err := reconcileQueueState(storage, registered); err == nil {
			t.Fatal("schema update failure ignored")
		}
	})
	t.Run("state read", func(t *testing.T) {
		registered, storage, engine := registerRemoteCrawlFaultCollections(t)
		engine.scanFailure = remoteCrawlOrderBucket
		if err := reconcileQueueState(storage, registered); err == nil {
			t.Fatal("state read failure ignored")
		}
	})
	t.Run("outer update", func(t *testing.T) {
		registered, storage, engine := registerRemoteCrawlFaultCollections(t)
		engine.updateFailure = true
		if err := applyQueueStateMutations(
			storage,
			registered,
			[]queueStateMutation{{}},
		); err == nil {
			t.Fatal("outer update failure ignored")
		}
	})
}

func TestRemoteCrawlDesiredQueueScanEnforcesCapacityDespiteCorruptLength(t *testing.T) {
	registered, storage, engine := registerRemoteCrawlFaultCollections(t)
	for sequence := range MaximumQueueCapacity + 1 {
		engine.putRaw(
			remoteCrawlOrderBucket,
			sequenceKey(uint64(sequence)),
			remoteCrawlRawJSON(t, queueRecord{
				Sequence: uint64(sequence),
				State:    queueStatePending,
			}),
		)
	}
	if err := storage.View(t.Context(), func(tx *vault.Txn) error {
		_, err := readDesiredQueueState(tx, registered)

		return err
	}); err == nil {
		t.Fatal("oversized queue scan accepted")
	}
}
