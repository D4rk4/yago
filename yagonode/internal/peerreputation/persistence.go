package peerreputation

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

const (
	recordVersion = 1
	recordBucket  = vault.Name("peer_reputation")
	peerKeyPrefix = "peer/"
)

var stateKey = vault.Key("state")

type ledgerState struct {
	Configuration        Configuration `json:"configuration"`
	LastBatchSequence    uint64        `json:"last_batch_sequence"`
	LastBatchFingerprint string        `json:"last_batch_fingerprint"`
}

type peerRecord struct {
	Peer                 SignedPeerIdentity `json:"signed_peer_identity"`
	NetworkGroup         NetworkGroupKey    `json:"network_group"`
	SuccessEvidence      float64            `json:"success_evidence"`
	FailureEvidence      float64            `json:"failure_evidence"`
	LastObservedUnixNano int64              `json:"last_observed_unix_nano"`
}

type persistentRecord struct {
	Version int          `json:"version"`
	State   *ledgerState `json:"state,omitempty"`
	Peer    *peerRecord  `json:"peer,omitempty"`
}

type recordCodec struct{}

func (recordCodec) Encode(record persistentRecord) ([]byte, error) {
	if err := validatePersistentRecord(record); err != nil {
		return nil, err
	}
	encoded, _ := json.Marshal(record)

	return encoded, nil
}

func (recordCodec) Decode(raw []byte) (persistentRecord, error) {
	var record persistentRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		return persistentRecord{}, fmt.Errorf("decode peer reputation record: %w", err)
	}
	if err := validatePersistentRecord(record); err != nil {
		return persistentRecord{}, err
	}

	return record, nil
}

func validatePersistentRecord(record persistentRecord) error {
	if record.Version != recordVersion {
		return fmt.Errorf("peer reputation record version %d is unsupported", record.Version)
	}
	if (record.State == nil) == (record.Peer == nil) {
		return fmt.Errorf("peer reputation record kind is invalid")
	}
	if record.State != nil {
		return validateLedgerState(*record.State)
	}

	return validatePeerRecord(*record.Peer)
}

func validateLedgerState(state ledgerState) error {
	if err := validateConfiguration(state.Configuration); err != nil {
		return err
	}
	if state.LastBatchSequence == 0 {
		if state.LastBatchFingerprint != "" {
			return fmt.Errorf("peer reputation initial batch fingerprint is invalid")
		}

		return nil
	}
	decoded, err := hex.DecodeString(state.LastBatchFingerprint)
	if err != nil || len(decoded) != sha256FingerprintBytes {
		return fmt.Errorf("peer reputation batch fingerprint is invalid")
	}

	return nil
}

func validatePeerRecord(record peerRecord) error {
	if err := validateBoundedLabel(
		string(record.Peer),
		"signed peer identity",
	); err != nil {
		return err
	}
	if err := validateBoundedLabel(string(record.NetworkGroup), "network group"); err != nil {
		return err
	}
	if !finite(record.SuccessEvidence) || record.SuccessEvidence < 0 ||
		!finite(record.FailureEvidence) || record.FailureEvidence < 0 ||
		record.SuccessEvidence+record.FailureEvidence > maximumEvidence {
		return fmt.Errorf("peer reputation evidence is invalid")
	}
	if record.LastObservedUnixNano <= 0 {
		return fmt.Errorf("peer reputation last observation time is invalid")
	}

	return nil
}

func stateRecord(state ledgerState) persistentRecord {
	return persistentRecord{Version: recordVersion, State: &state}
}

func peerEntry(record peerRecord) persistentRecord {
	return persistentRecord{Version: recordVersion, Peer: &record}
}

func peerKey(identity SignedPeerIdentity) vault.Key {
	return vault.Key(peerKeyPrefix + string(identity))
}

const sha256FingerprintBytes = 32
