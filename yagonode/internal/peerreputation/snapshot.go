package peerreputation

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const snapshotVersion = 1

type Snapshot struct {
	generatedAtNanos   int64
	neutralReliability float64
	peers              []PeerReputation
	byIdentity         map[SignedPeerIdentity]PeerReputation
}

type snapshotJSON struct {
	Version             int              `json:"version"`
	GeneratedAtUnixNano int64            `json:"generated_at_unix_nano"`
	NeutralReliability  float64          `json:"neutral_reliability"`
	Peers               []PeerReputation `json:"peers"`
}

func (ledger *ReputationLedger) Snapshot(ctx context.Context, at time.Time) (Snapshot, error) {
	atNanos, err := validatedUnixNanos(at)
	if err != nil {
		return Snapshot{}, err
	}
	var records map[SignedPeerIdentity]peerRecord
	if err := ledger.vault.View(ctx, func(tx *vault.Txn) error {
		var readErr error
		records, readErr = ledger.readPeers(tx)

		return readErr
	}); err != nil {
		return Snapshot{}, fmt.Errorf("snapshot peer reputations: %w", err)
	}
	for _, record := range records {
		atNanos = max(atNanos, record.LastObservedUnixNano)
	}
	peers := make([]PeerReputation, 0, len(records))
	for _, record := range records {
		peers = append(peers, reputationAt(record, atNanos, ledger.configuration))
	}
	neutral := ledger.configuration.PriorSuccess /
		(ledger.configuration.PriorSuccess + ledger.configuration.PriorFailure)

	return newSnapshot(atNanos, neutral, peers)
}

func (snapshot Snapshot) GeneratedAt() time.Time {
	return time.Unix(0, snapshot.generatedAtNanos).UTC()
}

func (snapshot Snapshot) Peers() []PeerReputation {
	return append([]PeerReputation(nil), snapshot.peers...)
}

func (snapshot Snapshot) Peer(identity SignedPeerIdentity) PeerReputation {
	if reputation, found := snapshot.byIdentity[identity]; found {
		return reputation
	}

	return PeerReputation{
		Peer:         identity,
		Reliability:  snapshot.neutral(),
		FusionWeight: 1,
	}
}

func (snapshot Snapshot) MarshalJSON() ([]byte, error) {
	if err := validateSnapshot(snapshot); err != nil {
		return nil, err
	}

	encoded, _ := json.Marshal(snapshotJSON{
		Version:             snapshotVersion,
		GeneratedAtUnixNano: snapshot.generatedAtNanos,
		NeutralReliability:  snapshot.neutralReliability,
		Peers:               snapshot.peers,
	})

	return encoded, nil
}

func (snapshot *Snapshot) UnmarshalJSON(raw []byte) error {
	if snapshot == nil {
		return fmt.Errorf("peer reputation snapshot destination is nil")
	}
	var encoded snapshotJSON
	if err := json.Unmarshal(raw, &encoded); err != nil {
		return fmt.Errorf("decode peer reputation snapshot: %w", err)
	}
	if encoded.Version != snapshotVersion {
		return fmt.Errorf("peer reputation snapshot version %d is unsupported", encoded.Version)
	}
	candidate, err := newSnapshot(
		encoded.GeneratedAtUnixNano,
		encoded.NeutralReliability,
		encoded.Peers,
	)
	if err != nil {
		return err
	}
	*snapshot = candidate

	return nil
}

func newSnapshot(
	generatedAtNanos int64,
	neutralReliability float64,
	peers []PeerReputation,
) (Snapshot, error) {
	candidate := Snapshot{
		generatedAtNanos:   generatedAtNanos,
		neutralReliability: neutralReliability,
		peers:              append([]PeerReputation(nil), peers...),
		byIdentity:         make(map[SignedPeerIdentity]PeerReputation, len(peers)),
	}
	sort.Slice(candidate.peers, func(left, right int) bool {
		return candidate.peers[left].Peer < candidate.peers[right].Peer
	})
	for _, reputation := range candidate.peers {
		if _, duplicate := candidate.byIdentity[reputation.Peer]; duplicate {
			return Snapshot{}, fmt.Errorf("peer reputation snapshot identity is duplicated")
		}
		candidate.byIdentity[reputation.Peer] = reputation
	}
	if err := validateSnapshot(candidate); err != nil {
		return Snapshot{}, err
	}

	return candidate, nil
}

func validateSnapshot(snapshot Snapshot) error {
	if snapshot.generatedAtNanos <= 0 {
		return fmt.Errorf("peer reputation snapshot time is invalid")
	}
	if !finitePositive(snapshot.neutralReliability) || snapshot.neutralReliability > 0.5 {
		return fmt.Errorf("peer reputation snapshot neutral reliability is invalid")
	}
	if len(snapshot.peers) != len(snapshot.byIdentity) {
		return fmt.Errorf("peer reputation snapshot index is invalid")
	}
	for index, reputation := range snapshot.peers {
		if err := validateSnapshotPeer(reputation, snapshot.generatedAtNanos); err != nil {
			return err
		}
		if indexed, found := snapshot.byIdentity[reputation.Peer]; !found || indexed != reputation {
			return fmt.Errorf("peer reputation snapshot index entry is invalid")
		}
		if index > 0 && snapshot.peers[index-1].Peer >= reputation.Peer {
			return fmt.Errorf("peer reputation snapshot order is invalid")
		}
	}

	return nil
}

func validateSnapshotPeer(reputation PeerReputation, generatedAtNanos int64) error {
	if !reputation.Known {
		return fmt.Errorf("peer reputation snapshot contains an unknown peer")
	}
	if err := validateBoundedLabel(
		string(reputation.Peer),
		"signed peer identity",
	); err != nil {
		return err
	}
	if err := validateBoundedLabel(string(reputation.NetworkGroup), "network group"); err != nil {
		return err
	}
	if !finite(reputation.Reliability) ||
		reputation.Reliability < 0 || reputation.Reliability > 1 {
		return fmt.Errorf("peer reputation snapshot calibration is invalid")
	}
	if !finite(reputation.FusionWeight) ||
		reputation.FusionWeight < minimumFusionWeight ||
		reputation.FusionWeight > maximumFusionWeight {
		return fmt.Errorf("peer reputation snapshot calibration is invalid")
	}
	if !finite(reputation.Confidence) ||
		reputation.Confidence < 0 || reputation.Confidence > 1 {
		return fmt.Errorf("peer reputation snapshot calibration is invalid")
	}
	if !finite(reputation.SuccessEvidence) || reputation.SuccessEvidence < 0 ||
		!finite(reputation.FailureEvidence) || reputation.FailureEvidence < 0 ||
		reputation.SuccessEvidence+reputation.FailureEvidence > maximumEvidence {
		return fmt.Errorf("peer reputation snapshot evidence is invalid")
	}
	if reputation.LastObservedUnixNano <= 0 || reputation.LastObservedUnixNano > generatedAtNanos {
		return fmt.Errorf("peer reputation snapshot observation time is invalid")
	}

	return nil
}

func (snapshot Snapshot) neutral() float64 {
	if finitePositive(snapshot.neutralReliability) && snapshot.neutralReliability <= 0.5 {
		return snapshot.neutralReliability
	}

	return 0.5
}
