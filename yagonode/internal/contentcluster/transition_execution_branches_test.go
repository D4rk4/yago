package contentcluster

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/D4rk4/yago/yagonode/internal/vault"
)

func TestPrepareTransitionProjectionsReportsMixedContextAndProjectionFailures(t *testing.T) {
	record := fingerprintRecord{
		URL:         "https://prepare-transition.example",
		ContentHash: "hash",
		ClusterID:   "cluster",
	}
	transition := fingerprintTransition{
		Token:        "token",
		URL:          record.URL,
		Current:      record,
		CurrentFound: true,
	}
	t.Run("mixed transitions", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		putRawTransition(t, engine, transition)
		if err := index.prepareTransitionProjections(t.Context(), []fingerprintTransition{
			{Token: "delete", URL: "https://prepare-deletion.example"},
			transition,
		}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("context", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		putRawTransition(t, engine, transition)
		ctx := &stagedCancellationContext{Context: context.Background(), cancelAt: 3}
		if err := index.prepareTransitionProjections(
			ctx,
			[]fingerprintTransition{transition},
		); !errors.Is(err, context.Canceled) {
			t.Fatalf("prepare transition context error = %v", err)
		}
	})
	t.Run("posting", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		putRawTransition(t, engine, transition)
		engine.putRaw(exactBucketName, vault.Key(record.ContentHash), []byte("{"))
		if err := index.prepareTransitionProjections(
			t.Context(),
			[]fingerprintTransition{transition},
		); err == nil {
			t.Fatal("corrupt prepared transition posting succeeded")
		}
	})
	t.Run("cluster", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		putRawTransition(t, engine, transition)
		engine.putRaw(clusterBucketName, vault.Key(record.ClusterID), []byte("{"))
		if err := index.prepareTransitionProjections(
			t.Context(),
			[]fingerprintTransition{transition},
		); err == nil {
			t.Fatal("corrupt prepared transition cluster succeeded")
		}
	})
}

func TestCleanTransitionProjectionsReportsContextAndFinalizationFailures(t *testing.T) {
	record := fingerprintRecord{
		URL:         "https://clean-transition.example",
		ContentHash: "hash",
		ClusterID:   "cluster",
	}
	transition := fingerprintTransition{
		Token:        "token",
		URL:          record.URL,
		Current:      record,
		CurrentFound: true,
	}
	t.Run("context", func(t *testing.T) {
		index, _ := openFaultIndex(t, Limits{})
		ctx := &stagedCancellationContext{Context: context.Background(), cancelAt: 3}
		if err := index.cleanTransitionProjections(
			ctx,
			[]fingerprintTransition{transition},
		); !errors.Is(err, context.Canceled) {
			t.Fatalf("clean transition context error = %v", err)
		}
	})
	t.Run("current posting", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		engine.putRaw(exactBucketName, vault.Key(record.ContentHash), []byte("{"))
		if err := index.cleanTransitionProjections(
			t.Context(),
			[]fingerprintTransition{transition},
		); err == nil {
			t.Fatal("corrupt finalized transition posting succeeded")
		}
	})
}

func TestFinalizeEvidenceTransitionsChecksContextInsideUpdate(t *testing.T) {
	for cancelAt := 2; cancelAt <= 6; cancelAt++ {
		index, engine := openFaultIndex(t, Limits{})
		url := "https://finalize-context.example"
		putRawTransition(t, engine, fingerprintTransition{Token: "token", URL: url})
		ctx := &stagedCancellationContext{
			Context:  context.Background(),
			cancelAt: cancelAt,
		}
		_ = index.FinalizeEvidenceTransitions(
			ctx,
			[]EvidenceFinalization{{url: url, token: "token"}},
		)
	}
}

func TestBuildReplacementAttemptReportsTransitionReadAndReconcileFailures(t *testing.T) {
	url := "https://build-replacement.example"
	preparedIndex, _ := openFaultIndex(t, Limits{})
	prepared, err := prepareEvidence(t.Context(), preparedIndex.limits, Evidence{
		URL:         url,
		ContentHash: "current",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("transition read", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		engine.putRaw(fingerprintBucketName, transitionKey(url), []byte("1"))
		if _, err := index.buildReplacementAttempt(
			t.Context(),
			[]preparedEvidence{prepared},
			[]string{url},
			nil,
		); err == nil {
			t.Fatal("corrupt replacement transition succeeded")
		}
	})
	t.Run("transition reconcile", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		putRawFingerprint(t, engine, fingerprintRecord{URL: url, ContentHash: "unexpected"})
		putRawTransition(t, engine, fingerprintTransition{
			Token:        "token",
			URL:          url,
			Current:      fingerprintRecord{URL: url, ContentHash: "current", ClusterID: "cluster"},
			CurrentFound: true,
		})
		if _, err := index.buildReplacementAttempt(
			t.Context(),
			[]preparedEvidence{prepared},
			[]string{url},
			nil,
		); !errors.Is(err, errEvidenceTransitionConflict) {
			t.Fatalf("replacement reconcile error = %v", err)
		}
	})
}

