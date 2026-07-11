package peerreputation

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/D4rk4/yago/yagonode/internal/boltvault"
	"github.com/D4rk4/yago/yagonode/internal/memvault"
)

func TestLedgerCalibrationDecayClampingAndIdempotency(t *testing.T) {
	t.Parallel()
	configuration := Configuration{
		HalfLife:     time.Hour,
		PriorSuccess: 2,
		PriorFailure: 2,
		MaximumPeers: 8,
	}
	ledger := openMemoryLedger(t, configuration)
	base := time.Unix(1_800_000_000, 0).UTC()
	batch := ObservationBatch{
		Sequence: 1,
		Observations: []Observation{
			{Peer: "good", NetworkGroup: "group-a", Outcome: OutcomeSuccess, ObservedAt: base},
			{Peer: "bad", NetworkGroup: "group-b", Outcome: OutcomeFailure, ObservedAt: base},
			{Peer: "bad", NetworkGroup: "group-b", Outcome: OutcomeTimeout, ObservedAt: base},
			{Peer: "bad", NetworkGroup: "group-b", Outcome: OutcomeInvalidResult, ObservedAt: base},
		},
	}
	assertIdempotentBatch(t, ledger, batch)
	assertMonotonicReplay(t, ledger, batch, base)
	assertSnapshotCalibration(t, ledger, base)
}

func assertIdempotentBatch(
	t *testing.T,
	ledger *ReputationLedger,
	batch ObservationBatch,
) {
	t.Helper()
	application, err := ledger.ObserveBatch(context.Background(), batch)
	if err != nil {
		t.Fatal(err)
	}
	if !application.Applied || application.Superseded || application.LastSequence != 1 ||
		application.RetainedPeers != 2 {
		t.Fatalf("unexpected application: %+v", application)
	}

	reordered := batch
	reordered.Observations = slices.Clone(batch.Observations)
	slices.Reverse(reordered.Observations)
	retry, err := ledger.ObserveBatch(context.Background(), reordered)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Applied || retry.Superseded || retry.LastSequence != 1 || retry.RetainedPeers != 2 {
		t.Fatalf("unexpected retry: %+v", retry)
	}

	conflict := batch
	conflict.Observations = slices.Clone(batch.Observations)
	conflict.Observations[0].Outcome = OutcomeFailure
	_, err = ledger.ObserveBatch(context.Background(), conflict)
	if !errors.Is(err, ErrBatchSequenceConflict) {
		t.Fatalf("expected sequence conflict, got %v", err)
	}
}

func assertMonotonicReplay(
	t *testing.T,
	ledger *ReputationLedger,
	batch ObservationBatch,
	base time.Time,
) {
	t.Helper()
	stale := batch
	stale.Sequence = 2
	stale.Observations = []Observation{{
		Peer:         "good",
		NetworkGroup: "stale-group",
		Outcome:      OutcomeFailure,
		ObservedAt:   base.Add(-time.Hour),
	}}
	if _, err := ledger.ObserveBatch(context.Background(), stale); err != nil {
		t.Fatal(err)
	}
	superseded := batch
	superseded.Sequence = 1
	old, err := ledger.ObserveBatch(context.Background(), superseded)
	if err != nil {
		t.Fatal(err)
	}
	if old.Applied || !old.Superseded || old.LastSequence != 2 {
		t.Fatalf("unexpected superseded application: %+v", old)
	}
}

