package contentcluster

import (
	"slices"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestExecuteDeletionAttemptRetriesReconcileConflict(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	url := "https://deletion-reconcile.example"
	putRawFingerprint(t, engine, fingerprintRecord{URL: url, ContentHash: "unexpected"})
	putRawTransition(t, engine, fingerprintTransition{
		Token:        "token",
		URL:          url,
		Current:      fingerprintRecord{URL: url, ContentHash: "current", ClusterID: "cluster"},
		CurrentFound: true,
	})
	leases, err := index.acquireReplacementLeases(t.Context(), []string{url})
	if err != nil {
		t.Fatal(err)
	}
	defer leases.close()
	_, _, retry, err := index.executeDeletionAttempt(t.Context(), url, leases)
	if err != nil || !retry {
		t.Fatalf("deletion reconcile conflict = %v/%v", retry, err)
	}
}

func TestExecuteDeletionAttemptExpandsAffectedClusterLease(t *testing.T) {
	index, engine := openFaultIndex(t, Limits{})
	record := fingerprintRecord{
		URL:         "https://deletion-expansion.example",
		ContentHash: "hash",
		ClusterID:   "cluster",
	}
	putRawFingerprint(t, engine, record)
	putRawCluster(t, engine, clusterRecord{ID: record.ClusterID, Members: []string{record.URL}})
	urlLease, err := index.boundaries.acquireLease(t.Context(), []string{record.URL})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := index.projections.acquireLease(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	leases := &replacementLeases{url: urlLease, projection: projection}
	defer leases.close()
	_, _, retry, err := index.executeDeletionAttempt(t.Context(), record.URL, leases)
	if err != nil || !retry {
		t.Fatalf("deletion lease expansion = %v/%v", retry, err)
	}
}

func TestPlanDeletionReportsFingerprintReadAndCarriesPriorClusters(t *testing.T) {
	t.Run("fingerprint read", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		url := "https://deletion-plan-read.example"
		engine.putRaw(fingerprintBucketName, vault.Key(url), []byte("1"))
		if _, _, err := index.planDeletion(t.Context(), url, nil); err == nil {
			t.Fatal("corrupt deletion fingerprint succeeded")
		}
	})
	t.Run("prior clusters", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		record := fingerprintRecord{
			URL:         "https://deletion-prior.example",
			ContentHash: "hash",
			ClusterID:   "current-cluster",
		}
		putRawFingerprint(t, engine, record)
		putRawCluster(t, engine, clusterRecord{ID: record.ClusterID, Members: []string{record.URL}})
		transition, found, err := index.planDeletion(
			t.Context(),
			record.URL,
			[]fingerprintTransition{{AffectedClusterIDs: []string{"prior-cluster"}}},
		)
		if err != nil || !found {
			t.Fatalf("planned deletion = %#v/%v/%v", transition, found, err)
		}
		want := []string{"current-cluster", "prior-cluster"}
		if !slices.Equal(transition.AffectedClusterIDs, want) {
			t.Fatalf("affected clusters = %v, want %v", transition.AffectedClusterIDs, want)
		}
	})
}