func TestExecuteReplacementAttemptRetriesPlanningAndCommitConflicts(t *testing.T) {
	url := "https://execute-replacement.example"
	t.Run("planning conflict", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		prepared, err := prepareEvidence(t.Context(), index.limits, Evidence{
			URL:         url,
			ContentHash: "current",
		})
		if err != nil {
			t.Fatal(err)
		}
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
		_, retry, err := index.executeReplacementAttempt(
			t.Context(),
			[]preparedEvidence{prepared},
			[]string{url},
			leases,
		)
		if err != nil || !retry {
			t.Fatalf("planning conflict = %v/%v", retry, err)
		}
	})
	t.Run("commit conflict", func(t *testing.T) {
		index, engine := openFaultIndex(t, Limits{})
		prepared, err := prepareEvidence(t.Context(), index.limits, Evidence{
			URL:         url,
			ContentHash: "current",
		})
		if err != nil {
			t.Fatal(err)
		}
		leases, err := index.acquireReplacementLeases(t.Context(), []string{url})
		if err != nil {
			t.Fatal(err)
		}
		defer leases.close()
		_, retry, err := index.executeReplacementAttempt(
			t.Context(),
			[]preparedEvidence{prepared},
			[]string{url},
			leases,
		)
		if err != nil || !retry {
			t.Fatalf("lease expansion = %v/%v", retry, err)
		}
		unexpected, err := json.Marshal(fingerprintRecord{URL: url, ContentHash: "unexpected"})
		if err != nil {
			t.Fatal(err)
		}
		engine.replayUpdate = func(engine *clusterFaultEngine) {
			engine.buckets[fingerprintBucketName][url] = unexpected
		}
		_, retry, err = index.executeReplacementAttempt(
			t.Context(),
			[]preparedEvidence{prepared},
			[]string{url},
			leases,
		)
		if err != nil || !retry {
			t.Fatalf("commit conflict = %v/%v", retry, err)
		}
	})
}

func TestReplacementLeaseAcquisitionAndExpansionHonorCancellation(t *testing.T) {
	index, _ := openFaultIndex(t, Limits{})
	url := "https://replacement-boundary.example"
	heldURL, err := index.boundaries.acquireLease(t.Context(), []string{url})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := index.acquireReplacementLeases(cancelled, []string{url}); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("replacement boundary error = %v", err)
	}
	heldURL.close()
	required := []string{"cluster"}
	heldProjection, err := index.projections.acquireLease(t.Context(), required)
	if err != nil {
		t.Fatal(err)
	}
	currentProjection, err := index.projections.acquireLease(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	leases := &replacementLeases{projection: currentProjection}
	if _, err := index.expandReplacementLeases(cancelled, leases, required); !errors.Is(
		err,
		context.Canceled,
	) {
		t.Fatalf("replacement expansion error = %v", err)
	}
	heldProjection.close()
}

func TestExecuteReplacementAttemptPropagatesCompletionCancellation(t *testing.T) {
	completionFailure := false
	for cancelAt := 2; cancelAt <= 80; cancelAt++ {
		index, _ := openFaultIndex(t, Limits{})
		input := Evidence{
			URL:         "https://replacement-completion.example",
			ContentHash: "hash",
		}
		prepared, err := prepareEvidence(t.Context(), index.limits, input)
		if err != nil {
			t.Fatal(err)
		}
		urlLease, err := index.boundaries.acquireLease(t.Context(), []string{input.URL})
		if err != nil {
			t.Fatal(err)
		}
		identities := []string{stableClusterID(input.URL, input.ContentHash)}
		projection, err := index.projections.acquireLease(t.Context(), identities)
		if err != nil {
			t.Fatal(err)
		}
		leases := &replacementLeases{
			url:        urlLease,
			projection: projection,
			identities: identities,
		}
		ctx := &stagedCancellationContext{
			Context:  context.Background(),
			cancelAt: cancelAt,
		}
		_, _, err = index.executeReplacementAttempt(
			ctx,
			[]preparedEvidence{prepared},
			[]string{input.URL},
			leases,
		)
		leases.close()
		if err != nil && strings.Contains(err.Error(), "complete content cluster replacements") {
			completionFailure = true
		}
	}
	if !completionFailure {
		t.Fatal("no staged cancellation reached replacement completion")
	}
}
