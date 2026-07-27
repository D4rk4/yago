package peerreputation

import (
	"testing"
	"time"
)

func TestObservationBatchAcceptsItsSizeLimitAndRefusesOneMore(t *testing.T) {
	t.Parallel()
	valid := Observation{
		Peer:         "peer",
		NetworkGroup: "group",
		Outcome:      OutcomeSuccess,
		ObservedAt:   time.Unix(1_800_000_000, 0).UTC(),
	}
	atLimit := make([]Observation, maximumBatchObservations)
	for index := range atLimit {
		atLimit[index] = valid
	}
	if _, _, err := normalizeBatch(ObservationBatch{
		Sequence: 1, Observations: atLimit,
	}); err != nil {
		t.Fatalf("batch of exactly %d observations rejected: %v", len(atLimit), err)
	}
	overLimit := make([]Observation, maximumBatchObservations+1)
	for index := range overLimit {
		overLimit[index] = valid
	}
	if _, _, err := normalizeBatch(ObservationBatch{
		Sequence: 1, Observations: overLimit,
	}); err == nil {
		t.Fatalf("batch of %d individually valid observations accepted", len(overLimit))
	}
}

func TestSaturatedEvidenceStaysPersistableAndSnapshotable(t *testing.T) {
	t.Parallel()
	success, failure := boundedEvidence(maximumEvidence, maximumEvidence)
	saturated := peerRecord{
		Peer:                 "saturated",
		NetworkGroup:         "group",
		SuccessEvidence:      success,
		FailureEvidence:      failure,
		LastObservedUnixNano: 1,
	}
	encoded, err := (recordCodec{}).Encode(peerEntry(saturated))
	if err != nil {
		t.Fatalf("clamped evidence is not persistable: %v", err)
	}
	decoded, err := (recordCodec{}).Decode(encoded)
	if err != nil || decoded.Peer == nil || *decoded.Peer != saturated {
		t.Fatalf("clamped evidence round trip = %+v, %v", decoded.Peer, err)
	}
	if _, err := newSnapshot(2, 0.5, []PeerReputation{{
		Peer:                 "saturated",
		NetworkGroup:         "group",
		Known:                true,
		Reliability:          0.5,
		FusionWeight:         1,
		Confidence:           1,
		SuccessEvidence:      success,
		FailureEvidence:      failure,
		LastObservedUnixNano: 1,
	}}); err != nil {
		t.Fatalf("clamped evidence is not snapshotable: %v", err)
	}
	beyond := saturated
	beyond.SuccessEvidence = success + 1
	if _, err := (recordCodec{}).Encode(peerEntry(beyond)); err == nil {
		t.Fatal("evidence beyond the ceiling was persisted")
	}
}