func assertSnapshotCalibration(t *testing.T, ledger *ReputationLedger, base time.Time) {
	t.Helper()
	snapshot, err := ledger.Snapshot(context.Background(), base.Add(-2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.GeneratedAt().Equal(base) {
		t.Fatalf("generated at %v", snapshot.GeneratedAt())
	}
	good := snapshot.Peer("good")
	bad := snapshot.Peer("bad")
	unknown := snapshot.Peer("unknown")
	if good.NetworkGroup != "group-a" || good.SuccessEvidence != 1 || good.FailureEvidence != 1 {
		t.Fatalf("unexpected monotonic-clamped peer: %+v", good)
	}
	if bad.SuccessEvidence != 0 || bad.FailureEvidence != 3.5 {
		t.Fatalf("unexpected weighted outcomes: %+v", bad)
	}
	if good.FusionWeight != 1 || !(bad.FusionWeight < 1) || !(bad.Confidence > good.Confidence) {
		t.Fatalf("unexpected calibration: good=%+v bad=%+v", good, bad)
	}
	if unknown.Known || unknown.Confidence != 0 ||
		unknown.FusionWeight != 1 || unknown.Reliability != 0.5 {
		t.Fatalf("unexpected unknown peer: %+v", unknown)
	}

	decayed, err := ledger.Snapshot(context.Background(), base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	decayedBad := decayed.Peer("bad")
	if decayedBad.FailureEvidence != 1.75 || !(decayedBad.Confidence < bad.Confidence) ||
		!(decayedBad.FusionWeight > bad.FusionWeight) {
		t.Fatalf("unexpected decay: before=%+v after=%+v", bad, decayedBad)
	}
	for _, peer := range decayed.Peers() {
		if !finite(peer.Reliability) || !finite(peer.FusionWeight) || !finite(peer.Confidence) ||
			peer.Reliability < 0 || peer.Reliability > 1 ||
			peer.FusionWeight < minimumFusionWeight || peer.FusionWeight > maximumFusionWeight ||
			peer.Confidence < 0 || peer.Confidence > 1 {
			t.Fatalf("unbounded peer: %+v", peer)
		}
	}
	peers := decayed.Peers()
	peers[0].FusionWeight = 99
	if decayed.Peers()[0].FusionWeight == 99 {
		t.Fatal("snapshot peers leaked mutable storage")
	}
}

func TestLedgerPersistenceReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "peer-reputation.db")
	configuration := DefaultConfiguration()
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := Open(storage, configuration)
	if err != nil {
		t.Fatal(err)
	}
	batch := ObservationBatch{
		Sequence: 7,
		Observations: []Observation{{
			Peer:         "persistent-peer",
			NetworkGroup: "198.51.100.0/24",
			Outcome:      OutcomeSuccess,
			ObservedAt:   time.Unix(1_800_000_000, 0).UTC(),
		}},
	}
	if _, err := ledger.ObserveBatch(context.Background(), batch); err != nil {
		t.Fatal(err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	ledger, err = Open(reopened, configuration)
	if err != nil {
		t.Fatal(err)
	}
	lastSequence, err := ledger.LastBatchSequence(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if lastSequence != 7 {
		t.Fatalf("last batch sequence = %d", lastSequence)
	}
	retry, err := ledger.ObserveBatch(context.Background(), batch)
	if err != nil {
		t.Fatal(err)
	}
	if retry.Applied || retry.LastSequence != 7 || retry.RetainedPeers != 1 {
		t.Fatalf("unexpected persisted retry: %+v", retry)
	}
	snapshot, err := ledger.Snapshot(context.Background(), time.Unix(1_800_000_000, 0).UTC())
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Peer("persistent-peer").Known {
		t.Fatal("persisted peer was not reopened")
	}
}

func TestOpenRejectsConfigurationMismatchAndInvalidConfiguration(t *testing.T) {
	t.Parallel()
	invalid := []Configuration{
		{HalfLife: 0, PriorSuccess: 1, PriorFailure: 1, MaximumPeers: 1},
		{HalfLife: time.Hour, PriorSuccess: math.NaN(), PriorFailure: 1, MaximumPeers: 1},
		{
			HalfLife: time.Hour, PriorSuccess: maximumPriorEvidence + 1,
			PriorFailure: maximumPriorEvidence + 1, MaximumPeers: 1,
		},
		{HalfLife: time.Hour, PriorSuccess: 1, PriorFailure: math.Inf(1), MaximumPeers: 1},
		{
			HalfLife: time.Hour, PriorSuccess: 1,
			PriorFailure: maximumPriorEvidence + 1, MaximumPeers: 1,
		},
		{HalfLife: time.Hour, PriorSuccess: 2, PriorFailure: 1, MaximumPeers: 1},
		{HalfLife: time.Hour, PriorSuccess: 1, PriorFailure: 1, MaximumPeers: 0},
		{
			HalfLife: time.Hour, PriorSuccess: 1, PriorFailure: 1,
			MaximumPeers: maximumPeersLimit + 1,
		},
	}
	for _, configuration := range invalid {
		storage, err := memvault.Open(0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Open(storage, configuration); err == nil {
			t.Fatalf("accepted invalid configuration: %+v", configuration)
		}
		_ = storage.Close()
	}
	if _, err := Open(nil, DefaultConfiguration()); err == nil {
		t.Fatal("accepted nil vault")
	}

	path := filepath.Join(t.TempDir(), "configuration.db")
	storage, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(storage, DefaultConfiguration()); err != nil {
		t.Fatal(err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := boltvault.Open(path, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	changed := DefaultConfiguration()
	changed.HalfLife++
	if _, err := Open(reopened, changed); err == nil {
		t.Fatal("accepted persisted configuration mismatch")
	}
}

func TestDeterministicTieEviction(t *testing.T) {
	t.Parallel()
	configuration := Configuration{
		HalfLife:     time.Hour,
		PriorSuccess: 2,
		PriorFailure: 2,
		MaximumPeers: 2,
	}
	base := time.Unix(1_800_000_000, 0).UTC()
	orders := [][]SignedPeerIdentity{
		{"c", "a", "b"},
		{"a", "b", "c"},
		{"b", "c", "a"},
	}
	for _, order := range orders {
		ledger := openMemoryLedger(t, configuration)
		observations := make([]Observation, 0, len(order))
		for _, peer := range order {
			observations = append(observations, Observation{
				Peer: peer, NetworkGroup: "same", Outcome: OutcomeSuccess, ObservedAt: base,
			})
		}
		application, err := ledger.ObserveBatch(context.Background(), ObservationBatch{
			Sequence: 1, Observations: observations,
		})
		if err != nil {
			t.Fatal(err)
		}
		if application.RetainedPeers != 2 {
			t.Fatalf("retained %d peers", application.RetainedPeers)
		}
		snapshot, err := ledger.Snapshot(context.Background(), base)
		if err != nil {
			t.Fatal(err)
		}
		if got := []SignedPeerIdentity{
			snapshot.Peers()[0].Peer,
			snapshot.Peers()[1].Peer,
		}; !slices.Equal(
			got,
			[]SignedPeerIdentity{"a", "b"},
		) {
			t.Fatalf("order %v retained %v", order, got)
		}
	}
}

func TestStaleLowConfidenceEviction(t *testing.T) {
	t.Parallel()
	configuration := Configuration{
		HalfLife:     time.Hour,
		PriorSuccess: 2,
		PriorFailure: 2,
		MaximumPeers: 2,
	}
	base := time.Unix(1_800_000_000, 0).UTC()
	ledger := openMemoryLedger(t, configuration)
	highConfidence := make([]Observation, 8, 9)
	for index := range highConfidence {
		highConfidence[index] = Observation{
			Peer: "established", NetworkGroup: "old", Outcome: OutcomeSuccess, ObservedAt: base,
		}
	}
	highConfidence = append(highConfidence, Observation{
		Peer: "stale", NetworkGroup: "old", Outcome: OutcomeSuccess, ObservedAt: base,
	})
	if _, err := ledger.ObserveBatch(context.Background(), ObservationBatch{
		Sequence: 1, Observations: highConfidence,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.ObserveBatch(context.Background(), ObservationBatch{
		Sequence: 2,
		Observations: []Observation{
			{
				Peer:         "fresh",
				NetworkGroup: "new",
				Outcome:      OutcomeSuccess,
				ObservedAt:   base.Add(4 * time.Hour),
			},
		},
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, err := ledger.Snapshot(context.Background(), base.Add(4*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Peer("established").Known || !snapshot.Peer("fresh").Known ||
		snapshot.Peer("stale").Known {
		t.Fatalf("unexpected confidence eviction: %+v", snapshot.Peers())
	}
}

func openMemoryLedger(t *testing.T, configuration Configuration) *ReputationLedger {
	t.Helper()
	storage, err := memvault.Open(0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	ledger, err := Open(storage, configuration)
	if err != nil {
		t.Fatal(err)
	}

	return ledger
}
