package peerreputation

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestObservationValidationAndCanonicalization(t *testing.T) {
	t.Parallel()
	base := Observation{
		Peer: "peer", NetworkGroup: "group", Outcome: OutcomeSuccess, ObservedAt: time.Unix(10, 0),
	}
	emptyPeer := mutateObservation(base, func(value *Observation) { value.Peer = "" })
	longPeer := mutateObservation(base, func(value *Observation) {
		value.Peer = SignedPeerIdentity(strings.Repeat("p", maximumPeerLabelBytes+1))
	})
	spacedPeer := mutateObservation(base, func(value *Observation) { value.Peer = " peer" })
	invalidUTF8Peer := mutateObservation(base, func(value *Observation) {
		value.Peer = SignedPeerIdentity(string([]byte{0xff}))
	})
	emptyGroup := mutateObservation(base, func(value *Observation) { value.NetworkGroup = "" })
	invalidOutcome := mutateObservation(base, func(value *Observation) { value.Outcome = 0 })
	zeroTime := mutateObservation(base, func(value *Observation) { value.ObservedAt = time.Time{} })
	overflowTime := mutateObservation(base, func(value *Observation) {
		value.ObservedAt = time.Date(2300, 1, 1, 0, 0, 0, 0, time.UTC)
	})
	invalid := []ObservationBatch{
		{Sequence: 0, Observations: []Observation{base}},
		{Sequence: 1},
		{Sequence: 1, Observations: make([]Observation, maximumBatchObservations+1)},
		{Sequence: 1, Observations: []Observation{emptyPeer}},
		{Sequence: 1, Observations: []Observation{longPeer}},
		{Sequence: 1, Observations: []Observation{spacedPeer}},
		{Sequence: 1, Observations: []Observation{invalidUTF8Peer}},
		{Sequence: 1, Observations: []Observation{emptyGroup}},
		{Sequence: 1, Observations: []Observation{invalidOutcome}},
		{Sequence: 1, Observations: []Observation{zeroTime}},
		{Sequence: 1, Observations: []Observation{overflowTime}},
	}
	for _, batch := range invalid {
		if _, _, err := normalizeBatch(batch); err == nil {
			t.Fatalf("accepted batch %+v", batch)
		}
	}

	observations := []Observation{
		{Peer: "b", NetworkGroup: "b", Outcome: OutcomeFailure, ObservedAt: time.Unix(20, 0)},
		{Peer: "a", NetworkGroup: "b", Outcome: OutcomeFailure, ObservedAt: time.Unix(20, 0)},
		{Peer: "a", NetworkGroup: "a", Outcome: OutcomeFailure, ObservedAt: time.Unix(20, 0)},
		{Peer: "a", NetworkGroup: "a", Outcome: OutcomeSuccess, ObservedAt: time.Unix(20, 0)},
		{Peer: "a", NetworkGroup: "a", Outcome: OutcomeSuccess, ObservedAt: time.Unix(10, 0)},
	}
	batch := ObservationBatch{Sequence: 3, Observations: observations}
	normalized, fingerprint, err := normalizeBatch(batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(fingerprint) != 64 || normalized[0].observedAtNanos != time.Unix(10, 0).UnixNano() ||
		normalized[1].outcome != OutcomeSuccess || normalized[2].networkGroup != "a" ||
		normalized[3].networkGroup != "b" || normalized[4].peer != "b" {
		t.Fatalf("unexpected canonical observations: %+v", normalized)
	}
	if got := appendCanonicalString(nil, "abc"); string(got) != "3:abc" {
		t.Fatalf("unexpected canonical string: %x", got)
	}
	for _, outcome := range []Outcome{OutcomeSuccess, OutcomeFailure, OutcomeTimeout, OutcomeInvalidResult} {
		if _, _, ok := outcomeEvidence(outcome); !ok {
			t.Fatalf("valid outcome %d rejected", outcome)
		}
	}
}

func TestPersistentRecordCodecRoundTrip(t *testing.T) {
	t.Parallel()
	configuration := DefaultConfiguration()
	validState := stateRecord(ledgerState{Configuration: configuration})
	encoded, err := (recordCodec{}).Encode(validState)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (recordCodec{}).Decode(encoded); err != nil {
		t.Fatal(err)
	}
	validPeer := peerEntry(peerRecord{
		Peer: "peer", NetworkGroup: "group", SuccessEvidence: 1,
		FailureEvidence: 2, LastObservedUnixNano: 1,
	})
	if _, err := (recordCodec{}).Encode(validPeer); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentRecordValidation(t *testing.T) {
	t.Parallel()
	configuration := DefaultConfiguration()
	validState := stateRecord(ledgerState{Configuration: configuration})
	validPeer := peerEntry(peerRecord{
		Peer: "peer", NetworkGroup: "group", LastObservedUnixNano: 1,
	})
	invalid := []persistentRecord{
		{},
		{Version: recordVersion},
		{Version: recordVersion, State: validState.State, Peer: validPeer.Peer},
		stateRecord(ledgerState{Configuration: configuration, LastBatchFingerprint: "unexpected"}),
		stateRecord(
			ledgerState{
				Configuration:        configuration,
				LastBatchSequence:    1,
				LastBatchFingerprint: "invalid",
			},
		),
		peerEntry(peerRecord{Peer: "", NetworkGroup: "group", LastObservedUnixNano: 1}),
		peerEntry(peerRecord{Peer: "peer", NetworkGroup: "", LastObservedUnixNano: 1}),
		peerEntry(
			peerRecord{
				Peer:                 "peer",
				NetworkGroup:         "group",
				SuccessEvidence:      math.NaN(),
				LastObservedUnixNano: 1,
			},
		),
		peerEntry(
			peerRecord{
				Peer:                 "peer",
				NetworkGroup:         "group",
				FailureEvidence:      -1,
				LastObservedUnixNano: 1,
			},
		),
		peerEntry(
			peerRecord{
				Peer:                 "peer",
				NetworkGroup:         "group",
				SuccessEvidence:      maximumEvidence,
				FailureEvidence:      1,
				LastObservedUnixNano: 1,
			},
		),
		peerEntry(peerRecord{Peer: "peer", NetworkGroup: "group", LastObservedUnixNano: 0}),
	}
	for _, record := range invalid {
		if _, err := (recordCodec{}).Encode(record); err == nil {
			t.Fatalf("encoded invalid record: %+v", record)
		}
	}
}

func TestPersistentStateDecodeAndFingerprint(t *testing.T) {
	t.Parallel()
	configuration := DefaultConfiguration()
	validState := stateRecord(ledgerState{Configuration: configuration})
	if _, err := (recordCodec{}).Decode([]byte(`{`)); err == nil {
		t.Fatal("decoded malformed record")
	}
	unsupported, _ := json.Marshal(persistentRecord{Version: 99, State: validState.State})
	if _, err := (recordCodec{}).Decode(unsupported); err == nil {
		t.Fatal("decoded unsupported record")
	}
	if err := validateLedgerState(ledgerState{}); err == nil {
		t.Fatal("accepted invalid state configuration")
	}
	fingerprint := strings.Repeat("00", sha256FingerprintBytes)
	if err := validateLedgerState(ledgerState{
		Configuration: configuration, LastBatchSequence: 1, LastBatchFingerprint: fingerprint,
	}); err != nil {
		t.Fatal(err)
	}
	if string(peerKey("identity")) != "peer/identity" {
		t.Fatal("unexpected peer key")
	}
}

func TestEvidenceBoundsAndDecay(t *testing.T) {
	t.Parallel()
	if factor := decayFactor(10, 10, time.Hour); factor != 1 {
		t.Fatalf("same-time decay %v", factor)
	}
	if factor := decayFactor(10, 10+int64(time.Hour), time.Hour); factor != 0.5 {
		t.Fatalf("half-life decay %v", factor)
	}
	success, failure := boundedEvidence(maximumEvidence, maximumEvidence)
	if success != maximumEvidence/2 || failure != maximumEvidence/2 {
		t.Fatalf("unexpected bounded evidence: %v %v", success, failure)
	}
	success, failure = boundedEvidence(1, 2)
	if success != 1 || failure != 2 {
		t.Fatalf("unexpected unchanged evidence: %v %v", success, failure)
	}
	if clamp(-1, 0, 1) != 0 || clamp(2, 0, 1) != 1 || clamp(0.5, 0, 1) != 0.5 {
		t.Fatal("clamp failed")
	}
	peers := map[SignedPeerIdentity]peerRecord{
		"old": {Peer: "old", NetworkGroup: "group", LastObservedUnixNano: 1},
		"new": {Peer: "new", NetworkGroup: "group", LastObservedUnixNano: 2},
	}
	configuration := DefaultConfiguration()
	configuration.MaximumPeers = 1
	retainBoundedPeers(peers, 2, configuration)
	if _, found := peers["old"]; found {
		t.Fatal("stale zero-confidence peer was retained")
	}
}

func mutateObservation(value Observation, mutate func(*Observation)) Observation {
	mutate(&value)

	return value
}
