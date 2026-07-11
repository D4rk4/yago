package peerreputation

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

var ErrBatchSequenceConflict = errors.New("peer reputation batch sequence conflict")

type ReputationLedger struct {
	vault         *vault.Vault
	records       *vault.Collection[persistentRecord]
	configuration Configuration
}

func Open(storage *vault.Vault, configuration Configuration) (*ReputationLedger, error) {
	if err := validateConfiguration(configuration); err != nil {
		return nil, err
	}
	records, err := vault.Register(storage, recordBucket, recordCodec{})
	if err != nil {
		return nil, fmt.Errorf("register peer reputation records: %w", err)
	}
	ledger := &ReputationLedger{
		vault:         storage,
		records:       records,
		configuration: configuration,
	}
	if err := storage.Update(context.Background(), func(tx *vault.Txn) error {
		record, found, readErr := records.Get(tx, stateKey)
		if readErr != nil {
			return fmt.Errorf("read initial peer reputation state: %w", readErr)
		}
		if !found {
			if writeErr := records.Put(
				tx,
				stateKey,
				stateRecord(ledgerState{Configuration: configuration}),
			); writeErr != nil {
				return fmt.Errorf("initialize peer reputation state: %w", writeErr)
			}
		} else if record.State == nil || record.State.Configuration != configuration {
			return fmt.Errorf("peer reputation configuration does not match persisted state")
		}
		peers, scanErr := ledger.readPeers(tx)
		if scanErr != nil {
			return scanErr
		}
		if len(peers) > configuration.MaximumPeers {
			return fmt.Errorf("persisted peer reputation cardinality exceeds its bound")
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("open peer reputation ledger: %w", err)
	}

	return ledger, nil
}

func (ledger *ReputationLedger) ObserveBatch(
	ctx context.Context,
	batch ObservationBatch,
) (BatchApplication, error) {
	observations, fingerprint, err := normalizeBatch(batch)
	if err != nil {
		return BatchApplication{}, err
	}
	application := BatchApplication{}
	if err := ledger.vault.Update(ctx, func(tx *vault.Txn) error {
		var applyErr error
		application, applyErr = ledger.applyBatch(tx, batch.Sequence, observations, fingerprint)

		return applyErr
	}); err != nil {
		return BatchApplication{}, fmt.Errorf("observe peer reputation batch: %w", err)
	}

	return application, nil
}

func (ledger *ReputationLedger) LastBatchSequence(ctx context.Context) (uint64, error) {
	var sequence uint64
	if err := ledger.vault.View(ctx, func(tx *vault.Txn) error {
		state, err := ledger.readState(tx)
		if err != nil {
			return err
		}
		sequence = state.LastBatchSequence

		return nil
	}); err != nil {
		return 0, fmt.Errorf("read peer reputation batch sequence: %w", err)
	}

	return sequence, nil
}

func (ledger *ReputationLedger) applyBatch(
	tx *vault.Txn,
	sequence uint64,
	observations []normalizedObservation,
	fingerprint string,
) (BatchApplication, error) {
	state, err := ledger.readState(tx)
	if err != nil {
		return BatchApplication{}, err
	}
	peers, err := ledger.readPeers(tx)
	if err != nil {
		return BatchApplication{}, err
	}
	application := BatchApplication{
		LastSequence:  state.LastBatchSequence,
		RetainedPeers: len(peers),
	}
	if sequence < state.LastBatchSequence {
		application.Superseded = true

		return application, nil
	}
	if sequence == state.LastBatchSequence {
		if fingerprint != state.LastBatchFingerprint {
			return BatchApplication{}, ErrBatchSequenceConflict
		}

		return application, nil
	}

	original := clonePeers(peers)
	referenceNanos := ledger.applyObservations(peers, observations)
	retainBoundedPeers(peers, referenceNanos, ledger.configuration)
	if err := ledger.writePeers(tx, original, peers); err != nil {
		return BatchApplication{}, err
	}
	state.LastBatchSequence = sequence
	state.LastBatchFingerprint = fingerprint
	if err := ledger.records.Put(tx, stateKey, stateRecord(state)); err != nil {
		return BatchApplication{}, fmt.Errorf("store peer reputation batch state: %w", err)
	}

	return BatchApplication{
		Applied:       true,
		LastSequence:  sequence,
		RetainedPeers: len(peers),
	}, nil
}

func (ledger *ReputationLedger) applyObservations(
	peers map[SignedPeerIdentity]peerRecord,
	observations []normalizedObservation,
) int64 {
	referenceNanos := int64(0)
	for _, observation := range observations {
		referenceNanos = max(referenceNanos, observation.observedAtNanos)
		record, found := peers[observation.peer]
		if !found {
			record = peerRecord{
				Peer:                 observation.peer,
				NetworkGroup:         observation.networkGroup,
				LastObservedUnixNano: observation.observedAtNanos,
			}
		}
		peers[observation.peer] = addObservation(record, observation, ledger.configuration)
	}

	return referenceNanos
}

func (ledger *ReputationLedger) readState(tx *vault.Txn) (ledgerState, error) {
	record, found, err := ledger.records.Get(tx, stateKey)
	if err != nil {
		return ledgerState{}, fmt.Errorf("read peer reputation state: %w", err)
	}
	if !found || record.State == nil {
		return ledgerState{}, fmt.Errorf("peer reputation state is unavailable")
	}
	if record.State.Configuration != ledger.configuration {
		return ledgerState{}, fmt.Errorf("peer reputation configuration changed")
	}

	return *record.State, nil
}

func (ledger *ReputationLedger) readPeers(
	tx *vault.Txn,
) (map[SignedPeerIdentity]peerRecord, error) {
	peers := map[SignedPeerIdentity]peerRecord{}
	err := ledger.records.Scan(
		tx,
		vault.Key(peerKeyPrefix),
		func(key vault.Key, entry persistentRecord) (bool, error) {
			if entry.Peer == nil || string(key) != string(peerKey(entry.Peer.Peer)) {
				return false, fmt.Errorf("peer reputation record key is invalid")
			}
			peers[entry.Peer.Peer] = *entry.Peer

			return true, nil
		},
	)
	if err != nil {
		return nil, fmt.Errorf("scan peer reputations: %w", err)
	}

	return peers, nil
}

func clonePeers(peers map[SignedPeerIdentity]peerRecord) map[SignedPeerIdentity]peerRecord {
	cloned := make(map[SignedPeerIdentity]peerRecord, len(peers))
	for identity, record := range peers {
		cloned[identity] = record
	}

	return cloned
}

func (ledger *ReputationLedger) writePeers(
	tx *vault.Txn,
	original map[SignedPeerIdentity]peerRecord,
	retained map[SignedPeerIdentity]peerRecord,
) error {
	for identity := range original {
		if _, found := retained[identity]; found {
			continue
		}
		if _, err := ledger.records.Delete(tx, peerKey(identity)); err != nil {
			return fmt.Errorf("evict peer reputation: %w", err)
		}
	}
	for identity, record := range retained {
		if previous, found := original[identity]; found && previous == record {
			continue
		}
		if err := ledger.records.Put(tx, peerKey(identity), peerEntry(record)); err != nil {
			return fmt.Errorf("store peer reputation: %w", err)
		}
	}

	return nil
}

func retainBoundedPeers(
	peers map[SignedPeerIdentity]peerRecord,
	referenceNanos int64,
	configuration Configuration,
) {
	if len(peers) <= configuration.MaximumPeers {
		return
	}
	candidates := make([]peerRecord, 0, len(peers))
	for _, record := range peers {
		candidates = append(candidates, record)
	}
	sort.Slice(candidates, func(left, right int) bool {
		leftReputation := reputationAt(candidates[left], referenceNanos, configuration)
		rightReputation := reputationAt(candidates[right], referenceNanos, configuration)
		if leftReputation.Confidence != rightReputation.Confidence {
			return leftReputation.Confidence < rightReputation.Confidence
		}
		if candidates[left].LastObservedUnixNano != candidates[right].LastObservedUnixNano {
			return candidates[left].LastObservedUnixNano < candidates[right].LastObservedUnixNano
		}

		return candidates[left].Peer > candidates[right].Peer
	})
	for _, record := range candidates[:len(peers)-configuration.MaximumPeers] {
		delete(peers, record.Peer)
	}
}
