package contentcluster

import (
	"context"
	"errors"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestPlanReplacementsReportsContextAndFingerprintFailures(t *testing.T) {
	t.Run("context", func(t *testing.T) {
		index, _ := openFaultIndex(t, Limits{})
		prepared, err := prepareEvidence(t.Context(), index.limits, Evidence{
			URL:         "https://plan-context.example",
			ContentHash: "hash",
		})
		if err != nil {
			t.Fatal(err)
		}
		ctx := &stagedCancellationContext{Context: context.Background(), cancelAt: 3}
		if _, _, err := index.planReplacements(
			ctx,
			[]preparedEvidence{prepared},
			nil,
		); !errors.Is(err, context.Canceled) {
			t.Fatalf("replacement planning context error = %v", err)
		}
	})
	t.Run("fingerprint read", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		input := Evidence{URL: "https://plan-fingerprint.example", ContentHash: "hash"}
		prepared, err := prepareEvidence(t.Context(), index.limits, input)
		if err != nil {
			t.Fatal(err)
		}
		engine.putRaw(fingerprintBucketName, vault.Key(input.URL), []byte("1"))
		if _, _, err := index.planReplacements(
			t.Context(),
			[]preparedEvidence{prepared},
			nil,
		); err == nil {
			t.Fatal("corrupt replacement fingerprint succeeded")
		}
	})
}

func TestPlannedExactCandidatesCoverErrorsAndCapacity(t *testing.T) {
	limits := DefaultLimits()
	limits.ShingleWords = 1
	prepared, err := prepareEvidence(t.Context(), limits, Evidence{
		URL:         "https://planned-query.example",
		ContentHash: "exact",
		Text:        "one two three four",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("exact cluster read", func(t *testing.T) {
		index, engine := openFaultIndex(t, limits)
		engine.putRaw(clusterBucketName, vault.Key("broken"), []byte("{"))
		record := fingerprintRecord{
			URL:         "https://planned-exact.example",
			ContentHash: prepared.ContentHash,
			ClusterID:   "broken",
		}
		err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
			_, err := index.bestPlannedExactCandidate(
				tx,
				t.Context(),
				prepared,
				[]fingerprintRecord{record},
				candidateSelection{},
			)

			return err
		})
		if err == nil {
			t.Fatal("corrupt exact planned cluster succeeded")
		}
	})
	t.Run("exact capacity", func(t *testing.T) {
		bounded := limits
		bounded.MaximumClusterMembers = 1
		index, engine := openFaultIndex(t, bounded)
		member := fingerprintRecord{URL: "https://full-member.example", ClusterID: "full"}
		putRawFingerprint(t, engine, member)
		putRawCluster(t, engine, clusterRecord{ID: "full", Members: []string{member.URL}})
		record := fingerprintRecord{
			URL:         "https://planned-full.example",
			ContentHash: prepared.ContentHash,
			ClusterID:   "full",
		}
		err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
			selection, err := index.bestPlannedExactCandidate(
				tx,
				t.Context(),
				prepared,
				[]fingerprintRecord{record},
				candidateSelection{},
			)
			if selection.found {
				t.Fatal("full planned exact cluster accepted a candidate")
			}

			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	})
}

func TestPlannedNearCandidateReportsClusterFailure(t *testing.T) {
	limits, prepared := preparedPlannedCandidate(t)
	index, engine := openFaultIndex(t, limits)
	broken := fingerprintRecord{
		URL:       "https://planned-near-broken.example",
		ClusterID: "broken",
		Shingles:  prepared.Shingles,
	}
	engine.putRaw(clusterBucketName, vault.Key("broken"), []byte("{"))
	err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, err := index.bestPlannedNearCandidate(
			tx,
			t.Context(),
			prepared,
			[]fingerprintRecord{broken},
			candidateSelection{},
		)

		return err
	})
	if err == nil {
		t.Fatal("corrupt near planned cluster succeeded")
	}
}

