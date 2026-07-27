package contentcluster

import (
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func thresholdLimits() Limits {
	limits := DefaultLimits()
	limits.MinimumJaccard = 0.5

	return limits
}

func thresholdQuery(shingles ...uint64) preparedEvidence {
	return preparedEvidence{
		URL:         "https://threshold-query.example",
		ContentHash: "query",
		Shingles:    shingles,
	}
}

func storedCandidateOverlap(
	t *testing.T,
	index *Index,
	prepared preparedEvidence,
	url string,
) (candidateMatch, bool) {
	t.Helper()

	var match candidateMatch
	var eligible bool
	if err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
		var err error
		match, eligible, err = index.candidate(tx, t.Context(), prepared, url, false)

		return err
	}); err != nil {
		t.Fatalf("read stored candidate: %v", err)
	}

	return match, eligible
}

func plannedCandidateOverlap(
	t *testing.T,
	index *Index,
	prepared preparedEvidence,
	planned replacementBatchProjection,
) candidateSelection {
	t.Helper()

	var selection candidateSelection
	if err := index.vault.View(t.Context(), func(tx *vault.Txn) error {
		var err error
		selection, err = index.bestPlannedNearCandidate(
			tx,
			t.Context(),
			prepared,
			planned,
			candidateSelection{},
		)

		return err
	}); err != nil {
		t.Fatalf("read planned near candidate: %v", err)
	}

	return selection
}

func thresholdStoredEvidence(t *testing.T, engine *clusterFaultEngine) fingerprintRecord {
	t.Helper()

	stored := fingerprintRecord{
		URL:         "https://threshold-stored.example",
		ContentHash: "stored",
		ClusterID:   "threshold-cluster",
		Shingles:    []uint64{1},
	}
	putRawFingerprint(t, engine, stored)
	putRawCluster(t, engine, clusterRecord{
		ID:      stored.ClusterID,
		Members: []string{stored.URL},
	})

	return stored
}

// TestStoredCandidateAdmitsOverlapExactlyAtMinimumJaccard pairs the near-duplicate
// admission threshold against stored evidence: an overlap that exactly meets the
// configured minimum is a duplicate and joins the cluster, while an overlap one
// shingle short of it stays apart. The refusing side is pinned for clearly
// dissimilar evidence only, so reading the threshold as "more than" instead of
// "at least" would silently split every pair sitting on the operator's configured
// line while every existing test kept passing.
func TestStoredCandidateAdmitsOverlapExactlyAtMinimumJaccard(t *testing.T) {
	limits := thresholdLimits()
	index, engine := openFaultIndex(t, limits)
	stored := thresholdStoredEvidence(t, engine)

	atThreshold, eligible := storedCandidateOverlap(
		t,
		index,
		thresholdQuery(1, 2),
		stored.URL,
	)
	if !eligible {
		t.Fatal("overlap exactly at the minimum Jaccard was refused")
	}
	if atThreshold.similarity != limits.MinimumJaccard {
		t.Fatalf("similarity = %v, want %v", atThreshold.similarity, limits.MinimumJaccard)
	}

	_, eligible = storedCandidateOverlap(t, index, thresholdQuery(1, 2, 3), stored.URL)
	if eligible {
		t.Fatal("overlap below the minimum Jaccard was admitted")
	}
}

// TestPlannedCandidateAdmitsOverlapExactlyAtMinimumJaccard holds the same
// threshold for evidence that is still only planned inside the current batch. The
// two admission points carry the same rule and must not drift apart: a batch that
// clusters its own members on a different line than the stored index would give a
// URL a different cluster depending on whether its duplicate arrived in the same
// batch or the one before.
func TestPlannedCandidateAdmitsOverlapExactlyAtMinimumJaccard(t *testing.T) {
	limits := thresholdLimits()
	index, _ := openFaultIndex(t, limits)
	planned := replacementBatchProjection{
		current: []fingerprintRecord{{
			URL:         "https://threshold-planned.example",
			ContentHash: "planned",
			ClusterID:   "planned-cluster",
			Shingles:    []uint64{1},
		}},
	}

	atThreshold := plannedCandidateOverlap(t, index, thresholdQuery(1, 2), planned)
	if !atThreshold.found {
		t.Fatal("planned overlap exactly at the minimum Jaccard was refused")
	}
	if atThreshold.candidate.similarity != limits.MinimumJaccard {
		t.Fatalf(
			"planned similarity = %v, want %v",
			atThreshold.candidate.similarity,
			limits.MinimumJaccard,
		)
	}

	if below := plannedCandidateOverlap(
		t,
		index,
		thresholdQuery(1, 2, 3),
		planned,
	); below.found {
		t.Fatal("planned overlap below the minimum Jaccard was admitted")
	}
}