func TestPlannedNearCandidateSelectsSimilarEvidence(t *testing.T) {
	limits, prepared := preparedPlannedCandidate(t)
	index, _ := openFaultIndex(t, limits)
	near := fingerprintRecord{
		URL:         "https://planned-near.example",
		ContentHash: "near",
		ClusterID:   "near-cluster",
		Fingerprint: prepared.Fingerprint,
		Shingles:    prepared.Shingles,
	}
	distant := fingerprintRecord{
		URL:         "https://planned-distant.example",
		ContentHash: "distant",
		ClusterID:   "distant-cluster",
		Shingles:    []uint64{999},
	}
	err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
		selection, err := index.bestPlannedNearCandidate(
			tx,
			t.Context(),
			prepared,
			[]fingerprintRecord{{URL: prepared.URL}, distant, near},
			candidateSelection{},
		)
		if !selection.found || selection.candidate.record.URL != near.URL {
			t.Fatalf("near planned selection = %#v", selection)
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPlannedNearCandidateRespectsClusterCapacity(t *testing.T) {
	limits, prepared := preparedPlannedCandidate(t)
	limits.MaximumClusterMembers = 1
	index, engine := openFaultIndex(t, limits)
	member := fingerprintRecord{URL: "https://near-full-member.example", ClusterID: "full"}
	putRawFingerprint(t, engine, member)
	putRawCluster(t, engine, clusterRecord{ID: "full", Members: []string{member.URL}})
	record := fingerprintRecord{
		URL:         "https://planned-near-full.example",
		ContentHash: "near",
		ClusterID:   "full",
		Fingerprint: prepared.Fingerprint,
		Shingles:    prepared.Shingles,
	}
	err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
		selection, err := index.bestPlannedNearCandidate(
			tx,
			t.Context(),
			prepared,
			[]fingerprintRecord{record},
			candidateSelection{},
		)
		if selection.found {
			t.Fatal("full planned near cluster accepted a candidate")
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func preparedPlannedCandidate(t *testing.T) (Limits, preparedEvidence) {
	t.Helper()
	limits := DefaultLimits()
	limits.ShingleWords = 1
	prepared, err := prepareEvidence(t.Context(), limits, Evidence{
		URL:         "https://planned-query.example",
		ContentHash: "exact",
		Text:        "one two three four",
	})
	if err != nil {
		t.Fatal(err)
	}

	return limits, prepared
}

func TestPlannedClusterAvailabilityReportsCorruptionAndExistingMember(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	engine.putRaw(clusterBucketName, vault.Key("broken"), []byte("{"))
	err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
		_, err := index.plannedClusterAvailable(
			tx,
			t.Context(),
			"broken",
			"https://member.example",
			nil,
		)

		return err
	})
	if err == nil {
		t.Fatal("corrupt planned cluster succeeded")
	}
	index, _ = openFaultIndex(t, Limits{})
	err = index.vault.View(t.Context(), func(tx *vault.Txn) error {
		available, err := index.plannedClusterAvailable(
			tx,
			t.Context(),
			"planned",
			"https://member.example",
			[]fingerprintRecord{{
				URL:       "https://member.example",
				ClusterID: "planned",
			}},
		)
		if !available {
			t.Fatal("existing planned member was unavailable")
		}

		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCompleteReplacementOutputsReportsTransitionAndAssignmentFailures(t *testing.T) {
	url := "https://complete-output.example"
	output := EvidenceReplacement{Finalization: EvidenceFinalization{url: url, token: "token"}}
	t.Run("transition read", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		engine.putRaw(fingerprintBucketName, transitionKey(url), []byte("1"))
		if err := index.completeReplacementOutputs(
			t.Context(),
			[]EvidenceReplacement{output},
		); err == nil {
			t.Fatal("corrupt completion transition succeeded")
		}
	})
	t.Run("transition changed", func(t *testing.T) {
		index, _ := openFaultIndex(t, Limits{})
		if err := index.completeReplacementOutputs(
			t.Context(),
			[]EvidenceReplacement{output},
		); err == nil {
			t.Fatal("missing completion transition succeeded")
		}
	})
	t.Run("assignment", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		putRawTransition(t, engine, fingerprintTransition{
			Token:        "token",
			URL:          url,
			Current:      fingerprintRecord{URL: url, ClusterID: "missing"},
			CurrentFound: true,
		})
		if err := index.completeReplacementOutputs(
			t.Context(),
			[]EvidenceReplacement{output},
		); err == nil {
			t.Fatal("missing completion assignment succeeded")
		}
	})
}

func TestPlannedClusterIDPropagatesExactAndNearCandidateFailures(t *testing.T) {
	limits := DefaultLimits()
	limits.ShingleWords = 1
	prepared, err := prepareEvidence(t.Context(), limits, Evidence{
		URL:         "https://planned-cluster-id.example",
		ContentHash: "exact",
		Text:        "one two three four",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("exact", func(t *testing.T) {
		index, engine := openFaultIndex(t, limits)
		engine.putRaw(clusterBucketName, vault.Key("broken"), []byte("{"))
		err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
			_, err := index.plannedClusterID(
				tx,
				t.Context(),
				prepared,
				[]fingerprintRecord{{
					URL:         "https://planned-exact-id.example",
					ContentHash: prepared.ContentHash,
					ClusterID:   "broken",
				}},
			)

			return err
		})
		if err == nil {
			t.Fatal("corrupt exact planned cluster ID succeeded")
		}
	})
	t.Run("near", func(t *testing.T) {
		index, engine := openFaultIndex(t, limits)
		engine.putRaw(clusterBucketName, vault.Key("broken"), []byte("{"))
		err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
			_, err := index.plannedClusterID(
				tx,
				t.Context(),
				prepared,
				[]fingerprintRecord{{
					URL:         "https://planned-near-id.example",
					ContentHash: "near",
					ClusterID:   "broken",
					Fingerprint: prepared.Fingerprint,
					Shingles:    prepared.Shingles,
				}},
			)

			return err
		})
		if err == nil {
			t.Fatal("corrupt near planned cluster ID succeeded")
		}
	})
}

func TestPlannedClusterIDChecksAvailabilityContextAfterMatch(t *testing.T) {
	for cancelAt := 2; cancelAt <= 40; cancelAt++ {
		limits := DefaultLimits()
		index, engine := openFaultIndex(t, limits)
		input := Evidence{
			URL:         "https://planned-context-query.example",
			ContentHash: "hash",
		}
		prepared, err := prepareEvidence(t.Context(), limits, input)
		if err != nil {
			t.Fatal(err)
		}
		candidate := fingerprintRecord{
			URL:         "https://planned-context-candidate.example",
			ContentHash: input.ContentHash,
			ClusterID:   "cluster",
		}
		putRawFingerprint(t, engine, candidate)
		putRawCluster(t, engine, clusterRecord{
			ID:      candidate.ClusterID,
			Members: []string{candidate.URL},
		})
		putRawPosting(t, engine, vault.Key(input.ContentHash), postingRecord{
			URLs: []string{candidate.URL},
		})
		ctx := &stagedCancellationContext{
			Context:  context.Background(),
			cancelAt: cancelAt,
		}
		_ = index.vault.View(t.Context(), func(tx *vault.Txn) error {
			_, err := index.plannedClusterID(tx, ctx, prepared, nil)

			return err
		})
	}
}
